// Package session 实现流会话管理:流复用、观看者追踪、统计
// 核心职责: 同一个输入源被多个浏览器观看时,只拉 1 路输入,输出分发给所有观看者
package session

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/streambridge/streambridge/internal/config"
	"github.com/streambridge/streambridge/internal/input"
	"github.com/streambridge/streambridge/internal/media"
	"github.com/streambridge/streambridge/internal/media/hls"
)

// 订阅者帧通道缓冲大小
// 必须足够大以容纳突发帧(视频 25-30fps + 音频 40-50fps,约 1-2 秒数据)
const subscriberBuffer = 512

// Manager 会话管理器(全局单例)
type Manager struct {
	mu           sync.RWMutex
	sessions     map[string]*Session
	cfg          *config.Config
	totalViewers atomic.Int64
	totalBytesIn  atomic.Int64
	totalBytesOut atomic.Int64
}

// Session 单个流的会话
type Session struct {
	ID         string
	Source     input.Source
	SourceURL  string
	mgr        *Manager // 反向引用,用于更新全局计数器
	videoTrack *media.Track
	audioTrack *media.Track
	viewers    sync.Map // map[string]*Viewer
	viewerCount atomic.Int64
	createdAt  time.Time
	cancel     context.CancelFunc

	// 帧分发
	subscribers map[chan *media.Frame]struct{}
	subMu       sync.RWMutex

	// 关键帧缓存(新订阅者加入时立即发送,避免等待下一个 I 帧)
	keyFrame    *media.Frame
	keyFrameMu  sync.RWMutex

	// 编解码参数缓存(新订阅者加入时立即发送 AVC/AAC SeqHeader)
	codecFrames []*media.Frame // 最近的关键参数帧(SPS/PPS/AAC config)
	codecMu     sync.RWMutex

	// HLS 切片器(可选,按需创建)
	hlsSlicer *hls.Slicer
	hlsMu     sync.RWMutex

	// 统计
	bytesIn    atomic.Int64
	bytesOut   atomic.Int64
	frameCount atomic.Int64
	lastFrameAt atomic.Int64
}

// Viewer 观看者
type Viewer struct {
	ID        string
	IP        string
	UserAgent string
	Format    string // flv | hls | webrtc
	JoinedAt  time.Time
}

// NewManager 创建会话管理器
func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		cfg:      cfg,
	}
}

// Play 启动或复用一个流
// 返回 (session, isNew, wsURL)
func (m *Manager) Play(sourceURL string) (*Session, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 查找已有会话(同 URL 复用)
	for _, s := range m.sessions {
		if s.SourceURL == sourceURL {
			return s, false, nil
		}
	}

	// 检查上限
	if len(m.sessions) >= m.cfg.Performance.MaxStreams {
		return nil, false, fmt.Errorf("已达最大流数上限: %d", m.cfg.Performance.MaxStreams)
	}

	// 创建新会话
	ctx, cancel := context.WithCancel(context.Background())
	src := input.ParseURL(sourceURL)
	frameCh, err := src.Start(ctx, sourceURL)
	if err != nil {
		cancel()
		return nil, false, fmt.Errorf("启动输入源失败: %w", err)
	}

	sess := &Session{
		ID:          generateStreamID(),
		Source:      src,
		SourceURL:   sourceURL,
		mgr:         m,
		createdAt:   time.Now(),
		cancel:      cancel,
		subscribers: make(map[chan *media.Frame]struct{}),
	}
	video, audio := src.Tracks()
	sess.videoTrack = video
	sess.audioTrack = audio

	m.sessions[sess.ID] = sess

	// 启动帧分发
	go sess.dispatch(ctx, frameCh)

	return sess, true, nil
}

// Stop 停止一个流
func (m *Manager) Stop(streamID string) error {
	m.mu.Lock()
	sess, ok := m.sessions[streamID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("流 %s 不存在", streamID)
	}
	delete(m.sessions, streamID)
	m.mu.Unlock()

	sess.cancel()
	sess.Source.Stop()

	// 关闭所有订阅者通道,使 WebSocket/HTTP-FLV 消费者退出阻塞
	sess.subMu.Lock()
	for ch := range sess.subscribers {
		close(ch)
		delete(sess.subscribers, ch)
	}
	sess.subMu.Unlock()

	return nil
}

// Get 获取会话
func (m *Manager) Get(streamID string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[streamID]
	return s, ok
}

// List 列出所有会话
func (m *Manager) List() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s)
	}
	return list
}

// GetByURL 按 URL 查找
func (m *Manager) GetByURL(url string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.sessions {
		if s.SourceURL == url {
			return s, true
		}
	}
	return nil, false
}

// TotalViewers 总观看人数
func (m *Manager) TotalViewers() int64 { return m.totalViewers.Load() }

// TotalStreams 总流数
func (m *Manager) TotalStreams() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// AddBytesIn 累加输入字节
func (m *Manager) AddBytesIn(n int64) { m.totalBytesIn.Add(n) }

// AddBytesOut 累加输出字节
func (m *Manager) AddBytesOut(n int64) { m.totalBytesOut.Add(n) }

// BytesIn 输入字节总数
func (m *Manager) BytesIn() int64 { return m.totalBytesIn.Load() }

// BytesOut 输出字节总数
func (m *Manager) BytesOut() int64 { return m.totalBytesOut.Load() }

// ===== Session 方法 =====

// Subscribe 订阅帧流(观看者加入)
// 关键帧优先策略:新订阅者立即收到缓存的 codec 参数帧 + 最近关键帧
func (s *Session) Subscribe(viewer *Viewer) (<-chan *media.Frame, func()) {
	ch := make(chan *media.Frame, subscriberBuffer)
	s.subMu.Lock()
	s.subscribers[ch] = struct{}{}
	s.subMu.Unlock()
	s.viewers.Store(viewer.ID, viewer)
	s.viewerCount.Add(1)
	if s.mgr != nil {
		s.mgr.totalViewers.Add(1)
	}

	// 关键帧优先:立即发送缓存的 codec 参数帧 + 关键帧
	// 这样新观看者无需等待下一个 I 帧即可开始解码
	go func() {
		// 先发送 codec 参数帧(SPS/PPS/AVCSeqHeader 或 AACSeqHeader)
		s.codecMu.RLock()
		codecFrames := make([]*media.Frame, len(s.codecFrames))
		copy(codecFrames, s.codecFrames)
		s.codecMu.RUnlock()
		for _, f := range codecFrames {
			if f == nil || f.Payload == nil {
				continue
			}
			// 复制帧,避免共享底层数组被修改
			cf := cloneFrame(f)
			select {
			case ch <- cf:
			default:
				// 通道满,跳过(不阻塞)
			}
		}

		// 再发送最近的关键帧
		s.keyFrameMu.RLock()
		kf := s.keyFrame
		s.keyFrameMu.RUnlock()
		if kf != nil && kf.Payload != nil {
			cf := cloneFrame(kf)
			select {
			case ch <- cf:
			default:
			}
		}
	}()

	return ch, func() {
		s.subMu.Lock()
		delete(s.subscribers, ch)
		s.subMu.Unlock()
		// 安全关闭 channel (dispatch 可能已经关闭了它)
		defer func() { recover() }()
		close(ch)
		s.viewers.Delete(viewer.ID)
		s.viewerCount.Add(-1)
		if s.mgr != nil {
			s.mgr.totalViewers.Add(-1)
		}
	}
}

// cloneFrame 复制帧(深拷贝 Payload,避免订阅者修改影响其他订阅者)
func cloneFrame(f *media.Frame) *media.Frame {
	if f == nil {
		return nil
	}
	payload := make([]byte, len(f.Payload))
	copy(payload, f.Payload)
	return &media.Frame{
		Track:      f.Track,
		Codec:      f.Codec,
		IsKeyFrame: f.IsKeyFrame,
		Payload:    payload,
		PTS:        f.PTS,
		DTS:        f.DTS,
	}
}

// ViewerCount 当前观看人数
func (s *Session) ViewerCount() int64 { return s.viewerCount.Load() }

// VideoTrack 视频轨道
func (s *Session) VideoTrack() *media.Track { return s.videoTrack }

// AudioTrack 音频轨道
func (s *Session) AudioTrack() *media.Track { return s.audioTrack }

// CreatedAt 创建时间
func (s *Session) CreatedAt() time.Time { return s.createdAt }

// SourceType 源类型
func (s *Session) SourceType() string { return s.Source.Type() }

// Uptime 运行时长
func (s *Session) Uptime() time.Duration { return time.Since(s.createdAt) }

// FrameCount 帧数
func (s *Session) FrameCount() int64 { return s.frameCount.Load() }

// BytesIn 输入字节
func (s *Session) BytesIn() int64 { return s.bytesIn.Load() }

// HLSSlicer 获取或创建 HLS 切片器
func (s *Session) HLSSlicer() *hls.Slicer {
	s.hlsMu.Lock()
	defer s.hlsMu.Unlock()
	if s.hlsSlicer == nil {
		s.hlsSlicer = hls.NewSlicer(s.videoTrack, s.audioTrack, 3*time.Second)
	}
	return s.hlsSlicer
}

// HasHLSSlicer 是否已创建 HLS 切片器
func (s *Session) HasHLSSlicer() bool {
	s.hlsMu.RLock()
	defer s.hlsMu.RUnlock()
	return s.hlsSlicer != nil
}

// cacheCodecFrame 缓存 codec 参数帧(SPS/PPS 携带的关键帧)
func (s *Session) cacheCodecFrame(f *media.Frame) {
	if f == nil || !f.IsVideo() {
		return
	}
	// 仅缓存关键帧(包含 SPS/PPS)
	if !f.IsKeyFrame {
		return
	}
	s.codecMu.Lock()
	defer s.codecMu.Unlock()
	// 保留最近 1 个 codec 参数帧
	s.codecFrames = s.codecFrames[:0]
	s.codecFrames = append(s.codecFrames, cloneFrame(f))
}

// cacheKeyFrame 缓存最近的关键帧
func (s *Session) cacheKeyFrame(f *media.Frame) {
	if f == nil || !f.IsVideo() || !f.IsKeyFrame {
		return
	}
	s.keyFrameMu.Lock()
	defer s.keyFrameMu.Unlock()
	s.keyFrame = cloneFrame(f)
}

// dispatch 帧分发循环
func (s *Session) dispatch(ctx context.Context, frameCh <-chan *media.Frame) {
	defer func() {
		// 关闭所有订阅者通道,使 WebSocket/HTTP-FLV 消费者退出阻塞
		s.subMu.Lock()
		for ch := range s.subscribers {
			close(ch)
			delete(s.subscribers, ch)
		}
		s.subMu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case f, ok := <-frameCh:
			if !ok {
				return
			}
			if f == nil || f.Payload == nil {
				continue
			}
			s.frameCount.Add(1)
			bytes := int64(len(f.Payload))
			s.bytesIn.Add(bytes)
			s.lastFrameAt.Store(time.Now().UnixMilli())
			if s.mgr != nil {
				s.mgr.totalBytesIn.Add(bytes)
			}

			// 缓存关键帧与 codec 参数帧(供新订阅者立即使用)
			if f.IsVideo() && f.IsKeyFrame {
				s.cacheKeyFrame(f)
				s.cacheCodecFrame(f)
			}

			// 写入 HLS 切片器(如果已创建)
			if s.HasHLSSlicer() {
				if _, err := s.HLSSlicer().WriteFrame(f); err != nil {
					log.Printf("[HLS] 切片错误: %v", err)
				}
			}

			// 分发给所有订阅者
			s.subMu.RLock()
			for ch := range s.subscribers {
				select {
				case ch <- f:
				default:
					// 订阅者消费慢,丢帧(关键帧不能丢,强制发送)
					if f.IsVideo() && f.IsKeyFrame {
						// 关键帧:非阻塞尝试清除一个旧帧后重试
						select {
						case <-ch:
						default:
						}
						select {
						case ch <- f:
						default:
						}
					}
				}
			}
			s.subMu.RUnlock()
		}
	}
}

// generateStreamID 生成流 ID
func generateStreamID() string {
	id := uuid.New().String()
	if len(id) > 12 {
		id = id[:12]
	}
	return "stream_" + id
}
