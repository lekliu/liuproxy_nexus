package worker

import (
	"bufio"
	"fmt"
	"liuproxy_gateway/internal/shared"
	"liuproxy_gateway/internal/shared/globalstate"
	protocol2 "liuproxy_gateway/internal/shared/protocol"
	"liuproxy_gateway/internal/shared/types"
	"net"
	"sync"
)

// Initialize 启动一个本地 SOCKS5 监听器。
// 这个方法主要用于移动端等传统的转发代理场景。
func (s *WorkerStrategy) Initialize() error {
	addr := fmt.Sprintf("127.0.0.1:%d", s.profile.LocalPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("worker strategy failed to listen on %s: %w", err)
	}
	s.listener = listener

	tcpAddr := s.listener.Addr().(*net.TCPAddr)
	s.listenerInfo = &types.ListenerInfo{
		Address: tcpAddr.IP.String(),
		Port:    tcpAddr.Port,
	}

	//s.logger.Info().Str("strategy", "worker").Str("listen_addr", s.listener.Addr().String()).Msg("Strategy listener started")

	s.waitGroup.Add(1)
	go s.acceptLoop()

	return nil
}

// GetSocksConnection 为 Gateway 的转发代理模式提供一个内存中的 SOCKS5 连接。
func (s *WorkerStrategy) GetSocksConnection() (net.Conn, error) {
	//s.logger.Debug().Msg("Worker: Creating new in-memory pipe for Gateway.")
	clientPipe, serverPipe := net.Pipe()

	s.activeConnections.Add(1)
	s.waitGroup.Add(1)
	go func() {
		countedPipe := shared.NewCountedConn(serverPipe, &s.uplinkBytes, &s.downlinkBytes)
		defer s.waitGroup.Done()
		defer s.activeConnections.Add(-1)
		//s.logger.Debug().Str("func", "GetSocksConnection").Msg("Goroutine started, calling handleClientConnection.")
		s.handleClientConnection(countedPipe)
		//s.logger.Debug().Str("func", "GetSocksConnection").Msg("Goroutine finished, handleClientConnection returned.")
	}()

	return clientPipe, nil
}

// handleClientConnection 处理一个来自转发代理的客户端连接（无论是物理连接还是内存管道）。
func (s *WorkerStrategy) handleClientConnection(plainConn net.Conn) {
	l := s.logger.With().Str("func", "handleClientConnection").Logger()
	//l.Debug().Msg("Handling new client connection (pipe)...")

	reader := bufio.NewReader(plainConn)
	agent := NewAgent(s.config)

	//l.Debug().Msg("Starting SOCKS5 handshake with client (proxy.Dialer)...")
	cmd, targetAddr, err := agent.HandshakeWithClient(plainConn, reader)
	if err != nil {
		l.Error().Err(err).Msg("SOCKS5 handshake failed.")
		return
	}

	if cmd != 1 { // 1 = CONNECT
		if cmd == 3 { // 3 = UDP ASSOCIATE, Worker不支持
			_, _ = plainConn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		}
		//s.logger.Debug().Int("command", int(cmd)).Msg("Worker received unsupported SOCKS5 command")
		return
	}

	//l.Debug().Msg("Creating tunnel...")
	tunnelConn, cipher, err := s.createTunnel()
	if err != nil {
		l.Error().Err(err).Msg("Failed to create tunnel.")
		if s.stateManager != nil {
			s.stateManager.SetServerStatusDown(s.profile.ID, err.Error())
		}
		return
	}
	defer tunnelConn.Close()
	//l.Debug().Msg("Tunnel created.")

	const streamID uint16 = 1
	packet := protocol2.Packet{
		StreamID: streamID,
		Flag:     protocol2.FlagControlNewStreamTCP,
		Payload:  buildMetadataForWorker(1, targetAddr),
	}

	//l.Debug().Msg("Writing NewStream request to worker...")
	if err := protocol2.WriteSecurePacket(tunnelConn, &packet, cipher); err != nil {
		l.Error().Err(err).Msg("Failed to write NewStream request.")
		return
	}
	//l.Debug().Msg("NewStream request sent.")

	// ** DEADLOCK FIX: Send the SOCKS5 success reply in a new goroutine **
	// This prevents this function from blocking if the other end of the pipe isn't reading yet.
	go func() {
		l.Debug().Msg("Writing SOCKS5 success reply to client (proxy.Dialer) in a goroutine...")
		if _, err := plainConn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
			l.Error().Err(err).Msg("Failed to write SOCKS5 success reply.")
			// Closing the pipe is the best we can do to signal an error here.
			plainConn.Close()
			return
		}
		l.Debug().Msg("SOCKS5 success reply sent.")
	}()

	//l.Debug().Msg("Starting bidirectional copy.")
	globalstate.GlobalStatus.Set(fmt.Sprintf("Connected (Worker via %s)", s.profile.Address))

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		defer tunnelConn.Close()
		buf := make([]byte, s.config.CommonConf.BufferSize)
		for {
			n, err := plainConn.Read(buf)
			if err != nil {
				//l.Debug().Err(err).Msg("Uplink: read from pipe failed, closing stream.")
				closePacket := protocol2.Packet{StreamID: streamID, Flag: protocol2.FlagControlCloseStream}
				_ = protocol2.WriteSecurePacket(tunnelConn, &closePacket, cipher)
				return
			}
			//l.Debug().Int("bytes", n).Msg("Uplink: read from pipe, writing to worker.")
			packet := protocol2.Packet{StreamID: streamID, Flag: protocol2.FlagTCPData, Payload: buf[:n]}
			if err := protocol2.WriteSecurePacket(tunnelConn, &packet, cipher); err != nil {
				l.Error().Err(err).Msg("Uplink: write to worker failed.")
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		defer plainConn.Close() // Now, closing the pipe here is correct to terminate the other side
		firstPacket := true
		for {
			packet, err := protocol2.ReadUnsecurePacket(tunnelConn)
			if err != nil {
				//l.Debug().Err(err).Msg("Downlink: read from worker failed.")
				return
			}

			if firstPacket {
				firstPacket = false
				if packet.Flag == protocol2.FlagControlNewStreamTCPSuccess {
					//l.Debug().Msg("Downlink: received and discarded the success packet.")
					continue
				}
			}

			//l.Debug().Uint8("flag", packet.Flag).Int("payload_size", len(packet.Payload)).Msg("Downlink: packet received from worker.")
			if packet.Flag == protocol2.FlagTCPData {
				if _, err := plainConn.Write(packet.Payload); err != nil {
					l.Error().Err(err).Msg("Downlink: write to pipe failed.")
					return
				}
			} else if packet.Flag == protocol2.FlagControlCloseStream {
				//l.Debug().Msg("Downlink: received CloseStream, closing pipe.")
				return
			}
		}
	}()
	wg.Wait()
}
