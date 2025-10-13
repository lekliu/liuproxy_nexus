// --- START OF NEW FILE internal/core/securecrypt/cipher_chacha20.go ---
package securecrypt

import (
	"crypto/cipher"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

// newChaCha20AEAD 根据给定的密钥创建一个 XChaCha20-Poly1305 AEAD 实例。
// 这是推荐用于 Go-to-Go 通信的加密模式，因为它有更大的 Nonce 空间。
func newChaCha20AEAD(key []byte) (cipher.AEAD, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create XChaCha20-Poly1305 instance: %w", err)
	}
	return aead, nil
}
