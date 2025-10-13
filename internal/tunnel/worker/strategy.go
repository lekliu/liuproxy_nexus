package worker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/proxy"
	"io"
	protocol2 "liuproxy_gateway/internal/shared/protocol"
	"liuproxy_gateway/internal/shared/securecrypt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"liuproxy_gateway/internal/shared/types"
)

type WorkerStrategy struct {
	config            *types.Config
	profile           *types.ServerProfile
	listener          net.Listener
	listenerInfo      *types.ListenerInfo
	closeOnce         sync.Once
	waitGroup         sync.WaitGroup
	activeConnections atomic.Int64
	logger            zerolog.Logger
	activeConns       sync.Map
	uplinkBytes       atomic.Uint64
	downlinkBytes     atomic.Uint64
	stateManager      types.StateManager
}

// Ensure WorkerStrategy implements TunnelStrategy interface
var _ types.TunnelStrategy = (*WorkerStrategy)(nil)

func NewWorkerStrategy(cfg *types.Config, profile *types.ServerProfile, stateManager types.StateManager) (types.TunnelStrategy, error) {
	return &WorkerStrategy{
		config:       cfg,
		profile:      profile,
		stateManager: stateManager,
		logger: log.With().
			Str("strategy_type", "worker").
			Str("server_id", profile.ID).
			Str("remarks", profile.Remarks).Logger(),
	}, nil
}

// InitializeForGateway 在网关模式下被调用。对于worker这种无状态策略，这是一个空操作。
func (s *WorkerStrategy) InitializeForGateway() error {
	s.logger.Debug().Msg("Worker: Initializing for Gateway (no-op).")
	return nil
}

func (s *WorkerStrategy) acceptLoop() {
	defer s.waitGroup.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.logger.Debug().Err(err).Msgf("[WorkerStrategy] Listener on %s stopped accepting connections", s.listener.Addr())
			return
		}

		s.activeConns.Store(conn, struct{}{})
		s.activeConnections.Add(1)
		s.waitGroup.Add(1)
		go func(c net.Conn) {
			defer s.waitGroup.Done()
			defer func() {
				if r := recover(); r != nil {
					s.logger.Error().Msgf("[WorkerStrategy] Panic recovered in connection handler for %s: %v", c.RemoteAddr(), r)
				}
				c.Close()
				s.activeConnections.Add(-1)
				s.activeConns.Delete(c)
			}()
			s.handleClientConnection(c)
		}(conn)
	}
}

// HandleTraffic 存在以满足接口，但在当前的网关方案中不被直接调用。
func (s *WorkerStrategy) HandleTraffic(inboundConn net.Conn, reader *bufio.Reader, targetDest string, proto types.Protocol) {
	s.logger.Warn().Msg("Worker Strategy: HandleTraffic is called, which is unexpected in the new architecture.")
	inboundConn.Close()
}

func (s *WorkerStrategy) GetListenerInfo() *types.ListenerInfo {
	if s.listener == nil {
		return nil // In Gateway mode
	}
	return s.listenerInfo
}

func (s *WorkerStrategy) GetMetrics() *types.Metrics {
	return &types.Metrics{
		ActiveConnections: s.activeConnections.Load(),
	}
}

func (s *WorkerStrategy) GetType() string {
	return "worker"
}

func (s *WorkerStrategy) CloseTunnel() {
	s.closeOnce.Do(func() {
		if s.listener != nil {
			s.logger.Info().Str("listen_addr", s.listener.Addr().String()).Msg("[WorkerStrategy] Closing listener")
			s.listener.Close()
		}
		s.activeConns.Range(func(key, value interface{}) bool {
			if conn, ok := key.(net.Conn); ok {
				conn.Close()
			}
			return true
		})
		s.waitGroup.Wait()
		s.logger.Info().Msg("[WorkerStrategy] Listener and all connections closed.")
	})
}

func (s *WorkerStrategy) UpdateServer(profile *types.ServerProfile) error {
	s.profile = profile
	s.logger = log.With().
		Str("strategy_type", "worker").
		Str("server_id", profile.ID).
		Str("remarks", profile.Remarks).Logger()
	s.logger.Info().Msg("Worker profile updated. New settings will apply to subsequent connections.")
	return nil
}

func (s *WorkerStrategy) createTunnel() (net.Conn, *securecrypt.Cipher, error) {
	l := s.logger.With().Str("func", "createTunnel").Logger()
	l.Debug().Msg("Attempting to create tunnel...")

	u := url.URL{
		Scheme: s.profile.Scheme,
		Host:   net.JoinHostPort(s.profile.Address, strconv.Itoa(s.profile.Port)),
		Path:   s.profile.Path,
	}
	l.Debug().Str("url", u.String()).Str("edge_ip", s.profile.EdgeIP).Msg("Dialing worker...")
	tunnelConn, err := Dial(u.String(), s.profile.EdgeIP)
	if err != nil {
		l.Error().Err(err).Msg("Dial failed.")
		return nil, nil, err
	}
	l.Debug().Msg("Dial successful.")
	cipher, err := securecrypt.NewCipherWithAlgo(s.config.CommonConf.Crypt, securecrypt.AES_256_GCM)
	if err != nil {
		l.Error().Err(err).Msg("Failed to create cipher.")
		_ = tunnelConn.Close()
		return nil, nil, err
	}
	l.Debug().Msg("Tunnel created successfully.")
	return tunnelConn, cipher, nil
}

// waitForSuccess expects an UNENCRYPTED success packet from the worker.
func (s *WorkerStrategy) waitForSuccess(conn net.Conn) error {
	l := s.logger.With().Str("func", "waitForSuccess").Logger()
	l.Debug().Msg("Waiting for success packet...")

	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetReadDeadline(time.Time{})
	packet, err := protocol2.ReadUnsecurePacket(conn)
	if err != nil {
		l.Error().Err(err).Msg("Failed to read packet.")
		return err
	}
	if packet.Flag != protocol2.FlagControlNewStreamTCPSuccess {
		l.Warn().Uint8("flag", packet.Flag).Msg("Received unexpected flag.")
		return fmt.Errorf("unexpected flag from worker: got %d, want %d", packet.Flag, protocol2.FlagControlNewStreamTCPSuccess)
	}
	l.Debug().Msg("Success packet received.")
	return nil
}

func buildMetadataForWorker(cmd byte, targetAddr string) []byte {
	host, portStr, _ := net.SplitHostPort(targetAddr)
	port, _ := strconv.Atoi(portStr)
	addrBytes := []byte(host)
	addrType := byte(0x03)
	if ip := net.ParseIP(host); ip != nil {
		if ipv4 := ip.To4(); ipv4 != nil {
			addrType = 0x01
			addrBytes = ipv4
		} else {
			addrType = 0x04
			addrBytes = ip.To16()
		}
	}
	var buf bytes.Buffer
	buf.WriteByte(cmd)
	buf.WriteByte(addrType)
	if addrType == 0x03 {
		buf.WriteByte(byte(len(addrBytes)))
	}
	buf.Write(addrBytes)
	_ = binary.Write(&buf, binary.BigEndian, uint16(port))
	return buf.Bytes()
}

func (s *WorkerStrategy) GetTrafficStats() types.TrafficStats {
	return types.TrafficStats{
		Uplink:   s.uplinkBytes.Load(),
		Downlink: s.downlinkBytes.Load(),
	}
}

// CheckHealth (旧接口实现): 轻量级TCP拨号，用于移动端。
func (s *WorkerStrategy) CheckHealth() error {
	s.logger.Debug().Msg("WorkerStrategy.CheckHealth: Attempting to create tunnel for health check...")
	conn, _, err := s.createTunnel()
	if err != nil {
		s.logger.Warn().Err(err).Msg("WorkerStrategy.CheckHealth: Failed.")
		return err
	}
	conn.Close()
	s.logger.Debug().Msg("WorkerStrategy.CheckHealth: Passed.")
	return nil
}

func (s *WorkerStrategy) CheckHealthAdvanced() (latency int64, exitIP string, err error) {
	l := s.logger.With().Str("func", "CheckHealthAdvanced").Logger()
	l.Debug().Msg("Starting advanced health check...")

	pipeConn, err := s.GetSocksConnection()
	if err != nil {
		l.Error().Err(err).Msg("Failed to get pipe connection.")
		return -1, "", fmt.Errorf("worker CheckHealth: failed to get pipe connection: %w", err)
	}
	defer pipeConn.Close()

	dialer, err := proxy.SOCKS5("tcp", "placeholder-unused:1080", nil, &pipeDialer{conn: pipeConn})
	if err != nil {
		l.Error().Err(err).Msg("Failed to create SOCKS5 dialer.")
		return -1, "", fmt.Errorf("worker CheckHealth: failed to create SOCKS5 dialer: %w", err)
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			l.Debug().Str("network", network).Str("addr", addr).Msg("Transport DialContext called.")
			return dialer.Dial(network, addr)
		},
		DisableKeepAlives: true,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}

	const healthCheckURL = "https://ipinfo.io/ip"
	l.Debug().Str("url", healthCheckURL).Msg("Executing HTTP GET request...")
	start := time.Now()
	resp, err := client.Get(healthCheckURL)
	if err != nil {
		l.Error().Err(err).Msg("HTTP GET request failed.")
		return -1, "", fmt.Errorf("worker CheckHealth: http get failed: %w", err)
	}
	defer resp.Body.Close()

	latencyMs := time.Since(start).Milliseconds()
	l.Debug().Int64("latency_ms", latencyMs).Int("status_code", resp.StatusCode).Msg("HTTP GET request successful.")

	if resp.StatusCode != http.StatusOK {
		return latencyMs, "", fmt.Errorf("worker CheckHealth: unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		l.Error().Err(err).Msg("Failed to read response body.")
		return latencyMs, "", fmt.Errorf("worker CheckHealth: failed to read response body: %w", err)
	}

	exitIP = strings.TrimSpace(string(body))

	l.Debug().Msg("Advanced health check finished.")
	return latencyMs, exitIP, nil
}

// pipeDialer 实现了 proxy.Dialer 接口，但它的 Dial 方法总是返回预设的 net.Conn
type pipeDialer struct {
	conn net.Conn
}

func (d *pipeDialer) Dial(network, addr string) (net.Conn, error) {
	return d.conn, nil
}
