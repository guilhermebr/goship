package vault

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	masterKeyFile = "master.key"
	masterKeyLen  = 32
)

// LoadOrCreateMasterKey loads the master key from dataDir/master.key,
// or generates a new 32-byte random key on first use.
func LoadOrCreateMasterKey(dataDir string) ([]byte, error) {
	keyPath := filepath.Join(dataDir, masterKeyFile)

	data, err := os.ReadFile(keyPath)
	if err == nil {
		return hex.DecodeString(strings.TrimSpace(string(data)))
	}

	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read master key: %w", err)
	}

	// Generate a new random key.
	key := make([]byte, masterKeyLen)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("failed to generate master key: %w", err)
	}

	// Ensure directory exists.
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	encoded := hex.EncodeToString(key)
	if err := os.WriteFile(keyPath, []byte(encoded), 0o600); err != nil {
		return nil, fmt.Errorf("failed to write master key: %w", err)
	}

	return key, nil
}
