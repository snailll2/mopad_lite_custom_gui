#!/bin/sh
# ==============================================================================
# 摩派 Lite (MOPAD Lite, SSD201 480x480) - Custom GUI Installer
# 参照 oopuuu/AqaraS1E-Custom-GUI 的部署结构，适配摩派 Lite 面板:
#   - 精确 kill 停原厂 UI(先 reset 看门狗后 ui_process,保留 49150/wifi 后端)
#   - 解包为根级 overlay(含主题图标/字体/启动脚本),解包后关键路径断言
#   - telnetd 开机自启固化(start.sh 必写 + rcS 尽力而为)
#   - 零重启热启动: GUI + 8080 httpd + ha_daemon
# 更新地址: 本仓库 snailll2/mopad_lite_custom_gui
# ==============================================================================

TARGET_DIR="/data/scripts"
TAR_NAME="mopad_lite_custom_gui_latest.tar.gz"
URL_LIST="
https://ghfast.top/https://raw.githubusercontent.com/snailll2/mopad_lite_custom_gui/main/release/${TAR_NAME}
https://cdn.jsdelivr.net/gh/snailll2/mopad_lite_custom_gui@main/release/${TAR_NAME}
https://raw.githubusercontent.com/snailll2/mopad_lite_custom_gui/main/release/${TAR_NAME}
"

echo "========================================================"
echo "   摩派 Lite Custom GUI Installer"
echo "========================================================"

mkdir -p "$TARGET_DIR"

# ---------------------------------------------------------------- 1. HTTPS curl
CURL_EXEC=""
if [ -x /tmp/curl ]; then
    CURL_EXEC="/tmp/curl"
elif [ -x "$TARGET_DIR/curl" ]; then
    CURL_EXEC="$TARGET_DIR/curl"
elif command -v curl >/dev/null 2>&1; then
    CURL_EXEC="curl"
else
    echo "[*] 无 HTTPS 能力(busybox wget 不支持),经 HTTP 获取便携 curl..."
    wget -O /tmp/curl "http://master.dl.sourceforge.net/project/aqarahub/binutils/curl?viasf=1" 2>/dev/null || true
    chmod a+x /tmp/curl 2>/dev/null || true
    [ -x /tmp/curl ] && CURL_EXEC="/tmp/curl"
fi
if [ -z "$CURL_EXEC" ]; then
    echo "[-] 获取 curl 失败。手动执行:"
    echo "    wget -O /tmp/curl http://master.dl.sourceforge.net/project/aqarahub/binutils/curl?viasf=1 && chmod +x /tmp/curl"
    exit 1
fi
echo "[+] curl: $CURL_EXEC"

# ---------------------------------------------------------------- 2. /tmp 空间
AVAIL=$(df -m /tmp 2>/dev/null | awk '$NF=="/tmp"{print $4}')
case "$AVAIL" in ''|*[!0-9]*) AVAIL=64 ;; esac
if [ "$AVAIL" -lt 12 ]; then
    echo "[*] /tmp 剩余 ${AVAIL}M,清理残留..."
    rm -f /tmp/fb0.bin /tmp/fb_stream.bin /tmp/nidx /tmp/ncgi /tmp/*.anim /tmp/s1e_ota_latest.tar.gz 2>/dev/null
    : > /tmp/aqgui.log 2>/dev/null || true
    : > /tmp/api_access.log 2>/dev/null || true
    AVAIL=$(df -m /tmp 2>/dev/null | awk '$NF=="/tmp"{print $4}')
fi
if [ -z "$AVAIL" ] || [ "$AVAIL" -lt 12 ]; then
    echo "[-] /tmp 空间不足(需 >=12M, 现 ${AVAIL}M),无法安全下载。"
    exit 1
fi
echo "[+] /tmp 可用 ${AVAIL}M"

# ---------------------------------------------------------------- 3. 备份用户配置
BACKUP_DIR="/data/scripts_backup_$(date +%s)"
HAS_CFG=0
for f in "$TARGET_DIR"/*.json; do
    [ -f "$f" ] && HAS_CFG=1 && break
done
if [ "$HAS_CFG" -eq 1 ]; then
    echo "[1/7] 备份用户配置 -> $BACKUP_DIR"
    mkdir -p "$BACKUP_DIR"
    cp -f "$TARGET_DIR"/*.json "$BACKUP_DIR/" 2>/dev/null || true
    for extra in passwd .gesture_guide_seen; do
        [ -f "$TARGET_DIR/$extra" ] && cp -f "$TARGET_DIR/$extra" "$BACKUP_DIR/" 2>/dev/null
    done
else
    echo "[1/7] 无既有配置,跳过备份"
fi
# 原厂 start.sh 只备份一次(回退依据)
[ -f /customer/start.sh ] && [ ! -f /customer/start.sh.bak ] && cp -f /customer/start.sh /customer/start.sh.bak

# ---------------------------------------------------------------- 4. 下载 release 包(先验包再动系统)
echo "[2/7] 下载最新 release 包..."
LOCAL_TAR="/tmp/$TAR_NAME"
DOWNLOAD_OK=0
for U in $URL_LIST; do
    echo "[*] 尝试源: $U"
    rm -f "$LOCAL_TAR"
    if $CURL_EXEC -s -k -L --connect-timeout 10 -m 180 -o "$LOCAL_TAR" "$U" && [ -s "$LOCAL_TAR" ]; then
        SZ=$(wc -c < "$LOCAL_TAR")
        if [ "$SZ" -gt 1000000 ] && tar -tzf "$LOCAL_TAR" >/dev/null 2>&1; then
            DOWNLOAD_OK=1
            echo "[+] 下载并校验成功 ($SZ bytes)"
            break
        fi
    fi
done
if [ "$DOWNLOAD_OK" -ne 1 ]; then
    echo "[-] 所有镜像均无法取得有效 release 包(面板需可访问外网)。"
    exit 1
fi

# ---------------------------------------------------------------- 5. 停进程(精确 kill,保留 49150 后端)
echo "[3/7] 停止现有进程..."
# 先停 reset 看门狗,防止它复活原厂 UI
p=$(ps | grep '[m]opad_lite_reset_process' | awk '{print $1}')
[ -z "$p" ] || kill $p 2>/dev/null
sleep 1
p=$(ps | grep '[m]opad_lite_ui_process.bin' | awk '{print $1}')
[ -z "$p" ] || kill $p 2>/dev/null
p=$(ps | grep '[m]opad_lite_standalone_app' | awk '{print $1}')
[ -z "$p" ] || kill $p 2>/dev/null
p=$(pidof ha_daemon); [ -z "$p" ] || kill $p 2>/dev/null
p=$(pidof httpd);     [ -z "$p" ] || kill $p 2>/dev/null
sleep 1
# 注意: mopad_lite_app_process.bin (49150/WiFi 后端) 保留运行

# ---------------------------------------------------------------- 6. 解包 + 关键路径断言
echo "[4/7] 解包到系统根目录..."
tar -zxvf "$LOCAL_TAR" -C / > /tmp/install_tar.log 2>&1
TAR_RC=$?
if [ "$TAR_RC" -ne 0 ]; then
    echo "[!] tar 返回 $TAR_RC(只读分区条目失败可忽略),详见 /tmp/install_tar.log"
fi
if [ ! -x /data/scripts/mopad_lite_standalone_app ] || \
   ! grep -q 'telnetd -l /bin/sh &' /customer/start.sh; then
    echo "[-] 解包断言失败(GUI 二进制或 start.sh telnetd 自启缺失),回退原厂 UI..."
    rm -f "$LOCAL_TAR"
    cd /customer 2>/dev/null && ./mopad_lite_reset_process >/dev/null 2>&1 &
    sleep 1
    cd /customer 2>/dev/null && ./ui_display.sh >/dev/null 2>&1 &
    exit 1
fi
rm -f "$LOCAL_TAR"

# 还原用户配置
if [ -d "$BACKUP_DIR" ]; then
    echo "[*] 还原用户配置..."
    cp -f "$BACKUP_DIR"/*.json "$TARGET_DIR/" 2>/dev/null || true
    for extra in passwd .gesture_guide_seen; do
        [ -f "$BACKUP_DIR/$extra" ] && cp -f "$BACKUP_DIR/$extra" "$TARGET_DIR/" 2>/dev/null
    done
    rm -rf "$BACKUP_DIR"
fi

# ---------------------------------------------------------------- 7. 权限 + telnetd 自启固化
echo "[5/7] 修复权限与 telnetd 开机自启..."
chmod +x "$TARGET_DIR/mopad_lite_standalone_app" \
         "$TARGET_DIR/ha_daemon" \
         "$TARGET_DIR/api.cgi" \
         "$TARGET_DIR/fetch_https.sh" \
         "$TARGET_DIR/curl" 2>/dev/null || true
chmod +x "$TARGET_DIR"/*.sh 2>/dev/null || true

grep -q 'telnetd -l /bin/sh &' /customer/start.sh || \
    sed -i '/ifconfig lo up/a [ -z "$(pidof telnetd)" ] && telnetd -l /bin/sh &' /customer/start.sh
grep -q 'telnetd -l /bin/sh &' /etc/init.d/rcS || \
    sed -i '/^#telnetd -l sh/a [ -z "$(pidof telnetd)" ] && telnetd -l /bin/sh &' /etc/init.d/rcS 2>/dev/null
if [ -z "$(pidof telnetd)" ]; then
    telnetd -l /bin/sh &
    sleep 1
fi
# curl 持久化(fetch_https.sh 的 OTA 快速通道)
if [ -x /tmp/curl ] && [ ! -x "$TARGET_DIR/curl" ]; then
    cp -f /tmp/curl "$TARGET_DIR/curl" 2>/dev/null && chmod +x "$TARGET_DIR/curl" 2>/dev/null
fi

# NSS DNS 修复: vendor 把 libnss_*.so.2 放在 /config/wifi,面板无 ld.so.cache/ld.so.conf,
# 动态进程 getaddrinfo→NSS→dlopen 在 /lib、/usr/lib 默认路径搜不到 → busybox
# nslookup/ping/wget 报 "bad address"。落位 /lib + 补 nsswitch.conf;缺失才补,幂等。
for _nss in libnss_dns.so.2 libnss_files.so.2 libnss_db.so.2 libnss_hesiod.so.2; do
    [ -e "/lib/$_nss" ] || cp "/config/wifi/$_nss" /lib/ 2>/dev/null
done
[ -e /etc/nsswitch.conf ] || echo 'hosts: files dns' > /etc/nsswitch.conf

# ---------------------------------------------------------------- 8. 零重启热启动
echo "[6/7] 热启动 GUI + Web + ha_daemon(免重启)..."
mkdir -p /tmp/www/cgi-bin
cp -f "$TARGET_DIR/index.html" /tmp/www/index.html 2>/dev/null || true
cp -f "$TARGET_DIR/api.cgi" /tmp/www/cgi-bin/api.cgi 2>/dev/null || true
chmod +x /tmp/www/cgi-bin/api.cgi 2>/dev/null || true
killall httpd 2>/dev/null || true
httpd -p 8080 -h /tmp/www >/dev/null 2>&1 &

killall ha_daemon 2>/dev/null || true
"$TARGET_DIR/ha_daemon" >/tmp/ha_daemon.log 2>&1 &

( cd /data/scripts && LD_LIBRARY_PATH=/lib:/customer/lib:/data/scripts \
    nohup ./mopad_lite_standalone_app >/tmp/aqgui.log 2>&1 & )

# ---------------------------------------------------------------- 9. 验证
echo "[7/7] 验证..."
sleep 4
GUI_OK=0
p=$(ps | grep '[m]opad_lite_standalone_app' | awk '{print $1}')
[ -n "$p" ] && GUI_OK=1

# 有线 eth0 优先(摩派 Lite 主用有线),无线 wlan0 兜底
DEV_IP=$(ip -4 addr show eth0 2>/dev/null | grep -o 'inet [0-9.]*' | cut -d' ' -f2)
[ -z "$DEV_IP" ] && DEV_IP=$(ip -4 addr show wlan0 2>/dev/null | grep -o 'inet [0-9.]*' | cut -d' ' -f2)
[ -z "$DEV_IP" ] && DEV_IP=$(ifconfig eth0 2>/dev/null | awk '/inet addr/{print $2}' | cut -d: -f2)
[ -z "$DEV_IP" ] && DEV_IP=$(ifconfig wlan0 2>/dev/null | awk '/inet addr/{print $2}' | cut -d: -f2)
[ -z "$DEV_IP" ] && DEV_IP="<面板IP>"

if [ "$GUI_OK" -eq 1 ]; then
    echo "========================================================"
    echo " [SUCCESS] 摩派 Lite Custom GUI 安装成功并已运行!"
    echo " -> 屏幕仪表已切换  |  Telnet: $(pidof telnetd >/dev/null && echo 已自启 || echo 异常)"
    echo " -> Web 控制台: http://${DEV_IP}:8080/"
    echo " -> 原厂 49150/WiFi 后端保持运行"
    echo "========================================================"
else
    echo "[-] GUI 未能立即拉起,查看 /tmp/aqgui.log 尾部:"
    tail -n 8 /tmp/aqgui.log 2>/dev/null
    exit 1
fi
