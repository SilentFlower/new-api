package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeriveTokenLeakIdentityStableAndDomainSeparated(t *testing.T) {
	secret := []byte(strings.Repeat("s", 32))
	tokenKey := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUV"

	fingerprint1, anchor1, err := deriveTokenLeakIdentity(secret, tokenKey)
	require.NoError(t, err)
	fingerprint2, anchor2, err := deriveTokenLeakIdentity(secret, tokenKey)
	require.NoError(t, err)
	fingerprintWithRotatedSecret, anchorWithRotatedSecret, err := deriveTokenLeakIdentity([]byte(strings.Repeat("t", 32)), tokenKey)
	require.NoError(t, err)

	assert.Equal(t, fingerprint1, fingerprint2)
	assert.Equal(t, anchor1, anchor2)
	assert.Len(t, fingerprint1, 64)
	assert.Len(t, anchor1, tokenLeakAnchorLength)
	assert.Contains(t, tokenKey, anchor1)
	assert.NotEqual(t, fingerprint1[:tokenLeakAnchorLength], anchor1)
	assert.NotEqual(t, fingerprint1, fingerprintWithRotatedSecret)
	assert.NotEqual(t, anchor1, anchorWithRotatedSecret)
}

func TestDeriveTokenLeakIdentityRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		secret []byte
		key    string
	}{
		{name: "短密钥", secret: []byte("short"), key: strings.Repeat("a", 48)},
		{name: "短令牌", secret: []byte(strings.Repeat("s", 32)), key: "short"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := deriveTokenLeakIdentity(test.secret, test.key)
			assert.Error(t, err)
		})
	}
}
