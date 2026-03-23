package node

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"

	"cdml/internal/crypto"
)

// Config: 노드 설정.
type Config struct {
	PrivKeyPath string   `json:"priv_key_path"`
	DBPath      string   `json:"db_path"`
	P2PAddr     string   `json:"p2p_addr"`
	APIAddr     string   `json:"api_addr"`
	SeedPeers   []string `json:"seed_peers"`
	NetworkID   string   `json:"network_id"`
}

// LoadConfig: JSON 파일에서 설정 로드.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

// loadOrCreateKey: 개인키 파일 로드 또는 새로 생성.
func loadOrCreateKey(path string) (*crypto.KeyPair, error) {
	data, err := os.ReadFile(path)
	if err == nil && len(data) == ed25519.PrivateKeySize {
		return crypto.LoadFromBytes(data)
	}

	kp, err := crypto.Generate()
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, kp.PrivateBytes(), 0600); err != nil {
		return nil, fmt.Errorf("write key: %w", err)
	}
	return kp, nil
}
