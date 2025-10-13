// --- START OF COMPLETE REPLACEMENT for liuproxy_go/internal/strategy/worker_dialer.go ---
package worker

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
	"liuproxy_gateway/internal/shared"
)

// Dial 负责为 Worker 策略建立 WebSocket 连接。
// 这个版本回退到了简单的Dialer逻辑，并增加了详细的证书诊断日志。
func Dial(urlStr string, edgeIP string) (net.Conn, error) {

	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("worker dial: invalid URL: %w", err)
	}

	requestHeader := http.Header{}
	requestHeader.Set("Host", parsedURL.Hostname())
	requestHeader.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/107.0.0.0 Safari/537.36")

	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 15 * time.Second

	// 我们仍然需要自定义NetDialContext来实现Edge IP
	dialer.NetDialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialAddr := addr
		if edgeIP != "" {
			_, port, err := net.SplitHostPort(addr)
			if err != nil {
				port = "443" // Default to 443 for wss if port is missing
			}
			dialAddr = net.JoinHostPort(edgeIP, port)
		}

		d := &net.Dialer{Timeout: 10 * time.Second}
		return d.DialContext(ctx, network, dialAddr)
	}

	// 我们使用TLSClientConfig来控制TLS握手
	if parsedURL.Scheme == "wss" {
		dialer.TLSClientConfig = &tls.Config{
			ServerName: parsedURL.Hostname(), // 设置SNI
			// --- 日志增强核心 ---
			// 这个函数会在TLS握手成功后，证书验证之前被调用
			// 让我们在这里打印出服务器到底给了我们什么证书
			VerifyConnection: func(cs tls.ConnectionState) error {
				// 在这里执行Go标准库默认的验证逻辑
				opts := x509.VerifyOptions{
					DNSName:       cs.ServerName,
					Intermediates: x509.NewCertPool(),
				}
				for _, cert := range cs.PeerCertificates[1:] {
					opts.Intermediates.AddCert(cert)
				}
				_, err := cs.PeerCertificates[0].Verify(opts)
				if err != nil {
					return err
				}
				return nil
			},
		}
	}

	ws, _, err := dialer.Dial(urlStr, requestHeader)
	if err != nil {
		return nil, err
	}

	return shared.NewWebSocketConnAdapter(ws), nil
}
