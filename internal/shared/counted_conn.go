// FILE: internal/shared/counted_conn.go
package shared

import (
	"net"
	"sync/atomic"
)

// CountedConn 是一个 net.Conn 的包装器，用于原子地统计上行和下行流量。
type CountedConn struct {
	net.Conn
	uplink   *atomic.Uint64
	downlink *atomic.Uint64
}

// NewCountedConn 创建一个新的 CountedConn 实例。
func NewCountedConn(conn net.Conn, uplink, downlink *atomic.Uint64) *CountedConn {
	return &CountedConn{
		Conn:     conn,
		uplink:   uplink,
		downlink: downlink,
	}
}

// Read 从底层连接读取数据，并增加下行流量计数。
func (c *CountedConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 {
		c.downlink.Add(uint64(n))
	}
	return n, err
}

// Write 将数据写入底层连接，并增加上行流量计数。
func (c *CountedConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	if n > 0 {
		c.uplink.Add(uint64(n))
	}
	return n, err
}
