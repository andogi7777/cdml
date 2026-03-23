//go:build ignore

// scripts/cli/main.go — CDML CLI
// 사용법:
//   go run scripts/cli/main.go status
//   go run scripts/cli/main.go balance <pubkey>
//   go run scripts/cli/main.go send <to_pubkey> <amount> [-node 1]
//   go run scripts/cli/main.go tx <pubkey> <seq>
//   go run scripts/cli/main.go peers
//   go run scripts/cli/main.go faucet <pubkey> [amount]
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

func main() {
	nodeFlag := flag.Int("node", 1, "노드 번호 (1~N)")
	configDir := flag.String("testnet", "testnet", "테스트넷 디렉터리")
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		printHelp()
		return
	}

	apiAddr, err := loadAPIAddr(*configDir, *nodeFlag)
	if err != nil {
		fatalf("config 로드 실패: %v\n  testnet/nodeN/config.json 파일이 있는지 확인하세요.", err)
	}
	base := "http://" + apiAddr

	switch args[0] {
	case "status":
		cmdStatus(*configDir)
	case "balance":
		if len(args) < 2 {
			fatalf("사용법: balance <pubkey>")
		}
		cmdBalance(base, args[1])
	case "send":
		if len(args) < 3 {
			fatalf("사용법: send <to> <amount>")
		}
		amount, err := strconv.ParseUint(args[2], 10, 64)
		if err != nil {
			fatalf("amount 오류: %v", err)
		}
		cmdSend(base, args[1], amount)
	case "tx":
		if len(args) < 3 {
			fatalf("사용법: tx <pubkey> <seq>")
		}
		cmdGetTx(base, args[1], args[2])
	case "peers":
		cmdPeers(base)
	case "faucet":
		if len(args) < 2 {
			fatalf("사용법: faucet <pubkey> [amount]")
		}
		amount := uint64(10_000 * 1_000_000)
		if len(args) >= 3 {
			amount, _ = strconv.ParseUint(args[2], 10, 64)
		}
		cmdFaucet(base, args[1], amount)
	default:
		fmt.Printf("알 수 없는 명령어: %s\n\n", args[0])
		printHelp()
	}
}

// loadAPIAddr: testnet/nodeN/config.json에서 api_addr 읽기.
func loadAPIAddr(testnetDir string, nodeNum int) (string, error) {
	path := fmt.Sprintf("%s/node%d/config.json", testnetDir, nodeNum)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	var cfg struct {
		APIAddr string `json:"api_addr"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("parse config: %w", err)
	}
	if cfg.APIAddr == "" {
		return "", fmt.Errorf("api_addr not set in %s", path)
	}
	return cfg.APIAddr, nil
}

// loadAllNodes: 모든 노드의 API 주소 수집.
func loadAllNodes(testnetDir string) []nodeInfo {
	var nodes []nodeInfo
	for i := 1; i <= 20; i++ {
		addr, err := loadAPIAddr(testnetDir, i)
		if err != nil {
			break
		}
		nodes = append(nodes, nodeInfo{num: i, addr: addr})
	}
	return nodes
}

type nodeInfo struct {
	num  int
	addr string
}

func cmdStatus(testnetDir string) {
	nodes := loadAllNodes(testnetDir)
	if len(nodes) == 0 {
		fatalf("노드 config를 찾을 수 없습니다. testnet 디렉터리를 확인하세요.")
	}
	fmt.Printf("=== CDML 노드 상태 [%s] ===\n\n", time.Now().Format("15:04:05"))
	for _, n := range nodes {
		base := "http://" + n.addr
		b, err := get(base + "/v1/status")
		if err != nil {
			fmt.Printf("  node%d (%s)  오프라인\n", n.num, n.addr)
			continue
		}
		var s struct {
			PubKey string `json:"pubkey"`
			Peers  int    `json:"peers"`
		}
		json.Unmarshal(b, &s)
		pub := s.PubKey
		if len(pub) > 16 {
			pub = pub[:16] + "..."
		}
		fmt.Printf("  node%d (%s)  pubkey=%-22s  peers=%d\n", n.num, n.addr, pub, s.Peers)
	}
}

func cmdBalance(base, pubkey string) {
	b, err := get(fmt.Sprintf("%s/v1/balance/%s", base, pubkey))
	if err != nil {
		fatalf("잔고 조회 실패: %v", err)
	}
	var r struct {
		Balance uint64 `json:"balance"`
		Seq     uint64 `json:"seq"`
	}
	json.Unmarshal(b, &r)
	fmt.Printf("pubkey  : %s\n", pubkey)
	fmt.Printf("balance : %d micro (%s CDML)\n", r.Balance, microToCDML(r.Balance))
	fmt.Printf("seq     : %d\n", r.Seq)
}

func cmdSend(base, to string, amount uint64) {
	sb, _ := get(base + "/v1/status")
	var s struct {
		PubKey string `json:"pubkey"`
	}
	json.Unmarshal(sb, &s)

	fmt.Printf("송금:\n  from: %s\n  to:   %s\n  amount: %d micro (%s CDML)\n\n",
		s.PubKey, to, amount, microToCDML(amount))

	b, err := post(base+"/v1/tx", map[string]interface{}{"to": to, "amount": amount})
	if err != nil {
		fatalf("거래 실패: %v", err)
	}

	var errR struct{ Code, Message string }
	json.Unmarshal(b, &errR)
	if errR.Code != "" {
		fatalf("[%s] %s", errR.Code, errR.Message)
	}

	var r struct {
		TxHash string `json:"tx_hash"`
		Seq    uint64 `json:"seq"`
	}
	json.Unmarshal(b, &r)
	fmt.Printf("✅ 거래 전송 완료\n  tx_hash: %s\n  seq: %d\n", r.TxHash, r.Seq)
}

func cmdGetTx(base, pubkey, seq string) {
	b, err := get(fmt.Sprintf("%s/v1/tx/%s/%s", base, pubkey, seq))
	if err != nil {
		fatalf("조회 실패: %v", err)
	}
	var v interface{}
	json.Unmarshal(b, &v)
	out, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(out))
}

func cmdPeers(base string) {
	b, err := get(base + "/v1/peers")
	if err != nil {
		fatalf("피어 조회 실패: %v", err)
	}
	var r struct {
		Count int `json:"count"`
		Peers []struct {
			PubKey, Addr string
		} `json:"peers"`
	}
	json.Unmarshal(b, &r)
	fmt.Printf("피어 수: %d\n", r.Count)
	for _, p := range r.Peers {
		pk := p.PubKey
		if len(pk) > 16 {
			pk = pk[:16] + "..."
		}
		fmt.Printf("  %s  %s\n", pk, p.Addr)
	}
}

func cmdFaucet(base, pubkey string, amount uint64) {
	b, err := post(base+"/v1/faucet", map[string]interface{}{
		"pubkey": pubkey, "amount": amount,
	})
	if err != nil {
		fatalf("faucet 실패: %v", err)
	}
	var r struct {
		Balance uint64 `json:"balance"`
		Amount  uint64 `json:"amount"`
	}
	json.Unmarshal(b, &r)
	fmt.Printf("✅ Faucet 완료\n  지급: %s CDML\n  잔고: %s CDML\n",
		microToCDML(r.Amount), microToCDML(r.Balance))
}

func microToCDML(micro uint64) string {
	if micro%1_000_000 == 0 {
		return fmt.Sprintf("%d", micro/1_000_000)
	}
	return fmt.Sprintf("%d.%06d", micro/1_000_000, micro%1_000_000)
}

func get(url string) ([]byte, error) {
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func post(url string, payload interface{}) ([]byte, error) {
	data, _ := json.Marshal(payload)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Post(
		url, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "❌ "+format+"\n", args...)
	os.Exit(1)
}

func printHelp() {
	fmt.Println(`CDML CLI

명령어:
  status                        전체 노드 상태
  balance <pubkey>              잔고 조회
  send <to> <amount_micro>      거래 전송
  tx <pubkey> <seq>             거래 조회
  peers                         피어 목록
  faucet <pubkey> [amount]      테스트 잔고 지급

옵션:
  -node 1~N        사용할 노드 번호 (기본: 1)
  -testnet <dir>   테스트넷 디렉터리 (기본: testnet)

예시:
  go run scripts/cli/main.go status
  go run scripts/cli/main.go faucet <pubkey>
  go run scripts/cli/main.go send <pubkey> 1000000 -node 2`)
}
