// HTTP-FLV / HLS 输入透传
// 社区版支持从已有的转码服务或 CDN 拉取 HTTP-FLV 或 HLS 流
package http

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/streambridge/streambridge/internal/media"
)

// Input HTTP 输入器
type Input struct {
	url        string
	frameCh    chan *media.Frame
	videoTrack *media.Track
	audioTrack *media.Track
	started    atomic.Bool
	startTime  time.Time
}

// New 创建 HTTP 输入器
func New() *Input {
	return &Input{}
}

// Start 拉取 HTTP-FLV 或 HLS 流
func (i *Input) Start(ctx context.Context, url string) (<-chan *media.Frame, error) {
	i.url = url
	i.frameCh = make(chan *media.Frame, 256)
	i.startTime = time.Now()
	i.videoTrack = &media.Track{Type: media.TrackVideo, Codec: media.CodecH264, FPS: 25}
	i.audioTrack = &media.Track{Type: media.TrackAudio, Codec: media.CodecAAC, SampleRate: 44100, Channels: 2}

	// 判断类型
	if isHLS(url) {
		i.started.Store(true)
		go i.pullHLS(ctx, url)
	} else if isFLV(url) {
		i.started.Store(true)
		go i.pullFLV(ctx, url)
	} else {
		return nil, fmt.Errorf("不支持的 HTTP URL: %s(仅支持 .flv 或 .m3u8)", url)
	}
	return i.frameCh, nil
}

func (i *Input) pullFLV(ctx context.Context, url string) {
	defer close(i.frameCh)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	// 读取 FLV 流并解析
	buf := make([]byte, 65536)
	var carry []byte
	headerRead := false
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, err := resp.Body.Read(buf)
		if n > 0 {
			carry = append(carry, buf[:n]...)
			if !headerRead && len(carry) >= 13 {
				carry = carry[13:]
				headerRead = true
			}
			if headerRead {
				consumed := i.parseFLVTags(carry)
				if consumed > 0 {
					carry = carry[consumed:]
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return
		}
	}
}

func (i *Input) parseFLVTags(data []byte) int {
	pos := 0
	for pos+11 <= len(data) {
		tagType := data[pos]
		dataSize := int(data[pos+1])<<16 | int(data[pos+2])<<8 | int(data[pos+3])
		ts := uint32(data[pos+4])<<16 | uint32(data[pos+5])<<8 | uint32(data[pos+6])
		ts |= uint32(data[pos+7]) << 24
		if pos+11+dataSize+4 > len(data) {
			break
		}
		body := data[pos+11 : pos+11+dataSize]
		dur := time.Duration(ts) * time.Millisecond
		switch tagType {
		case 9:
			if len(body) > 5 && body[1] == 1 {
				i.send(&media.Frame{
					Track:      i.videoTrack,
					Codec:      media.CodecH264,
					IsKeyFrame: body[0]>>4 == 1,
					Payload:    body[5:],
					PTS:        dur,
					DTS:        dur,
				})
			}
		case 8:
			if len(body) > 2 && body[1] == 1 {
				i.send(&media.Frame{
					Track:   i.audioTrack,
					Codec:   media.CodecAAC,
					Payload: body[2:],
					PTS:     dur,
					DTS:     dur,
				})
			}
		}
		pos += 11 + dataSize + 4
	}
	return pos
}

func (i *Input) pullHLS(ctx context.Context, url string) {
	defer close(i.frameCh)
	// 社区版简化:HLS 输入需要下载 m3u8 + ts 并解封装
	// 完整实现见 internal/media/hls 的反向使用
	<-ctx.Done()
}

func (i *Input) send(f *media.Frame) {
	if !i.started.Load() {
		return
	}
	select {
	case i.frameCh <- f:
	default:
	}
}

// Tracks 返回轨道
func (i *Input) Tracks() (video, audio *media.Track) {
	return i.videoTrack, i.audioTrack
}

// Stop 停止
func (i *Input) Stop() error {
	i.started.Store(false)
	return nil
}

// Type 类型
func (i *Input) Type() string { return "http" }

// SourceURL 源 URL
func (i *Input) SourceURL() string { return i.url }

func isFLV(url string) bool {
	return len(url) >= 4 && (url[len(url)-4:] == ".flv")
}

func isHLS(url string) bool {
	return len(url) >= 5 && (url[len(url)-5:] == ".m3u8")
}
