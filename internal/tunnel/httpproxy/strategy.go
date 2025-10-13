package httpproxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/proxy"

	"liuproxy_gateway/internal/shared"
	"liuproxy_gateway/internal/shared/types"
)

// HTTPStrategy 实现了 TunnelStrategy 接口，用于连接上游 HTTP 代理。
type HTTPStrategy struct {
	config            *types.Config
	profile           *types.ServerProfile
	logger            zerolog.Logger
	stateManager      types.StateManager
	activeConnections atomic.Int64
	uplinkBytes       atomic.Uint64
	downlinkBytes     atomic.Uint64
}

// Ensure HTTPStrategy implements TunnelStrategy interface
var _ types.TunnelStrategy = (*HTTPStrategy)(nil)

func NewHTTPStrategy(cfg *types.Config, profile *types.ServerProfile, stateManager types.StateManager) (types.TunnelStrategy, error) {
	s := &HTTPStrategy{
		config:       cfg,
		profile:      profile,
		stateManager: stateManager,
		logger: log.With().
			Str("strategy_type", "http").
			Str("server_id", profile.ID).
			Str("remarks", profile.Remarks).Logger(),
	}
	return s, nil
}

// GetSocksConnection 为 Gateway 提供一个内存中的 SOCKS5 连接。
func (s *HTTPStrategy) GetSocksConnection() (net.Conn, error) {
	clientPipe, serverPipe := net.Pipe()

	s.activeConnections.Add(1)
	go func() {
		defer s.activeConnections.Add(-1)
		// 使用 CountedConn 包装 serverPipe 以进行流量统计
		countedPipe := shared.NewCountedConn(serverPipe, &s.uplinkBytes, &s.downlinkBytes)
		s.handleSocksConnection(countedPipe)
	}()

	return clientPipe, nil
}

// handleSocksConnection 处理来自 Gateway 的 SOCKS5 请求，并将其转换为 HTTP CONNECT 请求。
func (s *HTTPStrategy) handleSocksConnection(clientConn net.Conn) {
	defer clientConn.Close()

	// 1. 完成 SOCKS5 握手以获取目标地址
	reader := bufio.NewReader(clientConn)
	cmd, targetAddr, err := s.socks5Handshake(clientConn, reader)
	if err != nil {
		s.logger.Warn().Err(err).Msg("SOCKS5 handshake failed.")
		return
	}
	if cmd != 0x01 { // 仅支持 CONNECT
		s.logger.Warn().Uint8("cmd", cmd).Msg("Unsupported SOCKS5 command.")
		return
	}

	// 2. 连接到上游 HTTP 代理
	proxyAddr := net.JoinHostPort(s.profile.Address, strconv.Itoa(s.profile.Port))
	proxyConn, err := net.DialTimeout("tcp", proxyAddr, 10*time.Second)
	if err != nil {
		s.logger.Error().Err(err).Str("proxy_addr", proxyAddr).Msg("Failed to dial HTTP proxy.")
		if s.stateManager != nil {
			s.stateManager.SetServerStatusDown(s.profile.ID, err.Error())
		}
		// 向 SOCKS 客户端发送 "Host unreachable" 错误
		clientConn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer proxyConn.Close()

	// 3. 发送 HTTP CONNECT 请求
	connectReq := &http.Request{
		Method: "CONNECT",
		URL:    &url.URL{Host: targetAddr},
		Host:   targetAddr,
		Header: make(http.Header),
	}
	if s.profile.Username != "" {
		auth := s.profile.Username + ":" + s.profile.Password
		basicAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(auth))
		connectReq.Header.Set("Proxy-Authorization", basicAuth)
	}
	connectReq.Header.Set("User-Agent", "liuproxy-client/1.0")

	if err := connectReq.Write(proxyConn); err != nil {
		s.logger.Error().Err(err).Msg("Failed to write CONNECT request to proxy.")
		clientConn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // General server failure
		return
	}

	// 4. 读取并验证 CONNECT 响应
	br := bufio.NewReader(proxyConn)
	resp, err := http.ReadResponse(br, connectReq)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to read CONNECT response from proxy.")
		clientConn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		s.logger.Warn().Int("status_code", resp.StatusCode).Msg("HTTP proxy returned non-200 status for CONNECT.")
		clientConn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // Connection refused
		return
	}

	// 5. 回复 SOCKS5 客户端连接成功
	if _, err := clientConn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		s.logger.Warn().Err(err).Msg("Failed to write SOCKS5 success reply.")
		return
	}

	// 6. 双向转发数据
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(proxyConn, reader) // 使用已有的 reader
		if tcpConn, ok := proxyConn.(*net.TCPConn); ok {
			tcpConn.CloseWrite()
		}
	}()
	go func() {
		defer wg.Done()
		io.Copy(clientConn, br) // 使用已有的 br
		// CORRECTED: Type switch on the interface to find the concrete type
		switch c := clientConn.(type) {
		case *net.TCPConn:
			c.CloseWrite()
		default:
			// For other types, just close the whole thing.
			c.Close()
		}
	}()

	wg.Wait()
}

// --- 接口的其他方法实现 ---

func (s *HTTPStrategy) Initialize() error                    { return nil }
func (s *HTTPStrategy) InitializeForGateway() error          { return nil }
func (s *HTTPStrategy) CloseTunnel()                         {}
func (s *HTTPStrategy) GetType() string                      { return "http" }
func (s *HTTPStrategy) GetListenerInfo() *types.ListenerInfo { return nil }
func (s *HTTPStrategy) GetMetrics() *types.Metrics {
	return &types.Metrics{ActiveConnections: s.activeConnections.Load()}
}
func (s *HTTPStrategy) HandleRawTCP(inboundConn net.Conn, targetDest string) {
	s.logger.Error().Msg("HTTP strategy does not support HandleRawTCP (transparent proxy).")
	inboundConn.Close()
}
func (s *HTTPStrategy) HandleUDPPacket(packet *types.UDPPacket, sessionKey string) error {
	s.logger.Warn().Msg("HTTP strategy does not support UDP. Packet dropped.")
	return nil
}

func (s *HTTPStrategy) UpdateServer(newProfile *types.ServerProfile) error {
	s.profile = newProfile
	s.logger = log.With().
		Str("strategy_type", "http").
		Str("server_id", newProfile.ID).
		Str("remarks", newProfile.Remarks).Logger()
	s.logger.Info().Msg("HTTP proxy profile updated.")
	return nil
}

func (s *HTTPStrategy) GetTrafficStats() types.TrafficStats {
	return types.TrafficStats{
		Uplink:   s.uplinkBytes.Load(),
		Downlink: s.downlinkBytes.Load(),
	}
}

// CheckHealth (旧接口实现)
func (s *HTTPStrategy) CheckHealth() error {
	_, _, err := s.CheckHealthAdvanced()
	return err
}

// pipeDialer 实现了 proxy.Dialer 接口，但它的 Dial 方法总是返回预设的 net.Conn
type pipeDialer struct {
	conn net.Conn
}

func (d *pipeDialer) Dial(network, addr string) (net.Conn, error) {
	return d.conn, nil
}

// CheckHealthAdvanced 实现了高级健康检查
func (s *HTTPStrategy) CheckHealthAdvanced() (latency int64, exitIP string, err error) {
	pipeConn, err := s.GetSocksConnection()
	if err != nil {
		return -1, "", fmt.Errorf("http CheckHealth: failed to get pipe connection: %w", err)
	}
	defer pipeConn.Close()

	dialer, err := proxy.SOCKS5("tcp", "placeholder:1080", nil, &pipeDialer{conn: pipeConn})
	if err != nil {
		return -1, "", fmt.Errorf("http CheckHealth: failed to create SOCKS5 dialer: %w", err)
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		},
		DisableKeepAlives: true,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}

	const healthCheckURL = "https://www.cloudflare.com/cdn-cgi/trace"
	start := time.Now()
	resp, err := client.Get(healthCheckURL)
	if err != nil {
		return -1, "", fmt.Errorf("http CheckHealth: http get failed: %w", err)
	}
	defer resp.Body.Close()

	latencyMs := time.Since(start).Milliseconds()

	if resp.StatusCode != http.StatusOK {
		return latencyMs, "", fmt.Errorf("http CheckHealth: unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return latencyMs, "", fmt.Errorf("http CheckHealth: failed to read response body: %w", err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "ip=") {
			exitIP = strings.TrimPrefix(line, "ip=")
			break
		}
	}
	return latencyMs, exitIP, nil
}

// socks5Handshake (辅助函数)
func (s *HTTPStrategy) socks5Handshake(conn net.Conn, reader *bufio.Reader) (cmd byte, targetAddr string, err error) {
	// Auth phase
	authHeader := make([]byte, 2)
	if _, err = io.ReadFull(reader, authHeader); err != nil {
		return 0, "", fmt.Errorf("failed to read auth header: %w", err)
	}
	if authHeader[0] != 0x05 {
		return 0, "", fmt.Errorf("unsupported socks version: %d", authHeader[0])
	}
	nMethods := int(authHeader[1])
	if _, err = io.CopyN(io.Discard, reader, int64(nMethods)); err != nil {
		return 0, "", fmt.Errorf("failed to discard auth methods: %w", err)
	}
	if _, err = conn.Write([]byte{0x05, 0x00}); err != nil { // Respond with NO AUTH
		return 0, "", fmt.Errorf("failed to write auth response: %w", err)
	}

	// Request phase
	reqHeader := make([]byte, 4)
	if _, err = io.ReadFull(reader, reqHeader); err != nil {
		return 0, "", fmt.Errorf("failed to read request header: %w", err)
	}

	cmd = reqHeader[1]
	var host string
	addrType := reqHeader[3]
	switch addrType {
	case 0x01: // IPv4
		addr := make([]byte, 4)
		io.ReadFull(reader, addr)
		host = net.IP(addr).String()
	case 0x03: // Domain
		lenBuf := make([]byte, 1)
		io.ReadFull(reader, lenBuf)
		domain := make([]byte, lenBuf[0])
		io.ReadFull(reader, domain)
		host = string(domain)
	case 0x04: // IPv6
		addr := make([]byte, 16)
		io.ReadFull(reader, addr)
		host = net.IP(addr).String()
	default:
		return cmd, "", fmt.Errorf("unsupported address type: %d", addrType)
	}

	portBuf := make([]byte, 2)
	io.ReadFull(reader, portBuf)
	port := binary.BigEndian.Uint16(portBuf)

	return cmd, net.JoinHostPort(host, strconv.Itoa(int(port))), nil
}
