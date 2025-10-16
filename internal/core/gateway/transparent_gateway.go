// FILE: internal/core/gateway/transparent_gateway.go
package gateway

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"liuproxy_nexus/internal/firewall"
	"liuproxy_nexus/internal/service/web"
	"liuproxy_nexus/internal/shared/logger"
	"liuproxy_nexus/internal/shared/settings"
	"liuproxy_nexus/internal/shared/types"
	"liuproxy_nexus/internal/sys/tproxy"
	"net"
	"strings"
	"sync"
	"time"
)

// TransparentGateway 负责处理被iptables等工具重定向的透明流量。
type TransparentGateway struct {
	listenPort  int
	tcpListener net.Listener
	udpListener net.PacketConn
	firewall    firewall.Firewall
	dispatcher  types.Dispatcher
	hub         *web.Hub
	direct      VirtualStrategy
	reject      VirtualStrategy
	closeOnce   sync.Once
	waitGroup   sync.WaitGroup
}

// NewTransparent 创建一个新的 TransparentGateway 实例。
func NewTransparent(port int, fw firewall.Firewall, disp types.Dispatcher, hub *web.Hub) *TransparentGateway {
	return &TransparentGateway{
		listenPort: port,
		firewall:   fw,
		dispatcher: disp,
		hub:        hub,
		direct:     NewDirectStrategy(), // 复用现有的 direct 策略
		reject:     NewRejectStrategy(), // 复用现有的 reject 策略
	}
}

func (g *TransparentGateway) Start() error {
	addr := fmt.Sprintf("0.0.0.0:%d", g.listenPort)

	// 启动 TCP 监听
	tcpListener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("transparent gateway failed to listen TCP on %s: %w", addr, err)
	}
	g.tcpListener = tcpListener
	logger.Info().Str("listen_addr", tcpListener.Addr().String()).Msg(">>> Transparent Gateway is listening for TCP.")

	// 启动 UDP 监听
	udpListener, err := net.ListenPacket("udp", addr)
	if err != nil {
		tcpListener.Close()
		return fmt.Errorf("transparent gateway failed to listen UDP on %s: %w", addr, err)
	}
	g.udpListener = udpListener
	logger.Info().Str("listen_addr", udpListener.LocalAddr().String()).Msg(">>> Transparent Gateway is listening for UDP.")

	g.waitGroup.Add(2) // 只为 TCP 和 UDP 循环
	go g.acceptTCPLoop()
	go g.acceptUDPLoop()
	// 【移除】不再启动 session cleanup loop

	return nil
}

// acceptLoop 更名为 acceptTCPLoop
func (g *TransparentGateway) acceptTCPLoop() {
	defer g.waitGroup.Done()
	for {
		conn, err := g.tcpListener.Accept()
		if err != nil {
			if opErr, ok := err.(*net.OpError); ok && strings.Contains(opErr.Err.Error(), "use of closed network connection") {
				logger.Info().Msg("Transparent Gateway TCP listener is closing.")
				return
			}
			logger.Warn().Err(err).Msg("Transparent Gateway failed to accept connection")
			continue
		}
		g.waitGroup.Add(1)
		go g.handleTCPConnection(conn)
	}
}

func (g *TransparentGateway) handleTCPConnection(inboundConn net.Conn) {
	defer g.waitGroup.Done()
	defer inboundConn.Close()

	traceID := uuid.NewString()
	l := log.With().Str("trace_id", traceID).Logger()
	ctx := l.WithContext(context.Background())

	originalDst, err := tproxy.GetOriginalDst(inboundConn)
	if err != nil {
		l.Error().Err(err).Str("client_ip", inboundConn.RemoteAddr().String()).Msg("TPROXY: Failed to get original destination")
		return
	}
	//l.Info().Str("client_ip", inboundConn.RemoteAddr().String()).Str("original_dest", originalDst.String()).Msg("TPROXY: Intercepted connection")

	// 【流量日志】记录拦截
	g.hub.BroadcastTrafficLog(&web.TrafficLogEntry{
		Timestamp:   time.Now(),
		ClientIP:    inboundConn.RemoteAddr().String(),
		Protocol:    "tproxy_tcp",
		Destination: originalDst.String(),
		Action:      "Intercepted",
	})

	// 1. 防火墙检查
	meta := &firewall.ConnectionMetadata{
		Protocol:    "tcp",
		Source:      inboundConn.RemoteAddr(),
		Destination: originalDst,
	}
	action := g.firewall.Check(meta)
	if action == settings.ActionDeny {
		l.Warn().Str("dest", originalDst.String()).Msg("TPROXY: Connection denied by firewall")
		// 【流量日志】更新决策
		g.hub.BroadcastTrafficLog(&web.TrafficLogEntry{
			Timestamp:   time.Now(),
			ClientIP:    inboundConn.RemoteAddr().String(),
			Protocol:    "tproxy_tcp",
			Destination: originalDst.String(),
			Action:      "Denied",
			Target:      "Firewall",
		})
		return
	}

	// 2. 调度器分流
	targetDestStr := originalDst.String()
	strategy, serverID, err := g.dispatcher.Dispatch(ctx, inboundConn.RemoteAddr(), targetDestStr)
	if err != nil {
		l.Warn().Err(err).Str("target", targetDestStr).Msg("TPROXY: Dispatcher returned error")
		return
	}

	// 3. 执行决策
	if strategy == nil {
		l.Debug().Str("decision", serverID).Msg("TPROXY: Dispatching to virtual strategy")
		// 【流量日志】更新决策
		g.hub.BroadcastTrafficLog(&web.TrafficLogEntry{
			Timestamp:   time.Now(),
			ClientIP:    inboundConn.RemoteAddr().String(),
			Protocol:    "tproxy_tcp",
			Destination: targetDestStr,
			Action:      "Decided",
			Target:      serverID,
		})
		switch serverID {
		case "DIRECT":
			// 对于透明代理的直连，reader是nil，因为我们没有预读任何数据
			g.direct.Handle(inboundConn, nil, originalDst)
		case "REJECT":
			g.reject.Handle(inboundConn, nil, originalDst)
		}
		return
	}
	// 【流量日志】更新决策
	g.hub.BroadcastTrafficLog(&web.TrafficLogEntry{
		Timestamp:   time.Now(),
		ClientIP:    inboundConn.RemoteAddr().String(),
		Protocol:    "tproxy_tcp",
		Destination: targetDestStr,
		Action:      "Decided",
		Target:      strategy.GetType() + " (" + serverID[:8] + ")",
	})

	l.Debug().Str("strategy", strategy.GetType()).Str("server_id", serverID).Msg("TPROXY: Dispatching to strategy")
	// 将连接的控制权完全移交给策略实例
	strategy.HandleRawTCP(inboundConn, targetDestStr)
}

// 【新增】UDP 循环
func (g *TransparentGateway) acceptUDPLoop() {
	defer g.waitGroup.Done()
	buf := make([]byte, 4096)
	for {
		n, clientAddr, err := g.udpListener.ReadFrom(buf)
		if err != nil {
			if opErr, ok := err.(*net.OpError); ok && strings.Contains(opErr.Err.Error(), "use of closed network connection") {
				logger.Info().Msg("Transparent Gateway UDP listener is closing.")
				return
			}
			logger.Warn().Err(err).Msg("TPROXY-UDP: Failed to read from UDP listener")
			continue
		}

		payload := make([]byte, n)
		copy(payload, buf[:n])

		go g.handleUDPPacket(payload, clientAddr)
	}
}

// 【修改】无状态的 UDP 包处理器
func (g *TransparentGateway) handleUDPPacket(payload []byte, clientAddr net.Addr) {
	// 由于无法可靠获取原始目标地址，我们必须依赖 Dispatcher 的规则
	// 我们将使用一个“虚拟目标”来触发规则匹配。对于DNS，这通常是 8.8.8.8:53
	// 这要求用户必须配置一条匹配此虚拟目标的路由规则。
	const virtualTarget = "8.8.8.8:53"

	l := log.With().Str("client_ip", clientAddr.String()).Logger()
	ctx := l.WithContext(context.Background())

	// Dispatcher 分流
	strategy, _, err := g.dispatcher.Dispatch(ctx, clientAddr, virtualTarget)
	if err != nil || strategy == nil {
		l.Debug().Msg("TPROXY-UDP: No rule matched for virtual target. Packet dropped.")
		return
	}

	// 从路由规则的目标中获取真实的 UDP 目标地址
	// 这是个 hack：我们假设用户为 UDP 流量配置的规则，其 Value 字段就是真实的目标
	// 例如，规则: { "type": "dest_ip", "value": ["8.8.8.8/32"], "target": "goremote" }
	// 我们需要一种方法来知道该把包发到哪里。
	// 修正：我们直接把虚拟目标地址传递给策略。策略知道如何处理它。

	destUDPAddr, _ := net.ResolveUDPAddr("udp", virtualTarget)

	packet := &types.UDPPacket{
		Source:      clientAddr,
		Destination: destUDPAddr,
		Payload:     payload,
	}

	if err := strategy.HandleUDPPacket(packet, clientAddr.String()); err != nil {
		l.Warn().Err(err).Msg("TPROXY-UDP: Strategy failed to handle UDP packet.")
	}
}

// GetListener 返回网关的 UDP PacketConn 监听器
func (g *TransparentGateway) GetListener() net.PacketConn {
	return g.udpListener
}

func (g *TransparentGateway) Close() {
	g.closeOnce.Do(func() {
		if g.tcpListener != nil {
			g.tcpListener.Close()
		}
		if g.udpListener != nil {
			g.udpListener.Close()
		}
		g.waitGroup.Wait()
		log.Info().Msg("Transparent Gateway has been shut down")
	})
}
