package internet

import (
	"golang.org/x/sys/unix"
	"liuproxy_nexus/internal/xray_core/common/errors"
	"net"
	"syscall"
)

func bindAddr(fd uintptr, ip []byte, port uint32) error {
	setReuseAddr(fd)
	setReusePort(fd)

	var sockaddr syscall.Sockaddr

	switch len(ip) {
	case net.IPv4len:
		a4 := &syscall.SockaddrInet4{
			Port: int(port),
		}
		copy(a4.Addr[:], ip)
		sockaddr = a4
	case net.IPv6len:
		a6 := &syscall.SockaddrInet6{
			Port: int(port),
		}
		copy(a6.Addr[:], ip)
		sockaddr = a6
	default:
		return errors.NewError("unexpected length of ip")
	}

	return syscall.Bind(int(fd), sockaddr)
}

func applyOutboundSocketOptions(network string, address string, fd uintptr, config *SocketConfig) error {
	if config.Mark != 0 {
		if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK, int(config.Mark)); err != nil {
			return errors.NewError("failed to set SO_MARK").Base(err)
		}
	}

	if config.Interface != "" {
		if err := syscall.BindToDevice(int(fd), config.Interface); err != nil {
			return errors.NewError("failed to set Interface").Base(err)
		}
	}

	if isTCPSocket(network) {
		tfo := config.ParseTFOValue()
		if tfo > 0 {
			tfo = 1
		}
		if tfo >= 0 {
			if err := syscall.SetsockoptInt(int(fd), syscall.SOL_TCP, unix.TCP_FASTOPEN_CONNECT, tfo); err != nil {
				return errors.NewError("failed to set TCP_FASTOPEN_CONNECT", tfo).Base(err)
			}
		}

		if config.TcpKeepAliveInterval > 0 || config.TcpKeepAliveIdle > 0 {
			if config.TcpKeepAliveInterval > 0 {
				if err := syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, syscall.TCP_KEEPINTVL, int(config.TcpKeepAliveInterval)); err != nil {
					return errors.NewError("failed to set TCP_KEEPINTVL", err)
				}
			}
			if config.TcpKeepAliveIdle > 0 {
				if err := syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, syscall.TCP_KEEPIDLE, int(config.TcpKeepAliveIdle)); err != nil {
					return errors.NewError("failed to set TCP_KEEPIDLE", err)
				}
			}
			if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_KEEPALIVE, 1); err != nil {
				return errors.NewError("failed to set SO_KEEPALIVE", err)
			}
		} else if config.TcpKeepAliveInterval < 0 || config.TcpKeepAliveIdle < 0 {
			if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_KEEPALIVE, 0); err != nil {
				return errors.NewError("failed to unset SO_KEEPALIVE", err)
			}
		}

		if config.TcpCongestion != "" {
			if err := syscall.SetsockoptString(int(fd), syscall.SOL_TCP, syscall.TCP_CONGESTION, config.TcpCongestion); err != nil {
				return errors.NewError("failed to set TCP_CONGESTION", err)
			}
		}

		if config.TcpWindowClamp > 0 {
			if err := syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, syscall.TCP_WINDOW_CLAMP, int(config.TcpWindowClamp)); err != nil {
				return errors.NewError("failed to set TCP_WINDOW_CLAMP", err)
			}
		}

		if config.TcpUserTimeout > 0 {
			if err := syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, unix.TCP_USER_TIMEOUT, int(config.TcpUserTimeout)); err != nil {
				return errors.NewError("failed to set TCP_USER_TIMEOUT", err)
			}
		}

		if config.TcpMaxSeg > 0 {
			if err := syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, unix.TCP_MAXSEG, int(config.TcpMaxSeg)); err != nil {
				return errors.NewError("failed to set TCP_MAXSEG", err)
			}
		}

		if config.TcpNoDelay {
			if err := syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, unix.TCP_NODELAY, 1); err != nil {
				return errors.NewError("failed to set TCP_NODELAY", err)
			}
		}

	}

	return nil
}

func setReuseAddr(fd uintptr) error {
	if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		return errors.NewError("failed to set SO_REUSEADDR").Base(err)
	}
	return nil
}

func setReusePort(fd uintptr) error {
	if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, unix.SO_REUSEPORT, 1); err != nil {
		return errors.NewError("failed to set SO_REUSEPORT").Base(err)
	}
	return nil
}
