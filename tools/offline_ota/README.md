# 离线 OTA 工具 (mopad_offline_ota)

**不走 GitHub 在线 OTA**，从你的电脑（Windows / macOS / Linux）把固件直推到面板本地安装。

专治两类场景：
- **OTA 卡 80%/90% 死锁**的设备（固件 v1.0.9 ~ v1.0.11 的 do_ota.sh 语法错误 bug）
- **无外网 / GitHub 访问不通**的环境

## 使用方法

### 1. 下载

| 平台 | 文件 |
|---|---|
| Windows | `mopad_offline_ota_windows_amd64.exe` |
| macOS (Apple Silicon) | `mopad_offline_ota_darwin_arm64` |
| Linux (amd64) | `mopad_offline_ota_linux_amd64` |
| 源码 | `mopad_offline_ota.go`（`go build` 自行编译任意平台） |

同时下载最新固件包：[release/mopad_lite_custom_gui_latest.tar.gz](../../release/mopad_lite_custom_gui_latest.tar.gz)
（与工具放同一目录可省略 `-tar` 参数）

### 2. 运行

**Windows (CMD/PowerShell):**
```bat
mopad_offline_ota_windows_amd64.exe -ip 192.168.50.223
mopad_offline_ota_windows_amd64.exe -ip 192.168.50.223 -tar 新固件.tar.gz -verjson version.json
```

**macOS / Linux:**
```bash
chmod +x mopad_offline_ota_darwin_arm64
./mopad_offline_ota_darwin_arm64 -ip 192.168.50.223
```

### 3. 参数

| 参数 | 缺省 | 说明 |
|---|---|---|
| `-ip` | 192.168.50.143 | 面板 IP |
| `-port` | 23 | 面板 telnet 端口 |
| `-tar` | `mopad_lite_custom_gui_latest.tar.gz` | 固件 tarball 路径 |
| `-verjson` | 内置最新版 | version.json（可选） |
| `-httpport` | 18090 | 本机临时 HTTP 端口 |

### 4. 前提条件

- 电脑与面板在**同一局域网**
- 面板 telnet(23) 可达（本工具依赖面板的本地管理通道执行安装）
- 若设备此前 OTA 卡死：**先断电重启设备**（回到旧版正常界面）再运行本工具

## 工作流程

```
本机临时 HTTP 服务 → 面板 wget 固件 → MD5 校验
→ 只解程序树(防 /tmp 溢出) → 保留用户配置 → cp 落位
→ 同步 /tmp/www → 覆盖 version.json → 重启进程 → HTTP 版本终验
```

用户配置（首页布局 / HA 配置 / WiFi / config.kv 等）**全部保留**，升级不影响个人设置。

## SHA256 校验

```
mopad_offline_ota_windows_amd64.exe  811e08971d51fa7530d18966011152179731a9fd23e496d37bc317c15516ad8c
mopad_offline_ota_darwin_arm64       d642f57ca23c4d44bf0870775ce59f5883c18bc9aa929637718a3e53e6f19783
mopad_offline_ota_linux_amd64        86e2257037f483e9d19af79574395d7c3f0d49657b731065b176405bb260ca6e
mopad_offline_ota.go                 5ced7b4432be83a7d60ec4ea0778ba1c8a65e337f9716578d5602fe1dd529f17
```

## 从源码编译

```bash
go build -o mopad_offline_ota.exe .                     # Windows
GOOS=darwin  GOARCH=arm64 go build -o mopad_offline_ota_mac .
GOOS=linux   GOARCH=amd64 go build -o mopad_offline_ota_linux .
```

需要 Go 1.21+，纯标准库、零第三方依赖。
