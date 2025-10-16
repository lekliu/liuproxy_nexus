package worker

import (
	"io"
	"liuproxy_nexus/internal/shared"
	protocol2 "liuproxy_nexus/internal/shared/protocol"
	"liuproxy_nexus/internal/shared/types"
	"net"
	"sync"
	"time"
)

// HandleRawTCP 是处理透明代理 TCP 流量的入口。
func (s *WorkerStrategy) HandleRawTCP(inboundConn net.Conn, targetDest string) {
	s.logger.Debug().Str("target", targetDest).Msg("Worker: Handling raw TCP connection.")
	s.activeConnections.Add(1)
	defer s.activeConnections.Add(-1)

	// 用 CountedConn 包装 inboundConn
	countedInbound := shared.NewCountedConn(inboundConn, &s.uplinkBytes, &s.downlinkBytes)

	// 这个逻辑与 handleClientConnection 非常相似，但完全跳过了 SOCKS5 握手。

	tunnelConn, cipher, err := s.createTunnel()
	if err != nil {
		s.logger.Error().Err(err).Msg("[WorkerStrategy-RAW] Failed to create tunnel")
		if s.stateManager != nil {
			s.stateManager.SetServerStatusDown(s.profile.ID, err.Error())
		}
		return
	}
	defer tunnelConn.Close()

	const streamID uint16 = 1

	// 1. 发送 NewStream 控制包
	newStreamPacket := protocol2.Packet{
		StreamID: streamID,
		Flag:     protocol2.FlagControlNewStreamTCP,
		Payload:  buildMetadataForWorker(1, targetDest),
	}
	if err := protocol2.WriteSecurePacket(tunnelConn, &newStreamPacket, cipher); err != nil {
		s.logger.Error().Err(err).Msg("[WorkerStrategy-RAW] Failed to write NewStream request")
		return
	}

	// 2. 【关键修复】立即读取客户端的第一个数据块并发送
	// 为首次读取设置一个短暂的超时，以防客户端不发送任何数据
	_ = inboundConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	initialData := make([]byte, s.config.CommonConf.BufferSize)
	n, readErr := inboundConn.Read(initialData)
	_ = inboundConn.SetReadDeadline(time.Time{}) // 清除超时

	if n > 0 {
		s.logger.Debug().Int("bytes", n).Msg("[WorkerStrategy-RAW] Forwarding initial client data packet.")
		firstDataPacket := protocol2.Packet{
			StreamID: streamID,
			Flag:     protocol2.FlagTCPData,
			Payload:  initialData[:n],
		}
		if err := protocol2.WriteSecurePacket(tunnelConn, &firstDataPacket, cipher); err != nil {
			s.logger.Error().Err(err).Msg("[WorkerStrategy-RAW] Failed to write initial data packet")
			return
		}
	}
	// 如果首次读取出错(非超时)，也应继续，因为可能只是一个FIN包
	if readErr != nil && readErr != io.EOF && !isTimeoutError(readErr) {
		s.logger.Warn().Err(readErr).Msg("[WorkerStrategy-RAW] Error reading initial data from client")
	}

	// 3. 现在才等待 Worker 的确认
	if err := s.waitForSuccess(tunnelConn); err != nil {
		s.logger.Error().Err(err).Msg("[WorkerStrategy-RAW] Did not receive success from worker")
		return
	}

	// 4. 启动双向转发
	var wg sync.WaitGroup
	wg.Add(2)

	// Uplink (客户端 -> Worker)
	go func() {
		defer wg.Done()
		defer tunnelConn.Close() // 关闭写端以通知对端

		// 注意：这里的 inboundConn 已经被读取过一次了，io.Copy 会从剩余的数据开始
		buf := make([]byte, s.config.CommonConf.BufferSize)
		for {
			n, err := countedInbound.Read(buf)
			if err != nil {
				// 发送 CloseStream 包以优雅关闭
				closePacket := protocol2.Packet{StreamID: streamID, Flag: protocol2.FlagControlCloseStream}
				_ = protocol2.WriteSecurePacket(tunnelConn, &closePacket, cipher)
				return
			}
			packet := protocol2.Packet{StreamID: streamID, Flag: protocol2.FlagTCPData, Payload: buf[:n]}
			if err := protocol2.WriteSecurePacket(tunnelConn, &packet, cipher); err != nil {
				return
			}
		}
	}()

	// Downlink (Worker -> 客户端)
	go func() {
		defer wg.Done()
		defer countedInbound.Close()
		for {
			packet, err := protocol2.ReadUnsecurePacket(tunnelConn)
			if err != nil {
				return // 通常是EOF或连接关闭
			}

			if packet.Flag == protocol2.FlagTCPData {
				if _, err := countedInbound.Write(packet.Payload); err != nil {
					return
				}
			} else if packet.Flag == protocol2.FlagControlCloseStream {
				return
			}
		}
	}()

	wg.Wait()
}

// HandleUDPPacket 是处理透明代理 UDP 流量的入口。
func (s *WorkerStrategy) HandleUDPPacket(packet *types.UDPPacket, sessionKey string) error {
	s.logger.Warn().Str("session_key", sessionKey).Msg("Worker strategy does not support UDP. Packet dropped.")
	return nil // 返回 nil 表示我们已经“处理”了这个包（即丢弃）
}

// isTimeoutError 是一个辅助函数，用于检查错误是否为网络超时
func isTimeoutError(err error) bool {
	e, ok := err.(net.Error)
	return ok && e.Timeout()
}
