package vless

import (
	"bytes"
	"context"
	"io"
	"liuproxy_gateway/internal/shared"
	"liuproxy_gateway/internal/shared/types"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// HandleRawTCP 是处理透明代理 TCP 流量的入口。
func (s *VlessStrategyNative) HandleRawTCP(inboundConn net.Conn, targetDest string) {
	s.activeConnections.Add(1)
	defer s.activeConnections.Add(-1)

	l := s.logger.With().Str("target", targetDest).Logger()
	ctx := l.WithContext(context.Background())

	host, portStr, err := net.SplitHostPort(targetDest)
	if err != nil {
		l.Error().Err(err).Str("target", targetDest).Msg("VLESS-RAW: Failed to parse target destination")
		return
	}
	port, _ := strconv.Atoi(portStr)

	// 用 CountedConn 包装 inboundConn
	countedInbound := shared.NewCountedConn(inboundConn, &s.uplinkBytes, &s.downlinkBytes)

	// 1. Dial a new remote connection (gRPC or WS)
	var remoteConn net.Conn
	network := s.profile.Network
	if network == "" {
		network = "ws" // Default to ws
	}
	dialCtx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	switch network {
	case "grpc":
		remoteConn, err = DialVlessGRPC(dialCtx, s.profile)
	case "ws":
		remoteConn, err = DialVlessWS(dialCtx, s.profile)
	default:
		l.Error().Str("network", network).Msg("VLESS-RAW: Unsupported network type")
		return
	}

	if err != nil {
		l.Error().Err(err).Msg("VLESS-RAW: Failed to dial remote")
		if s.stateManager != nil {
			s.stateManager.SetServerStatusDown(s.profile.ID, err.Error())
		}
		return
	}
	defer remoteConn.Close()

	// 2. Encode VLESS header with targetDest
	headerBuf := new(bytes.Buffer)
	if err := EncodeRequestHeader(headerBuf, RequestCommandTCP, host, port, s.profile.UUID); err != nil {
		l.Error().Err(err).Msg("VLESS-RAW: Failed to encode request header")
		return
	}

	// 3. Pipe data between inboundConn and remoteConn
	var wg sync.WaitGroup
	wg.Add(2)

	// Uplink
	go func() {
		defer wg.Done()
		// Create a reader that first reads the VLESS header, then the rest of the inbound connection.
		reader := io.MultiReader(headerBuf, countedInbound) // 使用 countedInbound
		bytesCopied, copyErr := io.Copy(remoteConn, reader)
		log.Ctx(ctx).Debug().Int64("bytes", bytesCopied).Err(copyErr).Msg("VLESS-RAW: [UPLINK] Piping finished.")
		CloseWriter(remoteConn)
	}()

	// Downlink
	go func() {
		defer wg.Done()
		if err := DecodeResponseHeader(remoteConn); err != nil {
			log.Ctx(ctx).Error().Err(err).Msg("VLESS-RAW: [DOWNLINK] Failed to decode VLESS response header.")
			CloseWriter(countedInbound)
			return
		}
		bytesCopied, copyErr := io.Copy(countedInbound, remoteConn)
		log.Ctx(ctx).Debug().Int64("bytes", bytesCopied).Err(copyErr).Msg("VLESS-RAW: [DOWNLINK] Piping finished.")
		CloseWriter(countedInbound)
	}()

	wg.Wait()
}

// HandleUDPPacket 是处理透明代理 UDP 流量的入口。
func (s *VlessStrategyNative) HandleUDPPacket(packet *types.UDPPacket, sessionKey string) error {
	s.logger.Warn().Str("session_key", sessionKey).Msg("VLESS strategy does not support UDP. Packet dropped.")
	return nil // 返回 nil 表示我们已经“处理”了这个包（即丢弃）
}
