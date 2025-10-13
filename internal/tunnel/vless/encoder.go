package vless

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"

	"github.com/google/uuid"
)

const (
	Version = byte(0)
)

// AddressType constants according to VLESS specification.
const (
	AddressTypeIPv4   byte = 1
	AddressTypeDomain byte = 2
	AddressTypeIPv6   byte = 3
)

const (
	RequestCommandTCP = byte(0x01)
)

// EncodeRequestHeader 编码VLESS请求头，但使用标准库类型。
// 这是新的、解耦后的版本。
func EncodeRequestHeader(writer io.Writer, command byte, host string, port int, userUUID string) error {
	uid, err := uuid.Parse(userUUID)
	if err != nil {
		return fmt.Errorf("invalid vless uuid: %w", err)
	}

	buf := new(bytes.Buffer)

	// 1. Version
	buf.WriteByte(Version)
	// 2. UUID
	buf.Write(uid[:])
	// 3. Addons (currently none)
	buf.WriteByte(0x00)
	// 4. Command
	buf.WriteByte(command)
	// 5. Port (BigEndian)
	binary.Write(buf, binary.BigEndian, uint16(port))

	// 6. Address
	ip := net.ParseIP(host)
	if ip != nil {
		if ipv4 := ip.To4(); ipv4 != nil {
			buf.WriteByte(AddressTypeIPv4)
			buf.Write(ipv4)
		} else {
			buf.WriteByte(AddressTypeIPv6)
			buf.Write(ip.To16())
		}
	} else {
		if len(host) > 255 {
			return fmt.Errorf("domain length > 255: %s", host)
		}
		buf.WriteByte(AddressTypeDomain)
		buf.WriteByte(byte(len(host)))
		buf.WriteString(host)
	}

	// 7. Write buffer to the underlying writer
	_, err = writer.Write(buf.Bytes())
	return err
}
