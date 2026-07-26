// Package session 鉴权模块
package session

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/streambridge/streambridge/internal/config"
)

// Authenticator Token 鉴权器
type Authenticator struct {
	cfg *config.SecurityConfig
}

// NewAuthenticator 创建鉴权器
func NewAuthenticator(cfg *config.SecurityConfig) *Authenticator {
	return &Authenticator{cfg: cfg}
}

// Enabled 是否启用鉴权
func (a *Authenticator) Enabled() bool { return a.cfg.EnableAuth }

// GenerateToken 生成 Token
func (a *Authenticator) GenerateToken(streamID string) string {
	exp := time.Now().Unix() + int64(a.cfg.TokenExpire)
	payload := fmt.Sprintf("%s|%d", streamID, exp)
	mac := hmac.New(sha256.New, []byte(a.cfg.SecretKey))
	mac.Write([]byte(payload))
	sign := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%s|%d|%s", streamID, exp, sign)
}

// ValidateToken 校验 Token
func (a *Authenticator) ValidateToken(token string) (string, error) {
	parts := strings.Split(token, "|")
	if len(parts) != 3 {
		return "", fmt.Errorf("token 格式非法")
	}
	streamID := parts[0]
	exp := parts[1]
	sign := parts[2]

	// 校验签名
	payload := streamID + "|" + exp
	mac := hmac.New(sha256.New, []byte(a.cfg.SecretKey))
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sign), []byte(expected)) {
		return "", fmt.Errorf("token 签名非法")
	}

	// 校验过期时间
	var expTime int64
	fmt.Sscanf(exp, "%d", &expTime)
	if time.Now().Unix() > expTime {
		return "", fmt.Errorf("token 已过期")
	}
	return streamID, nil
}

// AuthMiddleware HTTP 鉴权中间件
func (a *Authenticator) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.cfg.EnableAuth {
			next.ServeHTTP(w, r)
			return
		}
		// 白名单路径
		if isWhitelisted(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		token := r.URL.Query().Get("token")
		if token == "" {
			token = r.Header.Get("X-StreamBridge-Token")
		}
		if token == "" {
			http.Error(w, "未提供 Token", http.StatusUnauthorized)
			return
		}
		if _, err := a.ValidateToken(token); err != nil {
			http.Error(w, "Token 校验失败: "+err.Error(), http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isWhitelisted(path string) bool {
	switch path {
	case "/", "/index.html", "/streambridge.js", "/flv-demuxer.js", "/hls.min.js", "/player.css", "/favicon.ico",
		"/api/health", "/api/ready", "/metrics":
		return true
	}
	return false
}

// CORSHandler CORS 中间件
func CORSHandler(cfg *config.SecurityConfig, next http.Handler) http.Handler {
	allowAll := false
	for _, o := range cfg.AllowOrigins {
		if o == "*" {
			allowAll = true
			break
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowAll {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			for _, o := range cfg.AllowOrigins {
				if o == origin {
					w.Header().Set("Access-Control-Allow-Origin", o)
					break
				}
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-StreamBridge-Token")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
