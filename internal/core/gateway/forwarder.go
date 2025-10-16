package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"github.com/rs/zerolog/log"
	"io"
	"liuproxy_nexus/internal/shared/logger"
	"liuproxy_nexus/internal/shared/types"
	"net"
	"net/http"
	"strconv"
	"sync"
)

// forwardSocks5 将 SOCKS5 流量转发到后端策略。
func (g *Gateway) forwardSocks5(inboundConn net.Conn, inboundReader *bufio.Reader, serverID string, builder types.TunnelBuilder) {
	outboundConn, err := builder.GetSocksConnection()
	if err != nil {
		logger.Error().Err(err).Str("server_id", serverID).Msg("SOCKS5: Failed to get socks connection from builder")
		return
	}
	defer outboundConn.Close()

	outboundReader := bufio.NewReader(outboundConn)
	if err := handleSocks5BackendHandshake(outboundConn, outboundReader); err != nil {
		logger.Error().Err(err).Str("server_id", serverID).Msg("SOCKS5: backend handshake failed")
		return
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, _ = io.Copy(outboundConn, inboundReader)
		if tcp, ok := outboundConn.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(inboundConn, outboundReader)
		if tcp, ok := inboundConn.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
	}()
	wg.Wait()
}

// handleHttpProxy 将 HTTP 代理流量转发到后端策略。
func (g *Gateway) handleHttpProxy(ctx context.Context, inboundConn net.Conn, inboundReader *bufio.Reader, targetDest, serverID string, builder types.TunnelBuilder) {
	clientIP := inboundConn.RemoteAddr().String()

	bufferedBytes, _ := inboundReader.Peek(inboundReader.Buffered())
	req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(bufferedBytes)))
	if err != nil {
		logger.Error().Err(err).Str("client_ip", clientIP).Msg("Gateway: Failed to re-parse HTTP request for proxy logic.")
		return
	}

	backendConn, err := builder.GetSocksConnection()
	if err != nil {
		log.Ctx(ctx).Error().Err(err).
			Str("server_id", serverID).
			Msg("Gateway: Failed to get SOCKS connection for HTTP proxy. Reporting failure.")
		inboundConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}
	defer backendConn.Close()

	if err := dialSocksProxyHandshake(backendConn, targetDest, serverID); err != nil {
		log.Ctx(ctx).Error().Err(err).
			Str("server_id", serverID).
			Msg("Gateway: Failed to perform SOCKS handshake on tunnel. Reporting failure.")
		inboundConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}

	if req.Method == "CONNECT" {
		b := make([]byte, inboundReader.Buffered())
		inboundReader.Read(b)
		_, err = inboundConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		if err != nil {
			logger.Error().Err(err).Str("client_ip", clientIP).Msg("Gateway: Failed to send CONNECT OK response to client.")
			return
		}
	} else {
		bytesToWrite := make([]byte, inboundReader.Buffered())
		n, _ := inboundReader.Read(bytesToWrite)
		if _, err := backendConn.Write(bytesToWrite[:n]); err != nil {
			logger.Error().Err(err).
				Str("client_ip", clientIP).
				Str("target", targetDest).
				Msg("Gateway: Failed to forward initial HTTP request through tunnel.")
			return
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(backendConn, inboundConn)
		if tcpConn, ok := backendConn.(interface{ CloseWrite() error }); ok {
			tcpConn.CloseWrite()
		}
	}()

	go func() {
		defer wg.Done()
		io.Copy(inboundConn, backendConn)
		if tcpConn, ok := inboundConn.(interface{ CloseWrite() error }); ok {
			tcpConn.CloseWrite()
		}
	}()

	wg.Wait()
	logger.Debug().Str("client_ip", clientIP).Str("target", targetDest).Msg("Gateway: HTTP proxy session finished.")
}

// forwardTCP 将原始 TCP (来自 TLS ClientHello) 流量转发到后端。
func (g *Gateway) forwardTCP(inboundConn net.Conn, inboundReader *bufio.Reader, serverID string, builder types.TunnelBuilder) {
	outboundConn, err := builder.GetSocksConnection()
	if err != nil {
		logger.Error().Err(err).Str("server_id", serverID).Msg("Gateway: Failed to get SOCKS connection for TLS forwarding")
		return
	}
	defer outboundConn.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	clientAddr := inboundConn.RemoteAddr().String()
	remoteAddr := outboundConn.RemoteAddr().String()

	go func() {
		defer wg.Done()
		bytesCopied, err := io.Copy(outboundConn, inboundReader)
		logger.Info().Str("trace", "PIPE-FORWARD-TCP").Str("direction", "Client -> Backend").
			Str("client_addr", clientAddr).Str("backend_addr", remoteAddr).Int64("bytes", bytesCopied).Err(err).Msg("Pipe finished")
		if tcp, ok := outboundConn.(*net.TCPConn); ok {
			tcp.CloseWrite()
		}
	}()

	go func() {
		defer wg.Done()
		bytesCopied, err := io.Copy(inboundConn, outboundConn)
		logger.Info().Str("trace", "PIPE-FORWARD-TCP").Str("direction", "Backend -> Client").
			Str("client_addr", clientAddr).Str("backend_addr", remoteAddr).Int64("bytes", bytesCopied).Err(err).Msg("Pipe finished")
		if tcp, ok := inboundConn.(*net.TCPConn); ok {
			tcp.CloseWrite()
		}
	}()
	wg.Wait()
}

// dialSocksProxyHandshake 在给定的连接上执行 SOCKS5 客户端握手，以连接到指定的 targetAddr。
func dialSocksProxyHandshake(conn net.Conn, targetAddr, serverID string) error {
	authRequest := []byte{0x05, 0x01, 0x00}
	if _, err := conn.Write(authRequest); err != nil {
		return fmt.Errorf("socks_handshake: failed to send auth request: %w", err)
	}

	authResponse := make([]byte, 2)
	if _, err := io.ReadFull(conn, authResponse); err != nil {
		return fmt.Errorf("socks_handshake: failed to read auth response: %w", err)
	}
	if authResponse[0] != 0x05 || authResponse[1] != 0x00 {
		return fmt.Errorf("socks_handshake: backend proxy requires authentication or returned invalid response: %v", authResponse)
	}

	host, portStr, err := net.SplitHostPort(targetAddr)
	if err != nil {
		return fmt.Errorf("socks_handshake: invalid target address '%s': %w", targetAddr, err)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("socks_handshake: invalid target port '%s': %w", portStr, err)
	}

	req := []byte{0x05, 0x01, 0x00} // VER=5, CMD=1(CONNECT), RSV=0

	if ip := net.ParseIP(host); ip != nil {
		if ipv4 := ip.To4(); ipv4 != nil {
			req = append(req, 0x01) // ATYP=1(IPv4)
			req = append(req, ipv4...)
		} else {
			req = append(req, 0x04) // ATYP=4(IPv6)
			req = append(req, ip.To16()...)
		}
	} else {
		if len(host) > 255 {
			return fmt.Errorf("socks_handshake: hostname too long: %s", host)
		}
		req = append(req, 0x03) // ATYP=3(Domain)
		req = append(req, byte(len(host)))
		req = append(req, host...)
	}

	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(port))
	req = append(req, portBytes...)

	if _, err := conn.Write(req); err != nil {
		return fmt.Errorf("socks_handshake: failed to send connect request: %w", err)
	}

	resp := make([]byte, 4)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fmt.Errorf("socks_handshake: failed to read final response header: %w", err)
	}

	if resp[0] != 0x05 {
		return fmt.Errorf("socks_handshake: invalid response version: %d", resp[0])
	}
	if resp[1] != 0x00 {
		return fmt.Errorf("socks_handshake: connection failed with status: %d", resp[1])
	}

	addrType := resp[3]
	addrLen := 0
	switch addrType {
	case 0x01: // IPv4
		addrLen = 4
	case 0x04: // IPv6
		addrLen = 16
	case 0x03: // Domain
		lenByte := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenByte); err != nil {
			return fmt.Errorf("socks_handshake: failed to read domain len in response: %w", err)
		}
		addrLen = int(lenByte[0])
	default:
		return fmt.Errorf("socks_handshake: unknown address type in response: %d", addrType)
	}

	remainingLen := addrLen + 2 // address + port
	if _, err := io.ReadFull(conn, make([]byte, remainingLen)); err != nil {
		return fmt.Errorf("socks_handshake: failed to read remaining response data: %w", err)
	}

	return nil
}

// handleSocks5BackendHandshake 处理与后端 SOCKS5 代理的握手。
func handleSocks5BackendHandshake(backendConn net.Conn, backendReader *bufio.Reader) error {
	_, err := backendConn.Write([]byte{0x05, 0x01, 0x00})
	if err != nil {
		return err
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(backendReader, resp); err != nil {
		return err
	}
	if resp[0] != 0x05 {
		return fmt.Errorf("not a SOCKS5 CONNECT request")
	}
	return nil
}
