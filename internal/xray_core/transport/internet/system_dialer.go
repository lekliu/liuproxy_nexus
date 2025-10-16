package internet

import (
	"context"
	"liuproxy_nexus/internal/shared/logger"
	"syscall"
	"time"

	"github.com/sagernet/sing/common/control"
	"liuproxy_nexus/internal/xray_core/common/net"
)

var effectiveSystemDialer SystemDialer = &DefaultSystemDialer{}

type SystemDialer interface {
	Dial(ctx context.Context, source net.Address, destination net.Destination, sockopt *SocketConfig) (net.Conn, error)
	DestIpAddress() net.IP
}

type DefaultSystemDialer struct {
	controllers []control.Func
}

func resolveSrcAddr(network net.Network, src net.Address) net.Addr {
	if src == nil || src == net.AnyIP {
		return nil
	}

	if network == net.Network_TCP {
		return &net.TCPAddr{
			IP:   src.IP(),
			Port: 0,
		}
	}

	return &net.UDPAddr{
		IP:   src.IP(),
		Port: 0,
	}
}

func hasBindAddr(sockopt *SocketConfig) bool {
	return sockopt != nil && len(sockopt.BindAddress) > 0 && sockopt.BindPort > 0
}

func (d *DefaultSystemDialer) Dial(ctx context.Context, src net.Address, dest net.Destination, sockopt *SocketConfig) (net.Conn, error) {
	//logger.Debug().Msg("dialing to " + dest.String())

	goStdKeepAlive := time.Duration(0)
	if sockopt != nil && (sockopt.TcpKeepAliveInterval != 0 || sockopt.TcpKeepAliveIdle != 0) {
		goStdKeepAlive = time.Duration(-1)
	}
	dialer := &net.Dialer{
		Timeout:   time.Second * 16,
		LocalAddr: resolveSrcAddr(dest.Network, src),
		KeepAlive: goStdKeepAlive,
	}

	if sockopt != nil || len(d.controllers) > 0 {
		if sockopt != nil && sockopt.TcpMptcp {
			dialer.SetMultipathTCP(true)
		}
		dialer.Control = func(network, address string, c syscall.RawConn) error {
			for _, ctl := range d.controllers {
				if err := ctl(network, address, c); err != nil {
					logger.Error().Msg("failed to apply external controller")
				}
			}
			return c.Control(func(fd uintptr) {
				if sockopt != nil {
					if err := applyOutboundSocketOptions(network, address, fd, sockopt); err != nil {
						logger.Error().Msg("failed to apply socket options")
					}
					if dest.Network == net.Network_UDP && hasBindAddr(sockopt) {
						if err := bindAddr(fd, sockopt.BindAddress, sockopt.BindPort); err != nil {
							logger.Error().Msgf("failed to bind source address to %d", sockopt.BindAddress)
						}
					}
				}
			})
		}
	}

	return dialer.DialContext(ctx, dest.Network.SystemString(), dest.NetAddr())
}

func (d *DefaultSystemDialer) DestIpAddress() net.IP {
	return nil
}

type SystemDialerAdapter interface {
	Dial(network string, address string) (net.Conn, error)
}
