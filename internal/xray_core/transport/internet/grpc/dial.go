package grpc

import (
	"context"
	"liuproxy_gateway/internal/shared/logger"
	"liuproxy_gateway/internal/xray_core/common/errors"
	gonet "net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"liuproxy_gateway/internal/xray_core/common"
	"liuproxy_gateway/internal/xray_core/common/net"
	"liuproxy_gateway/internal/xray_core/common/session"
	"liuproxy_gateway/internal/xray_core/transport/internet"
	"liuproxy_gateway/internal/xray_core/transport/internet/grpc/encoding"
	"liuproxy_gateway/internal/xray_core/transport/internet/reality"
	"liuproxy_gateway/internal/xray_core/transport/internet/tls"
)

func Dial(ctx context.Context, dest net.Destination, streamSettings *internet.MemoryStreamConfig) (net.Conn, error) {
	logger.Debug().Msgf("creating connection to %s", dest)

	conn, err := dialgRPC(ctx, dest, streamSettings)
	if err != nil {

		return nil, errors.NewError("failed to dial gRPC").Base(err)
	}
	return conn, nil
}

func init() {
	common.Must(internet.RegisterTransportDialer(protocolName, Dial))
}

func dialgRPC(ctx context.Context, dest net.Destination, streamSettings *internet.MemoryStreamConfig) (net.Conn, error) {
	grpcSettings := streamSettings.ProtocolSettings.(*Config)

	// --- 新增内联的拨号逻辑 ---
	tlsConfig := tls.ConfigFromStreamSettings(streamSettings)
	realityConfig := reality.ConfigFromStreamSettings(streamSettings)
	sockopt := streamSettings.SocketSettings

	dialOptions := []grpc.DialOption{
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff: backoff.Config{
				BaseDelay:  500 * time.Millisecond,
				Multiplier: 1.5,
				Jitter:     0.2,
				MaxDelay:   19 * time.Second,
			},
			MinConnectTimeout: 5 * time.Second,
		}),
		grpc.WithContextDialer(func(gctx context.Context, s string) (gonet.Conn, error) {
			select {
			case <-gctx.Done():
				return nil, gctx.Err()
			default:
			}

			rawHost, rawPort, err := net.SplitHostPort(s)
			if err != nil {
				return nil, err
			}
			if len(rawPort) == 0 {
				rawPort = "443"
			}
			port, err := net.PortFromString(rawPort)
			if err != nil {
				return nil, err
			}
			address := net.ParseAddress(rawHost)

			gctx = session.ContextWithID(gctx, session.IDFromContext(ctx))
			gctx = session.ContextWithOutbound(gctx, session.OutboundFromContext(ctx))
			gctx = session.ContextWithTimeoutOnly(gctx, true)

			c, err := internet.DialSystem(gctx, net.TCPDestination(address, port), sockopt)
			if err == nil {
				if tlsConfig != nil {
					config := tlsConfig.GetTLSConfig()
					if config.ServerName == "" && address.Family().IsDomain() {
						config.ServerName = address.Domain()
					}
					if fingerprint := tls.GetFingerprint(tlsConfig.Fingerprint); fingerprint != nil {
						return tls.UClient(c, config, fingerprint), nil
					} else { // Fallback to normal gRPC TLS
						return tls.Client(c, config), nil
					}
				}
				if realityConfig != nil {
					return reality.UClient(c, realityConfig, gctx, dest)
				}
			}
			return c, err
		}),
	}

	dialOptions = append(dialOptions, grpc.WithTransportCredentials(insecure.NewCredentials()))

	authority := ""
	if grpcSettings.Authority != "" {
		authority = grpcSettings.Authority
	} else if tlsConfig != nil && tlsConfig.ServerName != "" {
		authority = tlsConfig.ServerName
	} else if realityConfig == nil && dest.Address.Family().IsDomain() {
		authority = dest.Address.Domain()
	}
	dialOptions = append(dialOptions, grpc.WithAuthority(authority))

	if grpcSettings.IdleTimeout > 0 || grpcSettings.HealthCheckTimeout > 0 || grpcSettings.PermitWithoutStream {
		dialOptions = append(dialOptions, grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                time.Second * time.Duration(grpcSettings.IdleTimeout),
			Timeout:             time.Second * time.Duration(grpcSettings.HealthCheckTimeout),
			PermitWithoutStream: grpcSettings.PermitWithoutStream,
		}))
	}

	if grpcSettings.InitialWindowsSize > 0 {
		dialOptions = append(dialOptions, grpc.WithInitialWindowSize(grpcSettings.InitialWindowsSize))
	}

	if grpcSettings.UserAgent != "" {
		dialOptions = append(dialOptions, grpc.WithUserAgent(grpcSettings.UserAgent))
	}

	var grpcDestHost string
	if dest.Address.Family().IsDomain() {
		grpcDestHost = dest.Address.Domain()
	} else {
		grpcDestHost = dest.Address.IP().String()
	}

	conn, err := grpc.Dial(
		gonet.JoinHostPort(grpcDestHost, dest.Port.String()),
		dialOptions...,
	)
	if err != nil {
		return nil, errors.NewError("Cannot dial gRPC").Base(err)
	}

	client := encoding.NewGRPCServiceClient(conn)
	if grpcSettings.MultiMode {
		logger.Debug().Msg("using gRPC multi mode service name: `" + grpcSettings.getServiceName() + "` stream name: `" + grpcSettings.getTunMultiStreamName() + "`")
		grpcService, err := client.(encoding.GRPCServiceClientX).TunMultiCustomName(ctx, grpcSettings.getServiceName(), grpcSettings.getTunMultiStreamName())
		if err != nil {
			return nil, errors.NewError("Cannot dial gRPC").Base(err)
		}
		return encoding.NewMultiHunkConn(grpcService, nil), nil
	}

	logger.Debug().Msg("using gRPC tun mode service name: `" + grpcSettings.getServiceName() + "` stream name: `" + grpcSettings.getTunStreamName() + "`")
	grpcService, err := client.(encoding.GRPCServiceClientX).TunCustomName(ctx, grpcSettings.getServiceName(), grpcSettings.getTunStreamName())
	if err != nil {
		return nil, errors.NewError("Cannot dial gRPC").Base(err)
	}

	return encoding.NewHunkConn(grpcService, nil), nil
}
