package gateway

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"liuproxy_nexus/internal/shared/types"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// sniffTargetForRouting 检查连接的第一个字节，以确定协议并嗅探目标地址。
func sniffTargetForRouting(conn net.Conn, reader *bufio.Reader) (target string, ptl types.Protocol, req *http.Request, err error) {
	if err := fillBuffer(conn, reader, 1); err != nil {
		return "", types.ProtoUnknown, nil, fmt.Errorf("failed to read initial byte: %w", err)
	}
	firstByte, _ := reader.Peek(1)

	switch {
	case firstByte[0] == 0x05: // SOCKS5
		target, err := sniffTargetSocks5(conn, reader)
		return target, types.ProtoSOCKS5, nil, err
	case firstByte[0] == 0x16: // TLS ClientHello
		host, tlsErr := sniffTargetTLS(conn, reader)
		if tlsErr == nil && host != "" {
			return host, types.ProtoTLS, nil, nil
		}
		return "", types.ProtoUnknown, nil, fmt.Errorf("TLS SNI sniff failed: %w", tlsErr)
	case firstByte[0] >= 'A' && firstByte[0] <= 'Z': // HTTP Methods (GET, POST, CONNECT, etc.)
		host, request, httpErr := sniffTargetHTTP(conn, reader)
		if httpErr == nil && host != "" {
			return host, types.ProtoHTTP, request, nil
		}
		return "", types.ProtoUnknown, nil, fmt.Errorf("HTTP sniff failed: %w", httpErr)
	default:
		return "", types.ProtoUnknown, nil, fmt.Errorf("could not determine target protocol, initial byte: 0x%02x", firstByte[0])
	}
}

// sniffTargetTLS 被动嗅探 TLS ClientHello 中的 SNI (Server Name Indication)
func sniffTargetTLS(conn net.Conn, reader *bufio.Reader) (string, error) {
	// 确保缓冲区至少有5个字节 (TLS Record Header)
	if err := fillBuffer(conn, reader, 5); err != nil {
		return "", err
	}
	header, _ := reader.Peek(5)

	if header[0] != 0x16 {
		return "", fmt.Errorf("not a TLS handshake record")
	}

	if header[1] != 0x03 {
		return "", fmt.Errorf("unexpected TLS major version: %d", header[1])
	}

	recordLen := int(binary.BigEndian.Uint16(header[3:5]))
	totalHelloLen := 5 + recordLen

	if err := fillBuffer(conn, reader, totalHelloLen); err != nil {
		return "", fmt.Errorf("buffer does not contain full TLS ClientHello")
	}

	data, _ := reader.Peek(totalHelloLen)
	data = data[5:]

	if len(data) < 42 {
		return "", fmt.Errorf("invalid ClientHello: too short")
	}

	if data[0] != 0x01 {
		return "", fmt.Errorf("not a ClientHello message")
	}

	offset := 38

	sessionIDLen := int(data[offset])
	offset += 1 + sessionIDLen
	if offset+2 > len(data) {
		return "", fmt.Errorf("invalid ClientHello: session ID parsing error")
	}

	cipherSuitesLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2 + cipherSuitesLen
	if offset+1 > len(data) {
		return "", fmt.Errorf("invalid ClientHello: cipher suites parsing error")
	}

	compressionMethodsLen := int(data[offset])
	offset += 1 + compressionMethodsLen
	if offset+2 > len(data) {
		return "", fmt.Errorf("no extensions found")
	}

	extensionsLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2
	if offset+extensionsLen > len(data) {
		return "", fmt.Errorf("invalid ClientHello: extensions length mismatch")
	}
	extensionsData := data[offset : offset+extensionsLen]

	for len(extensionsData) >= 4 {
		extType := binary.BigEndian.Uint16(extensionsData[0:2])
		extLen := int(binary.BigEndian.Uint16(extensionsData[2:4]))
		extensionsData = extensionsData[4:]

		if len(extensionsData) < extLen {
			return "", fmt.Errorf("invalid extension length")
		}

		if extType == 0x0000 { // SNI
			sniData := extensionsData[:extLen]
			if len(sniData) < 5 {
				return "", fmt.Errorf("invalid SNI data")
			}
			sniData = sniData[2:]
			if sniData[0] != 0x00 {
				return "", fmt.Errorf("unsupported SNI name type: %d", sniData[0])
			}
			nameLen := int(binary.BigEndian.Uint16(sniData[1:3]))
			sniData = sniData[3:]
			if len(sniData) < nameLen {
				return "", fmt.Errorf("invalid SNI name length")
			}
			return string(sniData[:nameLen]), nil
		}
		extensionsData = extensionsData[extLen:]
	}

	return "", fmt.Errorf("SNI not found")
}

// sniffTargetHTTP 嗅探 Host 并确保始终返回 host:port 格式。
func sniffTargetHTTP(conn net.Conn, reader *bufio.Reader) (string, *http.Request, error) {
	buffered := reader.Buffered()
	if buffered == 0 {
		// 确保至少有一个字节可供读取
		if _, err := reader.Peek(1); err != nil {
			return "", nil, fmt.Errorf("failed to peek for http sniff: %w", err)
		}
		buffered = reader.Buffered()
	}

	data, _ := reader.Peek(buffered)
	req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(data)))
	if err != nil {
		return "", nil, fmt.Errorf("could not parse HTTP request: %w", err)
	}

	host := req.Host
	if host == "" {
		return "", req, fmt.Errorf("HTTP request host is empty")
	}

	// 关键修复：检查 Host 是否已包含端口，如果没有，则根据协议补充默认端口。
	if !strings.Contains(host, ":") {
		if req.Method == "CONNECT" {
			host = net.JoinHostPort(host, "443") // HTTPS 默认端口
		} else {
			host = net.JoinHostPort(host, "80") // HTTP 默认端口
		}
	}

	return host, req, nil
}

// sniffTargetSocks5 嗅探 SOCKS5 请求中的目标地址。
func sniffTargetSocks5(conn net.Conn, reader *bufio.Reader) (string, error) {
	err := handleSocks5ClientHandshake(conn, reader)
	if err != nil {
		return "handleSocks5ClientHandshake ", err
	}

	reqHeaderSize := 4
	if err := fillBuffer(conn, reader, reqHeaderSize); err != nil {
		return "", err
	}
	reqHeader, _ := reader.Peek(reqHeaderSize)
	if reqHeader[0] != 0x05 || reqHeader[1] != 0x01 {
		return "", fmt.Errorf("not a SOCKS5 CONNECT request")
	}
	addrType := reqHeader[3]
	addrBodyOffset := reqHeaderSize
	var host string
	var port int
	switch addrType {
	case 0x01: // IPv4
		peekSize := addrBodyOffset + 4 + 2
		if err := fillBuffer(conn, reader, peekSize); err != nil {
			return "", err
		}
		fullHeader, _ := reader.Peek(peekSize)
		host = net.IP(fullHeader[addrBodyOffset : addrBodyOffset+4]).String()
		port = int(binary.BigEndian.Uint16(fullHeader[addrBodyOffset+4 : addrBodyOffset+6]))
	case 0x03: // Domain
		peekSize := addrBodyOffset + 1
		if err := fillBuffer(conn, reader, peekSize); err != nil {
			return "", err
		}
		lenHeader, _ := reader.Peek(peekSize)
		domainLen := int(lenHeader[addrBodyOffset])
		peekSize = addrBodyOffset + 1 + domainLen + 2
		if err := fillBuffer(conn, reader, peekSize); err != nil {
			return "", err
		}
		fullHeader, _ := reader.Peek(peekSize)
		host = string(fullHeader[addrBodyOffset+1 : addrBodyOffset+1+domainLen])
		port = int(binary.BigEndian.Uint16(fullHeader[addrBodyOffset+1+domainLen : addrBodyOffset+1+domainLen+2]))
	case 0x04: // IPv6
		peekSize := addrBodyOffset + 16 + 2
		if err := fillBuffer(conn, reader, peekSize); err != nil {
			return "", err
		}
		fullHeader, _ := reader.Peek(peekSize)
		host = net.IP(fullHeader[addrBodyOffset : addrBodyOffset+16]).String()
		port = int(binary.BigEndian.Uint16(fullHeader[addrBodyOffset+16 : addrBodyOffset+18]))
	default:
		return "", fmt.Errorf("unsupported SOCKS5 address type: %d", addrType)
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

// handleSocks5ClientHandshake 处理 SOCKS5 的客户端握手阶段。
func handleSocks5ClientHandshake(conn net.Conn, reader *bufio.Reader) error {
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return err
	}
	nMethods := int(header[1])
	methods := make([]byte, nMethods)
	if _, err := io.ReadFull(reader, methods); err != nil {
		return err
	}
	_, err := conn.Write([]byte{0x05, 0x00})
	return err
}

// fillBuffer 确保 reader 的缓冲区至少有 n 个字节，带超时。
func fillBuffer(conn net.Conn, reader *bufio.Reader, n int) error {
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	defer conn.SetReadDeadline(time.Time{})
	for reader.Buffered() < n {
		_, err := reader.Peek(1)
		if err != nil {
			return err
		}
	}
	return nil
}
