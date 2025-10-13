package vless

import (
	"bufio"
	"context"
	"github.com/rs/zerolog/log"
	"net"

	"liuproxy_gateway/internal/shared/types"
)

// HandleConnection 是 VLESS 原生策略的统一连接处理器。
// 它接收一个带有 logger 的 context，并根据 profile 中的网络类型进行分发。
func HandleConnection(
	ctx context.Context,
	clientConn net.Conn,
	reader *bufio.Reader,
	profile *types.ServerProfile,
	stateManager types.StateManager,
) {
	network := profile.Network
	if network == "" {
		network = "ws" // 默认为 ws
	}

	switch network {
	case "grpc":
		HandleGRPCConnection(ctx, clientConn, reader, profile, stateManager)
	case "ws":
		HandleWSConnection(ctx, clientConn, reader, profile, stateManager)
	default:
		log.Ctx(ctx).Error().Str("network", network).Msg("Unsupported network type for VLESS native strategy")
		clientConn.Close()
	}
}
