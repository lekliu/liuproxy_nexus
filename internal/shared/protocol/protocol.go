// --- START OF COMPLETE REPLACEMENT for protocol.go ---
package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
)

// 定义 v2.2 协议中的标志位常量
const (
	FlagControlNewStreamTCP        byte = 0x01
	FlagControlNewStreamTCPSuccess byte = 0x02
	FlagTCPData                    byte = 0x03
	FlagUDPData                    byte = 0x04
	FlagControlCloseStream         byte = 0x05
)

// Packet 代表一个 v2.2 协议的数据包
type Packet struct {
	StreamID uint16
	Flag     byte
	Payload  []byte
}

// WritePacket 将一个 Packet 写入到 io.Writer
// --- 关键修复: 参数 p 修改为 *Packet 指针类型 ---
func WritePacket(writer io.Writer, p *Packet) error {
	packetContentLen := 2 + 1 + len(p.Payload)
	if packetContentLen > 65535 {
		return fmt.Errorf("packet payload is too large: %d", len(p.Payload))
	}

	totalSize := 2 + packetContentLen
	buf := make([]byte, totalSize)

	binary.BigEndian.PutUint16(buf[0:2], uint16(packetContentLen))
	binary.BigEndian.PutUint16(buf[2:4], p.StreamID)
	buf[4] = p.Flag
	copy(buf[5:], p.Payload)

	if _, err := writer.Write(buf); err != nil {
		return err
	}

	return nil
}

// ReadPacket 从 io.Reader 读取并解析一个 Packet
func ReadPacket(reader io.Reader) (*Packet, error) {
	lenBuf := make([]byte, 2)
	if _, err := io.ReadFull(reader, lenBuf); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("failed to read packet length: %w", err)
	}

	packetContentLen := binary.BigEndian.Uint16(lenBuf)

	if packetContentLen < 3 { // StreamID (2) + Flag (1)
		return nil, fmt.Errorf("received invalid packet with content length < 3")
	}

	data := make([]byte, packetContentLen)
	if _, err := io.ReadFull(reader, data); err != nil {
		return nil, fmt.Errorf("failed to read packet content (expected %d bytes): %w", packetContentLen, err)
	}

	p := &Packet{
		StreamID: binary.BigEndian.Uint16(data[0:2]),
		Flag:     data[2],
		Payload:  data[3:],
	}
	return p, nil
}

// ReadUnsecurePacket 从 io.Reader 读取并解析一个 Packet，但不进行解密。
// 专门用于 Worker 策略的下行数据处理。
func ReadUnsecurePacket(reader io.Reader) (*Packet, error) {
	lenBuf := make([]byte, 2)
	if _, err := io.ReadFull(reader, lenBuf); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("failed to read unsecure packet length: %w", err)
	}

	packetContentLen := binary.BigEndian.Uint16(lenBuf)
	if packetContentLen < 3 { // StreamID (2) + Flag (1)
		return nil, fmt.Errorf("received invalid unsecure packet with content length < 3")
	}

	data := make([]byte, packetContentLen)
	if _, err := io.ReadFull(reader, data); err != nil {
		return nil, fmt.Errorf("failed to read unsecure packet content (expected %d bytes): %w", packetContentLen, err)
	}

	p := &Packet{
		StreamID: binary.BigEndian.Uint16(data[0:2]),
		Flag:     data[2],
		Payload:  data[3:], // Payload is plaintext
	}
	// No decryption step
	return p, nil
}

// --- END OF COMPLETE REPLACEMENT ---
