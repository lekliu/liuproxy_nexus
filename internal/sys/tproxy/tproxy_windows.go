//go:build windows

package tproxy

import (
	"fmt"
	"net"
)

// GetOriginalDst 在 Windows 平台上的存根实现。
// 它会返回一个错误，明确指出透明代理功能在此平台上不受支持。
func GetOriginalDst2(conn net.Conn) (net.Addr, error) {
	return nil, fmt.Errorf("transparent proxy (TPROXY/REDIRECT) is not supported on Windows")
}
