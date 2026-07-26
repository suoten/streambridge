// Package config 负责解析 StreamBridge 的 YAML 配置文件与命令行参数
package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config StreamBridge 主配置
type Config struct {
	Server      ServerConfig      `yaml:"server"`
	Log         LogConfig         `yaml:"log"`
	Performance PerformanceConfig `yaml:"performance"`
	Input       InputConfig       `yaml:"input"`
	Security    SecurityConfig    `yaml:"security"`
}

// ServerConfig 服务监听端口
type ServerConfig struct {
	HTTPPort int    `yaml:"http_port"`
	RTMPPort int    `yaml:"rtmp_port"`
	WHEPPort int    `yaml:"whep_port"`
	BindAddr string `yaml:"bind_addr"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level   string `yaml:"level"`
	Output  string `yaml:"output"`
	File    string `yaml:"file"`
	MaxSize int    `yaml:"max_size"`
	MaxAge  int    `yaml:"max_age"`
}

// PerformanceConfig 性能与限流
type PerformanceConfig struct {
	MaxStreams          int  `yaml:"max_streams"`
	MaxViewersPerStream int  `yaml:"max_viewers_per_stream"`
	EnableH265Transcode bool `yaml:"enable_h265_transcode"`
	TranscodeMaxStreams int  `yaml:"transcode_max_streams"`
}

// InputConfig 输入协议配置
type InputConfig struct {
	RTP RTPInputConfig `yaml:"rtp"`
}

// RTPInputConfig RTP/PS 媒体接收
type RTPInputConfig struct {
	Listen       string `yaml:"listen"`
	PortRange    [2]int `yaml:"port_range"`
	Mode         string `yaml:"mode"`
	PSDepacketize bool  `yaml:"ps_depacketize"`
}

// SecurityConfig 鉴权与 CORS
type SecurityConfig struct {
	EnableAuth    bool     `yaml:"enable_auth"`
	SecretKey     string   `yaml:"secret_key"`
	TokenExpire   int      `yaml:"token_expire"`
	AllowOrigins  []string `yaml:"allow_origins"`
}

// Default 返回带默认值的配置(零配置可启动)
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			HTTPPort: 8080,
			RTMPPort: 1935,
			WHEPPort: 1985,
			BindAddr: "0.0.0.0",
		},
		Log: LogConfig{
			Level:   "info",
			Output:  "stdout",
			File:    "/var/log/streambridge.log",
			MaxSize: 100,
			MaxAge:  7,
		},
		Performance: PerformanceConfig{
			MaxStreams:          500,
			MaxViewersPerStream: 50,
			EnableH265Transcode: false,
			TranscodeMaxStreams: 10,
		},
		Input: InputConfig{
			RTP: RTPInputConfig{
				Listen:        "0.0.0.0",
				PortRange:     [2]int{20000, 30000},
				Mode:          "passive",
				PSDepacketize: true,
			},
		},
		Security: SecurityConfig{
			EnableAuth:   false,
			SecretKey:    "streambridge-default-key",
			TokenExpire:  3600,
			AllowOrigins: []string{"*"},
		},
	}
}

// Load 从 YAML 文件加载配置,缺失字段使用默认值
func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败 %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// validate 校验配置合法性
func (c *Config) validate() error {
	if c.Server.HTTPPort <= 0 || c.Server.HTTPPort > 65535 {
		return fmt.Errorf("server.http_port 非法: %d", c.Server.HTTPPort)
	}
	if c.Server.RTMPPort <= 0 || c.Server.RTMPPort > 65535 {
		return fmt.Errorf("server.rtmp_port 非法: %d", c.Server.RTMPPort)
	}
	if c.Server.WHEPPort <= 0 || c.Server.WHEPPort > 65535 {
		return fmt.Errorf("server.whep_port 非法: %d", c.Server.WHEPPort)
	}
	if c.Input.RTP.PortRange[0] > c.Input.RTP.PortRange[1] {
		return fmt.Errorf("input.rtp.port_range 非法: %v", c.Input.RTP.PortRange)
	}
	switch strings.ToLower(c.Log.Level) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log.level 非法: %s", c.Log.Level)
	}
	return nil
}

// String 输出配置摘要(脱敏)
func (c *Config) String() string {
	return fmt.Sprintf(
		"server{http=%d rtmp=%d whep=%d bind=%s} log{level=%s} perf{maxStreams=%d maxViewers=%d h265=%v} rtp{range=%d-%d mode=%s}",
		c.Server.HTTPPort, c.Server.RTMPPort, c.Server.WHEPPort, c.Server.BindAddr,
		c.Log.Level,
		c.Performance.MaxStreams, c.Performance.MaxViewersPerStream, c.Performance.EnableH265Transcode,
		c.Input.RTP.PortRange[0], c.Input.RTP.PortRange[1], c.Input.RTP.Mode,
	)
}
