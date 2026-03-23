package core

import "time"

// PubKey: Ed25519 공개키 (32바이트).
type PubKey [32]byte

// Hash32: Blake2b-256 해시 (32바이트).
type Hash32 [32]byte

// IsZero: 빈 해시 여부.
func (h Hash32) IsZero() bool { return h == Hash32{} }

// Amount: 거래 금액 (micro 단위, 1 CDML = 1_000_000 micro).
type Amount = uint64

// Signature: Ed25519 서명 (64바이트).
type Signature [64]byte

// ─── Transaction ────────────────────────────────────────────

// TxStatus: 거래 상태.
type TxStatus int

const (
	TxPending   TxStatus = 0
	TxConfirmed TxStatus = 1
	TxRejected  TxStatus = 2
)

// Transaction: 단일 거래.
type Transaction struct {
	Hash        Hash32
	Sequence    uint64
	Nonce       uint64
	From        PubKey
	To          PubKey
	Amount      Amount
	SenderSig   Signature
	ParentHashes []Hash32
	WitnessSigs []WitnessSignature
	CreatedAt   time.Time
	Status      TxStatus
}

// ─── Witness ─────────────────────────────────────────────────

// WitnessSignature: 증인의 거래 서명.
type WitnessSignature struct {
	WitnessPubKey  PubKey
	DAGTipAtSign   Hash32
	NonceLockSeen  bool
	Sig            Signature
}

// Witness: 증인 노드 정보.
type Witness struct {
	PubKey    PubKey
	Addr      string    // P2P 주소
	AddedAt   time.Time
}

// ─── Snapshot ────────────────────────────────────────────────

// Snapshot: 잔고 증명 스냅샷.
// 증인이 VerifyTx 시 발신자의 잔고를 확인하는 데 사용.
type Snapshot struct {
	PubKey    PubKey
	Sequence  uint64
	Balance   Amount
	DAGTips   []Hash32
	CreatedAt time.Time
	ExpiresAt time.Time
}

// IsExpired: 스냅샷 만료 여부.
func (s *Snapshot) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// ─── Network ─────────────────────────────────────────────────

// PacketType: 네트워크 패킷 타입.
type PacketType uint8

const (
	PacketPing        PacketType = 1
	PacketPong        PacketType = 2
	PacketTxVerify    PacketType = 10
	PacketTxWitnessSig PacketType = 11
	PacketTxConfirmed PacketType = 12
	PacketNonceLock   PacketType = 13
	PacketHandshake   PacketType = 20
)
