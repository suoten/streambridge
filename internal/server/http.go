// Package server 实现 HTTP/WebSocket 服务
// 包含: REST API、WebSocket-FLV、HLS、WHEP、内嵌静态资源
package server

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/streambridge/streambridge/internal/config"
	"github.com/streambridge/streambridge/internal/media"
	"github.com/streambridge/streambridge/internal/session"
	"github.com/streambridge/streambridge/internal/webrtc"
)

//go:embed web/*
var webFS embed.FS

// Server HTTP/WebSocket 服务器
type Server struct {
	cfg        *config.Config
	mgr        *session.Manager
	auth       *session.Authenticator
	stats      *session.Stats
	whepSrv    *webrtc.Server
	httpSrv    *http.Server
	logger     *log.Logger
	version    string
	startTime  time.Time
}

// New 创建服务器
func New(cfg *config.Config, mgr *session.Manager, logger *log.Logger, version string) *Server {
	s := &Server{
		cfg:       cfg,
		mgr:       mgr,
		auth:      session.NewAuthenticator(&cfg.Security),
		stats:     session.NewStats(),
		whepSrv:   webrtc.NewServer(mgr, logger),
		logger:    logger,
		version:   version,
		startTime: time.Now(),
	}
	serverCORSOrigins = cfg.Security.AllowOrigins
	s.httpSrv = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.BindAddr, cfg.Server.HTTPPort),
		Handler:      s.buildHandler(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 0, // WebSocket/FLV 需要长连接,不设 WriteTimeout
		IdleTimeout:  120 * time.Second,
	}
	return s
}

// buildHandler 构建路由
func (s *Server) buildHandler() http.Handler {
	mux := http.NewServeMux()

	// 静态资源(添加 no-cache 头)
	staticFS, _ := fs.Sub(webFS, "web")
	fileServer := http.FileServer(http.FS(staticFS))
	wrappedFileServer := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		fileServer.ServeHTTP(w, r)
	})

	// REST API
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/ready", s.handleReady)
	mux.HandleFunc("/api/play", s.handlePlay)
	mux.HandleFunc("/api/stop", s.handleStop)
	mux.HandleFunc("/api/streams", s.handleStreams)
	mux.HandleFunc("/api/stats", s.handleStreamStats)
	mux.HandleFunc("/api/file/play", s.handleFilePlay)
	mux.HandleFunc("/api/rtp/listen", s.handleRTPListen)
	mux.HandleFunc("/api/demo", s.handleDemo)
	mux.HandleFunc("/api/version", s.handleVersion)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/api/logs", s.handleLogs)
	mux.HandleFunc("/api/js", s.handleJS)
	mux.HandleFunc("/api/docs", s.handleAPIDocs)

	// WebSocket-FLV
	mux.HandleFunc("/ws/", s.handleWebSocketFLV)
	mux.HandleFunc("/ws/flv/", s.handleWebSocketFLV)

	// HTTP-FLV 直出
	mux.HandleFunc("/live/", s.handleHTTPFLV)

	// HLS
	mux.HandleFunc("/hls/play", s.handleHLSPlay)
	mux.HandleFunc("/hls/", s.handleHLS)

	// WHEP (WebRTC)
	mux.HandleFunc("/whep/", s.handleWHEP)

	// 播放器页面
	mux.HandleFunc("/play", s.handlePlayPage)
	mux.HandleFunc("/doctor", s.handleDoctorPage)

	// 默认: 静态文件
	mux.Handle("/", wrappedFileServer)

	// CORS + 鉴权
	handler := session.CORSHandler(&s.cfg.Security, mux)
	handler = s.auth.AuthMiddleware(handler)
	return handler
}

// Start 启动 HTTP 服务
func (s *Server) Start(ctx context.Context) error {
	s.logger.Printf("HTTP 服务监听 %s", s.httpSrv.Addr)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpSrv.Shutdown(shutdownCtx)
	}()
	if err := s.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("HTTP 服务启动失败: %w", err)
	}
	return nil
}

// ===== REST API =====

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, map[string]interface{}{
		"status":            "ok",
		"version":           s.version,
		"uptime":            s.stats.Uptime(),
		"streams":           s.mgr.TotalStreams(),
		"viewers":           s.mgr.TotalViewers(),
		"cpu":               session.CPUUsage(),
		"memory_mb":         session.MemStatsMB(),
		"transcode_enabled": s.cfg.Performance.EnableH265Transcode,
	})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, map[string]interface{}{
		"ready": true,
		"components": map[string]bool{
			"http": true,
			"rtmp": true,
		},
	})
}

func (s *Server) handlePlay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return
	}
	url := r.URL.Query().Get("url")
	if url == "" {
		s.writeError(w, http.StatusBadRequest, "缺少 url 参数")
		return
	}
	if err := validateSourceURL(url); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "flv"
	}
	sess, _, err := s.mgr.Play(url)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "启动流失败: "+err.Error())
		return
	}
	wsScheme := "ws"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		wsScheme = "wss"
	}
	wsURL := fmt.Sprintf("%s://%s/ws/%s", wsScheme, r.Host, sess.ID)
	s.writeJSON(w, map[string]interface{}{
		"wsUrl":    wsURL,
		"streamId": sess.ID,
		"format":   format,
		"source":   url,
	})
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		s.writeError(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "缺少 id 参数")
		return
	}
	if err := s.mgr.Stop(id); err != nil {
		s.writeError(w, http.StatusNotFound, err.Error())
		return
	}
	s.writeJSON(w, map[string]interface{}{"status": "stopped", "id": id})
}

func (s *Server) handleStreams(w http.ResponseWriter, r *http.Request) {
	list := s.mgr.List()
	result := make([]map[string]interface{}, 0, len(list))
	for _, sess := range list {
		result = append(result, map[string]interface{}{
			"id":       sess.ID,
			"source":   sess.SourceURL,
			"type":     sess.SourceType(),
			"viewers":  sess.ViewerCount(),
			"uptime":   int64(sess.Uptime().Seconds()),
			"frames":   sess.FrameCount(),
			"bytesIn":  sess.BytesIn(),
			"videoCodec": codecName(sess.VideoTrack()),
			"audioCodec": codecName(sess.AudioTrack()),
		})
	}
	s.writeJSON(w, result)
}

func (s *Server) handleStreamStats(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	sess, ok := s.mgr.Get(id)
	if !ok {
		s.writeError(w, http.StatusNotFound, "流不存在")
		return
	}
	vt := sess.VideoTrack()
	stats := map[string]interface{}{
		"streamId":  sess.ID,
		"viewers":   sess.ViewerCount(),
		"uptime":    int64(sess.Uptime().Seconds()),
		"frames":    sess.FrameCount(),
		"bytesIn":   sess.BytesIn(),
		"codec":     codecName(vt),
	}
	if vt != nil {
		stats["resolution"] = fmt.Sprintf("%dx%d", vt.Width, vt.Height)
		stats["fps"] = vt.FPS
	}
	s.writeJSON(w, stats)
}

func (s *Server) handleFilePlay(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		s.writeError(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}
	var req struct {
		Path string `json:"path"`
		Loop bool   `json:"loop"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "请求体解析失败: "+err.Error())
		return
	}
	if req.Path == "" {
		s.writeError(w, http.StatusBadRequest, "缺少 path")
		return
	}
	if err := validateSourceURL(req.Path); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	url := req.Path
	if !strings.HasPrefix(url, "/") && !strings.HasPrefix(url, "file://") {
		url = "file://" + url
	}
	sess, _, err := s.mgr.Play(url)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	wsScheme := "ws"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		wsScheme = "wss"
	}
	s.writeJSON(w, map[string]interface{}{
		"streamId": sess.ID,
		"wsUrl":    fmt.Sprintf("%s://%s/ws/%s", wsScheme, r.Host, sess.ID),
	})
}

func (s *Server) handleRTPListen(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		s.writeError(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}
	var req struct {
		Port int    `json:"port"`
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Port == 0 {
		req.Port = s.cfg.Input.RTP.PortRange[0]
	}
	if req.Mode == "" {
		req.Mode = "ps"
	}
	url := fmt.Sprintf("rtp://%s:%d", s.cfg.Input.RTP.Listen, req.Port)
	sess, _, err := s.mgr.Play(url)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	wsScheme := "ws"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		wsScheme = "wss"
	}
	s.writeJSON(w, map[string]interface{}{
		"streamId": sess.ID,
		"wsUrl":    fmt.Sprintf("%s://%s/ws/%s", wsScheme, r.Host, sess.ID),
		"port":     req.Port,
		"mode":     req.Mode,
	})
}

func (s *Server) handleDemo(w http.ResponseWriter, r *http.Request) {
	// 内置示例流(实际生产可循环播放内置 H264 测试视频)
	s.writeJSON(w, map[string]interface{}{
		"wsUrl":    fmt.Sprintf("ws://%s/ws/demo", r.Host),
		"streamId": "demo",
		"format":   "flv",
	})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, map[string]interface{}{
		"version": s.version,
		"edition": "community",
		"license": "MIT",
	})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# HELP streambridge_streams_total 当前活跃流数\n")
	fmt.Fprintf(w, "# TYPE streambridge_streams_total gauge\n")
	fmt.Fprintf(w, "streambridge_streams_total %d\n", s.mgr.TotalStreams())
	fmt.Fprintf(w, "# HELP streambridge_viewers_total 当前观看人数\n")
	fmt.Fprintf(w, "# TYPE streambridge_viewers_total gauge\n")
	fmt.Fprintf(w, "streambridge_viewers_total %d\n", s.mgr.TotalViewers())
	fmt.Fprintf(w, "# HELP streambridge_bytes_in_total 输入字节总数\n")
	fmt.Fprintf(w, "# TYPE streambridge_bytes_in_total counter\n")
	fmt.Fprintf(w, "streambridge_bytes_in_total %d\n", s.mgr.BytesIn())
	fmt.Fprintf(w, "# HELP streambridge_bytes_out_total 输出字节总数\n")
	fmt.Fprintf(w, "# TYPE streambridge_bytes_out_total counter\n")
	fmt.Fprintf(w, "streambridge_bytes_out_total %d\n", s.mgr.BytesOut())
}

// ===== WebSocket-FLV =====

// serverCORSOrigins 全局 CORS 白名单(由 Server 初始化)
var serverCORSOrigins = []string{"*"}

var upgrader = websocket.Upgrader{
	CheckOrigin:     checkOrigin,
	WriteBufferSize: 65536,     // 64KB 写缓冲区(默认 4KB 太小,影响 FLV 流吞吐)
	ReadBufferSize:  4096,
	EnableCompression: true,    // 启用 permessage-deflate 压缩(减少带宽)
}

// checkOrigin 校验 WebSocket 来源(防止 CSRF)
func checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // 非浏览器客户端(如 curl)
	}
	for _, o := range serverCORSOrigins {
		if o == "*" || o == origin {
			return true
		}
	}
	return false
}

func (s *Server) handleWebSocketFLV(w http.ResponseWriter, r *http.Request) {
	streamID := strings.TrimPrefix(r.URL.Path, "/ws/")
	streamID = strings.TrimPrefix(streamID, "flv/")
	if streamID == "" {
		s.writeError(w, http.StatusBadRequest, "缺少 stream ID")
		return
	}

	// demo 流:返回内置测试画面
	if streamID == "demo" {
		s.serveDemoFLV(w, r)
		return
	}

	sess, ok := s.mgr.Get(streamID)
	if !ok {
		// 可能是直接通过 WS 拉流(带 url 参数)
		if url := r.URL.Query().Get("url"); url != "" {
			newSess, _, err := s.mgr.Play(url)
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			sess = newSess
		} else {
			s.writeError(w, http.StatusNotFound, "流不存在")
			return
		}
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Printf("WebSocket 升级失败: %v", err)
		return
	}
	defer conn.Close()

	viewer := &session.Viewer{
		ID:       fmt.Sprintf("v_%d", time.Now().UnixNano()),
		IP:       r.RemoteAddr,
		UserAgent: r.UserAgent(),
		Format:   "flv",
		JoinedAt: time.Now(),
	}
	frameCh, unsub := sess.Subscribe(viewer)
	defer unsub()

	s.logger.Printf("观看者 %s 加入流 %s", viewer.ID, sess.ID)

	// 发送 FLV 流
	flvMuxer := newFLVWriter(conn, sess.VideoTrack(), sess.AudioTrack())
	if err := flvMuxer.WriteHeader(); err != nil {
		s.logger.Printf("写 FLV 头失败: %v", err)
		return
	}
	if err := flvMuxer.Flush(); err != nil {
		s.logger.Printf("发送 FLV 头失败: %v", err)
		return
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case f, ok := <-frameCh:
			if !ok {
				return
			}
			if err := flvMuxer.WriteFrame(f); err != nil {
				s.logger.Printf("写帧失败: %v", err)
				return
			}
			// 非阻塞排空:收集 channel 中已就绪的帧,合并为一个 WebSocket 消息发送
			// 这减少了 WebSocket 消息数量和网络帧开销,同时不增加延迟
			batchCount := 0
		drainLoop:
			for batchCount < 16 {
				select {
				case f2, ok := <-frameCh:
					if !ok {
						flvMuxer.Flush()
						return
					}
					if err := flvMuxer.WriteFrame(f2); err != nil {
						s.logger.Printf("写帧失败: %v", err)
						return
					}
					batchCount++
				default:
					// 没有更多帧,发送缓冲区中的数据
					if err := flvMuxer.Flush(); err != nil {
						s.logger.Printf("WebSocket 写入失败: %v", err)
						return
					}
					break drainLoop
				}
			}
			if batchCount >= 16 {
				// 批量上限,刷新并继续
				if err := flvMuxer.Flush(); err != nil {
					s.logger.Printf("WebSocket 写入失败: %v", err)
					return
				}
			}
		}
	}
}

// ===== HTTP-FLV 直出 =====

func (s *Server) handleHTTPFLV(w http.ResponseWriter, r *http.Request) {
	streamID := strings.TrimPrefix(r.URL.Path, "/live/")
	sess, ok := s.mgr.Get(streamID)
	if !ok {
		if url := r.URL.Query().Get("url"); url != "" {
			newSess, _, err := s.mgr.Play(url)
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			sess = newSess
		} else {
			s.writeError(w, http.StatusNotFound, "流不存在")
			return
		}
	}

	w.Header().Set("Content-Type", "video/x-flv")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	viewer := &session.Viewer{
		ID:       fmt.Sprintf("v_%d", time.Now().UnixNano()),
		IP:       r.RemoteAddr,
		Format:   "flv",
		JoinedAt: time.Now(),
	}
	frameCh, unsub := sess.Subscribe(viewer)
	defer unsub()

	flusher, _ := w.(http.Flusher)
	flvMuxer := newFLVHTTPWriter(w, sess.VideoTrack(), sess.AudioTrack())
	if err := flvMuxer.WriteHeader(); err != nil {
		return
	}
	if flusher != nil {
		flusher.Flush()
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case f, ok := <-frameCh:
			if !ok {
				return
			}
			if err := flvMuxer.WriteFrame(f); err != nil {
				return
			}
			// 非阻塞排空:批量收集已就绪的帧,减少 flush 系统调用次数
		drainLoop:
			for batchCount := 0; batchCount < 16; {
				select {
				case f2, ok := <-frameCh:
					if !ok {
						if flusher != nil {
							flusher.Flush()
						}
						return
					}
					if err := flvMuxer.WriteFrame(f2); err != nil {
						return
					}
					batchCount++
				default:
					break drainLoop
				}
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

// ===== HLS =====

func (s *Server) handleHLS(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	// /hls/{streamId}.m3u8
	// /hls/{streamId}/{segment}.ts
	if strings.HasSuffix(path, ".m3u8") {
		s.handleHLSPlaylist(w, r)
		return
	}
	if strings.HasSuffix(path, ".ts") {
		s.handleHLSSegment(w, r)
		return
	}
	s.writeError(w, http.StatusNotFound, "未知的 HLS 路径")
}

func (s *Server) handleHLSPlay(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Query().Get("url")
	if url == "" {
		s.writeError(w, http.StatusBadRequest, "缺少 url 参数")
		return
	}
	sess, _, err := s.mgr.Play(url)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 触发 HLS 切片器创建
	sess.HLSSlicer()
	// 根据请求协议选择 http/https(避免混合内容错误)
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	s.writeJSON(w, map[string]interface{}{
		"streamId": sess.ID,
		"hlsUrl":   fmt.Sprintf("%s://%s/hls/%s.m3u8", scheme, r.Host, sess.ID),
	})
}

func (s *Server) handleHLSPlaylist(w http.ResponseWriter, r *http.Request) {
	streamID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/hls/"), ".m3u8")
	sess, ok := s.mgr.Get(streamID)
	if !ok {
		s.writeError(w, http.StatusNotFound, "流不存在")
		return
	}
	slicer := sess.HLSSlicer()
	// 确保 stream ID 已设置(防止切片 URL 缺少前缀导致 404)
	slicer.SetStreamID(streamID)

	// 如果还没有切片完成,返回一个空的 live playlist 让 hls.js 持续轮询
	// 不返回 503,因为 hls.js 会将 503 视为 manifestLoadError(致命错误)
	if !slicer.HasSegments() {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-cache")
		// 返回一个有效的空 live playlist,hls.js 会按 target duration 轮询
		fmt.Fprint(w, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:0\n")
		return
	}

	playlist := slicer.Playlist()
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(playlist)
}

func (s *Server) handleHLSSegment(w http.ResponseWriter, r *http.Request) {
	// /hls/{streamId}/{name}.ts
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/hls/"), "/")
	if len(parts) < 2 {
		s.writeError(w, http.StatusBadRequest, "路径非法")
		return
	}
	streamID := parts[0]
	segName := parts[1]
	sess, ok := s.mgr.Get(streamID)
	if !ok {
		s.writeError(w, http.StatusNotFound, "流不存在")
		return
	}
	slicer := sess.HLSSlicer()
	data, ok := slicer.GetSegment(segName)
	if !ok {
		s.writeError(w, http.StatusNotFound, "切片不存在")
		return
	}
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

// ===== WHEP =====

func (s *Server) handleWHEP(w http.ResponseWriter, r *http.Request) {
	s.whepSrv.HandleWHEP(w, r)
}

// ===== 播放器页面 =====

func (s *Server) handlePlayPage(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Query().Get("url")
	demo := r.URL.Query().Get("demo")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if demo == "1" {
		fmt.Fprintf(w, playerPageTemplate, "", "demo")
	} else {
		fmt.Fprintf(w, playerPageTemplate, url, "")
	}
}

func (s *Server) handleDoctorPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(doctorPageTemplate))
}

// ===== 日志 =====

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)
	// 简化:推送心跳
	for {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(2 * time.Second):
			fmt.Fprintf(w, "data: {\"ts\":%d,\"level\":\"info\",\"msg\":\"heartbeat\"}\n\n", time.Now().Unix())
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

// ===== 工具方法 =====

func (s *Server) writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) writeError(w http.ResponseWriter, code int, msg string) {
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error":  true,
		"code":   code,
		"reason": msg,
	})
}

func codecName(t *media.Track) string {
	if t == nil {
		return ""
	}
	return t.Codec.String()
}

// handleJS 提供 streambridge.js(绕过缓存)
func (s *Server) handleJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	data, err := webFS.ReadFile("web/streambridge.js")
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, _ = w.Write(data)
}

// serveDemoFLV 演示流:推送 H264 测试画面(循环变色测试图案)
func (s *Server) serveDemoFLV(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	demoTrack := newDemoTrack()
	flvMuxer := newFLVWriter(conn, demoTrack, nil)
	if err := flvMuxer.WriteHeader(); err != nil {
		return
	}
	serveDemoStream(flvMuxer, r.Context())
}

// validateSourceURL 校验源 URL 安全性
func validateSourceURL(url string) error {
	if len(url) > 2048 {
		return fmt.Errorf("URL 过长")
	}
	allowed := []string{"rtsp://", "rtsps://", "rtmp://", "rtp://", "file://", "http://", "https://", "/"}
	for _, p := range allowed {
		if strings.HasPrefix(url, p) {
			return nil
		}
	}
	return fmt.Errorf("不支持的 URL 协议: %s", url)
}

// handleAPIDocs API 文档
func (s *Server) handleAPIDocs(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, map[string]interface{}{
		"name":    "StreamBridge API",
		"version": s.version,
		"endpoints": []map[string]string{
			{"method": "GET", "path": "/api/health", "desc": "健康检查"},
			{"method": "GET", "path": "/api/ready", "desc": "就绪检查"},
			{"method": "GET", "path": "/api/play?url=<url>&format=flv", "desc": "启动播放,返回 wsUrl"},
			{"method": "POST", "path": "/api/stop?id=<streamId>", "desc": "停止流"},
			{"method": "GET", "path": "/api/streams", "desc": "流列表"},
			{"method": "GET", "path": "/api/stats?id=<streamId>", "desc": "单流统计"},
			{"method": "POST", "path": "/api/file/play", "desc": "文件回放,body: {path,loop}"},
			{"method": "POST", "path": "/api/rtp/listen", "desc": "RTP 监听,body: {port,mode}"},
			{"method": "GET", "path": "/api/demo", "desc": "示例流地址"},
			{"method": "GET", "path": "/api/version", "desc": "版本信息"},
			{"method": "GET", "path": "/metrics", "desc": "Prometheus 指标"},
			{"method": "GET", "path": "/hls/play?url=<url>", "desc": "HLS 播放,返回 hlsUrl"},
			{"method": "GET", "path": "/hls/<streamId>.m3u8", "desc": "HLS 播放列表"},
			{"method": "GET", "path": "/ws/<streamId>", "desc": "WebSocket-FLV 流"},
			{"method": "GET", "path": "/live/<streamId>", "desc": "HTTP-FLV 流"},
			{"method": "POST", "path": "/whep/<streamId>", "desc": "WebRTC WHEP 信令"},
		},
	})
}
