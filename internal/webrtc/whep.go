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

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"

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
		http.Error(w, "创建 PeerConnection 失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/sdp")
	w.Write([]byte(answer))
}

// createPeerConnection 创建 WebRTC 连接并协商 SDP
func (s *Server) createPeerConnection(sess *session.Session, offerSDP string) (string, error) {
	// 创建 PeerConnection
	setting := webrtc.SettingEngine{}
	setting.SetLite(true) // 轻量模式(适用于单机部署)
	api := webrtc.NewAPI(webrtc.WithSettingEngine(setting))
	pc, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{},
	})
	if err != nil {
		return "", fmt.Errorf("创建 PC 失败: %w", err)
	}

	// 创建视频轨道(H264)
	videoTrack := sess.VideoTrack()
	var localVideoTrack *webrtc.TrackLocalStaticRTP
	if videoTrack != nil {
		codec := webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeH264,
			ClockRate: 90000,
		}
		if videoTrack.Codec == media.CodecH265 {
			codec.MimeType = webrtc.MimeTypeH265 // 注意:浏览器对 H265 WebRTC 支持有限
		}
		localVideoTrack, err = webrtc.NewTrackLocalStaticRTP(codec, "video", "streambridge")
		if err != nil {
			pc.Close()
			return "", fmt.Errorf("创建视频轨道失败: %w", err)
		}
		if _, err := pc.AddTrack(localVideoTrack); err != nil {
			pc.Close()
			return "", fmt.Errorf("添加视频轨道失败: %w", err)
		}
	}

	// 创建音频轨道(AAC -> Opus 转码需要,这里简化为透传)
	var localAudioTrack *webrtc.TrackLocalStaticRTP
	if sess.AudioTrack() != nil {
		localAudioTrack, err = webrtc.NewTrackLocalStaticRTP(
			webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2},
			"audio", "streambridge",
		)
		if err == nil {
			pc.AddTrack(localAudioTrack)
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

	// 等待 ICE 收集完成
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	<-gatherComplete

	// 订阅流并转发 RTP
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

	// 连接状态变化时清理资源
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateClosed || state == webrtc.PeerConnectionStateFailed {
			unsub()
			cancel()
			pc.Close()
			s.mu.Lock()
			delete(s.peers, peerID)
			s.mu.Unlock()
		}
	})

	go func() {
		defer unsub()
		defer cancel()
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
				s.forwardFrame(localVideoTrack, localAudioTrack, f)
			}
		}
	}()

	return pc.LocalDescription().SDP, nil
}

// forwardFrame 转发帧为 RTP 包
func (s *Server) forwardFrame(videoTrack, audioTrack *webrtc.TrackLocalStaticRTP, f *media.Frame) {
	if f.IsVideo() && videoTrack != nil {
		// 简化:把 NALU 封装为 RTP 包
		pkt := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    96,
				SequenceNumber: uint16(f.DTS.Milliseconds() / 40),
				Timestamp:      uint32(f.DTS.Microseconds() * 90),
			},
			Payload: f.Payload,
		}
		videoTrack.WriteRTP(pkt)
	}
}
