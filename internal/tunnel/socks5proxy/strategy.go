package socks5proxy

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/proxy"

	"liuproxy_nexus/internal/shared"
	"liuproxy_nexus/internal/shared/types"
)

// SOCKS5Strategy 实现了 TunnelStrategy 接口，用于连接上游 SOCKS5 代理。
type SOCKS5Strategy struct {
	config            *types.Config
	profile           *types.ServerProfile
	logger            zerolog.Logger
	stateManager      types.StateManager
	activeConnections atomic.Int64
	uplinkBytes       atomic.Uint64
	downlinkBytes     atomic.Uint64
}

var _ types.TunnelStrategy = (*SOCKS5Strategy)(nil)

func NewSOCKS5Strategy(cfg *types.Config, profile *types.ServerProfile, stateManager types.StateManager) (types.TunnelStrategy, error) {
	s := &SOCKS5Strategy{
		config:       cfg,
		profile:      profile,
		stateManager: stateManager,
		logger: log.With().
			Str("strategy_type", "socks5").
			Str("server_id", profile.ID).
			Str("remarks", profile.Remarks).Logger(),
	}
	return s, nil
}

func (s *SOCKS5Strategy) GetSocksConnection() (net.Conn, error) {
	clientPipe, serverPipe := net.Pipe()

	s.activeConnections.Add(1)
	go func() {
		defer s.activeConnections.Add(-1)
		countedPipe := shared.NewCountedConn(serverPipe, &s.uplinkBytes, &s.downlinkBytes)
		s.handleSocksConnection(countedPipe)
	}()

	return clientPipe, nil
}

func (s *SOCKS5Strategy) handleSocksConnection(clientConn net.Conn) {
	defer clientConn.Close()

	reader := bufio.NewReader(clientConn)
	cmd, targetAddr, err := s.socks5Handshake(clientConn, reader)
	if err != nil {
		s.logger.Warn().Err(err).Msg("SOCKS5 handshake with client failed.")
		return
	}
	if cmd != 0x01 { // Only support CONNECT
		s.logger.Warn().Uint8("cmd", cmd).Msg("Unsupported SOCKS5 command.")
		return
	}

	proxyAddr := net.JoinHostPort(s.profile.Address, strconv.Itoa(s.profile.Port))
	dialer, err := proxy.SOCKS5("tcp", proxyAddr, nil, &net.Dialer{Timeout: 10 * time.Second})
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to create SOCKS5 dialer.")
		clientConn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // General failure
		return
	}

	proxyConn, err := dialer.Dial("tcp", targetAddr)
	if err != nil {
		s.logger.Error().Err(err).Str("proxy_addr", proxyAddr).Str("target", targetAddr).Msg("Failed to dial target via SOCKS5 proxy.")
		if s.stateManager != nil {
			s.stateManager.SetServerStatusDown(s.profile.ID, err.Error())
		}
		clientConn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // Host unreachable
		return
	}
	defer proxyConn.Close()

	if _, err := clientConn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		s.logger.Warn().Err(err).Msg("Failed to write SOCKS5 success reply.")
		return
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(proxyConn, reader)
		if tcpConn, ok := proxyConn.(interface{ CloseWrite() error }); ok {
			tcpConn.CloseWrite()
		}
	}()
	go func() {
		defer wg.Done()
		io.Copy(clientConn, proxyConn)
		if tcpConn, ok := clientConn.(interface{ CloseWrite() error }); ok {
			tcpConn.CloseWrite()
		}
	}()

	wg.Wait()
}

// socks5Handshake is a helper to perform SOCKS5 handshake with the client.
func (s *SOCKS5Strategy) socks5Handshake(conn net.Conn, reader *bufio.Reader) (byte, string, error) {
	// Implementation is identical to httpproxy.Strategy, can be refactored into a shared package later if needed.
	// For now, we keep it simple by duplicating it.
	authHeader := make([]byte, 2)
	if _, err := io.ReadFull(reader, authHeader); err != nil {
		return 0, "", fmt.Errorf("failed to read auth header: %w", err)
	}
	if authHeader[0] != 0x05 {
		return 0, "", fmt.Errorf("unsupported socks version: %d", authHeader[0])
	}
	nMethods := int(authHeader[1])
	if _, err := io.CopyN(io.Discard, reader, int64(nMethods)); err != nil {
		return 0, "", fmt.Errorf("failed to discard auth methods: %w", err)
	}
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return 0, "", fmt.Errorf("failed to write auth response: %w", err)
	}

	reqHeader := make([]byte, 4)
	if _, err := io.ReadFull(reader, reqHeader); err != nil {
		return 0, "", fmt.Errorf("failed to read request header: %w", err)
	}

	// ... (rest of handshake logic is identical to httpproxy and can be copied here) ...
	cmd := reqHeader[1]
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

// --- Interface methods ---

func (s *SOCKS5Strategy) Initialize() error                    { return nil }
func (s *SOCKS5Strategy) InitializeForGateway() error          { return nil }
func (s *SOCKS5Strategy) CloseTunnel()                         {}
func (s *SOCKS5Strategy) GetType() string                      { return "http" } // Keep type as http for UI consistency
func (s *SOCKS5Strategy) GetListenerInfo() *types.ListenerInfo { return nil }

func (s *SOCKS5Strategy) GetMetrics() *types.Metrics {
	return &types.Metrics{ActiveConnections: s.activeConnections.Load()}
}

func (s *SOCKS5Strategy) GetTrafficStats() types.TrafficStats {
	return types.TrafficStats{
		Uplink:   s.uplinkBytes.Load(),
		Downlink: s.downlinkBytes.Load(),
	}
}

func (s *SOCKS5Strategy) HandleRawTCP(inboundConn net.Conn, targetDest string) {
	s.logger.Error().Msg("SOCKS5 proxy strategy does not support HandleRawTCP (transparent proxy).")
	inboundConn.Close()
}

func (s *SOCKS5Strategy) HandleUDPPacket(packet *types.UDPPacket, sessionKey string) error {
	s.logger.Warn().Msg("SOCKS5 proxy strategy does not support UDP. Packet dropped.")
	return nil
}

func (s *SOCKS5Strategy) UpdateServer(newProfile *types.ServerProfile) error {
	s.profile = newProfile
	s.logger.Info().Msg("SOCKS5 proxy profile updated.")
	return nil
}

func (s *SOCKS5Strategy) CheckHealth() error {
	_, _, err := s.CheckHealthAdvanced()
	return err
}

type pipeDialer struct{ conn net.Conn }

func (d *pipeDialer) Dial(network, addr string) (net.Conn, error) { return d.conn, nil }

func (s *SOCKS5Strategy) CheckHealthAdvanced() (latency int64, exitIP string, err error) {
	// Simple TCP dial check for maximum reliability.
	start := time.Now()
	proxyAddr := net.JoinHostPort(s.profile.Address, strconv.Itoa(s.profile.Port))

	conn, err := net.DialTimeout("tcp", proxyAddr, 10*time.Second)
	if err != nil {
		return -1, "", err
	}
	conn.Close()

	latencyMs := time.Since(start).Milliseconds()
	// Return a special string to indicate that this was just a TCP check.
	return latencyMs, "tcp_dial_ok", nil
}
