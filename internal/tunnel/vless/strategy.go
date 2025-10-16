package vless

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/proxy"
	"io"
	"liuproxy_nexus/internal/shared/logger"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"liuproxy_nexus/internal/shared/types"
)

// VlessStrategyNative 现在是一个无状态的监听器和分发器。
type VlessStrategyNative struct {
	config            *types.Config
	profile           *types.ServerProfile
	listener          net.Listener
	listenerInfo      *types.ListenerInfo
	closeOnce         sync.Once
	waitGroup         sync.WaitGroup
	activeConnections atomic.Int64
	logger            zerolog.Logger
	activeConns       sync.Map // 用于追踪所有活跃的客户端连接
	uplinkBytes       atomic.Uint64
	downlinkBytes     atomic.Uint64
	stateManager      types.StateManager
}

// NewVlessStrategyNative 创建一个新的 VLESS 原生策略实例。
func NewVlessStrategy(cfg *types.Config, profile *types.ServerProfile, stateManager types.StateManager) (types.TunnelStrategy, error) {
	logger.Info().Str("implementation", "native").Msg("Creating VLESS strategy")

	// 检查 profile 是否有效
	if profile == nil {
		return nil, fmt.Errorf("vless strategy requires a non-nil profile")
	}

	return &VlessStrategyNative{
		config:       cfg,
		profile:      profile,
		stateManager: stateManager,
		logger: log.With().
			Str("strategy_type", "vless-native").
			Str("server_id", profile.ID).
			Str("remarks", profile.Remarks).Logger(),
	}, nil
}

// InitializeForGateway 在网关模式下被调用。对于vless这种无状态策略，这是一个空操作。
func (s *VlessStrategyNative) InitializeForGateway() error {
	s.logger.Debug().Msg("VLESS: Initializing for Gateway (no-op).")
	return nil
}

func (s *VlessStrategyNative) acceptLoop() {
	defer s.waitGroup.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.logger.Debug().Err(err).Msgf("Listener on %s stopped accepting", s.listener.Addr())
			return
		}
		s.activeConns.Store(conn, struct{}{}) // 注册连接
		s.waitGroup.Add(1)
		go s.handleClientConnection(conn)
	}
}

// handleClientConnection 是旧的处理逻辑，保持不变以供 acceptLoop 使用
func (s *VlessStrategyNative) handleClientConnection(clientConn net.Conn) {
	defer s.activeConns.Delete(clientConn) // 注销连接
	s.activeConnections.Add(1)
	defer s.waitGroup.Done()
	defer s.activeConnections.Add(-1)

	// 将子 logger 注入到 vless 包的处理函数中
	ctx := s.logger.WithContext(context.Background())
	HandleConnection(ctx, clientConn, bufio.NewReader(clientConn), s.profile, s.stateManager)
}

// HandleTraffic 存在以满足接口，但在当前的网关方案中不被直接调用。
func (s *VlessStrategyNative) HandleTraffic(inboundConn net.Conn, reader *bufio.Reader, targetDest string, proto types.Protocol) {
	// This logic path is deprecated in favor of GetSocksConnection but kept for interface satisfaction.
	s.logger.Warn().Msg("VLESS Strategy: HandleTraffic is called, which is unexpected in the new architecture.")
	inboundConn.Close()
}

func (s *VlessStrategyNative) GetType() string { return "vless" }

func (s *VlessStrategyNative) CloseTunnel() {
	s.closeOnce.Do(func() {
		if s.listener != nil {
			s.listener.Close()
		}
		// 在等待 WaitGroup 之前，强制关闭所有活动的连接
		s.activeConns.Range(func(key, value interface{}) bool {
			if conn, ok := key.(net.Conn); ok {
				conn.Close()
			}
			return true
		})
		s.waitGroup.Wait()
	})
}

func (s *VlessStrategyNative) GetListenerInfo() *types.ListenerInfo {
	if s.listener == nil {
		return nil // In Gateway mode, there is no listener.
	}
	return s.listenerInfo
}

func (s *VlessStrategyNative) GetMetrics() *types.Metrics {
	return &types.Metrics{ActiveConnections: s.activeConnections.Load()}
}

func (s *VlessStrategyNative) UpdateServer(profile *types.ServerProfile) error {
	s.profile = profile
	s.logger = log.With().
		Str("strategy_type", "vless-native").
		Str("server_id", profile.ID).
		Str("remarks", profile.Remarks).Logger()
	s.logger.Info().Msg("VLESS native profile updated. New settings will apply to subsequent connections.")
	return nil
}

// CheckHealth (旧接口实现): 轻量级TCP拨号，用于移动端。
func (s *VlessStrategyNative) CheckHealth() error {
	var conn net.Conn
	var err error
	network := s.profile.Network
	if network == "" {
		network = "ws"
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	switch network {
	case "grpc":
		conn, err = DialVlessGRPC(ctx, s.profile)
	case "ws":
		conn, err = DialVlessWS(ctx, s.profile)
	default:
		err = fmt.Errorf("unsupported network type for health check: %s", network)
	}
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// pipeDialer 实现了 proxy.Dialer 接口，但它的 Dial 方法总是返回预设的 net.Conn
type pipeDialer struct {
	conn net.Conn
}

func (d *pipeDialer) Dial(network, addr string) (net.Conn, error) {
	return d.conn, nil
}

func (s *VlessStrategyNative) CheckHealthAdvanced() (latency int64, exitIP string, err error) {
	pipeConn, err := s.GetSocksConnection()
	if err != nil {
		return -1, "", fmt.Errorf("vless CheckHealth: failed to get pipe connection: %w", err)
	}
	defer pipeConn.Close()

	dialer, err := proxy.SOCKS5("tcp", "placeholder-unused:1080", nil, &pipeDialer{conn: pipeConn})
	if err != nil {
		pipeConn.Close()
		return -1, "", fmt.Errorf("vless CheckHealth: failed to create SOCKS5 dialer: %w", err)
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}

	const healthCheckURL = "https://www.cloudflare.com/cdn-cgi/trace"
	start := time.Now()
	resp, err := client.Get(healthCheckURL)
	if err != nil {
		return -1, "", fmt.Errorf("vless CheckHealth: http get failed: %w", err)
	}
	defer resp.Body.Close()

	latencyMs := time.Since(start).Milliseconds()

	if resp.StatusCode != http.StatusOK {
		return latencyMs, "", fmt.Errorf("vless CheckHealth: unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return latencyMs, "", fmt.Errorf("vless CheckHealth: failed to read response body: %w", err)
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

func (s *VlessStrategyNative) GetTrafficStats() types.TrafficStats {
	return types.TrafficStats{
		Uplink:   s.uplinkBytes.Load(),
		Downlink: s.downlinkBytes.Load(),
	}
}
