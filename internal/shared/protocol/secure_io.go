// --- START OF COMPLETE REPLACEMENT for secure_io.go ---
package protocol

import (
	"io"
	"liuproxy_gateway/internal/shared/securecrypt"
)

// WriteSecurePacket 负责加密、封装并发送一个协议包。
// --- 关键修复: 参数 p 修改为 *Packet 指针类型 ---
func WriteSecurePacket(writer io.Writer, p *Packet, cipher *securecrypt.Cipher) error {
	if len(p.Payload) > 0 {
		encryptedPayload, err := cipher.Encrypt(p.Payload)
		if err != nil {
			return &SecureIOError{Op: "encrypt", Err: err}
		}
		p.Payload = encryptedPayload // 就地修改，现在会生效
	}

	err := WritePacket(writer, p) // 传递指针
	if err != nil {
		return &SecureIOError{Op: "write", Err: err}
	}
	return nil
}

// ReadSecurePacket 负责接收、解封装并解密一个协议包。
func ReadSecurePacket(reader io.Reader, cipher *securecrypt.Cipher) (*Packet, error) {
	packet, err := ReadPacket(reader)
	if err != nil {
		if err == io.EOF {
			return nil, io.EOF
		}
		return nil, &SecureIOError{Op: "read", Err: err}
	}

	if len(packet.Payload) > 0 {
		decryptedPayload, err := cipher.Decrypt(packet.Payload)
		if err != nil {
			return nil, &SecureIOError{Op: "decrypt", Err: err}
		}
		packet.Payload = decryptedPayload
	}

	return packet, nil
}

// SecureIOError 是一个自定义错误类型
type SecureIOError struct {
	Op  string
	Err error
}

func (e *SecureIOError) Error() string {
	return "secure_io: operation '" + e.Op + "' failed: " + e.Err.Error()
}

func (e *SecureIOError) Unwrap() error {
	return e.Err
}
