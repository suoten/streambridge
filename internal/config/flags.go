// Package config 命令行参数解析
package config

import (
	"flag"
	"fmt"
	"strings"
)

// Flags 命令行参数
type Flags struct {
	ConfigFile  string
	HTTPPort    int
	RTMPPort    int
	LogLevel    string
	ShowVersion bool
	Subcommand  string // doctor | upgrade | https | ""
}

// ParseFlags 解析命令行参数
func ParseFlags(args []string) (*Flags, *Config, error) {
	f := &Flags{}
	cfg := Default()

	if len(args) > 1 {
		switch args[1] {
		case "doctor", "upgrade", "https", "migrate-zlm", "help", "version":
			f.Subcommand = args[1]
			return f, cfg, nil
		}
	}

	fs := flag.NewFlagSet("streambridge", flag.ContinueOnError)
	fs.StringVar(&f.ConfigFile, "c", "", "配置文件路径 (默认零配置启动)")
	fs.IntVar(&f.HTTPPort, "http-port", 0, "HTTP 端口 (覆盖配置文件)")
	fs.IntVar(&f.RTMPPort, "rtmp-port", 0, "RTMP 端口 (覆盖配置文件)")
	fs.StringVar(&f.LogLevel, "log-level", "", "日志级别 debug|info|warn|error")
	fs.BoolVar(&f.ShowVersion, "version", false, "显示版本号")

	if err := fs.Parse(args[1:]); err != nil {
		return nil, nil, err
	}

	loaded, err := Load(f.ConfigFile)
	if err != nil {
		return nil, nil, err
	}
	cfg = loaded

	// 命令行参数覆盖配置文件
	if f.HTTPPort > 0 {
		cfg.Server.HTTPPort = f.HTTPPort
	}
	if f.RTMPPort > 0 {
		cfg.Server.RTMPPort = f.RTMPPort
	}
	if f.LogLevel != "" {
		if err := validateLogLevel(f.LogLevel); err != nil {
			return nil, nil, err
		}
		cfg.Log.Level = strings.ToLower(f.LogLevel)
	}

	return f, cfg, nil
}

func validateLogLevel(l string) error {
	switch strings.ToLower(l) {
	case "debug", "info", "warn", "error":
		return nil
	}
	return fmt.Errorf("非法日志级别: %s", l)
}
