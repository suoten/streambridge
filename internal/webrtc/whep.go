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

	// 创建视频轨道(H264)
	codec := webrtc.RTPCodecCapability{
		MimeType:  webrtc.MimeTypeH264,
		ClockRate: 90000,
	}
	localVideoTrack, err := webrtc.NewTrackLocalStaticRTP(codec, "video", "streambridge")
	if err != nil {
		pc.Close()
		return "", fmt.Errorf("创建视频轨道失败: %w", err)
	}
	if _, err := pc.AddTrack(localVideoTrack); err != nil {
		pc.Close()
		return "", fmt.Errorf("添加视频轨道失败: %w", err)
	}

	// 创建音频轨道(仅当有音频时)
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

	// 等待 ICE 收集完成(带 5 秒超时,防止永久阻塞)
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	select {
	case <-gatherComplete:
	case <-time.After(5 * time.Second):
		// 超时也不一定是错误,可能已经有足够的候选
		s.logger.Printf("[WHEP] ICE 收集超时(5s),继续返回 Answer")
	}

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

	// RTP 转发器
	rtpForwarder := NewRTPForwarder(localVideoTrack, localAudioTrack)

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
				rtpForwarder.Forward(f)
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

// RTPForwarder 将 media.Frame 转为 RTP 包并发送到 WebRTC 轨道
type RTPForwarder struct {
	videoTrack *webrtc.TrackLocalStaticRTP
	audioTrack *webrtc.TrackLocalStaticRTP
	videoSeq   uint16
	audioSeq   uint16
	videoTS    uint32 // 视频时间戳基准(90kHz)
	audioTS    uint32 // 音频时间戳基准(48kHz)
	mtu        int
}

// NewRTPForwarder 创建 RTP 转发器
func NewRTPForwarder(video, audio *webrtc.TrackLocalStaticRTP) *RTPForwarder {
	return &RTPForwarder{
		videoTrack: video,
		audioTrack: audio,
		mtu:        1200, // WebRTC MTU (留余量给 IP/UDP/RTP 头)
	}
}

// Forward 转发一帧
func (rf *RTPForwarder) Forward(f *media.Frame) {
	if f.IsVideo() && rf.videoTrack != nil {
		rf.forwardVideo(f)
	}
	// 音频转发暂不支持(AAC→Opus 需要转码,社区版略过)
}

// forwardVideo 将 H264 帧转为 RTP 包
func (rf *RTPForwarder) forwardVideo(f *media.Frame) {
	// 计算 RTP 时间戳(90kHz 时钟)
	ts := uint32(f.DTS.Microseconds() * 90 / 1000)

	// 拆分 NALU(AnnexB 格式)
	nalus := media.H264SplitNALUs(f.Payload)
	if len(nalus) == 0 {
		return
	}

	// 只在最后一个 NALU 上设置 Marker 位(表示一帧结束)
	for i, nalu := range nalus {
		rf.videoSeq++
		isLast := i == len(nalus)-1
		rf.sendNALU(nalu, ts, rf.videoSeq, isLast)
	}
}

// sendNALU 发送一个 NALU,根据大小选择打包模式
func (rf *RTPForwarder) sendNALU(nalu []byte, ts uint32, seq uint16, isLast bool) {
	if len(nalu) == 0 {
		return
	}

	// 小 NALU:单包模式
	if len(nalu) <= rf.mtu {
		pkt := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    96,
				SequenceNumber: seq,
				Timestamp:      ts,
				Marker:         isLast, // 只在帧的最后一个 NALU 上设置 Marker
			},
			Payload: nalu,
		}
		if err := rf.videoTrack.WriteRTP(pkt); err != nil {
			// 忽略写入错误(连接可能已关闭)
		}
		return
	}

	// 大 NALU:FU-A 分片
	rf.sendFUAFragment(nalu, ts, seq, isLast)
}

// sendFUAFragment 将大 NALU 用 FU-A 分片发送
// FU-A 规范(RFC 6184):
//   FU indicator: F(1) + NRI(2) + Type(5) = 28(FU-A)
//   FU header:    S(1) + E(1) + R(1) + Type(5,原始 NALU 类型)
func (rf *RTPForwarder) sendFUAFragment(nalu []byte, ts uint32, baseSeq uint16, isLast bool) {
	naluType := nalu[0] & 0x1F    // 原始 NALU 类型
	naluNRI := nalu[0] & 0x60     // NRI 位

	// FU indicator: F=0 + NRI(来自原始 NALU) + Type=28(0x1C)
	fuIndicator := naluNRI | 0x1C

	offset := 1 // 跳过 NALU header 字节
	fragSeq := baseSeq

	for offset < len(nalu) {
		// 每个 FU-A 分片: 2 字节(indicator+header) + 数据
		fragLen := rf.mtu - 2
		if offset+fragLen > len(nalu) {
			fragLen = len(nalu) - offset
		}

		// FU header: S(1bit) + E(1bit) + R(1bit,0) + Type(5bit)
		fuHeader := byte(naluType & 0x1F)
		isStart := offset == 1
		isEnd := offset+fragLen >= len(nalu)

		if isStart {
			fuHeader |= 0x80 // S=1
		}
		if isEnd {
			fuHeader |= 0x40 // E=1
		}

		payload := make([]byte, 2+fragLen)
		payload[0] = fuIndicator  // FU indicator(含 NRI)
		payload[1] = fuHeader      // FU header(S/E/Type)
		copy(payload[2:], nalu[offset:offset+fragLen])

			pkt := &rtp.Packet{
				Header: rtp.Header{
					Version:        2,
					PayloadType:    96,
					SequenceNumber: fragSeq,
					Timestamp:      ts,
					Marker:         isEnd && isLast, // 只在帧的最后一个分片设置 Marker
				},
				Payload: payload,
			}
		rf.videoTrack.WriteRTP(pkt)

		fragSeq++
		offset += fragLen
	}
}
