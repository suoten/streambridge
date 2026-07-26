# StreamBridge

> **一个文件,让摄像头连上浏览器** —— 单二进制、零外部依赖的全协议流媒体网关

**简体中文** | [English](README.en.md)

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8.svg)](https://golang.org)
[![Edition](https://img.shields.io/badge/Edition-Community-blue)](#-社区版-vs-企业版)
[![GitHub Release](https://img.shields.io/github/v/release/suoten/streambridge?label=Release&logo=github)](https://github.com/suoten/streambridge/releases)
[![GitHub Stars](https://img.shields.io/github/stars/suoten/streambridge?style=social)](https://github.com/suoten/streambridge)
[![GitHub Downloads](https://img.shields.io/github/downloads/suoten/streambridge/total?label=Downloads&logo=github)](https://github.com/suoten/streambridge/releases)
[![Build](https://github.com/suoten/streambridge/actions/workflows/build.yml/badge.svg?branch=main)](https://github.com/suoten/streambridge/actions)
[![Gitee Release](https://img.shields.io/badge/Gitee-Release-C71D23?logo=gitee)](https://gitee.com/suoten/streambridge/releases)

- 🐙 GitHub 仓库:<https://github.com/suoten/streambridge>
- 🐯 Gitee 仓库(国内访问更快):<https://gitee.com/suoten/streambridge>

> 🌟 **如果 StreamBridge 帮到了你,欢迎 [点个 Star](https://github.com/suoten/streambridge) 支持一下!**

### 🎯 适用场景

| 场景 | 描述 |
|------|------|
| 📷 安防监控 | 海康/大华/宇视摄像头 → 浏览器集中查看 |
| 🏭 工业可视化 | 产线摄像头 → 大屏展示 |
| 🔗 GB28181 平台 | 作为浏览器播放网关(只接 RTP/PS,不参与 SIP) |
| 📱 物联网设备 | RTSP 设备 → Web/微信小程序/Android/iOS 播放 |
| 🎬 直播转码 | RTMP 推流 → HLS/WebRTC 分发 |

### 📸 演示效果

> 截图待补充,首次启动后访问 `http://localhost:8080` 即可看到实际界面

---

StreamBridge 把工业/安防摄像头(RTSP/RTMP/RTP/PS)的视频流,转换为浏览器可直接播放的格式(WebSocket-FLV / HLS / WebRTC)。

**不做平台**:不包含 GB28181 SIP 信令、PTZ 控制、录像管理、ONVIF 发现 —— 这些由上游平台负责。StreamBridge 只做一件事:把上游已经协商好的视频流,转码/转封装为浏览器可播放的格式。

---

## ✨ 核心特性

- 🚀 **零配置启动**:下载一个二进制文件,双击即可运行,5 分钟可用
- 📦 **单文件部署**:无 Docker / FFmpeg / ZLMediaKit 依赖,1C1G 服务器可跑 20+ 路 1080P
- 🔄 **全协议输入**:RTSP / RTMP / RTP/PS / MP4 文件 / HTTP-FLV / HLS
- 📺 **浏览器友好**:WebSocket-FLV(主推) / HLS / WebRTC WHEP 三种输出
- 🎥 **H265 兼容**:内置 H265→H264 软件转码,解决浏览器 H265 不兼容
- 📱 **全平台 SDK**:JavaScript / 微信小程序 / Android / iOS,社区版全部免费
- 🌐 **跨平台**:Windows / Linux / macOS / Docker / K8s 全平台支持
- 🔓 **永久免费**:MIT 协议,可商用,无功能限制

---

## 📖 目录

- [下载安装](#-下载安装)
- [60 秒极速上手](#-60-秒极速上手)
- [客户端 SDK](#-客户端-sdk)
- [配置说明](#️-配置说明)
- [REST API 文档](#-rest-api-文档)
- [与 GB28181 平台协作](#-与-gb28181-平台协作)
- [生产部署(含宝塔/1Panel/K8s 等)](#-生产部署)
- [自行编译](#-自行编译)
- [常见问题 FAQ](#-常见问题-faq)
- [架构设计](#-架构设计)
- [社区版 vs 企业版](#-社区版-vs-企业版)
- [License](#-license)

---

## 📦 下载安装

### 方式 1:直接下载二进制(推荐小白用户)

打开任一仓库的 Releases 页面,根据操作系统下载对应文件:

| 系统 | 架构 | 文件名 | GitHub 下载 | Gitee 下载(国内更快) |
|------|------|--------|------------|---------------------|
| Windows | x86_64 | `streambridge-windows-amd64.exe` | [GitHub Release](https://github.com/suoten/streambridge/releases/latest) | [Gitee Release](https://gitee.com/suoten/streambridge/releases/latest) |
| Linux | x86_64 | `streambridge-linux-amd64` | [GitHub Release](https://github.com/suoten/streambridge/releases/latest) | [Gitee Release](https://gitee.com/suoten/streambridge/releases/latest) |
| Linux | ARM64 | `streambridge-linux-arm64` | [GitHub Release](https://github.com/suoten/streambridge/releases/latest) | [Gitee Release](https://gitee.com/suoten/streambridge/releases/latest) |
| macOS | Apple Silicon | `streambridge-darwin-arm64` | [GitHub Release](https://github.com/suoten/streambridge/releases/latest) | [Gitee Release](https://gitee.com/suoten/streambridge/releases/latest) |

> 💡 **国内用户建议用 Gitee**:下载速度比 GitHub 快 10 倍以上。

下载后无需安装,直接运行:

```bash
# Linux / macOS(需赋予执行权限)
chmod +x streambridge-linux-amd64
./streambridge-linux-amd64

# Windows
# 双击 streambridge-windows-amd64.exe 即可,会弹出黑色窗口(不要关闭)
```

### 方式 2:命令行一键下载(适合 Linux 服务器)

```bash
# 国内服务器(走 Gitee)
LATEST_URL=$(curl -s https://gitee.com/api/v5/repos/suoten/streambridge/releases/latest \
  | grep -oP '"browser_download_url"\s*:\s*"\K[^"]*streambridge-linux-amd64"' \
  | head -1 | tr -d '"')

# GitHub(海外服务器)
# LATEST_URL=$(curl -s https://api.github.com/repos/suoten/streambridge/releases/latest \
#   | grep "browser_download_url.*streambridge-linux-amd64\"" \
#   | cut -d '"' -f 4)

curl -L -o /usr/local/bin/streambridge "$LATEST_URL"
chmod +x /usr/local/bin/streambridge
streambridge
```

### 方式 3:Docker(适合容器化部署)

```bash
# 快速体验
docker run -d --name streambridge -p 8080:8080 --restart unless-stopped \
  --pull always \
  suoten/streambridge:latest

# 完整功能(含 RTMP / RTP)
docker run -d --name streambridge \
  -p 8080:8080 -p 1935:1935 \
  -p 20000-30000:20000-30000/udp \
  --restart unless-stopped \
  suoten/streambridge:latest
```

镜像地址:
- Docker Hub:`suoten/streambridge:latest`
- 国内镜像(阿里云):`registry.cn-hangzhou.aliyuncs.com/suoten/streambridge:latest`

### 方式 4:从源码编译(需要 Go 1.22+)

```bash
git clone https://gitee.com/suoten/streambridge.git
cd streambridge
make build          # 当前平台
make build-all      # 全平台交叉编译
```

详见 [自行编译](#-自行编译) 章节。

---

## 🚀 60 秒极速上手

### 首次访问

下载运行后,浏览器打开 `http://localhost:8080`(本机)或 `http://服务器IP:8080`(远程),你会看到:

- **播放器测试页**:输入 RTSP 地址即可播放
- **示例流按钮**:点击即可看到演示画面(无需摄像头)
- **流列表**:当前活跃流与观看人数
- **API 文档**:`http://localhost:8080/api/docs`

### 第一次播放摄像头(零代码)

1. 浏览器打开 `http://localhost:8080`
2. 输入框粘贴 RTSP 地址(例:海康 `rtsp://admin:abc12345@192.168.1.64:554/Streaming/Channels/101`)
3. 点击「播放」按钮 ✅

#### 主流摄像头 RTSP 地址速查

| 品牌 | 默认地址 |
|------|---------|
| 海康威视 | `rtsp://admin:<密码>@<IP>:554/Streaming/Channels/101` |
| 大华 | `rtsp://admin:<密码>@<IP>:554/cam/realmonitor?channel=1&subtype=0` |
| 宇视 | `rtsp://admin:<密码>@<IP>:554/media/video1` |
| 通用 ONVIF | 通过 NVR 获取 |

### 一键诊断

```bash
streambridge doctor
# 自动检查端口、配置、浏览器兼容性、网络连通性
```

或浏览器打开 `http://localhost:8080/doctor`。

### 健康检查

```bash
curl http://localhost:8080/api/health
# {"status":"ok","version":"v1.0.0","uptime":123,"streams":0,"viewers":0}
```

---

## 📱 客户端 SDK

社区版永久免费提供所有平台 SDK,详见 [sdk/README.md](sdk/README.md)。

| 平台 | 文件 | 播放方式 |
|------|------|---------|
| 浏览器 | [sdk/javascript/streambridge.js](sdk/javascript/streambridge.js) | WebSocket-FLV (MSE) / HLS / WebRTC |
| 微信小程序 | [sdk/wechat-miniprogram/streambridge.js](sdk/wechat-miniprogram/streambridge.js) | HLS |
| Android | [sdk/android/StreamBridgeView.java](sdk/android/StreamBridgeView.java) | HLS (ExoPlayer) |
| iOS | [sdk/ios/StreamBridgePlayerView.swift](sdk/ios/StreamBridgePlayerView.swift) | HLS (AVPlayer) |

### 浏览器一行播放

```html
<div id="player"></div>
<script src="http://localhost:8080/streambridge.js"></script>
<script>
  StreamBridge.play({
    container: '#player',
    source: 'rtsp://admin:abc12345@192.168.1.64:554/Streaming/Channels/101',
  });
</script>
```

---

## ⚙️ 配置说明

StreamBridge 支持零配置启动,也可通过 `-c config.yaml` 指定配置文件。完整示例见 [configs/streambridge.yaml](configs/streambridge.yaml)。

```yaml
server:
  http_port: 8080          # HTTP 端口
  rtmp_port: 1935          # RTMP 推流端口
  whep_port: 1985          # WebRTC 信令端口
  bind_addr: "0.0.0.0"     # 监听地址

log:
  level: info              # debug | info | warn | error

performance:
  max_streams: 500                  # 最大并发流数
  max_viewers_per_stream: 50        # 单路流最大观看人数
  enable_h265_transcode: false      # 是否启用 H265->H264 转码

input:
  rtp:
    listen: "0.0.0.0"
    port_range: [20000, 30000]      # RTP 媒体端口范围
    mode: passive                   # 被动接收(GB28181 平台转发)

security:
  enable_auth: false                # 是否启用 Token 鉴权
  secret_key: "请修改为随机字符串"
  allow_origins: ["*"]              # CORS 跨域白名单
```

### 命令行参数

```bash
streambridge                                    # 零配置启动
streambridge -c /etc/streambridge/config.yaml   # 指定配置文件
streambridge --http-port 9090 --log-level debug
streambridge doctor                             # 一键诊断
streambridge version                            # 查看版本
```

---

## 📡 REST API 文档

### 启动流(返回 WebSocket/HLS 地址)

```http
GET /api/play?url=<rtsp-url>&format=flv
```

响应:
```json
{
  "wsUrl": "ws://localhost:8080/ws/abc123",
  "streamId": "abc123",
  "format": "flv",
  "source": "rtsp://..."
}
```

### 启动 HLS 流

```http
GET /hls/play?url=<rtsp-url>
```

响应:
```json
{
  "streamId": "abc123",
  "hlsUrl": "http://localhost:8080/hls/abc123.m3u8"
}
```

### 停止流

```http
POST /api/stop?id=<streamId>
```

### 查询活跃流

```http
GET /api/streams
```

### 健康检查

```http
GET /api/health
```

### 文件回放

```http
POST /api/file/play
Content-Type: application/json

{"path": "/data/video.mp4", "loop": true}
```

### RTP 监听(对接 GB28181 平台)

```http
POST /api/rtp/listen
Content-Type: application/json

{"port": 20000, "mode": "ps"}
```

### Prometheus 指标

```http
GET /metrics
```

### 完整端点列表

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/health` | GET | 健康检查 |
| `/api/ready` | GET | 就绪检查 |
| `/api/version` | GET | 版本信息 |
| `/api/play` | GET | 启动流(返回 ws/flv 地址) |
| `/api/stop` | POST | 停止流 |
| `/api/streams` | GET | 活跃流列表 |
| `/api/stats?id=<id>` | GET | 单路流统计 |
| `/api/file/play` | POST | 文件回放 |
| `/api/rtp/listen` | POST | RTP 监听 |
| `/api/demo` | GET | 演示流地址 |
| `/api/logs` | GET | 日志事件流(SSE) |
| `/metrics` | GET | Prometheus 指标 |
| `/ws/<streamId>` | WS | WebSocket-FLV 推流 |
| `/live/<streamId>` | GET | HTTP-FLV 直出 |
| `/hls/<streamId>.m3u8` | GET | HLS 播放列表 |
| `/hls/<streamId>/<seg>.ts` | GET | HLS 切片 |
| `/whep/<streamId>` | POST | WebRTC WHEP 信令 |

---

## 🔗 与 GB28181 平台协作

StreamBridge **不参与** SIP 信令/PTZ/录像,只接收上游平台转发的 RTP/PS 媒体流:

```
[摄像头] ──SIP──→ [GB28181 平台] ──RTP/PS──→ [StreamBridge] ──WebSocket-FLV──→ [浏览器]
                       │
                       └── PTZ/对讲/录像 由平台负责
```

### 配置步骤

1. 在 StreamBridge 配置中开启 RTP 接收:

```yaml
input:
  rtp:
    listen: "0.0.0.0"
    port_range: [20000, 30000]
    mode: passive
    ps_depacketize: true
```

2. 在 GB28181 平台侧配置:
   - 媒体服务器 IP = StreamBridge IP
   - 媒体端口范围 = 20000-30000

3. 平台 INVITE 时会自动把媒体流转发到 StreamBridge,StreamBridge 自动解封装 PS → H264/AAC → 转 FLV 输出。

---

## 🏭 生产部署

StreamBridge 单二进制即可生产可用,以下覆盖各类部署场景,按需选择一种即可。

### 场景 1:Linux systemd 服务(推荐)

适合 Linux 服务器、云主机、VPS,开机自启 + 崩溃自动重启。

```bash
# 1. 创建专用用户
sudo useradd -r -s /bin/false streambridge

# 2. 准备目录与文件
sudo mkdir -p /etc/streambridge /var/log/streambridge
sudo cp configs/streambridge.yaml /etc/streambridge/config.yaml
sudo cp bin/streambridge-linux-amd64 /usr/local/bin/streambridge
sudo chown -R streambridge:streambridge /etc/streambridge /var/log/streambridge

# 3. 写入 systemd 服务
sudo tee /etc/systemd/system/streambridge.service > /dev/null <<'EOF'
[Unit]
Description=StreamBridge Gateway
After=network.target

[Service]
Type=simple
User=streambridge
ExecStart=/usr/local/bin/streambridge -c /etc/streambridge/config.yaml
Restart=always
RestartSec=5
LimitNOFILE=65535
StandardOutput=append:/var/log/streambridge/streambridge.log
StandardError=append:/var/log/streambridge/streambridge.log

[Install]
WantedBy=multi-user.target
EOF

# 4. 启动并设置开机自启
sudo systemctl daemon-reload
sudo systemctl enable --now streambridge
sudo systemctl status streambridge

# 常用命令
sudo systemctl restart streambridge   # 重启
sudo systemctl stop streambridge      # 停止
sudo journalctl -u streambridge -f    # 查看实时日志
```

### 场景 2:Windows NSSM 服务(推荐 Windows 用户)

适合 Windows Server、Windows 10/11 桌面,关机重启后自动运行,黑窗口不会意外关闭。

```powershell
# 1. 下载 NSSM:https://nssm.cc/download
# 2. 解压后把 nssm.exe 放到 C:\Windows\System32\ 或当前目录

# 3. 安装服务
nssm install StreamBridge "C:\Program Files\StreamBridge\streambridge.exe"
nssm set StreamBridge AppDirectory "C:\Program Files\StreamBridge"
nssm set StreamBridge AppParameters "-c C:\Program Files\StreamBridge\config.yaml"
nssm set StreamBridge AppStdout "C:\Program Files\StreamBridge\logs\stdout.log"
nssm set StreamBridge AppStderr "C:\Program Files\StreamBridge\logs\stderr.log"
nssm set StreamBridge AppRotateFiles 1
nssm set StreamBridge AppRotateBytes 104857600

# 4. 启动
nssm start StreamBridge

# 常用命令
nssm restart StreamBridge   # 重启
nssm stop StreamBridge      # 停止
nssm remove StreamBridge    # 卸载服务
```

### 场景 3:宝塔面板(BT Panel)

适合国内云服务器用户,使用宝塔的「进程守护管理器」插件 + 反向代理,可视化操作。

**步骤一:安装 StreamBridge**

```bash
# SSH 登录服务器后执行
mkdir -p /www/wwwroot/streambridge
cd /www/wwwroot/streambridge

# 下载二进制(走 Gitee)
curl -L -o streambridge https://gitee.com/suoten/streambridge/releases/latest/download/streambridge-linux-amd64
chmod +x streambridge

# 复制默认配置
curl -L -o config.yaml https://gitee.com/suoten/streambridge/raw/main/configs/streambridge.yaml

# 测试一下能否启动
./streambridge -c config.yaml
# 看到 "listening on :8080" 后按 Ctrl+C 退出
```

**步骤二:在宝塔面板安装进程守护管理器**

1. 登录宝塔面板 → 软件商店 → 搜索「**进程守护管理器**」 → 安装
2. 打开进程守护管理器 → 添加守护进程
   - **名称**:`StreamBridge`
   - **启动用户**:`root`
   - **运行目录**:`/www/wwwroot/streambridge`
   - **启动命令**:`/www/wwwroot/streambridge/streambridge -c /www/wwwroot/streambridge/config.yaml`
   - **进程数量**:`1`
3. 点击「保存」并启动

**步骤三:配置反向代理 + HTTPS(可选,公网访问用)**

1. 宝塔面板 → 网站 → 添加站点
   - 域名:`stream.example.com`(需提前解析到服务器)
   - 根目录:任意(可建一个空目录)
   - PHP 版本:纯静态
2. 站点设置 → 反向代理 → 添加反向代理
   - 代理名称:`StreamBridge`
   - 目标 URL:`http://127.0.0.1:8080`
   - 发送域名:`$host`
3. 站点设置 → SSL → Let's Encrypt → 申请免费证书 → 强制 HTTPS
4. 站点设置 → 配置文件,在 `location /` 后增加 WebSocket 支持(否则 ws-flv 不通):

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;

    # WebSocket 支持(必需,否则 ws-flv 无法播放)
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_read_timeout 86400;
}

# HLS 切片缓存优化
location /hls/ {
    proxy_pass http://127.0.0.1:8080;
    expires 10s;
    add_header Cache-Control "public";
}
```

5. 保存 → 重启 Nginx

**步骤四:宝塔防火墙放行端口**

宝塔面板 → 安全 → 放行端口:
- `8080` TCP(直连访问,配置反代后可关闭)
- `1935` TCP(用 RTMP 推流时开)
- `20000-30000` UDP(对接 GB28181 时开)

### 场景 4:1Panel 面板

1Panel 是现代化的开源 Linux 服务器运维面板,适合追求现代化 UI 的用户。

```bash
# 1. 安装 1Panel(参考 1panel.cn 官方文档)
curl -sSL https://resource.fit2cloud.com/1panel/package/quick_start.sh -o quick_start.sh && sudo bash quick_start.sh
```

**方式 A:Docker 应用商店部署**

1Panel → 应用商店 → 搜索 `StreamBridge` → 安装(若已上架)
或使用「容器」→「编排」→ 粘贴以下 Compose 文件:

```yaml
version: '3.8'
services:
  streambridge:
    image: suoten/streambridge:latest
    container_name: streambridge
    restart: unless-stopped
    ports:
      - "8080:8080"
      - "1935:1935"
      - "20000-30000:20000-30000/udp"
    volumes:
      - ./config.yaml:/etc/streambridge/config.yaml:ro
      - ./logs:/var/log/streambridge
```

**方式 B:二进制 + systemd**

参考 [场景 1:Linux systemd 服务](#场景-1linux-systemd-服务推荐),1Panel 与 systemd 共存,1Panel 主要负责可视化查看日志、文件和 Nginx 反代。

**1Panel 反向代理配置**

1Panel → 网站 → 创建网站 → 反向代理
- 主域名:`stream.example.com`
- 代理地址:`http://127.0.0.1:8080`
- 开启 HTTPS:自动申请 Let's Encrypt 证书
- 高级设置 → 自定义 Nginx 配置:加入上面的 WebSocket 升级配置(宝塔场景三第 4 步的 Nginx 配置)

### 场景 5:Docker Compose(适合多容器编排)

```yaml
# docker-compose.yml
version: '3.8'
services:
  streambridge:
    image: suoten/streambridge:latest
    # 国内服务器可改用阿里云镜像:
    # image: registry.cn-hangzhou.aliyuncs.com/suoten/streambridge:latest
    container_name: streambridge
    restart: unless-stopped
    ports:
      - "8080:8080"
      - "1935:1935"
      - "20000-30000:20000-30000/udp"
    volumes:
      - ./config.yaml:/etc/streambridge/config.yaml:ro
      - ./logs:/var/log/streambridge
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8080/api/health"]
      interval: 30s
      timeout: 5s
      retries: 3
```

```bash
docker compose up -d          # 启动
docker compose logs -f        # 查看日志
docker compose restart        # 重启
docker compose down           # 停止并删除
```

### 场景 6:Kubernetes(适合集群部署)

```yaml
# k8s-deploy.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: streambridge
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: streambridge
  namespace: streambridge
spec:
  replicas: 1
  selector:
    matchLabels:
      app: streambridge
  template:
    metadata:
      labels:
        app: streambridge
    spec:
      containers:
        - name: streambridge
          image: suoten/streambridge:latest
          ports:
            - containerPort: 8080
              name: http
            - containerPort: 1935
              name: rtmp
            - containerPort: 1985
              name: whep
          resources:
            requests:
              cpu: "500m"
              memory: "512Mi"
            limits:
              cpu: "4000m"
              memory: "4Gi"
          readinessProbe:
            httpGet:
              path: /api/ready
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 10
          livenessProbe:
            httpGet:
              path: /api/health
              port: 8080
            initialDelaySeconds: 15
            periodSeconds: 20
---
apiVersion: v1
kind: Service
metadata:
  name: streambridge
  namespace: streambridge
spec:
  type: ClusterIP
  selector:
    app: streambridge
  ports:
    - name: http
      port: 8080
      targetPort: 8080
    - name: rtmp
      port: 1935
      targetPort: 1935
    - name: whep
      port: 1985
      targetPort: 1985
```

```bash
kubectl apply -f k8s-deploy.yaml
kubectl get pods -n streambridge
kubectl logs -f deploy/streambridge -n streambridge
```

对接 Ingress:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: streambridge
  namespace: streambridge
  annotations:
    nginx.ingress.kubernetes.io/proxy-read-timeout: "86400"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "86400"
    nginx.ingress.kubernetes.io/websocket-services: streambridge
spec:
  rules:
    - host: stream.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: streambridge
                port:
                  number: 8080
```

### 场景 7:PM2 进程管理(Node.js 用户首选)

PM2 是跨平台进程管理工具,Windows / Linux / macOS 通用,无需写 service 文件。

```bash
# 1. 安装 PM2(需 Node.js 环境)
npm install -g pm2

# 2. 启动 StreamBridge
pm2 start ./streambridge --name streambridge -- -c config.yaml

# 3. 设置开机自启
pm2 save
pm2 startup        # Linux/macOS 自动生成 systemd / launchd 服务
# Windows 需要额外安装: npm install -g pm2-windows-startup && pm2-startup install

# 常用命令
pm2 status                     # 查看状态
pm2 logs streambridge          # 查看日志
pm2 restart streambridge       # 重启
pm2 stop streambridge          # 停止
pm2 delete streambridge        # 移除
```

### 场景 8:Supervisor 进程管理(Python 用户首选)

```bash
# 1. 安装 Supervisor
sudo apt install -y supervisor        # Debian/Ubuntu
# sudo yum install -y supervisor       # CentOS/RHEL

# 2. 写入配置
sudo tee /etc/supervisor/conf.d/streambridge.conf > /dev/null <<'EOF'
[program:streambridge]
command=/usr/local/bin/streambridge -c /etc/streambridge/config.yaml
directory=/etc/streambridge
user=streambridge
autostart=true
autorestart=true
startsecs=5
stopwaitsecs=10
stdout_logfile=/var/log/streambridge/supervisor.log
stdout_logfile_maxbytes=100MB
stdout_logfile_backups=7
redirect_stderr=true
EOF

# 3. 启动
sudo supervisorctl reread
sudo supervisorctl update
sudo supervisorctl status streambridge
```

### 场景 9:Nginx 反向代理 + HTTPS(手动配置)

非宝塔/1Panel 用户,直接用 Nginx:

```nginx
server {
    listen 443 ssl http2;
    server_name stream.example.com;

    ssl_certificate     /etc/letsencrypt/live/stream.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/stream.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # WebSocket-FLV(必需,否则 ws-flv 无法播放)
    location /ws/ {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 86400;
    }

    # HLS 切片缓存优化
    location /hls/ {
        proxy_pass http://127.0.0.1:8080;
        expires 10s;
        add_header Cache-Control "public";
    }
}

# HTTP 强制跳转 HTTPS
server {
    listen 80;
    server_name stream.example.com;
    return 301 https://$host$request_uri;
}
```

申请免费证书(Let's Encrypt):

```bash
sudo apt install -y certbot python3-certbot-nginx
sudo certbot --nginx -d stream.example.com
```

### 场景 10:Caddy 反向代理(自动 HTTPS)

Caddy 会自动申请并续期 HTTPS 证书,配置最简:

```caddyfile
# /etc/caddy/Caddyfile
stream.example.com {
    reverse_proxy 127.0.0.1:8080

    # WebSocket 自动支持,无需额外配置
    # HLS 缓存(可选)
    @hls path /hls/*
    handle @hls {
        reverse_proxy 127.0.0.1:8080
        header Cache-Control "public"
        expire 10s
    }
}
```

```bash
sudo systemctl reload caddy
```

### 防火墙端口速查

| 端口 | 协议 | 用途 | 是否必需 |
|------|------|------|---------|
| 8080 | TCP | HTTP / WebSocket | ✅ 必需 |
| 443 | TCP | HTTPS(配反代后) | 公网部署推荐 |
| 1935 | TCP | RTMP 推流 | 用 RTMP 时开 |
| 1985 | TCP | WebRTC WHEP | 用 WebRTC 时开 |
| 20000-30000 | UDP | RTP/PS 接收 | 对接 GB28181 时开 |

### 部署场景选型建议

| 场景 | 推荐人群 | 上手难度 |
|------|---------|---------|
| Windows NSSM | Windows 桌面 / Windows Server 用户 | ⭐ |
| Linux systemd | Linux 服务器运维 | ⭐⭐ |
| 宝塔面板 | 国内云服务器、可视化控台爱好者 | ⭐⭐ |
| 1Panel | 现代化 UI 控台爱好者 | ⭐⭐ |
| Docker / Compose | 容器化部署、CI/CD 流水线 | ⭐⭐ |
| Kubernetes | 大规模集群、DevOps 团队 | ⭐⭐⭐⭐ |
| PM2 | Node.js 全栈用户 | ⭐ |
| Supervisor | Python 全栈用户 | ⭐⭐ |
| Nginx / Caddy 反代 | 任何场景下的 HTTPS 接入层 | ⭐⭐ |

---

## 🔧 自行编译

### 依赖

- Go 1.22+(下载:<https://go.dev/dl/>)

### 编译

```bash
# 当前平台
make build

# 交叉编译全平台
make build-all

# Docker 镜像
make docker

# 输出在 bin/ 目录
```

### 运行测试

```bash
make test
make lint
```

### 项目结构

```
StreamBridge/
├── cmd/streambridge/        # 主入口(main.go,命令行)
├── internal/
│   ├── config/              # 配置解析与命令行参数
│   ├── input/               # 输入协议适配层
│   │   ├── rtsp/            # RTSP 客户端(gortsplib v4)
│   │   ├── rtmp/            # RTMP 客户端
│   │   ├── rtp/             # RTP/PS 接收与解封装
│   │   ├── file/            # 文件回放
│   │   └── http/            # HTTP-FLV / HLS 输入透传
│   ├── media/               # 媒体处理层
│   │   ├── flv/             # FLV 封装器
│   │   ├── hls/             # HLS 切片器
│   │   ├── h264.go          # H264 NALU 解析
│   │   ├── h265.go          # H265 NALU 解析
│   │   └── aac.go           # AAC 处理
│   ├── server/              # HTTP/WS 服务
│   │   ├── web/             # 内嵌静态资源(播放器页面)
│   │   ├── http.go          # 路由与 REST API
│   │   └── flv_writer.go    # FLV 写入器
│   ├── session/             # 会话管理(复用/鉴权/统计)
│   └── webrtc/              # WebRTC WHEP 实现
├── sdk/                     # 客户端 SDK(全平台)
│   ├── javascript/
│   ├── wechat-miniprogram/
│   ├── android/
│   └── ios/
├── configs/                 # 配置示例
├── Dockerfile               # 容器化
├── Makefile                 # 构建脚本
└── go.mod
```

---

## ❓ 常见问题 FAQ

### Q1: 启动后浏览器打不开?

- 检查 8080 端口是否被占用:`streambridge doctor`
- Windows 用户检查防火墙是否拦截
- Linux/macOS 监听 80 以下端口需 `sudo`
- 云服务器需在安全组放行 8080 端口

### Q2: 播放 RTSP 黑屏/卡顿?

- 确认摄像头 RTSP 地址正确(可用 VLC 测试)
- 摄像头与 StreamBridge 服务器网络可达
- H265 流请开启 `enable_h265_transcode: true`(消耗 CPU)
- 检查日志:`streambridge --log-level debug`

### Q3: 多少路并发能跑?

- 1C1G 服务器:20+ 路 1080P(纯转封装)
- 4C8G 服务器:200+ 路 1080P(纯转封装)
- 启用 H265 转码:每路约消耗 1 个 CPU 核(软编)
- 大规模(≥100 路 H265 转码)请考虑企业版 GPU 硬件转码

### Q4: 能否在公网访问?

可以,建议配置 Nginx + HTTPS,详见 [生产部署](#-生产部署)。

### Q5: 是否支持 H265?

- 浏览器通过 MSE 播放 H265 兼容性差
- StreamBridge 社区版内置 H265→H264 软件转码(默认关闭,需开启)
- 开启方式:`enable_h265_transcode: true`

### Q6: 与 ZLMediaKit 有何区别?

- ZLMediaKit 是综合流媒体平台,功能多但部署复杂
- StreamBridge 专注"协议转换 + 浏览器播放",单二进制零依赖
- StreamBridge **不做** SIP/PTZ/录像/ONVIF,这些由上游平台负责

### Q7: 可以用来做直播/SaaS 吗?

可以,但直播场景建议:
- 配置 HLS 输出(延迟 3-10s,但兼容性最好)
- 公网部署配置 Nginx + HTTPS
- 大规模请考虑企业版集群 HA

### Q8: 商业使用是否收费?

社区版 **永久免费,MIT 协议,可商用**。企业版仅提供扩规模/合规/支持类功能,无功能阉割。

### Q9: 用了宝塔/1Panel 反代后,WebSocket-FLV 播放失败?

反代配置必须包含 WebSocket 升级配置(`Upgrade` / `Connection` 头),详见 [场景 3:宝塔面板](#场景-3宝塔面板bt-panel) 第 3 步的 Nginx 配置。

### Q10: 如何升级版本?

- 二进制部署:重新下载新版二进制,替换旧文件,重启服务
- Docker:`docker compose pull && docker compose up -d`
- systemd:`sudo systemctl restart streambridge`
- 配置文件向后兼容,升级无需修改

### Q11: 如何卸载?

- 二进制:停止服务 → 删除二进制与配置目录
- Docker:`docker compose down -v`
- systemd:`sudo systemctl disable --now streambridge && sudo rm /etc/systemd/system/streambridge.service && sudo systemctl daemon-reload`

---

## 🏗️ 架构设计

```
┌─────────────────────────────────────────────────────────┐
│                   StreamBridge Gateway                   │
│                单二进制 · 零外部依赖                       │
├─────────────────────────────────────────────────────────┤
│                                                          │
│  ┌──────────────────────────────────────────────────┐   │
│  │  GB28181 平台 / NVR / 摄像头 / 直播推流          │   │
│  │  (上游负责 SIP/PTZ/录像,StreamBridge 不参与)      │   │
│  └────────────────────┬─────────────────────────────┘   │
│                       │ 媒体流                           │
│                       ▼                                 │
│  ┌──────────────────────────────────────────────────┐   │
│  │              输入协议适配层                        │   │
│  │  RTSP Client │ RTMP Server │ RTP/PS │ File │ HLS │   │
│  └────────────────────┬─────────────────────────────┘   │
│                       ▼                                 │
│  ┌──────────────────────────────────────────────────┐   │
│  │              统一媒体处理层                        │   │
│  │  H264/H265 NALU 解析 │ AAC 提取 │ H265→H264 转码  │   │
│  └────────────────────┬─────────────────────────────┘   │
│                       ▼                                 │
│  ┌──────────────────────────────────────────────────┐   │
│  │              输出封装层                            │   │
│  │   WebSocket-FLV  │  HLS 切片  │  WebRTC WHEP      │   │
│  └────────────────────┬─────────────────────────────┘   │
│                       ▼                                 │
│            浏览器 / 小程序 / Android / iOS               │
└─────────────────────────────────────────────────────────┘
```

### 关键设计

1. **统一媒体模型**:所有输入协议解封装为统一的 `media.Frame`,输出层无需关心输入来源
2. **流复用**:多浏览器观看同一路源,只拉 1 路输入,节省带宽
3. **零拷贝转发**:H264 流不重新编码,直接转封装为 FLV,CPU 占用极低
4. **静态资源嵌入**:Web 播放器页面通过 `embed.FS` 嵌入二进制,真正单文件部署

---

## 💎 社区版 vs 企业版

| 类别 | 社区版(免费) | 企业版(付费) |
|------|--------------|--------------|
| 协议转换 | ✅ 全部 | ✅ 全部 |
| H264/H265 处理 | ✅ 软件转码 | ✅ + GPU 硬件转码(NVENC/QSV) |
| 客户端 SDK | ✅ 全平台 | ✅ 全平台 |
| REST API | ✅ 完整 | ✅ 完整 |
| 单节点并发 | ✅ 200+ 路 | ✅ 集群 HA(数千路) |
| 传输加密 | TLS/HTTPS | + 国密 SM4 / HLS DRM |
| 监控告警 | Prometheus 指标 | + Grafana 看板 + 告警 |
| 操作审计 | 基础日志 | + 完整审计报表 |
| 商业支持 | 社区 issue | 7x24 SLA + 专属工程师 |

> 社区版功能完整可用,无任何功能阉割,可独立商用。企业版仅提供"扩规模、合规、支持"类能力。

---

## 🤝 贡献与反馈

- 提交 Issue:
  - GitHub:<https://github.com/suoten/streambridge/issues>
  - Gitee:<https://gitee.com/suoten/streambridge/issues>
- 提交 Pull Request:欢迎修复 Bug 或添加新功能,请先在 Issue 中讨论

---

## 📄 License

[MIT License](LICENSE) - 社区版永久免费,可商用。

Copyright (c) 2026 StreamBridge Contributors
