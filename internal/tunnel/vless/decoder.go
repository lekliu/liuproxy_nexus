package vless

import (
	"fmt"
	"io"
)

// DecodeResponseHeader 解码VLESS响应头，目前只做验证
func DecodeResponseHeader(reader io.Reader) error {
	var version [1]byte
	if _, err := io.ReadFull(reader, version[:]); err != nil {
		return fmt.Errorf("failed to read response version: %w", err)
	}
	if version[0] != Version {
		return fmt.Errorf("unexpected response version. want %d, got %d", Version, version[0])
	}

	var addonsLen [1]byte
	if _, err := io.ReadFull(reader, addonsLen[:]); err != nil {
		return fmt.Errorf("failed to read response addons length: %w", err)
	}

	if addonsLen[0] > 0 {
		// Discard addons payload as we don't use it
		if _, err := io.CopyN(io.Discard, reader, int64(addonsLen[0])); err != nil {
			return fmt.Errorf("failed to discard response addons: %w", err)
		}
	}

	return nil
}
