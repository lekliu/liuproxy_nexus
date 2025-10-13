package vless

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"github.com/rs/zerolog/log"
	"io"
	"net"
	"strconv"
	"sync"

	"liuproxy_gateway/internal/shared/types"
	xraynet "liuproxy_gateway/internal/xray_core/common/net"
	"liuproxy_gateway/internal/xray_core/common/serial"
	"liuproxy_gateway/internal/xray_core/transport/internet"
	"liuproxy_gateway/internal/xray_core/transport/internet/grpc"
	"liuproxy_gateway/internal/xray_core/transport/internet/tls"
)

// HandleGRPCConnection encapsulates all logic for handling a VLESS+gRPC connection.
// It creates a new connection to the remote server for each request.
func HandleGRPCConnection(
	ctx context.Context,
	clientConn net.Conn,
	reader *bufio.Reader,
	profile *types.ServerProfile,
	stateManager types.StateManager,
) {
	defer clientConn.Close()

	l := log.Ctx(ctx)
	l.Debug().Msg("VLESS-NATIVE-GRPC: New connection received, starting SOCKS5 handshake...")

	cmd, targetAddr, err := HandshakeSocks5AndGetResponse(clientConn, reader)
	if err != nil {
		l.Debug().Err(err).Msg("VLESS-NATIVE-GRPC: SOCKS5 handshake failed")
		return
	}
	if cmd != 1 {
		l.Debug().Int("command", int(cmd)).Msg("VLESS-NATIVE-GRPC: Received non-CONNECT command, closing.")
		return
	}
	l.Debug().Str("target", targetAddr).Msg("VLESS-NATIVE-GRPC: SOCKS5 handshake successful.")

	host, portStr, _ := net.SplitHostPort(targetAddr)
	port, _ := strconv.Atoi(portStr)

	l.Debug().Str("remote", profile.Address).Msg("VLESS-NATIVE-GRPC: Dialing new connection to remote...")
	remoteConn, err := DialVlessGRPC(context.Background(), profile)
	if err != nil {
		l.Error().Err(err).Str("remote", profile.Address).Msg("VLESS-NATIVE-GRPC: failed to dial remote")
		// 【新增】调用 StateManager 将服务器状态设置为 Down
		if stateManager != nil {
			stateManager.SetServerStatusDown(profile.ID, err.Error())
		}
		_, _ = clientConn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remoteConn.Close()
	l.Debug().Msg("VLESS-NATIVE-GRPC: Successfully connected to remote.")

	headerBuf := new(bytes.Buffer)
	// Temporarily use xraynet types for header encoding
	if err := EncodeRequestHeader(headerBuf, RequestCommandTCP, host, port, profile.UUID); err != nil {
		l.Error().Err(err).Msg("VLESS-NATIVE-GRPC: failed to encode request header")
		return
	}
	l.Debug().Hex("vless_header", headerBuf.Bytes()).Msg("VLESS-NATIVE-GRPC: Generated VLESS Header")

	l.Debug().Msg("VLESS-NATIVE-GRPC: Starting data piping.")
	var wg sync.WaitGroup
	wg.Add(2)

	// UPLINK: Client -> Remote
	go func() {
		defer wg.Done()
		firstChunk := make([]byte, 4096)
		n, _ := reader.Read(firstChunk)
		firstChunk = firstChunk[:n]

		finalPayload := append(headerBuf.Bytes(), firstChunk...)

		if _, err := remoteConn.Write(finalPayload); err != nil {
			l.Error().Err(err).Msg("VLESS-NATIVE-GRPC: [UPLINK] Failed to write combined initial payload.")
			return
		}

		bytesCopied, copyErr := io.Copy(remoteConn, clientConn)
		totalBytes := int64(len(finalPayload)) + bytesCopied
		l.Debug().Int64("bytes", totalBytes).Err(copyErr).Msg("VLESS-NATIVE-GRPC: [UPLINK] Piping finished.")
		CloseWriter(remoteConn)
	}()

	// DOWNLINK: Remote -> Client
	go func() {
		defer wg.Done()
		if err := DecodeResponseHeader(remoteConn); err != nil {
			l.Error().Err(err).Msg("VLESS-NATIVE-GRPC: [DOWNLINK] Failed to decode VLESS response header.")
			CloseWriter(clientConn)
			return
		}
		l.Debug().Msg("VLESS-NATIVE-GRPC: [DOWNLINK] VLESS response header decoded successfully.")

		bytes, err := io.Copy(clientConn, remoteConn)
		l.Debug().Int64("bytes", bytes).Err(err).Msg("VLESS-NATIVE-GRPC: [DOWNLINK] Piping remote to client finished.")
		CloseWriter(clientConn)
	}()

	wg.Wait()
	l.Debug().Msg("VLESS-NATIVE-GRPC: Connection handling finished.")
}

// dialVlessGRPC is the specific dialer for gRPC transport.
func DialVlessGRPC(ctx context.Context, profile *types.ServerProfile) (net.Conn, error) {
	// Use xraynet types as internet.Dial still requires them.
	dest := xraynet.TCPDestination(xraynet.ParseAddress(profile.Address), xraynet.Port(profile.Port))
	streamSettings := &internet.StreamConfig{
		ProtocolName: "grpc",
		TransportSettings: []*internet.TransportConfig{
			{
				ProtocolName: "grpc",
				Settings: serial.ToTypedMessage(&grpc.Config{
					ServiceName: profile.GrpcServiceName,
					MultiMode:   profile.GrpcMode == "multi",
					Authority:   profile.GrpcAuthority,
				}),
			},
		},
	}

	if profile.Security == "tls" {
		streamSettings.SecurityType = serial.GetMessageType(&tls.Config{})
		streamSettings.SecuritySettings = []*serial.TypedMessage{
			serial.ToTypedMessage(&tls.Config{
				ServerName:   profile.SNI,
				NextProtocol: []string{"h2"},
			}),
		}
	}

	memoryStreamConfig, err := internet.ToMemoryStreamConfig(streamSettings)
	if err != nil {
		return nil, fmt.Errorf("failed to create memory stream config: %w", err)
	}

	return internet.Dial(ctx, dest, memoryStreamConfig)
}
