package internet

import (
	"golang.org/x/sys/unix"
	network "net"
	"syscall"
)

const (
	// TCP_FASTOPEN_SERVER is the value to enable TCP fast open on darwin for server connections.
	TCP_FASTOPEN_SERVER = 0x01
	// TCP_FASTOPEN_CLIENT is the value to enable TCP fast open on darwin for client connections.
	TCP_FASTOPEN_CLIENT = 0x02 // nolint: revive,stylecheck
	// syscall.TCP_KEEPINTVL is missing on some darwin architectures.
	sysTCP_KEEPINTVL = 0x101 // nolint: revive,stylecheck
)

const (
	PfOut       = 2
	IOCOut      = 0x40000000
	IOCIn       = 0x80000000
	IOCInOut    = IOCIn | IOCOut
	IOCPARMMask = 0x1FFF
	LEN         = 4*16 + 4*4 + 4*1
	// #define	_IOC(inout,group,num,len) (inout | ((len & IOCPARMMask) << 16) | ((group) << 8) | (num))
	// #define	_IOWR(g,n,t)	_IOC(IOCInOut,	(g), (n), sizeof(t))
	// #define DIOCNATLOOK		_IOWR('D', 23, struct pfioc_natlook)
	DIOCNATLOOK = IOCInOut | ((LEN & IOCPARMMask) << 16) | ('D' << 8) | 23
)

func applyOutboundSocketOptions(network string, address string, fd uintptr, config *SocketConfig) error {
	if isTCPSocket(network) {
		tfo := config.ParseTFOValue()
		if tfo > 0 {
			tfo = TCP_FASTOPEN_CLIENT
		}
		if tfo >= 0 {
			if err := unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_FASTOPEN, tfo); err != nil {
				return err
			}
		}
		if config.Interface != "" {
			InterfaceIndex := getInterfaceIndexByName(config.Interface)
			if InterfaceIndex != 0 {
				if err := unix.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_BOUND_IF, InterfaceIndex); err != nil {
					return newError("failed to set Interface").Base(err)
				}
			}
		}

		if config.TcpKeepAliveIdle > 0 || config.TcpKeepAliveInterval > 0 {
			if config.TcpKeepAliveIdle > 0 {
				if err := unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_KEEPALIVE, int(config.TcpKeepAliveInterval)); err != nil {
					return newError("failed to set TCP_KEEPINTVL", err)
				}
			}
			if config.TcpKeepAliveInterval > 0 {
				if err := unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, sysTCP_KEEPINTVL, int(config.TcpKeepAliveIdle)); err != nil {
					return newError("failed to set TCP_KEEPIDLE", err)
				}
			}
			if err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_KEEPALIVE, 1); err != nil {
				return newError("failed to set SO_KEEPALIVE", err)
			}
		} else if config.TcpKeepAliveInterval < 0 || config.TcpKeepAliveIdle < 0 {
			if err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_KEEPALIVE, 0); err != nil {
				return newError("failed to unset SO_KEEPALIVE", err)
			}
		}

		if config.TcpNoDelay {
			if err := unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_NODELAY, 1); err != nil {
				return newError("failed to set TCP_NODELAY", err)
			}
		}
	}

	return nil
}

func getInterfaceIndexByName(name string) int {
	ifaces, err := network.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			if (iface.Flags&network.FlagUp == network.FlagUp) && (iface.Flags&network.FlagLoopback != network.FlagLoopback) {
				addrs, _ := iface.Addrs()
				for _, addr := range addrs {
					if ipnet, ok := addr.(*network.IPNet); ok && !ipnet.IP.IsLoopback() {
						if ipnet.IP.To4() != nil {
							if iface.Name == name {
								return iface.Index
							}
						}
					}
				}
			}

		}
	}
	return 0
}
