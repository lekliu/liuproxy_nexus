// --- START OF NEW FILE internal/core/securecrypt/cipher_aesgcm.go ---
package securecrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"
)

// newAESGCMAEAD 根据给定的密钥创建一个 AES-256-GCM AEAD 实例。
// 这是与 Cloudflare Workers/JS 兼容的加密模式。
func newAESGCMAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES block cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES-GCM instance: %w", err)
	}

	return aead, nil
}
