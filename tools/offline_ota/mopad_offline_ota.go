// mopad_offline_ota.go — 摩派Lite 离线 OTA 工具 (Windows/macOS/Linux)
//
// 用途: 不走 GitHub 在线 OTA, 从本机把固件 tarball 直推面板并按修复版
//
//	do_ota 逻辑本地安装。专治 OTA 卡 80%/90% 死锁的设备(旧版 do_ota.sh
//	语法错误 bug)以及无外网/GitHub 不通的环境。
//
// 用法:
//
//	mopad_offline_ota.exe -ip 192.168.50.223 [-tar 固件.tar.gz] [-verjson version.json]
//
// 流程: telnet(免登录root) → wget tarball + md5 校验 → 解包落位(保留用户配置)
//
//	→ 同步 /tmp/www → 覆盖 version.json → 重启进程 → HTTP 验证版本
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

const installScript = `#!/bin/sh
echo 80 > /tmp/ota_progress_offline
mkdir -p /tmp/ota_staging && rm -rf /tmp/ota_staging/*
tar -tzf /tmp/ota_offline.tar.gz >/dev/null 2>&1 || { echo 84 > /tmp/ota_progress_offline; echo TAR_BAD; exit 1; }
tar -xzf /tmp/ota_offline.tar.gz -C /tmp/ota_staging/ ./customer/aqgui/scripts ./customer/start.sh ./etc 2>/dev/null
if [ ! -f /tmp/ota_staging/customer/aqgui/scripts/mopad_lite_standalone_app ]; then
  tar -xzf /tmp/ota_offline.tar.gz -C /tmp/ota_staging/ 2>/dev/null
fi
for CFG in layout_p0.json sys_settings.json ha_config.json ha_cards.json lock_text.json \
           notify_history.json energy_stats.json passwd config.kv wpa_supplicant.conf .gesture_guide_seen; do
  find /tmp/ota_staging -name "$CFG" 2>/dev/null | xargs rm -f 2>/dev/null
done
[ -d /tmp/ota_staging/customer ] && cp -rf /tmp/ota_staging/customer/. /customer/ 2>/dev/null
[ -d /tmp/ota_staging/etc ] && cp -rf /tmp/ota_staging/etc/. /etc/ 2>/dev/null
[ -d /tmp/ota_staging/data ] && cp -rf /tmp/ota_staging/data/. /data/ 2>/dev/null
find /tmp/ota_staging -maxdepth 1 -type f -exec cp -f {} /data/scripts/ \; 2>/dev/null
sync
mkdir -p /tmp/www/cgi-bin
cp -f /data/scripts/api.cgi /tmp/www/cgi-bin/api.cgi 2>/dev/null
cp -f /data/scripts/index.html /tmp/www/index.html 2>/dev/null
chmod +x /data/scripts/mopad_lite_standalone_app /data/scripts/ha_daemon /data/scripts/api.cgi /data/scripts/fetch_https.sh 2>/dev/null
echo 95 > /tmp/ota_progress_offline
echo INSTALL_OK
`

// 从固件 tarball 里提取 version.json (customer/aqgui/scripts/version.json 优先, 否则根)
func extractVerJSONFromTarball(tarBytes []byte) ([]byte, string, error) {
	gz, err := gzip.NewReader(bytes.NewReader(tarBytes))
	if err != nil {
		return nil, "", err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var fallback []byte
	ver := ""
	for {
		hd, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", err
		}
		name := strings.TrimPrefix(hd.Name, "./")
		if strings.HasPrefix(strings.TrimPrefix(name, "./"), "._") {
			continue // macOS AppleDouble 垃圾
		}
		if strings.HasSuffix(name, "version.json") {
			body, err := io.ReadAll(tr)
			if err != nil {
				return nil, "", err
			}
			var v struct {
				Version string `json:"version"`
			}
			json.Unmarshal(body, &v)
			if strings.Contains(name, "customer/") {
				return body, v.Version, nil
			}
			fallback = body
			ver = v.Version
		}
	}
	if fallback != nil {
		return fallback, ver, nil
	}
	return nil, "", io.ErrUnexpectedEOF
}

// ---------- 极简 telnet(raw shell) 客户端: 免登录 telnetd -l /bin/sh ----------
type tel struct {
	conn net.Conn
	buf  strings.Builder
	seq  int
}

func dialTel(ip string, port string) (*tel, error) {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(ip, port), 8*time.Second)
	if err != nil {
		return nil, err
	}
	t := &tel{conn: c}
	time.Sleep(1500 * time.Millisecond) // 等 shell 起来
	t.drain(800 * time.Millisecond)
	t.w("\n")
	t.drain(800 * time.Millisecond)
	// 兼容个别还带 login 的旧模式
	if strings.Contains(t.buf.String(), "login:") {
		t.w("root\n")
		time.Sleep(800 * time.Millisecond)
		t.drain(500 * time.Millisecond)
	}
	t.buf.Reset()
	return t, nil
}

func (t *tel) w(s string) { t.conn.Write([]byte(s)) }

func (t *tel) drain(d time.Duration) {
	t.conn.SetReadDeadline(time.Now().Add(d))
	b := make([]byte, 65536)
	for {
		n, err := t.conn.Read(b)
		if n > 0 {
			t.buf.Write(b[:n])
		}
		if err != nil {
			break
		}
	}
}

// run 执行命令并返回输出 (marker 带随机序号, 回显行里的 "MK_<n>_$?" 不含数字, 只匹配结果行)
func (t *tel) run(cmd string, timeout time.Duration) (string, int) {
	t.seq++
	m := fmt.Sprintf("MKZ%dQ", t.seq)
	full := fmt.Sprintf("%s; echo %s_$?", cmd, m)
	t.buf.Reset()
	t.w(full + "\n")
	deadline := time.Now().Add(timeout)
	re := regexp.MustCompile(m + `_(\d+)`)
	for time.Now().Before(deadline) {
		t.conn.SetReadDeadline(time.Now().Add(600 * time.Millisecond))
		b := make([]byte, 65536)
		n, _ := t.conn.Read(b)
		if n > 0 {
			t.buf.Write(b[:n])
			if mm := re.FindStringSubmatch(t.buf.String()); mm != nil {
				rc := 0
				fmt.Sscanf(mm[1], "%d", &rc)
				return t.buf.String(), rc
			}
		}
	}
	return t.buf.String(), -1
}

func (t *tel) mustRun(cmd string, timeout time.Duration, what string) string {
	out, rc := t.run(cmd, timeout)
	if rc != 0 {
		failf("%s 失败 (rc=%d, 输出尾: %s)", what, rc, tail(out, 120))
	}
	return out
}

// pushFile base64 分块推送本地/内存文件到面板 (每块 512B, 规避 tty 行缓冲限制)
func (t *tel) pushFile(content []byte, dest string) {
	b64 := base64.StdEncoding.EncodeToString(content)
	t.mustRun("rm -f "+dest, 10*time.Second, "rm")
	for i := 0; i < len(b64); i += 512 {
		end := i + 512
		if end > len(b64) {
			end = len(b64)
		}
		t.mustRun(fmt.Sprintf("echo -n %s >> %s.b64", b64[i:end], dest), 15*time.Second, "b64 追加")
	}
	t.mustRun(fmt.Sprintf("base64 -d %s.b64 > %s && rm -f %s.b64", dest, dest, dest), 15*time.Second, "base64 解码")
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func failf(f string, a ...interface{}) {
	fmt.Printf("[FAIL] "+f+"\n", a...)
	os.Exit(1)
}

func main() {
	ip := flag.String("ip", "192.168.50.143", "面板 IP")
	port := flag.String("port", "23", "面板 telnet 端口")
	tarPath := flag.String("tar", "mopad_lite_custom_gui_latest.tar.gz", "固件 tarball 路径")
	verJSON := flag.String("verjson", "", "version.json 路径 (缺省从固件 tarball 内提取)")
	httpPort := flag.Int("httpport", 18090, "本机临时 HTTP 端口")
	flag.Parse()

	fmt.Printf("[*] 面板:   %s:%s\n", *ip, *port)

	// 1) 读 tarball + md5
	tarBytes, err := os.ReadFile(*tarPath)
	if err != nil {
		failf("读 tarball 失败: %v", err)
	}
	sum := md5.Sum(tarBytes)
	md5Tar := hex.EncodeToString(sum[:])
	fmt.Printf("[*] 固件:   %s (%d B, md5 %s)\n", *tarPath, len(tarBytes), md5Tar)

	// 2) version.json: 优先从固件 tarball 内提取 (与固件版本永远一致), -verjson 可覆盖
	verBytes, tarVer, verr := extractVerJSONFromTarball(tarBytes)
	if verr != nil || verBytes == nil {
		failf("tarball 内未找到 version.json (%v); 可用 -verjson 手动指定", verr)
	}
	if *verJSON != "" {
		if vb, err := os.ReadFile(*verJSON); err == nil {
			verBytes = vb
		}
	}
	var verObj struct {
		Version string `json:"version"`
	}
	json.Unmarshal(verBytes, &verObj)
	tgtVer := verObj.Version
	if tgtVer == "" {
		tgtVer = tarVer
	}
	fmt.Printf("[*] 目标:   %s\n", tgtVer)

	// 3) 本机临时 HTTP 服务 (面板 wget 拉固件)
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", *httpPort))
	if err != nil {
		failf("HTTP 监听失败: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/ota.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(tarBytes)))
		w.Write(tarBytes)
	})
	go http.Serve(ln, mux)
	defer ln.Close()

	// 本机局域网 IP (UDP dial 技巧, 不实际发包)
	uc, err := net.Dial("udp4", net.JoinHostPort(*ip, "23"))
	if err != nil {
		failf("取本机 IP 失败: %v", err)
	}
	localIP := strings.Split(uc.LocalAddr().String(), ":")[0]
	uc.Close()
	fmt.Printf("[*] HTTP:   http://%s:%d/ota.tar.gz\n", localIP, ln.Addr().(*net.TCPAddr).Port)

	// 4) 连面板
	t, err := dialTel(*ip, *port)
	if err != nil {
		failf("连接面板失败: %v", err)
	}
	defer t.conn.Close()

	// 5) 下载 + md5 校验
	url := fmt.Sprintf("http://%s:%d/ota.tar.gz", localIP, ln.Addr().(*net.TCPAddr).Port)
	t.mustRun(fmt.Sprintf("wget -q -O /tmp/ota_offline.tar.gz %s", url), 120*time.Second, "wget 下载")
	out := t.mustRun("md5sum /tmp/ota_offline.tar.gz", 15*time.Second, "md5sum")
	reMD5 := regexp.MustCompile(`([0-9a-f]{32})`)
	got := reMD5.FindString(out)
	if got != md5Tar {
		failf("md5 不符: 收=%s 期望=%s", got, md5Tar)
	}
	fmt.Println("[OK] 下载 + md5 校验通过")

	// 6) 推安装脚本并执行
	t.pushFile([]byte(installScript), "/tmp/oinstall.sh")
	out, rcInst := t.run("sh /tmp/oinstall.sh", 150*time.Second)
	if rcInst != 0 || !strings.Contains(out, "INSTALL_OK") {
		failf("安装脚本失败 (rc=%d, 尾: %s)", rcInst, tail(out, 150))
	}
	fmt.Println("[OK] 安装落位完成 (用户配置已保留)")

	// 7) version.json
	t.pushFile(verBytes, "/data/scripts/version.json")
	fmt.Printf("[OK] version.json → %s\n", tgtVer)

	// 8) 重启进程 (精确 pid kill, 禁用 pkill -f)
	t.mustRun(`for p in $(ps | grep '[m]opad_lite_standalone_app' | awk '{print $1}'); do kill $p; done; killall ha_daemon httpd 2>/dev/null; sleep 1`, 20*time.Second, "停进程")
	t.mustRun(`cd /data/scripts; LD_LIBRARY_PATH=/lib:/customer/lib:/data/scripts nohup ./mopad_lite_standalone_app >/tmp/aqgui.log 2>&1 & nohup /data/scripts/ha_daemon >/tmp/ha_daemon.log 2>&1 & mkdir -p /tmp/www/cgi-bin; httpd -p 8080 -h /tmp/www; sleep 3`, 30*time.Second, "重启进程")
	fmt.Println("[OK] 进程重启完成")

	// 9) HTTP 验证版本 (跨网确认)
	time.Sleep(2 * time.Second)
	resp, err := http.Get(fmt.Sprintf("http://%s:8080/cgi-bin/api.cgi?action=get_version", *ip))
	if err != nil {
		failf("版本验证失败: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), tgtVer) {
		failf("版本不符: %s", tail(string(body), 120))
	}
	fmt.Printf("[OK] 面板版本已确认: %s\n\n", tgtVer)

	fmt.Println("============ 离线 OTA 完成 ============")
	fmt.Printf("  面板:     %s\n", *ip)
	fmt.Printf("  版本:     %s\n", tgtVer)
	fmt.Printf("  tarball:  %d B (md5 %s)\n", len(tarBytes), md5Tar)
	fmt.Printf("  用户配置: 已保留 (layout/ha_config/config.kv/wpa 等)\n")
	fmt.Printf("  验证:     http://%s:8080/\n", *ip)
}
