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
	"liuproxy_gateway/internal/xray_core/transport/internet/tls"
	"liuproxy_gateway/internal/xray_core/transport/internet/websocket"
)

// HandleWSConnection 封装了处理 VLESS+WS 连接的所有逻辑。
// 它为每一个客户端请求创建一个新的到远程服务器的连接。
func HandleWSConnection(
	ctx context.Context,
	clientConn net.Conn,
	reader *bufio.Reader,
	profile *types.ServerProfile,
	stateManager types.StateManager,
) {
	defer clientConn.Close()

	l := log.Ctx(ctx)
	//l.Debug().Msg("VLESS-NATIVE-WS: New connection received, starting SOCKS5 handshake...")

	cmd, targetAddr, err := HandshakeSocks5AndGetResponse(clientConn, reader)
	if err != nil {
		//l.Debug().Err(err).Msg("VLESS-NATIVE-WS: SOCKS5 handshake failed")
		return
	}
	if cmd != 1 {
		//l.Debug().Int("command", int(cmd)).Msg("VLESS-NATIVE-WS: Received non-CONNECT command, closing.")
		return
	}
	//l.Debug().Str("target", targetAddr).Msg("VLESS-NATIVE-WS: SOCKS5 handshake successful.")

	host, portStr, _ := net.SplitHostPort(targetAddr)
	port, _ := strconv.Atoi(portStr)

	// 为本次请求建立一个全新的远程连接
	remoteConn, err := DialVlessWS(context.Background(), profile)
	if err != nil {
		l.Error().Err(err).Str("remote", profile.Address).Msg("VLESS-NATIVE-WS: failed to dial remote")
		// 【新增】调用 StateManager 将服务器状态设置为 Down
		if stateManager != nil {
			stateManager.SetServerStatusDown(profile.ID, err.Error())
		}
		_, _ = clientConn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remoteConn.Close()
	//l.Debug().Msg("VLESS-NATIVE-WS: Successfully connected to remote for this request.")

	headerBuf := new(bytes.Buffer)
	// Temporarily use xraynet types for header encoding
	if err := EncodeRequestHeader(headerBuf, RequestCommandTCP, host, port, profile.UUID); err != nil {
		l.Error().Err(err).Msg("VLESS-NATIVE-WS: failed to encode request header")
		return
	}
	//l.Debug().Hex("vless_header", headerBuf.Bytes()).Msg("VLESS-NATIVE-WS: Generated VLESS Header")

	//l.Debug().Msg("VLESS-NATIVE-WS: Starting data piping.")
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
			l.Error().Err(err).Msg("VLESS-NATIVE-WS: [UPLINK] Failed to write combined initial payload.")
			return
		}

		_, _ = io.Copy(remoteConn, clientConn)
		//totalBytes := int64(len(finalPayload)) + bytesCopied
		//l.Debug().Int64("bytes", totalBytes).Err(copyErr).Msg("VLESS-NATIVE-WS: [UPLINK] Piping finished.")
		CloseWriter(remoteConn)
	}()

	// DOWNLINK: Remote -> Client
	go func() {
		defer wg.Done()
		if err := DecodeResponseHeader(remoteConn); err != nil {
			l.Error().Err(err).Msg("VLESS-NATIVE-WS: [DOWNLINK] Failed to decode VLESS response header.")
			CloseWriter(clientConn)
			return
		}
		//l.Debug().Msg("VLESS-NATIVE-WS: [DOWNLINK] VLESS response header decoded successfully.")

		_, _ = io.Copy(clientConn, remoteConn)
		//l.Debug().Int64("bytes", bytes).Err(err).Msg("VLESS-NATIVE-WS: [DOWNLINK] Piping remote to client finished.")
		CloseWriter(clientConn)
	}()

	wg.Wait()
	//l.Debug().Msg("VLESS-NATIVE-WS: Connection handling finished.")
}

// dialVlessWS 是 WebSocket 传输的专用拨号器。
func DialVlessWS(ctx context.Context, profile *types.ServerProfile) (net.Conn, error) {
	// Use xraynet types as internet.Dial still requires them.
	dest := xraynet.TCPDestination(xraynet.ParseAddress(profile.Address), xraynet.Port(profile.Port))

	streamSettings := &internet.StreamConfig{
		ProtocolName: "websocket",
		TransportSettings: []*internet.TransportConfig{
			{
				ProtocolName: "websocket",
				Settings: serial.ToTypedMessage(&websocket.Config{
					Path:   profile.Path,
					Header: []*websocket.Header{{Key: "Host", Value: profile.Host}},
				}),
			},
		},
	}

	if profile.Security == "tls" {
		streamSettings.SecurityType = serial.GetMessageType(&tls.Config{})
		streamSettings.SecuritySettings = []*serial.TypedMessage{
			serial.ToTypedMessage(&tls.Config{
				ServerName:  profile.SNI,
				Fingerprint: profile.Fingerprint,
			}),
		}
	}

	memoryStreamConfig, err := internet.ToMemoryStreamConfig(streamSettings)
	if err != nil {
		return nil, fmt.Errorf("failed to create memory stream config: %w", err)
	}

	return internet.Dial(ctx, dest, memoryStreamConfig)
}
