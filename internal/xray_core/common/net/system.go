package net

import "net"

var SplitHostPort = net.SplitHostPort

type (
	Addr       = net.Addr
	Conn       = net.Conn
	PacketConn = net.PacketConn
)

type (
	TCPAddr = net.TCPAddr
	TCPConn = net.TCPConn
)

type (
	UDPAddr = net.UDPAddr
	UDPConn = net.UDPConn
)

// IP is an alias for net.IP.
type (
	IP     = net.IP
	IPMask = net.IPMask
	IPNet  = net.IPNet
)

type (
	Dialer       = net.Dialer
	Listener     = net.Listener
	TCPListener  = net.TCPListener
	UnixListener = net.UnixListener
)
