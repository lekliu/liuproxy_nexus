package vless

import (
	"bufio"
	"context"
	"fmt"
	"liuproxy_nexus/internal/shared"
	"liuproxy_nexus/internal/shared/types"
	"net"
	"strconv"
)

// Initialize 启动一个本地 SOCKS5 监听器。
// 这个方法主要用于移动端等传统的转发代理场景。
func (s *VlessStrategyNative) Initialize() error {
	addr := s.profile.Address + ":" + strconv.Itoa(s.profile.LocalPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("native vless: failed to listen on %s: %w", addr, err)
	}
	s.listener = listener

	tcpAddr := s.listener.Addr().(*net.TCPAddr)
	s.listenerInfo = &types.ListenerInfo{
		Address: tcpAddr.IP.String(),
		Port:    tcpAddr.Port,
	}

	s.logger.Info().Str("listen_addr", s.listener.Addr().String()).Msg("Strategy listener started")

	s.waitGroup.Add(1)
	go s.acceptLoop()

	return nil
}

// GetSocksConnection 为 Gateway 的转发代理模式提供一个内存中的 SOCKS5 连接。
func (s *VlessStrategyNative) GetSocksConnection() (net.Conn, error) {
	//s.logger.Debug().Msg("VLESS: Creating new in-memory pipe for Gateway.")
	clientPipe, serverPipe := net.Pipe()

	s.activeConnections.Add(1)
	go func() {
		countedPipe := shared.NewCountedConn(serverPipe, &s.uplinkBytes, &s.downlinkBytes)
		defer s.activeConnections.Add(-1)
		ctx := s.logger.WithContext(context.Background())
		// HandleConnection 包含了完整的 SOCKS5 握手和后续逻辑。
		// 它将 serverPipe 视为一个标准的 net.Conn。
		HandleConnection(ctx, countedPipe, bufio.NewReader(countedPipe), s.profile, s.stateManager)
	}()

	return clientPipe, nil
}
