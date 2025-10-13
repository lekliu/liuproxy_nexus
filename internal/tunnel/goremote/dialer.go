package goremote

import (
	"fmt"
	"net"
	"time"
)

// DialTCP 负责为 GoRemote 策略建立一个纯 TCP 连接
func DialTCP(addr string) (net.Conn, error) {
	//logger.Debug().Str("addr", addr).Msg("[GoRemote Dialer] Dialing new raw TCP connection...")
	conn, err := net.DialTimeout("tcp", addr, 15*time.Second)
	if err != nil {
		return nil, fmt.Errorf("goremote raw TCP dial failed: %w", err)
	}
	//logger.Debug().Str("remote_addr", conn.RemoteAddr().String()).Msg("[GoRemote Dialer] SUCCESS: Raw TCP connection established.")
	return conn, nil
}
