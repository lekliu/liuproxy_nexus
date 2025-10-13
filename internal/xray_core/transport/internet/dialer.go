package internet

import (
	"context"
	"liuproxy_gateway/internal/xray_core/common/errors"
	"liuproxy_gateway/internal/xray_core/common/net"
	"liuproxy_gateway/internal/xray_core/common/session"
)

// dialFunc is an interface to dial network connection to a specific destination.
type dialFunc func(ctx context.Context, dest net.Destination, streamSettings *MemoryStreamConfig) (net.Conn, error)

var (
	transportDialerCache = make(map[string]dialFunc)
)

// RegisterTransportDialer registers a Dialer with given name.
func RegisterTransportDialer(protocol string, dialer dialFunc) error {
	if _, found := transportDialerCache[protocol]; found {

		return errors.NewError(protocol, " dialer already registered")
	}
	transportDialerCache[protocol] = dialer
	return nil
}

// Dial dials a internet connection towards the given destination.
func Dial(ctx context.Context, dest net.Destination, streamSettings *MemoryStreamConfig) (net.Conn, error) {

	if dest.Network == net.Network_TCP {
		if streamSettings == nil {
			s, err := ToMemoryStreamConfig(nil)
			if err != nil {
				return nil, errors.NewError("failed to create default stream settings").Base(err)
			}
			streamSettings = s
		}

		protocol := streamSettings.ProtocolName
		dialer := transportDialerCache[protocol]
		if dialer == nil {
			return nil, errors.NewError(protocol, " dialer not registered")
		}
		return dialer(ctx, dest, streamSettings)
	}

	return nil, errors.NewError("unknown network ", dest.Network)
}

// DialSystem calls system dialer to create a network connection.
func DialSystem(ctx context.Context, dest net.Destination, sockopt *SocketConfig) (net.Conn, error) {
	var src net.Address
	if outbound := session.OutboundFromContext(ctx); outbound != nil {
		src = outbound.Gateway
	}

	return effectiveSystemDialer.Dial(ctx, src, dest, sockopt)
}
