package vless

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
)

// HandshakeSocks5AndGetResponse performs a SOCKS5 handshake for a client connection.
// It reads the handshake and request, sends back appropriate responses,
// and returns the command and target address requested by the client.
func HandshakeSocks5AndGetResponse(conn net.Conn, reader *bufio.Reader) (byte, string, error) {
	// 1. Auth Phase
	authHeader := make([]byte, 2)
	if _, err := io.ReadFull(reader, authHeader); err != nil {
		return 0, "", err
	}
	if authHeader[0] != 0x05 {
		return 0, "", fmt.Errorf("unsupported socks version: %d", authHeader[0])
	}
	nMethods := int(authHeader[1])
	if _, err := io.CopyN(io.Discard, reader, int64(nMethods)); err != nil {
		return 0, "", err
	}
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return 0, "", err
	}

	// 2. Request Phase
	reqHeader := make([]byte, 4)
	if _, err := io.ReadFull(reader, reqHeader); err != nil {
		return 0, "", err
	}
	cmd, addrType := reqHeader[1], reqHeader[3]
	var host string
	switch addrType {
	case 0x01: // IPv4
		addrBuf := make([]byte, 4)
		if _, err := io.ReadFull(reader, addrBuf); err != nil {
			return 0, "", err
		}
		host = net.IP(addrBuf).String()
	case 0x03: // Domain
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(reader, lenBuf); err != nil {
			return 0, "", err
		}
		addrBuf := make([]byte, lenBuf[0])
		if _, err := io.ReadFull(reader, addrBuf); err != nil {
			return 0, "", err
		}
		host = string(addrBuf)
	case 0x04: // IPv6
		addrBuf := make([]byte, 16)
		if _, err := io.ReadFull(reader, addrBuf); err != nil {
			return 0, "", err
		}
		host = net.IP(addrBuf).String()
	default:
		return 0, "", fmt.Errorf("unsupported address type: %d", addrType)
	}
	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(reader, portBuf); err != nil {
		return 0, "", err
	}
	port := binary.BigEndian.Uint16(portBuf)

	// 3. Send Response
	if _, err := conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}); err != nil {
		return 0, "", err
	}

	return cmd, net.JoinHostPort(host, strconv.Itoa(int(port))), nil
}

// CloseWriter safely closes a connection's write side.
func CloseWriter(conn io.Closer) {
	type writeCloser interface {
		CloseWrite() error
	}
	if wc, ok := conn.(writeCloser); ok {
		wc.CloseWrite()
	} else {
		conn.Close()
	}
}
