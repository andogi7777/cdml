package storage

import (
	"encoding/json"
	"fmt"
	"time"

	"cdml/internal/core"
)

// SnapshotStore: 잔고 스냅샷 저장소.
type SnapshotStore struct{ db *DB }

func NewSnapshotStore(db *DB) *SnapshotStore { return &SnapshotStore{db: db} }

func (s *SnapshotStore) SaveSnapshot(snap *core.Snapshot) error {
	data, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	return s.db.Set(snapKey(snap.PubKey), data)
}

func (s *SnapshotStore) GetLatest(pk core.PubKey) (*core.Snapshot, error) {
	val, err := s.db.Get(snapKey(pk))
	if err != nil || val == nil {
		return nil, err
	}
	var snap core.Snapshot
	if err := json.Unmarshal(val, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

func snapKey(pk core.PubKey) []byte {
	key := make([]byte, 33)
	key[0] = 'P'
	copy(key[1:], pk[:])
	return key
}

// ─────────────────────────────────────────────────────────────

// WitnessStore: 활성 증인 집합 저장소.
type WitnessStore struct{ db *DB }

func NewWitnessStore(db *DB) *WitnessStore { return &WitnessStore{db: db} }

func (w *WitnessStore) SaveActiveWitnesses(witnesses []core.Witness) error {
	data, err := json.Marshal(witnesses)
	if err != nil {
		return fmt.Errorf("marshal witnesses: %w", err)
	}
	return w.db.Set([]byte("active_witnesses"), data)
}

func (w *WitnessStore) GetActiveWitnesses() ([]core.Witness, error) {
	val, err := w.db.Get([]byte("active_witnesses"))
	if err != nil || val == nil {
		return nil, err
	}
	var witnesses []core.Witness
	if err := json.Unmarshal(val, &witnesses); err != nil {
		return nil, err
	}
	return witnesses, nil
}

func (w *WitnessStore) GetActiveSet() (map[core.PubKey]struct{}, error) {
	witnesses, err := w.GetActiveWitnesses()
	if err != nil {
		return nil, err
	}
	set := make(map[core.PubKey]struct{}, len(witnesses))
	for _, ww := range witnesses {
		set[ww.PubKey] = struct{}{}
	}
	return set, nil
}

// ─────────────────────────────────────────────────────────────

// nonceLockTTL: NonceLock 자동 만료 시간. (내부 상수, 비공개)
const nonceLockTTL = 15 * time.Second

// NonceLockStore: 이중지불 방지 nonce 잠금 저장소.
type NonceLockStore struct{ db *DB }

func NewNonceLockStore(db *DB) *NonceLockStore { return &NonceLockStore{db: db} }

// Lock: nonce 잠금. TTL 이후 자동 만료.
func (n *NonceLockStore) Lock(pk core.PubKey, nonce uint64) error {
	key := nonceLockKey(pk, nonce)
	if existing, _ := n.db.Get(key); existing != nil {
		return core.ErrTxNonceLocked
	}
	return n.db.SetWithTTL(key, []byte{1}, nonceLockTTL)
}

// Unlock: nonce 잠금 수동 해제.
func (n *NonceLockStore) Unlock(pk core.PubKey, nonce uint64) error {
	return n.db.Delete(nonceLockKey(pk, nonce))
}

// IsLocked: nonce 잠금 여부 확인.
func (n *NonceLockStore) IsLocked(pk core.PubKey, nonce uint64) bool {
	val, _ := n.db.Get(nonceLockKey(pk, nonce))
	return val != nil
}

func nonceLockKey(pk core.PubKey, nonce uint64) []byte {
	key := make([]byte, 41)
	key[0] = 'N'
	copy(key[1:33], pk[:])
	for i := 0; i < 8; i++ {
		key[33+i] = byte(nonce >> (56 - 8*i))
	}
	return key
}
