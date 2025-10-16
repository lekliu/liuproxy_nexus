package internet

import (
	"context"

	"liuproxy_nexus/internal/xray_core/common/net"
)

type ConnHandler func(net.Conn)

type ListenFunc func(ctx context.Context, address net.Address, port net.Port, settings *MemoryStreamConfig, handler ConnHandler) (Listener, error)

type Listener interface {
	Close() error
	Addr() net.Addr
}
