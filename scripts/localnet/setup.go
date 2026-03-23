//go:build ignore

// scripts/localnet/setup.go
// 5노드 로컬 테스트넷 설정 자동 생성.
// 실행: go run scripts/localnet/setup.go
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	totalNodes  = 5
	basePeerPort = 7331
	baseAPIPort  = 8331
)

type Config struct {
	PrivKeyPath string   `json:"priv_key_path"`
	DBPath      string   `json:"db_path"`
	P2PAddr     string   `json:"p2p_addr"`
	APIAddr     string   `json:"api_addr"`
	SeedPeers   []string `json:"seed_peers"`
	NetworkID   string   `json:"network_id"`
}

func main() {
	base := "testnet"
	fmt.Println("=== CDML 5-Node Local Testnet Setup ===")

	pubkeys := make([]string, totalNodes)

	for i := 1; i <= totalNodes; i++ {
		dir := filepath.Join(base, fmt.Sprintf("node%d", i))
		os.MkdirAll(filepath.Join(dir, "db"), 0755)

		keyPath := filepath.Join(dir, "node.key")
		var pub ed25519.PublicKey
		if existing, err := os.ReadFile(keyPath); err == nil && len(existing) == 64 {
			pub = ed25519.PublicKey(existing[32:])
			fmt.Printf("  node%d: reusing key\n", i)
		} else {
			var priv ed25519.PrivateKey
			var err error
			pub, priv, err = ed25519.GenerateKey(rand.Reader)
			if err != nil {
				panic(err)
			}
			os.WriteFile(keyPath, []byte(priv), 0600)
		}
		pubkeys[i-1] = hex.EncodeToString(pub)

		// 씨드: node1은 없음, 나머지는 앞 노드들 모두
		seeds := []string{}
		for j := 1; j < i; j++ {
			seeds = append(seeds, fmt.Sprintf("127.0.0.1:%d", basePeerPort+j-1))
		}

		cfg := Config{
			PrivKeyPath: keyPath,
			DBPath:      filepath.Join(dir, "db"),
			P2PAddr:     fmt.Sprintf("127.0.0.1:%d", basePeerPort+i-1),
			APIAddr:     fmt.Sprintf("127.0.0.1:%d", baseAPIPort+i-1),
			SeedPeers:   seeds,
			NetworkID:   "cdml-local-1",
		}
		data, _ := json.MarshalIndent(cfg, "", "  ")
		os.WriteFile(filepath.Join(dir, "config.json"), data, 0644)
		fmt.Printf("  node%d  P2P=:%d  API=:%d  pubkey=%.8s...\n",
			i, basePeerPort+i-1, baseAPIPort+i-1, pubkeys[i-1])
	}

	// genesis.json: 초기 증인 목록
	type GenesisWitness struct {
		PubKey string `json:"pubkey"`
		Addr   string `json:"addr"`
	}
	type Genesis struct {
		NetworkID string           `json:"network_id"`
		Witnesses []GenesisWitness `json:"witnesses"`
	}
	witnesses := make([]GenesisWitness, totalNodes)
	for i := range witnesses {
		witnesses[i] = GenesisWitness{
			PubKey: pubkeys[i],
			Addr:   fmt.Sprintf("127.0.0.1:%d", basePeerPort+i),
		}
	}
	genesis := Genesis{NetworkID: "cdml-local-1", Witnesses: witnesses}
	data, _ := json.MarshalIndent(genesis, "", "  ")
	os.WriteFile(filepath.Join(base, "genesis.json"), data, 0644)
	fmt.Println("\n  testnet/genesis.json 생성 완료")

	// start.bat
	writeBat(base)

	fmt.Println("\n=== 완료 ===")
	fmt.Println("시작: testnet\\start_all.bat")
	fmt.Println("초기화: go run scripts\\localnet\\init.go")
}

func writeBat(base string) {
	var s string
	s += "@echo off\r\ncd /d %~dp0\\..\r\n\r\n"
	s += "echo Building cdml...\r\n"
	s += "go build -o testnet\\cdml.exe .\\cmd\\cdml\r\n"
	s += "if errorlevel 1 (echo BUILD FAILED && pause && exit /b 1)\r\n\r\n"
	for i := 1; i <= totalNodes; i++ {
		s += fmt.Sprintf("echo Starting node%d...\r\n", i)
		s += fmt.Sprintf("start \"node%d\" cmd /c \"testnet\\cdml.exe -config testnet\\node%d\\config.json >> testnet\\node%d\\node.log 2>&1\"\r\n", i, i, i)
		if i == 2 {
			s += "timeout /t 3 /nobreak >nul\r\n"
		}
	}
	s += "\r\necho All nodes started.\r\n"
	s += fmt.Sprintf("echo Dashboard: http://127.0.0.1:%d/healthz\r\n", 8331)
	s += "timeout /t 5 /nobreak >nul\r\n"
	s += "echo.\r\necho Next: go run scripts\\localnet\\init.go\r\necho.\r\npause\r\n"
	os.WriteFile(filepath.Join(base, "start_all.bat"), []byte(s), 0755)

	stop := "@echo off\r\ntaskkill /F /IM cdml.exe 2>nul\r\necho All nodes stopped.\r\npause\r\n"
	os.WriteFile(filepath.Join(base, "stop_all.bat"), []byte(stop), 0755)
	fmt.Println("  testnet/start_all.bat, stop_all.bat 생성 완료")
}
