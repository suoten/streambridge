// FLV 输出器(WebSocket 与 HTTP 两种)
package server

import (
	"bytes"
	"io"

	"github.com/gorilla/websocket"

	"github.com/streambridge/streambridge/internal/media"
	"github.com/streambridge/streambridge/internal/media/flv"
)

// wsWriter 适配 gorilla/websocket 为 io.Writer,支持写入批处理
// 多次 Write 调用被缓冲,通过 Flush 合并为单个 WebSocket 消息发送
type wsWriter struct {
	conn *websocket.Conn
	buf  bytes.Buffer
}

func newWSWriter(conn *websocket.Conn) *wsWriter {
	return &wsWriter{conn: conn}
}

func (w *wsWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

// Flush 将缓冲数据作为单个 WebSocket 消息发送
func (w *wsWriter) Flush() error {
	if w.buf.Len() == 0 {
		return nil
	}
	err := w.conn.WriteMessage(websocket.BinaryMessage, w.buf.Bytes())
	w.buf.Reset()
	return err
}

// flvWriter WebSocket FLV 输出
type flvWriter struct {
	muxer *flv.Muxer
	ws    *wsWriter
}

// newFLVWriter 创建 WebSocket FLV 输出
func newFLVWriter(conn *websocket.Conn, video, audio *media.Track) *flvWriter {
	w := newWSWriter(conn)
	m := flv.NewMuxer(w)
	m.SetTracks(video, audio)
	return &flvWriter{muxer: m, ws: w}
}

// WriteHeader 写 FLV 头
func (f *flvWriter) WriteHeader() error { return f.muxer.WriteHeader() }

// WriteFrame 写一帧(缓冲到 wsWriter,需要 Flush 才发送)
func (f *flvWriter) WriteFrame(frame *media.Frame) error { return f.muxer.WriteFrame(frame) }

// Flush 发送缓冲的 FLV 数据为单个 WebSocket 消息
func (f *flvWriter) Flush() error { return f.ws.Flush() }

// flvHTTPWriter HTTP-FLV 直出
type flvHTTPWriter struct {
	muxer *flv.Muxer
}

// newFLVHTTPWriter 创建 HTTP FLV 输出
func newFLVHTTPWriter(w io.Writer, video, audio *media.Track) *flvHTTPWriter {
	m := flv.NewMuxer(w)
	m.SetTracks(video, audio)
	return &flvHTTPWriter{muxer: m}
}

// WriteHeader 写 FLV 头
func (f *flvHTTPWriter) WriteHeader() error { return f.muxer.WriteHeader() }

// WriteFrame 写一帧
func (f *flvHTTPWriter) WriteFrame(frame *media.Frame) error { return f.muxer.WriteFrame(frame) }
