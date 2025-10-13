package internet

import (
	"encoding/binary"
	"net"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	sysPFINOUT     = 0x0
	sysPFIN        = 0x1
	sysPFOUT       = 0x2
	sysPFFWD       = 0x3
	sysDIOCNATLOOK = 0xc04c4417
)

type pfiocNatlook struct {
	Saddr     [16]byte /* pf_addr */
	Daddr     [16]byte /* pf_addr */
	Rsaddr    [16]byte /* pf_addr */
	Rdaddr    [16]byte /* pf_addr */
	Sport     uint16
	Dport     uint16
	Rsport    uint16
	Rdport    uint16
	Af        uint8
	Proto     uint8
	Direction uint8
	Pad       [1]byte
}

const (
	sizeofPfiocNatlook = 0x4c
	soReUsePort        = 0x00000200
	soReUsePortLB      = 0x00010000
)

func ioctl(s uintptr, ioc int, b []byte) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, s, uintptr(ioc), uintptr(unsafe.Pointer(&b[0]))); errno != 0 {
		return error(errno)
	}
	return nil
}

func (nl *pfiocNatlook) rdPort() int {
	return int(binary.BigEndian.Uint16((*[2]byte)(unsafe.Pointer(&nl.Rdport))[:]))
}

func (nl *pfiocNatlook) setPort(remote, local int) {
	binary.BigEndian.PutUint16((*[2]byte)(unsafe.Pointer(&nl.Sport))[:], uint16(remote))
	binary.BigEndian.PutUint16((*[2]byte)(unsafe.Pointer(&nl.Dport))[:], uint16(local))
}

func applyOutboundSocketOptions(network string, address string, fd uintptr, config *SocketConfig) error {
	if config.Mark != 0 {
		if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_USER_COOKIE, int(config.Mark)); err != nil {
			return newError("failed to set SO_USER_COOKIE").Base(err)
		}
	}

	if isTCPSocket(network) {
		tfo := config.ParseTFOValue()
		if tfo > 0 {
			tfo = 1
		}
		if tfo >= 0 {
			if err := syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, unix.TCP_FASTOPEN, tfo); err != nil {
				return newError("failed to set TCP_FASTOPEN_CONNECT=", tfo).Base(err)
			}
		}
		if config.TcpKeepAliveIdle > 0 || config.TcpKeepAliveInterval > 0 {
			if config.TcpKeepAliveIdle > 0 {
				if err := syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, syscall.TCP_KEEPIDLE, int(config.TcpKeepAliveIdle)); err != nil {
					return newError("failed to set TCP_KEEPIDLE", err)
				}
			}
			if config.TcpKeepAliveInterval > 0 {
				if err := syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, syscall.TCP_KEEPINTVL, int(config.TcpKeepAliveInterval)); err != nil {
					return newError("failed to set TCP_KEEPINTVL", err)
				}
			}
			if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_KEEPALIVE, 1); err != nil {
				return newError("failed to set SO_KEEPALIVE", err)
			}
		} else if config.TcpKeepAliveInterval < 0 || config.TcpKeepAliveIdle < 0 {
			if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_KEEPALIVE, 0); err != nil {
				return newError("failed to unset SO_KEEPALIVE", err)
			}
		}
	}

	if config.Tproxy.IsEnabled() {
		ip, _, _ := net.SplitHostPort(address)
		if net.ParseIP(ip).To4() != nil {
			if err := syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_BINDANY, 1); err != nil {
				return newError("failed to set outbound IP_BINDANY").Base(err)
			}
		} else {
			if err := syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IPV6, syscall.IPV6_BINDANY, 1); err != nil {
				return newError("failed to set outbound IPV6_BINDANY").Base(err)
			}
		}
	}
	return nil
}
