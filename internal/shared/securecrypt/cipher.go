// --- cipher.go ---
package securecrypt

import (
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
)

// Algorithm 定义了支持的加密算法类型
type Algorithm string

const (
	CHACHA20_POLY1305 Algorithm = "chacha20"
	AES_256_GCM       Algorithm = "aes-gcm"
)

type Cipher struct {
	aead cipher.AEAD
}

// NewCipher 创建一个默认的 (chacha20) 加密器，以保持向后兼容性。
func NewCipher(key int) (*Cipher, error) {
	return NewCipherWithAlgo(key, CHACHA20_POLY1305)
}

// NewCipherWithAlgo 根据指定的算法创建一个新的加密器。
// 这是新的总工厂函数。
func NewCipherWithAlgo(key int, algo Algorithm) (*Cipher, error) {
	// 密钥派生逻辑保持不变，确保两种算法使用相同的根密钥
	keyBytes := []byte(fmt.Sprintf("liuproxy-secure-v2-key-%d", key))
	hash := sha256.Sum256(keyBytes)
	finalKey := hash[:]

	var aead cipher.AEAD
	var err error

	switch algo {
	case AES_256_GCM:
		aead, err = newAESGCMAEAD(finalKey)
	case CHACHA20_POLY1305:
		fallthrough // 设为默认值
	default:
		aead, err = newChaCha20AEAD(finalKey)
	}

	if err != nil {
		return nil, err // 错误已在子函数中包装
	}

	return &Cipher{aead: aead}, nil
}

// Encrypt 方法保持不变
func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}
	ciphertext := c.aead.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt 方法保持不变
func (c *Cipher) Decrypt(ciphertext []byte) ([]byte, error) {
	nonceSize := c.aead.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext is too short")
	}
	nonce, encryptedMessage := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := c.aead.Open(nil, nonce, encryptedMessage, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}
	return plaintext, nil
}
