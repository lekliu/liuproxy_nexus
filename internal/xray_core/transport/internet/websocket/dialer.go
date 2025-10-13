// --- START OF COMPLETE REPLACEMENT for porting_vless/transport/internet/websocket/dialer.go ---
package websocket

import (
	"context"
	"liuproxy_gateway/internal/shared/logger"
	"liuproxy_gateway/internal/xray_core/common/errors"
	gonet "net"
	"time"

	"github.com/gorilla/websocket"
	"liuproxy_gateway/internal/xray_core/common"
	"liuproxy_gateway/internal/xray_core/common/net"
	"liuproxy_gateway/internal/xray_core/transport/internet"
	"liuproxy_gateway/internal/xray_core/transport/internet/tls"
)

// Dial dials a WebSocket connection to the given destination.
func Dial(ctx context.Context, dest net.Destination, streamSettings *internet.MemoryStreamConfig) (net.Conn, error) {
	logger.Debug().Msgf("creating connection to %d", dest)

	conn, err := dialWebSocket(ctx, dest, streamSettings)
	if err != nil {
		return nil, errors.NewError("failed to dial WebSocket").Base(err)
	}
	return conn, nil
}

func init() {
	common.Must(internet.RegisterTransportDialer(protocolName, Dial))
}

func dialWebSocket(ctx context.Context, dest net.Destination, streamSettings *internet.MemoryStreamConfig) (net.Conn, error) {
	wsSettings := streamSettings.ProtocolSettings.(*Config)

	dialer := &websocket.Dialer{
		NetDial: func(network, addr string) (net.Conn, error) {
			return internet.DialSystem(ctx, dest, streamSettings.SocketSettings)
		},
		ReadBufferSize:   4 * 1024,
		WriteBufferSize:  4 * 1024,
		HandshakeTimeout: time.Second * 8,
	}

	protocol := "ws"
	if config := tls.ConfigFromStreamSettings(streamSettings); config != nil {
		protocol = "wss"
		tlsConfig := config.GetTLSConfig(tls.WithDestination(dest), tls.WithNextProto("http/1.1"))
		dialer.TLSClientConfig = tlsConfig
		if fingerprint := tls.GetFingerprint(config.Fingerprint); fingerprint != nil {
			dialer.NetDialTLSContext = func(_ context.Context, _, addr string) (gonet.Conn, error) {
				pconn, err := internet.DialSystem(ctx, dest, streamSettings.SocketSettings)
				if err != nil {
					return nil, err
				}
				cn := tls.UClient(pconn, tlsConfig, fingerprint).(*tls.UConn)
				if err := cn.WebsocketHandshakeContext(ctx); err != nil {
					return nil, err
				}
				if !tlsConfig.InsecureSkipVerify {
					if err := cn.VerifyHostname(tlsConfig.ServerName); err != nil {
						return nil, err
					}
				}
				return cn, nil
			}
		}
	}

	host := dest.NetAddr()
	if (protocol == "ws" && dest.Port == 80) || (protocol == "wss" && dest.Port == 443) {
		host = dest.Address.String()
	}
	uri := protocol + "://" + host + wsSettings.GetNormalizedPath()

	header := wsSettings.GetRequestHeader()

	conn, resp, err := dialer.DialContext(ctx, uri, header)
	if err != nil {
		var reason string
		if resp != nil {
			reason = resp.Status
		}
		return nil, errors.NewError("failed to dial to (", uri, "): ", reason).Base(err)
	}

	return newConnection(conn, conn.RemoteAddr(), nil), nil
}

// --- END OF COMPLETE REPLACEMENT ---
