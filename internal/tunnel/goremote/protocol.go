package goremote

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

// StreamType 定义了 goremote v3 协议中的流类型
type StreamType = byte

const (
	StreamTCP StreamType = 0x01
	StreamUDP StreamType = 0x02
)

// AddressType 定义了地址类型
type AddressType = byte

const (
	AddrTypeIPv4   AddressType = 0x01
	AddrTypeDomain AddressType = 0x03
	AddrTypeIPv6   AddressType = 0x04
)

// Metadata 是每个 goremote v3 短连接的第一个明文包
type Metadata struct {
	Type    StreamType
	Addr    string
	Port    int
	UDPData []byte // For UDP, this contains the SOCKS5 UDP request header + payload
}

// WriteMetadata 将元数据编码并写入 writer
func WriteMetadata(writer io.Writer, meta *Metadata) error {
	var addrBytes []byte
	var addrType AddressType
	var addrLen byte

	ip := net.ParseIP(meta.Addr)
	if ip != nil {
		if ipv4 := ip.To4(); ipv4 != nil {
			addrType = AddrTypeIPv4
			addrBytes = ipv4
		} else {
			addrType = AddrTypeIPv6
			addrBytes = ip.To16()
		}
	} else {
		if len(meta.Addr) > 255 {
			return fmt.Errorf("domain length > 255")
		}
		addrType = AddrTypeDomain
		addrBytes = []byte(meta.Addr)
		addrLen = byte(len(addrBytes))
	}

	// [StreamType 1B | AddrType 1B | AddrLen 1B (if domain) | Address | Port 2B]
	headerSize := 1 + 1 + len(addrBytes) + 2
	if addrType == AddrTypeDomain {
		headerSize += 1
	}

	buf := make([]byte, headerSize)
	offset := 0
	buf[offset] = meta.Type
	offset++
	buf[offset] = addrType
	offset++
	if addrType == AddrTypeDomain {
		buf[offset] = addrLen
		offset++
	}
	copy(buf[offset:], addrBytes)
	offset += len(addrBytes)
	binary.BigEndian.PutUint16(buf[offset:], uint16(meta.Port))

	_, err := writer.Write(buf)
	return err
}

// ReadMetadata 从 reader 读取并解码元数据
func ReadMetadata(reader io.Reader) (*Metadata, error) {
	meta := &Metadata{}

	// Read StreamType and AddrType
	typeBuf := make([]byte, 2)
	if _, err := io.ReadFull(reader, typeBuf); err != nil {
		return nil, fmt.Errorf("failed to read metadata type bytes: %w", err)
	}
	meta.Type = typeBuf[0]
	addrType := typeBuf[1]

	var addrBytes []byte
	switch addrType {
	case AddrTypeIPv4:
		addrBytes = make([]byte, 4)
		if _, err := io.ReadFull(reader, addrBytes); err != nil {
			return nil, err
		}
		meta.Addr = net.IP(addrBytes).String()
	case AddrTypeIPv6:
		addrBytes = make([]byte, 16)
		if _, err := io.ReadFull(reader, addrBytes); err != nil {
			return nil, err
		}
		meta.Addr = net.IP(addrBytes).String()
	case AddrTypeDomain:
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(reader, lenBuf); err != nil {
			return nil, err
		}
		domainLen := int(lenBuf[0])
		addrBytes = make([]byte, domainLen)
		if _, err := io.ReadFull(reader, addrBytes); err != nil {
			return nil, err
		}
		meta.Addr = string(addrBytes)
	default:
		return nil, fmt.Errorf("unsupported address type: %d", addrType)
	}

	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(reader, portBuf); err != nil {
		return nil, err
	}
	meta.Port = int(binary.BigEndian.Uint16(portBuf))

	return meta, nil
}
