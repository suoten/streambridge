// Package webrtc 实现 WebRTC WHEP 输出
// 基于 pion/webrtc,提供低延迟(<1s)浏览器播放
package webrtc

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v3"
	pionmedia "github.com/pion/webrtc/v3/pkg/media"

	"github.com/streambridge/streambridge/internal/media"
	"github.com/streambridge/streambridge/internal/session"
)

// Server WHEP 服务
type Server struct {
	mgr    *session.Manager
	logger *log.Logger
	mu     sync.Mutex
	peers  map[string]*PeerConnection
}

// PeerConnection 单个 WebRTC 对等连接
type PeerConnection struct {
	pcInstance *webrtc.PeerConnection
	cancel     context.CancelFunc
}

// NewServer 创建 WHEP 服务
func NewServer(mgr *session.Manager, logger *log.Logger) *Server {
	return &Server{
		mgr:    mgr,
		logger: logger,
		peers:  make(map[string]*PeerConnection),
	}
}

// HandleWHEP 处理 WHEP 请求(POST SDP offer,返回 SDP answer)
func (s *Server) HandleWHEP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "仅支持 POST", http.StatusMethodNotAllowed)
		return
	}
	streamID := strings.TrimPrefix(r.URL.Path, "/whep/")
	if streamID == "" {
		if url := r.URL.Query().Get("url"); url != "" {
			sess, _, err := s.mgr.Play(url)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			streamID = sess.ID
		} else {
			http.Error(w, "缺少 stream ID", http.StatusBadRequest)
			return
		}
	}

	sess, ok := s.mgr.Get(streamID)
	if !ok {
		http.Error(w, "流不存在", http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "读取 SDP 失败: "+err.Error(), http.StatusBadRequest)
		return
	}

	answer, err := s.createPeerConnection(sess, string(body))
	if err != nil {
		s.logger.Printf("[WHEP] 创建 PeerConnection 失败: %v", err)
		http.Error(w, "创建 PeerConnection 失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/sdp")
	_, _ = w.Write([]byte(answer))
}

// createPeerConnection 创建 WebRTC 连接并协商 SDP
func (s *Server) createPeerConnection(sess *session.Session, offerSDP string) (string, error) {
	// 检查视频轨道
	videoTrack := sess.VideoTrack()
	if videoTrack == nil {
		return "", fmt.Errorf("流没有视频轨道,无法创建 WebRTC 连接")
	}

	// H265 不支持 WebRTC(浏览器不支持 H265 WebRTC 解码)
	if videoTrack.Codec == media.CodecH265 {
		return "", fmt.Errorf("H265 编码不支持 WebRTC,请使用 HLS 模式或在服务端配置 enable_h265_transcode: true")
	}

	// 创建带显式编解码器注册的 MediaEngine
	// 不使用默认 MediaEngine,因为默认的编解码器参数可能和浏览器 offer 不匹配
	m := &webrtc.MediaEngine{}
	// 注册 H264(带 SDP 参数,匹配浏览器常见 profile-level-id=42e01f)
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeH264,
			ClockRate:   90000,
			SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
		},
		PayloadType: 102,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		return "", fmt.Errorf("注册 H264 编解码器失败: %w", err)
	}
	// 注册 Opus 音频
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeOpus,
			ClockRate: 48000,
			Channels:  2,
		},
		PayloadType: 111,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		return "", fmt.Errorf("注册 Opus 编解码器失败: %w", err)
	}

	api := webrtc.NewAPI(webrtc.WithMediaEngine(m))
	pc, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	})
	if err != nil {
		return "", fmt.Errorf("创建 PC 失败: %w", err)
	}

	// 使用 TrackLocalStaticSample,让 pion 自动处理 H264 RTP 打包
	// (FU-A 分片、序列号管理、Marker 位等),避免手动打包的 bug
	localVideoTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeH264,
			ClockRate: 90000,
		},
		"video", "streambridge",
	)
	if err != nil {
		pc.Close()
		return "", fmt.Errorf("创建视频轨道失败: %w", err)
	}
	if _, err := pc.AddTrack(localVideoTrack); err != nil {
		pc.Close()
		return "", fmt.Errorf("添加视频轨道失败: %w", err)
	}

	// 创建音频轨道(仅当有音频时)
	// 注意:当前不支持 AAC→Opus 转码,音频轨道仅用于 SDP 协商
	var localAudioTrack *webrtc.TrackLocalStaticSample
	if sess.AudioTrack() != nil {
		localAudioTrack, err = webrtc.NewTrackLocalStaticSample(
			webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2},
			"audio", "streambridge",
		)
		if err == nil {
			_, _ = pc.AddTrack(localAudioTrack)
		}
	}

	// 设置远程 SDP
	offer := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offerSDP}
	if err := pc.SetRemoteDescription(offer); err != nil {
		pc.Close()
		return "", fmt.Errorf("设置 Offer 失败: %w", err)
	}

	// 创建 Answer
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		pc.Close()
		return "", fmt.Errorf("创建 Answer 失败: %w", err)
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		pc.Close()
		return "", fmt.Errorf("设置 Answer 失败: %w", err)
	}

	// 等待 ICE 收集完成(带 5 秒超时,防止永久阻塞)
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	select {
	case <-gatherComplete:
	case <-time.After(5 * time.Second):
		// 超时也不一定是错误,可能已经有足够的候选
		s.logger.Printf("[WHEP] ICE 收集超时(5s),继续返回 Answer")
	}

	// 订阅流并转发
	ctx, cancel := context.WithCancel(context.Background())
	peerID := fmt.Sprintf("webrtc_%d", time.Now().UnixNano())
	viewer := &session.Viewer{
		ID:       peerID,
		Format:   "webrtc",
		JoinedAt: time.Now(),
	}
	frameCh, unsub := sess.Subscribe(viewer)

	// 注册 PeerConnection 以便后续清理
	s.mu.Lock()
	s.peers[peerID] = &PeerConnection{pcInstance: pc, cancel: cancel}
	s.mu.Unlock()

	s.logger.Printf("[WHEP] PeerConnection 已创建, peerID=%s, streamId=%s", peerID, sess.ID)

	// 连接状态变化时清理资源
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		s.logger.Printf("[WHEP] 连接状态变化: %s, peerID=%s", state, peerID)
		if state == webrtc.PeerConnectionStateClosed || state == webrtc.PeerConnectionStateFailed {
			unsub()
			cancel()
			pc.Close()
			s.mu.Lock()
			delete(s.peers, peerID)
			s.mu.Unlock()
		}
	})

	// 帧转发器
	sampleForwarder := NewSampleForwarder(localVideoTrack, localAudioTrack, s.logger)

	go func() {
		defer unsub()
		defer cancel()
		var lastPTS time.Duration
		firstFrame := true
		for {
			select {
			case <-ctx.Done():
				pc.Close()
				return
			case f, ok := <-frameCh:
				if !ok {
					pc.Close()
					return
				}
				if firstFrame {
					lastPTS = f.PTS
					firstFrame = false
				}
				// 计算帧间间隔作为 sample duration
				dur := f.PTS - lastPTS
				if dur <= 0 {
					dur = time.Millisecond * 33 // 默认 30fps
				}
				lastPTS = f.PTS
				sampleForwarder.Forward(f, dur)
			}
		}
	}()

	return pc.LocalDescription().SDP, nil
}

// SampleForwarder 将 media.Frame 转为 pion Sample 并写入 TrackLocalStaticSample
type SampleForwarder struct {
	videoTrack *webrtc.TrackLocalStaticSample
	audioTrack *webrtc.TrackLocalStaticSample
	logger     *log.Logger
	frameCount uint64
}

// NewSampleForwarder 创建 Sample 转发器
func NewSampleForwarder(video, audio *webrtc.TrackLocalStaticSample, logger *log.Logger) *SampleForwarder {
	return &SampleForwarder{
		videoTrack: video,
		audioTrack: audio,
		logger:     logger,
	}
}

// Forward 转发一帧
func (sf *SampleForwarder) Forward(f *media.Frame, dur time.Duration) {
	if f.IsVideo() && sf.videoTrack != nil {
		sf.forwardVideo(f, dur)
	}
	// 音频转发暂不支持(AAC→Opus 需要转码,社区版略过)
}

// forwardVideo 将 H264 帧写入 TrackLocalStaticSample
// pion 的 H264 payloader 会自动处理:
// - AnnexB NALU 拆分
// - 单包/FU-A 模式选择
// - 序列号管理
// - Marker 位设置
func (sf *SampleForwarder) forwardVideo(f *media.Frame, dur time.Duration) {
	if sf.videoTrack == nil {
		return
	}

	// 帧数据已经是 AnnexB 格式(含 00 00 00 01 起始码)
	// pion 的 H264 payloader 接受 AnnexB 格式并自动拆分 NALU
	sample := pionmedia.Sample{
		Data:     f.Payload,
		Duration: dur,
	}

	if err := sf.videoTrack.WriteSample(sample); err != nil {
		// 写入错误通常是连接已关闭,忽略即可
		return
	}

	sf.frameCount++
	if sf.frameCount%100 == 1 {
		if sf.logger != nil {
			sf.logger.Printf("[WHEP] 视频帧已转发: count=%d, keyFrame=%v, size=%d, dur=%v",
				sf.frameCount, f.IsKeyFrame, len(f.Payload), dur)
		}
	}
}
