//go:build linux

// FILE: internal/sys/tproxy/tproxy_linux.go
package tproxy

import (
	"fmt"
	"net"
	"syscall"
)

const SO_ORIGINAL_DST = 80

// GetOriginalDst 从一个被TPROXY/REDIRECT的TCP连接中获取其原始目标地址
func GetOriginalDst(conn net.Conn) (net.Addr, error) {
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return nil, fmt.Errorf("not a TCP connection")
	}

	file, err := tcpConn.File()
	if err != nil {
		return nil, fmt.Errorf("failed to get connection file descriptor: %w", err)
	}
	defer file.Close()
	fd := file.Fd()

	// 尝试获取IPv4原始目标地址
	// The getsockopt syscall for SO_ORIGINAL_DST returns a sockaddr_in structure.
	// In Go's syscall package, this structure is represented by syscall.RawSockaddrInet4.
	addr, err := syscall.GetsockoptIPv6Mreq(int(fd), syscall.IPPROTO_IP, SO_ORIGINAL_DST)
	if err != nil {
		// As a fallback or for IPv6, you might need a different getsockopt call,
		// but for classic iptables REDIRECT, this is the one.
		return nil, fmt.Errorf("getsockopt(SO_ORIGINAL_DST) failed: %w", err)
	}

	//  ipv6mr_multiaddr [16]byte
	// The actual sockaddr_in struct is smaller and is contained within this field.
	// struct sockaddr_in {
	//    sa_family_t    sin_family; /* address family: AF_INET */
	//    in_port_t      sin_port;   /* port in network byte order */
	//    struct in_addr sin_addr;   /* internet address */
	// };
	// sin_family (2 bytes), sin_port (2 bytes), sin_addr (4 bytes)
	ip := net.IP(addr.Multiaddr[4:8])
	port := uint16(addr.Multiaddr[2])<<8 + uint16(addr.Multiaddr[3])

	return &net.TCPAddr{IP: ip, Port: int(port)}, nil
}
