package gateway

import (
	"bufio"
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"liuproxy_nexus/internal/service/web"
	"liuproxy_nexus/internal/shared/logger"
	"liuproxy_nexus/internal/shared/types"
	"net"
	"strings"
	"sync"
	"time"
)

type Protocol string

//const (
//	ProtoSOCKS5  Protocol = "SOCKS5"
//	ProtoHTTP    Protocol = "HTTP"
//	ProtoTLS     Protocol = "TLS"
//	ProtoUnknown Protocol = "UNKNOWN"
//)

type Gateway struct {
	listener     net.Listener
	listenerInfo *types.ListenerInfo // 新增: 存储监听信息
	dispatcher   types.Dispatcher
	hub          *web.Hub
	closeOnce    sync.Once
	waitGroup    sync.WaitGroup
	listenPort   int
	directConn   VirtualStrategy
	rejectConn   VirtualStrategy
}

func New(listenPort int, dispatcher types.Dispatcher, hub *web.Hub) *Gateway {
	return &Gateway{
		listenPort: listenPort,
		dispatcher: dispatcher,
		hub:        hub,
		directConn: NewDirectStrategy(),
		rejectConn: NewRejectStrategy(),
	}
}

// InitializeListener 负责监听端口并准备服务，但不阻塞。
// 它返回实际监听的端口号。
func (g *Gateway) InitializeListener() (int, error) {
	// 如果 listenPort 为 0, net.Listen 会选择一个可用的动态端口
	listenAddr := fmt.Sprintf("0.0.0.0:%d", g.listenPort)
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return 0, fmt.Errorf("gateway failed to listen on %s: %w", listenAddr, err)
	}
	g.listener = listener

	// 存储监听器信息
	tcpAddr := g.listener.Addr().(*net.TCPAddr)
	g.listenerInfo = &types.ListenerInfo{
		Address: tcpAddr.IP.String(),
		Port:    tcpAddr.Port,
	}
	logger.Info().Str("listen_addr", g.listener.Addr().String()).Msg(">>> Gateway is listening on unified port.")

	return g.listenerInfo.Port, nil
}

// Serve 启动阻塞的 accept 循环。必须在 InitializeListener 之后调用。
func (g *Gateway) Serve() {
	if g.listener == nil {
		logger.Error().Msg("Gateway.Serve() called before InitializeListener()")
		return
	}
	g.waitGroup.Add(1)
	g.acceptLoop()
}

// GetListenerInfo 返回网关的监听信息。
func (g *Gateway) GetListenerInfo() *types.ListenerInfo {
	return g.listenerInfo
}

// Start 是旧的启动方法，现在封装了新流程以保持向后兼容。
func (g *Gateway) Start() error {
	if _, err := g.InitializeListener(); err != nil {
		return err
	}
	g.Serve()
	return nil
}

func (g *Gateway) acceptLoop() {
	defer g.waitGroup.Done()
	for {
		conn, err := g.listener.Accept()
		if err != nil {
			if opErr, ok := err.(*net.OpError); ok && strings.Contains(opErr.Err.Error(), "use of closed network connection") {
				logger.Info().Msg("Gateway listener is closing.")
				return
			}
			logger.Warn().Err(err).Msg("Gateway failed to accept connection")
			continue
		}
		g.waitGroup.Add(1)
		go g.handleConnection(conn)
	}
}

func (g *Gateway) handleConnection(inboundConn net.Conn) {
	defer g.waitGroup.Done()
	defer inboundConn.Close()

	traceID := uuid.NewString()
	l := log.With().Str("trace_id", traceID).Logger()
	ctx := l.WithContext(context.Background())
	clientIP := inboundConn.RemoteAddr().String()
	inboundReader := bufio.NewReader(inboundConn)

	targetDest, proto, req, err := sniffTargetForRouting(inboundConn, inboundReader)
	if err != nil {
		l.Warn().Err(err).Str("client_ip", clientIP).Msg("Could not determine target")
		return
	}
	//l.Debug().Str("proto", string(proto)).Str("client_ip", clientIP).Str("target", targetDest).Msg("Gateway: Sniffed target for routing")

	// 【流量日志】记录拦截
	g.hub.BroadcastTrafficLog(&web.TrafficLogEntry{
		Timestamp:   time.Now(),
		ClientIP:    clientIP,
		Protocol:    string(proto),
		Destination: targetDest,
		Action:      "Intercepted",
	})

	strategy, serverID, err := g.dispatcher.Dispatch(ctx, inboundConn.RemoteAddr(), targetDest)
	if err != nil {
		l.Warn().Err(err).Str("client_ip", clientIP).Str("target", targetDest).Msg("Gateway: Dispatcher returned error")
		return
	}

	if strategy == nil {
		targetNetAddr, _ := net.ResolveTCPAddr("tcp", targetDest)
		// 【流量日志】更新决策
		g.hub.BroadcastTrafficLog(&web.TrafficLogEntry{
			Timestamp:   time.Now(),
			ClientIP:    clientIP,
			Protocol:    string(proto),
			Destination: targetDest,
			Action:      "Decided",
			Target:      serverID,
		})

		// 如果协议是 SOCKS5，必须在这里发送成功响应！
		switch proto {
		case types.ProtoSOCKS5:
			// 发送 SOCKS5 CONNECT 成功响应
			if _, err := inboundConn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
				l.Warn().Err(err).Msg("Gateway: Failed to write SOCKS5 success reply for DIRECT connection")
				return
			}
		case types.ProtoHTTP:
			// 如果是 HTTP CONNECT 请求，发送 200 OK
			if req != nil && req.Method == "CONNECT" {
				if _, err := inboundConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
					l.Warn().Err(err).Msg("Gateway: Failed to write HTTP CONNECT success reply for DIRECT connection")
					return
				}
			}
			// 对于普通的 HTTP GET/POST 等请求，我们什么都不用发，直接转发请求本身即可
		}

		switch serverID {
		case "DIRECT":
			g.directConn.Handle(inboundConn, inboundReader, targetNetAddr)
		case "REJECT":
			g.rejectConn.Handle(inboundConn, inboundReader, targetNetAddr)
		}
		return
	}

	// 【流量日志】更新决策
	g.hub.BroadcastTrafficLog(&web.TrafficLogEntry{
		Timestamp:   time.Now(),
		ClientIP:    clientIP,
		Protocol:    string(proto),
		Destination: targetDest,
		Action:      "Decided",
		Target:      strategy.GetType() + " (" + serverID[:8] + ")",
	})

	tunnelBuilder, ok := strategy.(types.TunnelBuilder)
	if !ok {
		l.Error().Str("strategy_type", strategy.GetType()).Msg("Strategy does not implement TunnelBuilder interface.")
		return
	}

	switch proto {
	case types.ProtoSOCKS5:
		g.forwardSocks5(inboundConn, inboundReader, serverID, tunnelBuilder)
	case types.ProtoHTTP:
		g.handleHttpProxy(ctx, inboundConn, inboundReader, targetDest, serverID, tunnelBuilder)
	case types.ProtoTLS:
		g.forwardTCP(inboundConn, inboundReader, serverID, tunnelBuilder)
	default:
		l.Warn().Str("client_ip", clientIP).Msg("Unsupported protocol")
	}
}

func (g *Gateway) Close() {
	g.closeOnce.Do(func() {
		if g.listener != nil {
			g.listener.Close()
		}
		g.waitGroup.Wait()
		log.Info().Msg("Gateway has been shut down")
	})
}
