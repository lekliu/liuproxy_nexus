//go:build !linux

// FILE: internal/sys/tproxy/tproxy_other.go
package tproxy

import (
	"fmt"
	"net"
)

// GetOriginalDst 在非Linux系统上的存根实现
func GetOriginalDst(conn net.Conn) (net.Addr, error) {
	return nil, fmt.Errorf("transparent proxy is not supported on this platform")
}
