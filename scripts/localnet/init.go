//go:build ignore

// scripts/localnet/init.go — 로컬 테스트넷 초기화
// 실행: go run scripts/localnet/init.go
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	testnetDir     = "testnet"
	initialBalance = 10_000 * 1_000_000 // 10,000 CDML
)

// nodeConfig: config.json 구조 (필요한 필드만).
type nodeConfig struct {
	APIAddr string `json:"api_addr"`
	P2PAddr string `json:"p2p_addr"`
}

// loadNodeConfig: testnet/nodeN/config.json 로드.
func loadNodeConfig(n int) (*nodeConfig, error) {
	path := fmt.Sprintf("%s/node%d/config.json", testnetDir, n)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg nodeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &cfg, nil
}

// loadAllNodes: 존재하는 노드 config를 순서대로 로드.
func loadAllNodes() []nodeConfig {
	var nodes []nodeConfig
	for i := 1; i <= 20; i++ {
		cfg, err := loadNodeConfig(i)
		if err != nil {
			break
		}
		nodes = append(nodes, *cfg)
	}
	return nodes
}

func main() {
	fmt.Println("=== CDML Testnet Init ===\n")

	// 노드 config 로드
	nodes := loadAllNodes()
	if len(nodes) == 0 {
		fmt.Println("❌ 노드 config를 찾을 수 없습니다.")
		fmt.Println("   먼저 go run scripts/localnet/setup.go 를 실행하세요.")
		return
	}
	totalNodes := len(nodes)

	// 1. 노드 온라인 확인
	fmt.Printf("[1/4] 노드 상태 확인 (%d개)\n", totalNodes)
	pubkeys := make([]string, totalNodes)
	for i, node := range nodes {
		base := "http://" + node.APIAddr
		for retry := 0; retry < 10; retry++ {
			b, err := get(base + "/v1/status")
			if err == nil {
				var s struct {
					PubKey string `json:"pubkey"`
				}
				json.Unmarshal(b, &s)
				pubkeys[i] = s.PubKey
				fmt.Printf("  node%d ✅  pubkey=%.16s...\n", i+1, s.PubKey)
				break
			}
			fmt.Printf("  node%d ⏳ 대기중...\n", i+1)
			time.Sleep(2 * time.Second)
			if retry == 9 {
				fmt.Printf("  node%d ❌ 오프라인 — 노드를 먼저 시작하세요\n", i+1)
				return
			}
		}
	}

	// 2. 잔고 지급 (faucet)
	fmt.Println("\n[2/4] 잔고 지급")
	for i, node := range nodes {
		base := "http://" + node.APIAddr
		payload := map[string]interface{}{
			"pubkey": pubkeys[i],
			"amount": initialBalance,
		}
		if _, err := post(base+"/v1/faucet", payload); err != nil {
			fmt.Printf("  node%d ❌ faucet 실패: %v\n", i+1, err)
			continue
		}
		fmt.Printf("  node%d ✅  잔고 %d micro 지급\n", i+1, initialBalance)
	}

	// 3. 증인 등록 (모든 노드에 동일하게)
	fmt.Println("\n[3/4] 증인 집합 등록")
	type Witness struct {
		PubKey string `json:"PubKey"`
		Addr   string `json:"Addr"`
	}
	witnesses := make([]Witness, totalNodes)
	for i, node := range nodes {
		witnesses[i] = Witness{
			PubKey: pubkeys[i],
			Addr:   node.P2PAddr, // config에서 읽은 P2P 주소 사용
		}
	}
	for i, node := range nodes {
		base := "http://" + node.APIAddr
		if _, err := post(base+"/v1/witness/register", witnesses); err != nil {
			fmt.Printf("  node%d ❌ witness 등록 실패: %v\n", i+1, err)
			continue
		}
		fmt.Printf("  node%d ✅  %d명 증인 등록\n", i+1, totalNodes)
	}

	// 4. 결과 확인
	fmt.Println("\n[4/4] 결과 확인")
	fmt.Printf("  %-8s %-18s %s\n", "노드", "잔고(micro)", "pubkey")
	fmt.Println("  " + "─────────────────────────────────────────────────────")
	for i, node := range nodes {
		base := "http://" + node.APIAddr
		b, _ := get(fmt.Sprintf("%s/v1/balance/%s", base, pubkeys[i]))
		var r struct {
			Balance uint64 `json:"balance"`
		}
		json.Unmarshal(b, &r)
		fmt.Printf("  node%-2d   %-18d %.16s...\n", i+1, r.Balance, pubkeys[i])
	}

	fmt.Println("\n=== 완료 ===")
	fmt.Println("\n거래 테스트:")
	fmt.Println("  go run scripts/cli/main.go status")
	if len(pubkeys) > 1 {
		fmt.Printf("  go run scripts/cli/main.go send %s 1000000 -node 1\n", pubkeys[1])
	}
}

func get(url string) ([]byte, error) {
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func post(url string, payload interface{}) ([]byte, error) {
	data, _ := json.Marshal(payload)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Post(
		url, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
