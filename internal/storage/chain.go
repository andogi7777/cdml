package storage

import (
	"encoding/binary"
	"encoding/json"
	"fmt"

	badger "github.com/dgraph-io/badger/v4"
	"cdml/internal/core"
)

// ChainStore: 거래 체인 + 잔고 저장소.
type ChainStore struct{ db *DB }

func NewChainStore(db *DB) *ChainStore { return &ChainStore{db: db} }

// ── 잔고 ──────────────────────────────────────────────────────

func (c *ChainStore) GetBalance(pk core.PubKey) (core.Amount, error) {
	val, err := c.db.Get(balKey(pk))
	if err != nil || val == nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(val), nil
}

func (c *ChainStore) SaveBalance(pk core.PubKey, bal core.Amount) error {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], bal)
	return c.db.Set(balKey(pk), buf[:])
}

func (c *ChainStore) HasBalance(pk core.PubKey) bool {
	val, _ := c.db.Get(balKey(pk))
	return val != nil
}

// ── 시퀀스 ────────────────────────────────────────────────────

func (c *ChainStore) GetLatestSeq(pk core.PubKey) (uint64, error) {
	val, err := c.db.Get(seqKey(pk))
	if err != nil || val == nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(val), nil
}

func (c *ChainStore) SaveLatestSeq(pk core.PubKey, seq uint64) error {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], seq)
	return c.db.Set(seqKey(pk), buf[:])
}

// ── 거래 저장 ─────────────────────────────────────────────────

func (c *ChainStore) SaveTx(tx *core.Transaction) error {
	data, err := json.Marshal(tx)
	if err != nil {
		return fmt.Errorf("marshal tx: %w", err)
	}
	return c.db.Set(txKey(tx.From, tx.Sequence), data)
}

func (c *ChainStore) GetTx(from core.PubKey, seq uint64) (*core.Transaction, error) {
	val, err := c.db.Get(txKey(from, seq))
	if err != nil || val == nil {
		return nil, err
	}
	var tx core.Transaction
	if err := json.Unmarshal(val, &tx); err != nil {
		return nil, err
	}
	return &tx, nil
}

// ── ConfirmTxBatch: 거래 확정 원자적 저장 ────────────────────
// 발신자 잔고, 수신자 잔고, 시퀀스, 거래 기록을 단일 트랜잭션으로 저장.
// 기존 개별 저장 방식의 중간 장애 시 잔고 불일치 버그 수정.

type ConfirmBatch struct {
	Tx          *core.Transaction
	SenderBal   core.Amount
	ReceiverBal core.Amount
}

func (c *ChainStore) ConfirmTxBatch(b *ConfirmBatch) error {
	txData, err := json.Marshal(b.Tx)
	if err != nil {
		return fmt.Errorf("marshal tx: %w", err)
	}

	var senderBuf, recvBuf, seqBuf [8]byte
	binary.BigEndian.PutUint64(senderBuf[:], b.SenderBal)
	binary.BigEndian.PutUint64(recvBuf[:], b.ReceiverBal)
	binary.BigEndian.PutUint64(seqBuf[:], b.Tx.Sequence)

	return c.db.AtomicWrite(func(txn *badger.Txn) error {
		if err := txn.Set(balKey(b.Tx.From), senderBuf[:]); err != nil {
			return err
		}
		if err := txn.Set(balKey(b.Tx.To), recvBuf[:]); err != nil {
			return err
		}
		if err := txn.Set(seqKey(b.Tx.From), seqBuf[:]); err != nil {
			return err
		}
		return txn.Set(txKey(b.Tx.From, b.Tx.Sequence), txData)
	})
}

// ── TxConfirmed 적용 마킹 (gossip 중복 방지) ─────────────────

func (c *ChainStore) IsConfirmedTxApplied(hash core.Hash32) bool {
	val, _ := c.db.Get(appliedKey(hash))
	return val != nil
}

func (c *ChainStore) MarkConfirmedTxApplied(hash core.Hash32) error {
	return c.db.Set(appliedKey(hash), []byte{1})
}

// ── 보상 풀 ──────────────────────────────────────────────────

func (c *ChainStore) GetRewardPool() (core.Amount, error) {
	val, err := c.db.Get([]byte("reward_pool"))
	if err != nil || val == nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(val), nil
}

func (c *ChainStore) AccumulateBond(bond core.Amount) error {
	pool, err := c.GetRewardPool()
	if err != nil {
		return err
	}
	newPool, err := core.SafeAdd(pool, bond)
	if err != nil {
		return err
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], newPool)
	return c.db.Set([]byte("reward_pool"), buf[:])
}

// ── 키 헬퍼 ──────────────────────────────────────────────────

func balKey(pk core.PubKey) []byte {
	key := make([]byte, 33)
	key[0] = 'B'
	copy(key[1:], pk[:])
	return key
}

func seqKey(pk core.PubKey) []byte {
	key := make([]byte, 33)
	key[0] = 'S'
	copy(key[1:], pk[:])
	return key
}

func txKey(pk core.PubKey, seq uint64) []byte {
	key := make([]byte, 41)
	key[0] = 'T'
	copy(key[1:33], pk[:])
	binary.BigEndian.PutUint64(key[33:], seq)
	return key
}

func appliedKey(hash core.Hash32) []byte {
	key := make([]byte, 33)
	key[0] = 'A'
	copy(key[1:], hash[:])
	return key
}
