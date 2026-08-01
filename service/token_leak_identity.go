package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
)

const tokenLeakAnchorLength = 16

func deriveTokenLeakIdentity(secret []byte, tokenKey string) (string, string, error) {
	if len(secret) < 32 {
		return "", "", errors.New("scan_secret_invalid")
	}
	if len(tokenKey) < tokenLeakAnchorLength {
		return "", "", errors.New("token_key_invalid")
	}

	fingerprintMAC := hmac.New(sha256.New, secret)
	_, _ = fingerprintMAC.Write([]byte("fingerprint\x00"))
	_, _ = fingerprintMAC.Write([]byte(tokenKey))
	fingerprint := hex.EncodeToString(fingerprintMAC.Sum(nil))

	anchorMAC := hmac.New(sha256.New, secret)
	_, _ = anchorMAC.Write([]byte("anchor\x00"))
	_, _ = anchorMAC.Write([]byte(tokenKey))
	anchorDigest := anchorMAC.Sum(nil)
	windowCount := len(tokenKey) - tokenLeakAnchorLength + 1
	offset := int(binary.BigEndian.Uint64(anchorDigest[:8]) % uint64(windowCount))

	return fingerprint, tokenKey[offset : offset+tokenLeakAnchorLength], nil
}
