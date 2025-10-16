package gateway

import (
	"bufio"
	"io"
	"liuproxy_nexus/internal/shared/logger"
	"net"
	"sync"
	"time"
)

// VirtualStrategy 定义了不需要监听端口的特殊处理策略（如直连、拒绝）的接口。
type VirtualStrategy interface {
	// Handle 处理一个入站连接，根据策略逻辑决定如何处理。
	// initialReader 包含了可能已从 inboundConn 中预读的数据。
	// target 是从流量中嗅探出的原始目标地址。
	Handle(inboundConn net.Conn, initialReader *bufio.Reader, target net.Addr)
}

// --- DirectStrategy: 实现直连逻辑 ---

type DirectStrategy struct{}

// NewDirectStrategy 创建一个直连策略实例。
func NewDirectStrategy() VirtualStrategy {
	return &DirectStrategy{}
}

// Handle 实现了 VirtualStrategy 接口。
func (s *DirectStrategy) Handle(inboundConn net.Conn, initialReader *bufio.Reader, target net.Addr) {
	defer inboundConn.Close()

	targetAddr := target.String()
	logger.Debug().
		Str("client_ip", inboundConn.RemoteAddr().String()).
		Str("target_addr", targetAddr).
		Msg("Gateway: [DIRECT] Handling direct connection.")

	// 1. 连接到原始目标地址
	outboundConn, err := net.DialTimeout(target.Network(), targetAddr, 10*time.Second)
	if err != nil {
		logger.Error().
			Err(err).
			Str("target_addr", targetAddr).
			Msg("Gateway: [DIRECT] Failed to dial target.")
		return
	}
	defer outboundConn.Close()

	// 2. 双向转发数据
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		// 如果 initialReader 为 nil (例如在透明代理模式下)，则直接使用 inboundConn。
		var reader io.Reader = inboundConn

		// 从正确的 reader 拷贝到目标连接
		io.Copy(outboundConn, reader)

		if tcpConn, ok := outboundConn.(*net.TCPConn); ok {
			tcpConn.CloseWrite()
		}
	}()

	go func() {
		defer wg.Done()
		// 从目标连接拷贝回客户端连接 (这部分逻辑是正确的)
		io.Copy(inboundConn, outboundConn)
		if tcpConn, ok := inboundConn.(*net.TCPConn); ok {
			tcpConn.CloseWrite()
		}
	}()

	wg.Wait()
}

// --- RejectStrategy: 实现拒绝逻辑 ---

type RejectStrategy struct{}

// NewRejectStrategy 创建一个拒绝策略实例。
func NewRejectStrategy() VirtualStrategy {
	return &RejectStrategy{}
}

// Handle 实现了 VirtualStrategy 接口，直接关闭连接。
func (s *RejectStrategy) Handle(inboundConn net.Conn, initialReader *bufio.Reader, target net.Addr) {
	logger.Debug().
		Str("client_ip", inboundConn.RemoteAddr().String()).
		Str("target_addr", target.String()).
		Msg("Gateway: [REJECT] Rejecting connection.")
	inboundConn.Close()
}
