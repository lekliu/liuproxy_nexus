package worker

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"

	"liuproxy_nexus/internal/shared/types"
)

// Agent for the worker package is a simplified version.
// It provides the SOCKS5 handshake method needed by the worker_strategy.
type Agent struct {
	config *types.Config
}

// NewAgent creates a simplified agent just for protocol handling.
func NewAgent(cfg *types.Config) *Agent {
	return &Agent{
		config: cfg,
	}
}

// HandshakeWithClient is the SOCKS5 handshake implementation copied from goremote.
// It is now a method of the simplified worker.Agent.
func (a *Agent) HandshakeWithClient(conn net.Conn, reader *bufio.Reader) (byte, string, error) {
	authBuf := make([]byte, 2)
	if _, err := io.ReadFull(reader, authBuf); err != nil {
		return 0, "", err
	}
	if authBuf[0] != 0x05 {
		return 0, "", fmt.Errorf("unsupported socks version: %d", authBuf[0])
	}
	nMethods := int(authBuf[1])
	if nMethods > 0 {
		if _, err := io.ReadFull(reader, make([]byte, nMethods)); err != nil {
			return 0, "", err
		}
	}
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return 0, "", err
	}
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
	default:
		return 0, "", fmt.Errorf("unsupported address type: %d", addrType)
	}
	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(reader, portBuf); err != nil {
		return 0, "", err
	}
	port := binary.BigEndian.Uint16(portBuf)
	return cmd, net.JoinHostPort(host, strconv.Itoa(int(port))), nil
}
