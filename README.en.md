# StreamBridge

> **One file, connect cameras to browsers** — Single-binary, zero-dependency, full-protocol streaming gateway

[简体中文](README.md) | **English**

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8.svg)](https://golang.org)
[![Edition](https://img.shields.io/badge/Edition-Community-blue)](#-community-vs-enterprise-edition)
[![GitHub Release](https://img.shields.io/github/v/release/suoten/streambridge?label=Release&logo=github)](https://github.com/suoten/streambridge/releases)
[![GitHub Stars](https://img.shields.io/github/stars/suoten/streambridge?style=social)](https://github.com/suoten/streambridge)
[![GitHub Downloads](https://img.shields.io/github/downloads/suoten/streambridge/total?label=Downloads&logo=github)](https://github.com/suoten/streambridge/releases)
[![Build](https://github.com/suoten/streambridge/actions/workflows/build.yml/badge.svg?branch=main)](https://github.com/suoten/streambridge/actions)
[![Gitee Release](https://img.shields.io/badge/Gitee-Release-C71D23?logo=gitee)](https://gitee.com/suoten/streambridge/releases)

- 🐙 GitHub Repository: <https://github.com/suoten/streambridge>
- 🐯 Gitee Repository (faster for users in mainland China): <https://gitee.com/suoten/streambridge>

> 🌟 **If StreamBridge helps you, please [give it a Star](https://github.com/suoten/streambridge)!**
> 🎬 **Online Demo**: [http://demo.streambridge.example.com:8080](http://demo.streambridge.example.com:8080) (if deployed)

### 🎯 Use Cases

| Scenario | Description |
|------|------|
| 📷 Security Monitoring | Hikvision/Dahua/Uniview cameras → browser viewing |
| 🏭 Industrial Visualization | Production-line cameras → dashboard display |
| 🔗 GB28181 Platform | Browser playback gateway (RTP/PS only, no SIP) |
| 📱 IoT Devices | RTSP devices → Web/MiniProgram/Android/iOS playback |
| 🎬 Live Transcoding | RTMP push → HLS/WebRTC distribution |

### 📸 Demo Screenshots

> Screenshots coming soon. After first launch, visit `http://localhost:8080` to see the actual UI.

---

StreamBridge converts video streams from industrial / security cameras (RTSP / RTMP / RTP / PS) into formats that browsers can play directly (WebSocket-FLV / HLS / WebRTC).

**Not a Platform**: StreamBridge does NOT include GB28181 SIP signaling, PTZ control, recording management, or ONVIF discovery — these are handled by upstream platforms. StreamBridge does one thing: transcode / remux the upstream-negotiated video streams into browser-playable formats.

---

## ✨ Key Features

- 🚀 **Zero-config startup**: Download a single binary, double-click to run, ready in 5 minutes
- 📦 **Single-file deployment**: No Docker / FFmpeg / ZLMediaKit dependencies, 20+ 1080P streams on a 1C1G server
- 🔄 **Full protocol input**: RTSP / RTMP / RTP/PS / MP4 file / HTTP-FLV / HLS
- 📺 **Browser-friendly**: WebSocket-FLV (recommended) / HLS / WebRTC WHEP outputs
- 🎥 **H265 compatibility**: Built-in H265→H264 software transcoding for browser H265 incompatibility
- 📱 **Full-platform SDK**: JavaScript / WeChat MiniProgram / Android / iOS, all free in Community Edition
- 🌐 **Cross-platform**: Windows / Linux / macOS / Docker / K8s
- 🔓 **Permanently free**: MIT license, commercial use allowed, no feature limitations

---

## 📖 Table of Contents

- [Download & Install](#-download--install)
- [60-Second Quick Start](#-60-second-quick-start)
- [Client SDK](#-client-sdk)
- [Configuration](#️-configuration)
- [REST API Reference](#-rest-api-reference)
- [Working with GB28181 Platforms](#-working-with-gb28181-platforms)
- [Production Deployment (BaoTa / 1Panel / K8s / etc.)](#-production-deployment)
- [Build from Source](#-build-from-source)
- [FAQ](#-faq)
- [Architecture](#-architecture)
- [Community vs Enterprise Edition](#-community-vs-enterprise-edition)
- [License](#-license)

---

## 📦 Download & Install

### Option 1: Direct Binary Download (Recommended for Beginners)

Open the Releases page of either repository and download the file matching your OS:

| OS | Architecture | Filename | GitHub | Gitee (Faster in China) |
|------|------|--------|------------|---------------------|
| Windows | x86_64 | `streambridge-windows-amd64.exe` | [GitHub Release](https://github.com/suoten/streambridge/releases/latest) | [Gitee Release](https://gitee.com/suoten/streambridge/releases/latest) |
| Linux | x86_64 | `streambridge-linux-amd64` | [GitHub Release](https://github.com/suoten/streambridge/releases/latest) | [Gitee Release](https://gitee.com/suoten/streambridge/releases/latest) |
| Linux | ARM64 | `streambridge-linux-arm64` | [GitHub Release](https://github.com/suoten/streambridge/releases/latest) | [Gitee Release](https://gitee.com/suoten/streambridge/releases/latest) |
| macOS | Apple Silicon | `streambridge-darwin-arm64` | [GitHub Release](https://github.com/suoten/streambridge/releases/latest) | [Gitee Release](https://gitee.com/suoten/streambridge/releases/latest) |

> 💡 **Users in mainland China: prefer Gitee** — download speed is 10x faster than GitHub.

No installation needed, just run it:

```bash
# Linux / macOS (needs execute permission)
chmod +x streambridge-linux-amd64
./streambridge-linux-amd64

# Windows
# Double-click streambridge-windows-amd64.exe — a black console window will appear (do not close it)
```

### Option 2: One-Line Command Download (For Linux Servers)

```bash
# International server (GitHub)
LATEST_URL=$(curl -s https://api.github.com/repos/suoten/streambridge/releases/latest \
  | grep "browser_download_url.*streambridge-linux-amd64\"" \
  | cut -d '"' -f 4)

# China server (Gitee)
# LATEST_URL=$(curl -s https://gitee.com/api/v5/repos/suoten/streambridge/releases/latest \
#   | grep -oP '"browser_download_url"\s*:\s*"\K[^"]*streambridge-linux-amd64"' \
#   | head -1 | tr -d '"')

curl -L -o /usr/local/bin/streambridge "$LATEST_URL"
chmod +x /usr/local/bin/streambridge
streambridge
```

### Option 3: Docker (For Containerized Deployment)

```bash
# Quick try
docker run -d --name streambridge -p 8080:8080 --restart unless-stopped \
  --pull always \
  suoten/streambridge:latest

# Full features (with RTMP / RTP)
docker run -d --name streambridge \
  -p 8080:8080 -p 1935:1935 \
  -p 20000-30000:20000-30000/udp \
  --restart unless-stopped \
  suoten/streambridge:latest
```

Image sources:
- Docker Hub: `suoten/streambridge:latest`
- Aliyun mirror (China): `registry.cn-hangzhou.aliyuncs.com/suoten/streambridge:latest`

### Option 4: Build from Source (Requires Go 1.22+)

```bash
git clone https://github.com/suoten/streambridge.git
cd streambridge
make build          # Current platform
make build-all      # Cross-compile all platforms
```

See [Build from Source](#-build-from-source) section.

---

## 🚀 60-Second Quick Start

### First Visit

After downloading and running, open `http://localhost:8080` (local) or `http://SERVER_IP:8080` (remote) in your browser:

- **Player Test Page**: Paste an RTSP URL to play
- **Demo Stream Button**: Click to see a demo without a camera
- **Stream List**: Active streams and viewer counts
- **API Docs**: `http://localhost:8080/api/docs`

### Play a Camera for the First Time (Zero Code)

1. Open `http://localhost:8080` in your browser
2. Paste an RTSP URL (e.g., Hikvision `rtsp://admin:abc12345@192.168.1.64:554/Streaming/Channels/101`)
3. Click the "Play" button ✅

#### Common Camera RTSP URLs

| Brand | Default URL |
|------|---------|
| Hikvision | `rtsp://admin:<password>@<IP>:554/Streaming/Channels/101` |
| Dahua | `rtsp://admin:<password>@<IP>:554/cam/realmonitor?channel=1&subtype=0` |
| Uniview | `rtsp://admin:<password>@<IP>:554/media/video1` |
| Generic ONVIF | Get from NVR |

### One-Click Diagnostics

```bash
streambridge doctor
# Auto-checks ports, config, browser compatibility, network reachability
```

Or visit `http://localhost:8080/doctor` in your browser.

### Health Check

```bash
curl http://localhost:8080/api/health
# {"status":"ok","version":"v1.0.0","uptime":123,"streams":0,"viewers":0}
```

---

## 📱 Client SDK

Community Edition provides all-platform SDKs for free, see [sdk/README.md](sdk/README.md).

| Platform | File | Playback Method |
|------|------|---------|
| Browser | [sdk/javascript/streambridge.js](sdk/javascript/streambridge.js) | WebSocket-FLV (MSE) / HLS / WebRTC |
| WeChat MiniProgram | [sdk/wechat-miniprogram/streambridge.js](sdk/wechat-miniprogram/streambridge.js) | HLS |
| Android | [sdk/android/StreamBridgeView.java](sdk/android/StreamBridgeView.java) | HLS (ExoPlayer) |
| iOS | [sdk/ios/StreamBridgePlayerView.swift](sdk/ios/StreamBridgePlayerView.swift) | HLS (AVPlayer) |

### One-Line Browser Playback

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

## ⚙️ Configuration

StreamBridge supports zero-config startup, or specify a config file with `-c config.yaml`. Full example at [configs/streambridge.yaml](configs/streambridge.yaml).

```yaml
server:
  http_port: 8080          # HTTP port
  rtmp_port: 1935          # RTMP push port
  whep_port: 1985          # WebRTC signaling port
  bind_addr: "0.0.0.0"     # Listen address

log:
  level: info              # debug | info | warn | error

performance:
  max_streams: 500                  # Max concurrent streams
  max_viewers_per_stream: 50        # Max viewers per stream
  enable_h265_transcode: false      # Enable H265->H264 transcoding

input:
  rtp:
    listen: "0.0.0.0"
    port_range: [20000, 30000]      # RTP media port range
    mode: passive                   # Passive (forwarded by GB28181 platform)

security:
  enable_auth: false                # Enable Token auth
  secret_key: "change-me-to-random-string"
  allow_origins: ["*"]              # CORS whitelist
```

### Command-Line Flags

```bash
streambridge                                    # Zero-config startup
streambridge -c /etc/streambridge/config.yaml   # Specify config file
streambridge --http-port 9090 --log-level debug
streambridge doctor                             # Run diagnostics
streambridge version                            # Show version
```

---

## 📡 REST API Reference

### Start Stream (Returns WebSocket/HLS URL)

```http
GET /api/play?url=<rtsp-url>&format=flv
```

Response:
```json
{
  "wsUrl": "ws://localhost:8080/ws/abc123",
  "streamId": "abc123",
  "format": "flv",
  "source": "rtsp://..."
}
```

### Start HLS Stream

```http
GET /hls/play?url=<rtsp-url>
```

Response:
```json
{
  "streamId": "abc123",
  "hlsUrl": "http://localhost:8080/hls/abc123.m3u8"
}
```

### Stop Stream

```http
POST /api/stop?id=<streamId>
```

### List Active Streams

```http
GET /api/streams
```

### Health Check

```http
GET /api/health
```

### File Playback

```http
POST /api/file/play
Content-Type: application/json

{"path": "/data/video.mp4", "loop": true}
```

### RTP Listener (For GB28181 Integration)

```http
POST /api/rtp/listen
Content-Type: application/json

{"port": 20000, "mode": "ps"}
```

### Prometheus Metrics

```http
GET /metrics
```

### Full Endpoint List

| Endpoint | Method | Description |
|------|------|------|
| `/api/health` | GET | Health check |
| `/api/ready` | GET | Readiness check |
| `/api/version` | GET | Version info |
| `/api/play` | GET | Start stream (returns ws/flv URL) |
| `/api/stop` | POST | Stop stream |
| `/api/streams` | GET | Active streams list |
| `/api/stats?id=<id>` | GET | Per-stream stats |
| `/api/file/play` | POST | File playback |
| `/api/rtp/listen` | POST | RTP listener |
| `/api/demo` | GET | Demo stream URL |
| `/api/logs` | GET | Log event stream (SSE) |
| `/metrics` | GET | Prometheus metrics |
| `/ws/<streamId>` | WS | WebSocket-FLV push |
| `/live/<streamId>` | GET | HTTP-FLV direct |
| `/hls/<streamId>.m3u8` | GET | HLS playlist |
| `/hls/<streamId>/<seg>.ts` | GET | HLS segment |
| `/whep/<streamId>` | POST | WebRTC WHEP signaling |

---

## 🔗 Working with GB28181 Platforms

StreamBridge does **NOT** participate in SIP signaling / PTZ / recording. It only receives RTP/PS media forwarded by upstream platforms:

```
[Camera] ──SIP──→ [GB28181 Platform] ──RTP/PS──→ [StreamBridge] ──WebSocket-FLV──→ [Browser]
                       │
                       └── PTZ / Intercom / Recording handled by platform
```

### Configuration Steps

1. Enable RTP reception in StreamBridge config:

```yaml
input:
  rtp:
    listen: "0.0.0.0"
    port_range: [20000, 30000]
    mode: passive
    ps_depacketize: true
```

2. Configure on the GB28181 platform side:
   - Media server IP = StreamBridge IP
   - Media port range = 20000-30000

3. On platform INVITE, streams are automatically forwarded to StreamBridge, which auto-decapsulates PS → H264/AAC → FLV output.

---

## 🏭 Production Deployment

StreamBridge's single binary is production-ready. Choose any of the scenarios below.

### Scenario 1: Linux systemd Service (Recommended)

For Linux servers, cloud hosts, VPS — auto-start on boot + auto-restart on crash.

```bash
# 1. Create dedicated user
sudo useradd -r -s /bin/false streambridge

# 2. Prepare directories and files
sudo mkdir -p /etc/streambridge /var/log/streambridge
sudo cp configs/streambridge.yaml /etc/streambridge/config.yaml
sudo cp bin/streambridge-linux-amd64 /usr/local/bin/streambridge
sudo chown -R streambridge:streambridge /etc/streambridge /var/log/streambridge

# 3. Write systemd service
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

# 4. Start and enable auto-start
sudo systemctl daemon-reload
sudo systemctl enable --now streambridge
sudo systemctl status streambridge

# Common commands
sudo systemctl restart streambridge   # Restart
sudo systemctl stop streambridge      # Stop
sudo journalctl -u streambridge -f    # Tail logs
```

### Scenario 2: Windows NSSM Service (Recommended for Windows Users)

For Windows Server, Windows 10/11 desktops — auto-start after reboot, no accidental console closure.

```powershell
# 1. Download NSSM: https://nssm.cc/download
# 2. Place nssm.exe in C:\Windows\System32\ or current directory

# 3. Install service
nssm install StreamBridge "C:\Program Files\StreamBridge\streambridge.exe"
nssm set StreamBridge AppDirectory "C:\Program Files\StreamBridge"
nssm set StreamBridge AppParameters "-c C:\Program Files\StreamBridge\config.yaml"
nssm set StreamBridge AppStdout "C:\Program Files\StreamBridge\logs\stdout.log"
nssm set StreamBridge AppStderr "C:\Program Files\StreamBridge\logs\stderr.log"
nssm set StreamBridge AppRotateFiles 1
nssm set StreamBridge AppRotateBytes 104857600

# 4. Start
nssm start StreamBridge

# Common commands
nssm restart StreamBridge   # Restart
nssm stop StreamBridge      # Stop
nssm remove StreamBridge    # Uninstall service
```

### Scenario 3: BaoTa Panel (BT Panel)

For users of China cloud servers — uses BaoTa's "Process Guardian Manager" plugin + reverse proxy, fully visual.

**Step 1: Install StreamBridge**

```bash
# SSH into your server and run:
mkdir -p /www/wwwroot/streambridge
cd /www/wwwroot/streambridge

# Download binary (via Gitee)
curl -L -o streambridge https://gitee.com/suoten/streambridge/releases/latest/download/streambridge-linux-amd64
chmod +x streambridge

# Copy default config
curl -L -o config.yaml https://gitee.com/suoten/streambridge/raw/main/configs/streambridge.yaml

# Test startup
./streambridge -c config.yaml
# After you see "listening on :8080", press Ctrl+C to stop
```

**Step 2: Install Process Guardian Manager in BaoTa**

1. BaoTa Panel → App Store → Search "**进程守护管理器** (Process Guardian Manager)" → Install
2. Open Process Guardian Manager → Add guardian process:
   - **Name**: `StreamBridge`
   - **Run User**: `root`
   - **Working Dir**: `/www/wwwroot/streambridge`
   - **Start Command**: `/www/wwwroot/streambridge/streambridge -c /www/wwwroot/streambridge/config.yaml`
   - **Process Count**: `1`
3. Click "Save" and start

**Step 3: Configure Reverse Proxy + HTTPS (Optional, for public access)**

1. BaoTa Panel → Website → Add Site
   - Domain: `stream.example.com` (must resolve to your server first)
   - Root directory: any (can be empty)
   - PHP version: Pure static
2. Site Settings → Reverse Proxy → Add reverse proxy
   - Proxy name: `StreamBridge`
   - Target URL: `http://127.0.0.1:8080`
   - Send domain: `$host`
3. Site Settings → SSL → Let's Encrypt → Apply for free cert → Force HTTPS
4. Site Settings → Config file, add WebSocket support after `location /` (otherwise ws-flv fails):

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;

    # WebSocket support (REQUIRED, otherwise ws-flv won't play)
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_read_timeout 86400;
}

# HLS segment cache optimization
location /hls/ {
    proxy_pass http://127.0.0.1:8080;
    expires 10s;
    add_header Cache-Control "public";
}
```

5. Save → Restart Nginx

**Step 4: Open Firewall Ports in BaoTa**

BaoTa Panel → Security → Open ports:
- `8080` TCP (direct access; can close after reverse proxy is configured)
- `1935` TCP (for RTMP push)
- `20000-30000` UDP (for GB28181 integration)

### Scenario 4: 1Panel

1Panel is a modern open-source Linux server management panel — for users who prefer a modern UI.

```bash
# 1. Install 1Panel (see 1panel.cn docs)
curl -sSL https://resource.fit2cloud.com/1panel/package/quick_start.sh -o quick_start.sh && sudo bash quick_start.sh
```

**Option A: Docker App Store**

1Panel → App Store → Search `StreamBridge` → Install (if listed)
Or use "Containers" → "Compose" → paste the Compose file below:

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

**Option B: Binary + systemd**

Follow [Scenario 1: Linux systemd Service](#scenario-1-linux-systemd-service-recommended). 1Panel coexists with systemd; 1Panel mainly provides visual log/file viewing and Nginx reverse proxy.

**1Panel Reverse Proxy Configuration**

1Panel → Websites → Create Website → Reverse Proxy
- Primary Domain: `stream.example.com`
- Proxy Address: `http://127.0.0.1:8080`
- Enable HTTPS: auto-apply Let's Encrypt cert
- Advanced Settings → Custom Nginx config: add the WebSocket upgrade config from BaoTa Scenario 3 Step 4

### Scenario 5: Docker Compose (For Multi-Container Orchestration)

```yaml
# docker-compose.yml
version: '3.8'
services:
  streambridge:
    image: suoten/streambridge:latest
    # For China servers, use Aliyun mirror:
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
docker compose up -d          # Start
docker compose logs -f        # View logs
docker compose restart        # Restart
docker compose down           # Stop and remove
```

### Scenario 6: Kubernetes (For Cluster Deployment)

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

With Ingress:

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

### Scenario 7: PM2 Process Manager (For Node.js Users)

PM2 is a cross-platform process manager, works on Windows / Linux / macOS — no service file needed.

```bash
# 1. Install PM2 (requires Node.js)
npm install -g pm2

# 2. Start StreamBridge
pm2 start ./streambridge --name streambridge -- -c config.yaml

# 3. Enable auto-start on boot
pm2 save
pm2 startup        # Linux/macOS: auto-generates systemd / launchd service
# Windows requires extra: npm install -g pm2-windows-startup && pm2-startup install

# Common commands
pm2 status                     # Show status
pm2 logs streambridge          # View logs
pm2 restart streambridge       # Restart
pm2 stop streambridge          # Stop
pm2 delete streambridge        # Remove
```

### Scenario 8: Supervisor Process Manager (For Python Users)

```bash
# 1. Install Supervisor
sudo apt install -y supervisor        # Debian/Ubuntu
# sudo yum install -y supervisor       # CentOS/RHEL

# 2. Write config
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

# 3. Start
sudo supervisorctl reread
sudo supervisorctl update
sudo supervisorctl status streambridge
```

### Scenario 9: Nginx Reverse Proxy + HTTPS (Manual Config)

For users without BaoTa / 1Panel — direct Nginx:

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

    # WebSocket-FLV (REQUIRED, otherwise ws-flv won't play)
    location /ws/ {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 86400;
    }

    # HLS segment cache optimization
    location /hls/ {
        proxy_pass http://127.0.0.1:8080;
        expires 10s;
        add_header Cache-Control "public";
    }
}

# Force HTTP → HTTPS redirect
server {
    listen 80;
    server_name stream.example.com;
    return 301 https://$host$request_uri;
}
```

Apply for a free cert (Let's Encrypt):

```bash
sudo apt install -y certbot python3-certbot-nginx
sudo certbot --nginx -d stream.example.com
```

### Scenario 10: Caddy Reverse Proxy (Automatic HTTPS)

Caddy automatically obtains and renews HTTPS certificates — simplest config:

```caddyfile
# /etc/caddy/Caddyfile
stream.example.com {
    reverse_proxy 127.0.0.1:8080

    # WebSocket is auto-supported, no extra config needed
    # HLS cache (optional)
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

### Firewall Port Reference

| Port | Protocol | Purpose | Required? |
|------|------|------|---------|
| 8080 | TCP | HTTP / WebSocket | ✅ Required |
| 443 | TCP | HTTPS (with reverse proxy) | Recommended for public |
| 1935 | TCP | RTMP push | For RTMP usage |
| 1985 | TCP | WebRTC WHEP | For WebRTC usage |
| 20000-30000 | UDP | RTP/PS reception | For GB28181 integration |

### Deployment Scenario Selection Guide

| Scenario | Recommended For | Difficulty |
|------|---------|---------|
| Windows NSSM | Windows desktop / Server users | ⭐ |
| Linux systemd | Linux server ops | ⭐⭐ |
| BaoTa Panel | China cloud servers, visual console fans | ⭐⭐ |
| 1Panel | Modern UI console fans | ⭐⭐ |
| Docker / Compose | Containerized deployment, CI/CD | ⭐⭐ |
| Kubernetes | Large-scale clusters, DevOps teams | ⭐⭐⭐⭐ |
| PM2 | Node.js full-stack users | ⭐ |
| Supervisor | Python full-stack users | ⭐⭐ |
| Nginx / Caddy Reverse Proxy | HTTPS edge for any scenario | ⭐⭐ |

---

## 🔧 Build from Source

### Requirements

- Go 1.22+ (download: <https://go.dev/dl/>)

### Build

```bash
# Current platform
make build

# Cross-compile all platforms
make build-all

# Docker image
make docker

# Output goes to bin/ directory
```

### Run Tests

```bash
make test
make lint
```

### Project Structure

```
StreamBridge/
├── cmd/streambridge/        # Entry point (main.go, CLI)
├── internal/
│   ├── config/              # Config parsing & CLI flags
│   ├── input/               # Input protocol adapter layer
│   │   ├── rtsp/            # RTSP client (gortsplib v4)
│   │   ├── rtmp/            # RTMP client
│   │   ├── rtp/             # RTP/PS receiver & depacketizer
│   │   ├── file/            # File playback
│   │   └── http/            # HTTP-FLV / HLS input passthrough
│   ├── media/               # Media processing layer
│   │   ├── flv/             # FLV muxer
│   │   ├── hls/             # HLS slicer
│   │   ├── h264.go          # H264 NALU parsing
│   │   ├── h265.go          # H265 NALU parsing
│   │   └── aac.go           # AAC handling
│   ├── server/              # HTTP/WS server
│   │   ├── web/             # Embedded static assets (player pages)
│   │   ├── http.go          # Routing & REST API
│   │   └── flv_writer.go    # FLV writer
│   ├── session/             # Session management (muxing / auth / stats)
│   └── webrtc/              # WebRTC WHEP implementation
├── sdk/                     # Client SDK (all platforms)
│   ├── javascript/
│   ├── wechat-miniprogram/
│   ├── android/
│   └── ios/
├── configs/                 # Config examples
├── Dockerfile               # Containerization
├── Makefile                 # Build scripts
└── go.mod
```

---

## ❓ FAQ

### Q1: Browser can't open after startup?

- Check if port 8080 is in use: `streambridge doctor`
- Windows users: check if firewall is blocking
- Linux/macOS: ports below 80 require `sudo`
- Cloud servers: open port 8080 in security groups

### Q2: Black screen / stuttering when playing RTSP?

- Verify the RTSP URL is correct (test with VLC)
- Ensure network reachability between camera and StreamBridge server
- For H265 streams, enable `enable_h265_transcode: true` (consumes CPU)
- Check logs: `streambridge --log-level debug`

### Q3: How many concurrent streams can it handle?

- 1C1G server: 20+ 1080P streams (pure remuxing)
- 4C8G server: 200+ 1080P streams (pure remuxing)
- With H265 transcoding: ~1 CPU core per stream (software encoding)
- For large-scale (≥100 H265 transcodes), consider Enterprise GPU hardware transcoding

### Q4: Can it be accessed over the public internet?

Yes. Recommended to configure Nginx + HTTPS, see [Production Deployment](#-production-deployment).

### Q5: Does it support H265?

- Browsers have poor MSE H265 compatibility
- StreamBridge Community Edition has built-in H265→H264 software transcoding (off by default)
- Enable with: `enable_h265_transcode: true`

### Q6: How is it different from ZLMediaKit?

- ZLMediaKit is a comprehensive streaming platform — more features but complex deployment
- StreamBridge focuses on "protocol conversion + browser playback", single binary, zero dependencies
- StreamBridge does **NOT** do SIP / PTZ / recording / ONVIF — those are upstream platform's job

### Q7: Can it be used for live streaming / SaaS?

Yes, but for live scenarios we recommend:
- Configure HLS output (3-10s latency, but best compatibility)
- Public deployment with Nginx + HTTPS
- For large scale, consider Enterprise cluster HA

### Q8: Is commercial use free?

Community Edition is **permanently free, MIT license, commercial use allowed**. Enterprise Edition only provides scale/compliance/support features — no feature crippling.

### Q9: WebSocket-FLV playback fails after BaoTa / 1Panel reverse proxy?

Reverse proxy config MUST include WebSocket upgrade headers (`Upgrade` / `Connection`). See the Nginx config in [Scenario 3: BaoTa Panel](#scenario-3-baota-panel-bt-panel) Step 4.

### Q10: How to upgrade?

- Binary: re-download the new binary, replace the old file, restart service
- Docker: `docker compose pull && docker compose up -d`
- systemd: `sudo systemctl restart streambridge`
- Config files are backward compatible — no need to modify on upgrade

### Q11: How to uninstall?

- Binary: stop service → delete binary and config directory
- Docker: `docker compose down -v`
- systemd: `sudo systemctl disable --now streambridge && sudo rm /etc/systemd/system/streambridge.service && sudo systemctl daemon-reload`

---

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────┐
│                   StreamBridge Gateway                   │
│            Single Binary · Zero External Dependencies    │
├─────────────────────────────────────────────────────────┤
│                                                          │
│  ┌──────────────────────────────────────────────────┐   │
│  │  GB28181 Platform / NVR / Camera / Live Push     │   │
│  │  (Upstream handles SIP/PTZ/Recording;            │   │
│  │   StreamBridge does NOT participate)             │   │
│  └────────────────────┬─────────────────────────────┘   │
│                       │ Media Stream                    │
│                       ▼                                 │
│  ┌──────────────────────────────────────────────────┐   │
│  │           Input Protocol Adapter Layer           │   │
│  │  RTSP Client │ RTMP Server │ RTP/PS │ File │ HLS │   │
│  └────────────────────┬─────────────────────────────┘   │
│                       ▼                                 │
│  ┌──────────────────────────────────────────────────┐   │
│  │           Unified Media Processing Layer         │   │
│  │  H264/H265 NALU Parsing │ AAC Extract │ H265→264 │   │
│  └────────────────────┬─────────────────────────────┘   │
│                       ▼                                 │
│  ┌──────────────────────────────────────────────────┐   │
│  │              Output Muxer Layer                  │   │
│  │   WebSocket-FLV  │  HLS Slicing  │  WebRTC WHEP  │   │
│  └────────────────────┬─────────────────────────────┘   │
│                       ▼                                 │
│         Browser / MiniProgram / Android / iOS           │
└─────────────────────────────────────────────────────────┘
```

### Key Designs

1. **Unified Media Model**: All input protocols are demuxed into a unified `media.Frame`; output layer doesn't care about input source
2. **Stream Multiplexing**: Multiple browsers viewing the same source only pull 1 input — saves bandwidth
3. **Zero-Copy Forwarding**: H264 streams are not re-encoded — directly remuxed to FLV, very low CPU
4. **Embedded Static Assets**: Web player pages are embedded into the binary via `embed.FS` — true single-file deployment

---

## 💎 Community vs Enterprise Edition

| Category | Community (Free) | Enterprise (Paid) |
|------|--------------|--------------|
| Protocol Conversion | ✅ All | ✅ All |
| H264/H265 Processing | ✅ Software transcoding | ✅ + GPU hardware transcoding (NVENC/QSV) |
| Client SDK | ✅ All platforms | ✅ All platforms |
| REST API | ✅ Full | ✅ Full |
| Single-Node Concurrency | ✅ 200+ streams | ✅ Cluster HA (thousands) |
| Transport Encryption | TLS/HTTPS | + National Crypto SM4 / HLS DRM |
| Monitoring & Alerting | Prometheus metrics | + Grafana dashboard + alerts |
| Operation Audit | Basic logs | + Full audit reports |
| Commercial Support | Community issues | 7x24 SLA + dedicated engineer |

> Community Edition is fully functional, no feature crippling, can be used commercially standalone. Enterprise Edition only provides "scale, compliance, support" capabilities.

---

## 🤝 Contributing & Feedback

- File Issues:
  - GitHub: <https://github.com/suoten/streambridge/issues>
  - Gitee: <https://gitee.com/suoten/streambridge/issues>
- Submit Pull Requests: Bug fixes and new features welcome — please discuss in an Issue first

---

## 📄 License

[MIT License](LICENSE) - Community Edition is permanently free, commercial use allowed.

Copyright (c) 2026 StreamBridge Contributors
