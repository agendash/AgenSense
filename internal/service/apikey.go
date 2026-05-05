package service

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const clientNamespacePrefix = "apikey"

// NamespaceFromAPIKey returns a stable storage namespace derived from a caller API key.
// The raw API key is never persisted directly.
func NamespaceFromAPIKey(apiKey string) string {
	trimmed := strings.TrimSpace(apiKey)
	if trimmed == "" {
		return ""
	}

	sum := sha256.Sum256([]byte(trimmed))
	return clientNamespacePrefix + "_" + hex.EncodeToString(sum[:8])
}
