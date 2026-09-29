# 摩派 Lite 自定义 GUI（mopad_lite_custom_gui）

面向 **摩派 Lite 智能面板（SigmaStar SSD201，480×480 触屏）** 的原生 C 语言多屏仪表固件与 Home Assistant 双向联动系统。

- 多屏磁贴仪表（1~8 屏全景滑动）、全量 HA 实体控制、8080 Web 控制台与所见即所得布局编辑
- 硬件深度适配：蜂鸣门铃、接近感应唤醒、Si7021 温湿度、pwm1 背光、原厂 49150/WiFi 后端共存
- 在线检查更新与 OTA 升级——**更新源即本仓库** `release/` 目录
- 安装脚本自动固化 **telnetd 开机自启**（写入 `start.sh`/`rcS`），免 TTL 免串口

部署脚本结构参照 [oopuuu/AqaraS1E-Custom-GUI](https://github.com/oopuuu/AqaraS1E-Custom-GUI)（MIT），GUI 基于 AqaraS1E-GUI-app 多屏仪表版移植，特此致谢。

---

## 🚀 一键安装（Telnet）

面板已开启 telnet（23 端口，`telnet <面板IP>`，无密码）且连接网络（有线/Wi-Fi）后，**整段复制粘贴**执行：

```bash
cd /tmp
C=""
for p in /data/scripts/curl /tmp/curl /usr/bin/curl; do
    [ -x "$p" ] && C="$p" && break
done
if [ -z "$C" ]; then
    wget -O curl "http://master.dl.sourceforge.net/project/aqarahub/binutils/curl?viasf=1"
    chmod +x curl
    C=./curl
fi
G="snailll2/mopad_lite_custom_gui"
for h in "ghfast.top/https://raw.githubusercontent.com/$G/main" \
         "cdn.jsdelivr.net/gh/$G@main" \
         "raw.githubusercontent.com/$G/main"; do
    rm -f i.sh
    "$C" -skfL -m 10 "https://$h/install.sh" -o i.sh 2>/dev/null
    if [ -s i.sh ]; then
        sh i.sh
        break
    fi
done
```

> 优先复用设备上的便携 curl（本机型 busybox wget 域名解析异常，且不支持 HTTPS）；
> 三行镜像自动切换（ghfast.top 加速反代 → jsdelivr CDN → GitHub 直连），全部失败才会报错。
> 各行控制在 90 字符内，避免 telnet 终端单行缓冲截断。

### 安装脚本做什么

1. 自动获取 HTTPS 能力（便携 curl，busybox wget 不支持 HTTPS）；
2. 检查 `/tmp` 空间并清理残留（tmpfs 满会导致下载截断）；
3. **备份**全部用户配置 JSON 与原厂 `start.sh`（回退依据）；
4. 下载并校验 `release/mopad_lite_custom_gui_latest.tar.gz`（**先验包，失败不动系统**）；
5. 精确停掉原厂 UI：先杀 `reset_process` 看门狗再杀 `ui_process.bin`（**保留 49150/WiFi 后端**）；
6. 解包根级 overlay（GUI/Web/主题图标/字体/启动脚本）+ 关键路径断言，失败自动回退原厂 UI；
7. 还原用户配置、写入 telnetd 开机自启（`start.sh` 必写 + `rcS` 尽力而为）；
8. **零重启热启动**：GUI + 8080 httpd + ha_daemon，装完即用。

安装成功后面板屏幕直接切换到新仪表，Web 控制台 `http://<面板IP>:8080/`。

---

## 🔄 在线更新（OTA）

屏幕【设置】→【系统更新】或 Web 后台检查更新：

1. `check_update` 拉取本仓库 `version.json` 比对 `version_code`；
2. `perform_ota` 经 `fetch_https.sh`（curl 快速通道 → ghfast → jsdelivr → 直连）下载本仓库 release 包；
3. 校验解压热重启，全程免 telnet。

本地版本信息位于 `/data/scripts/version.json`，与本仓库根目录同名文件保持同步。

---

## 📦 产物清单（`release/`）

| 文件 | 说明 |
| :--- | :--- |
| `mopad_lite_custom_gui_latest.tar.gz` | 根级 overlay 运行包：GUI/ha_daemon/api.cgi/index.html/JSON 配置/字体/表情/主题 PNG/libpng16/libz/`start.sh`/`rcS` |
| `anims/*.anim` | 锁屏动效流媒体样例（480×480，QOI+mmap 零拷贝直刷 fb） |

---

## 🔙 卸载 / 回退原厂 UI

```sh
cp -f /customer/start.sh.bak /customer/start.sh
reboot
```

原厂启动链恢复（`reset_process` + 原厂 LVGL UI），原厂 49150/WiFi 后端始终未被破坏。

---

## 🛠️ 开发者：本地构建部署

源码与一键部署脚本在 `mopai_lite` 主仓库：

```bash
./deploy.sh <面板IP>            # 自动构建 + 自动探测 fresh/update + 推送 + 复验
./deploy.sh <面板IP> --reboot   # 重启面板验证开机自启（telnetd + GUI + Web）
```

发布流程：改码 → `mk_ui.sh`/`mk_api.sh` 编译 → `make_overlay.sh` 打包 → 覆盖本仓库 `release/` 与根目录 `version.json`（更新 `tarball_size_bytes`）→ 推送。

---

## 📄 许可

MIT。
