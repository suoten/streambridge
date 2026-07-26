// Package session 统计模块
package session

import (
	"runtime"
	"sync/atomic"
	"time"
)

// Stats 全局统计
type Stats struct {
	startTime   time.Time
	totalIn     atomic.Int64
	totalOut    atomic.Int64
	totalFrames atomic.Int64
}

// NewStats 创建统计器
func NewStats() *Stats {
	return &Stats{startTime: time.Now()}
}

// Uptime 运行时长
func (s *Stats) Uptime() int64 { return int64(time.Since(s.startTime).Seconds()) }

// AddIn 累加输入字节
func (s *Stats) AddIn(n int64) { s.totalIn.Add(n) }

// AddOut 累加输出字节
func (s *Stats) AddOut(n int64) { s.totalOut.Add(n) }

// AddFrames 累加帧数
func (s *Stats) AddFrames(n int64) { s.totalFrames.Add(n) }

// TotalIn 输入字节
func (s *Stats) TotalIn() int64 { return s.totalIn.Load() }

// TotalOut 输出字节
func (s *Stats) TotalOut() int64 { return s.totalOut.Load() }

// TotalFrames 总帧数
func (s *Stats) TotalFrames() int64 { return s.totalFrames.Load() }

// MemStatsMB 内存占用 MB
func MemStatsMB() float64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return float64(m.Alloc) / 1024 / 1024
}

// CPUUsage CPU 占用(近似,基于 GC 时间)
// 准确的 CPU 占用需 cgo 调用系统 API,这里用 Go GC 占比近似
func CPUUsage() float64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	if m.NumGC == 0 {
		return 0
	}
	return float64(m.GCCPUFraction) * 100
}
