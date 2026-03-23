package protocol

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"time"

	"cdml/internal/core"
	"cdml/internal/crypto"
	"cdml/internal/storage"
)

// 내부 파라미터 비공개
const (
	snapshotInterval = 1000
	snapshotTTL      = 6 * time.Hour
)

// VerifyPacket: 증인에게 전달되는 검증 패킷.
type VerifyPacket struct {
	Tx        *core.Transaction
	Snapshot  *core.Snapshot
	KnownTips map[core.Hash32]struct{}
}

// TxProcessor: 거래 처리기.
type TxProcessor struct {
	chain   *storage.ChainStore
	snap    *storage.SnapshotStore
	witness *storage.WitnessStore
	nonce   *storage.NonceLockStore
	dag     *DAGManager
}

func NewTxProcessor(
	chain *storage.ChainStore,
	snap *storage.SnapshotStore,
	witness *storage.WitnessStore,
	nonce *storage.NonceLockStore,
	dag *DAGManager,
) *TxProcessor {
	return &TxProcessor{chain: chain, snap: snap, witness: witness, nonce: nonce, dag: dag}
}

// BuildTx: 거래 생성 및 서명.
func (tp *TxProcessor) BuildTx(kp *crypto.KeyPair, to core.PubKey, amount core.Amount) (*core.Transaction, error) {
	if err := core.ValidateAmount(amount); err != nil {
		return nil, err
	}

	bal, err := tp.chain.GetBalance(kp.Public)
	if err != nil {
		return nil, fmt.Errorf("get balance: %w", err)
	}
	bond := core.BondAmount(amount)
	needed, err := core.SafeAdd(amount, bond)
	if err != nil {
		return nil, err
	}
	if bal < needed {
		return nil, core.ErrTxInsufficientBal
	}

	seq, err := tp.chain.GetLatestSeq(kp.Public)
	if err != nil {
		return nil, err
	}
	seq++

	var nonceBuf [8]byte
	if _, err := rand.Read(nonceBuf[:]); err != nil {
		return nil, err
	}
	nonce := binary.BigEndian.Uint64(nonceBuf[:])

	tips, _ := tp.dag.CurrentTips(kp.Public)

	tx := &core.Transaction{
		Sequence:     seq,
		Nonce:        nonce,
		From:         kp.Public,
		To:           to,
		Amount:       amount,
		ParentHashes: tips,
		CreatedAt:    time.Now(),
		Status:       core.TxPending,
	}
	if err := crypto.SignTx(kp, tx); err != nil {
		return nil, err
	}
	return tx, nil
}

// AcquireNonceLock: NonceLock 획득.
func (tp *TxProcessor) AcquireNonceLock(tx *core.Transaction) error {
	return tp.nonce.Lock(tx.From, tx.Nonce)
}

// ReleaseNonceLock: NonceLock 해제.
func (tp *TxProcessor) ReleaseNonceLock(tx *core.Transaction) {
	_ = tp.nonce.Unlock(tx.From, tx.Nonce)
}

// VerifyTx: 증인 측 거래 검증. (내부 검증 로직 비공개)
func (tp *TxProcessor) VerifyTx(pkt *VerifyPacket) error {
	return tp.verifyTxInternal(pkt)
}

func (tp *TxProcessor) verifyTxInternal(pkt *VerifyPacket) error {
	tx := pkt.Tx

	if pkt.Snapshot != nil && pkt.Snapshot.IsExpired() {
		return fmt.Errorf("snapshot expired: %w", core.ErrSnapshotExpired)
	}
	if err := crypto.VerifyTx(tx); err != nil {
		return err
	}
	// NonceLock은 이중지불 방지 신호 — 잠겨있는 것이 정상
	// 거부 조건이 아니라 증인이 nonceLockSeen 플래그로 기록함

	chainSeq, err := tp.chain.GetLatestSeq(tx.From)
	if err != nil {
		return err
	}
	if chainSeq > 0 && tx.Sequence != chainSeq+1 {
		return fmt.Errorf("%w: expected %d got %d", core.ErrTxInvalidSequence, chainSeq+1, tx.Sequence)
	}

	var effectiveBal core.Amount
	if pkt.Snapshot != nil {
		effectiveBal = pkt.Snapshot.Balance
	}
	if tp.chain.HasBalance(tx.From) {
		dbBal, err := tp.chain.GetBalance(tx.From)
		if err != nil {
			return err
		}
		if pkt.Snapshot != nil && dbBal < effectiveBal {
			effectiveBal = dbBal
		} else if pkt.Snapshot == nil {
			effectiveBal = dbBal
		}
	}
	bond := core.BondAmount(tx.Amount)
	needed, _ := core.SafeAdd(tx.Amount, bond)
	if effectiveBal < needed {
		return core.ErrTxInsufficientBal
	}
	return nil
}

// ConfirmTx: 거래 확정 (원자적 저장).
func (tp *TxProcessor) ConfirmTx(tx *core.Transaction) error {
	bal, err := tp.chain.GetBalance(tx.From)
	if err != nil {
		return fmt.Errorf("ConfirmTx GetBalance: %w", err)
	}

	bond := core.BondAmount(tx.Amount)
	total, err := core.SafeAdd(tx.Amount, bond)
	if err != nil {
		return err
	}
	newSenderBal, err := core.SafeSub(bal, total)
	if err != nil {
		return fmt.Errorf("ConfirmTx SafeSub: %w", err)
	}

	recvBal, _ := tp.chain.GetBalance(tx.To)
	newRecvBal, err := core.SafeAdd(recvBal, tx.Amount)
	if err != nil {
		return err
	}

	if err := tp.dag.AdvanceTip(tx.From, tx.Hash, tx.ParentHashes); err != nil {
		return fmt.Errorf("ConfirmTx DAGTip: %w", err)
	}

	tx.Status = core.TxConfirmed
	if err := tp.chain.ConfirmTxBatch(&storage.ConfirmBatch{
		Tx:          tx,
		SenderBal:   newSenderBal,
		ReceiverBal: newRecvBal,
	}); err != nil {
		return fmt.Errorf("ConfirmTx batch: %w", err)
	}

	_ = tp.chain.AccumulateBond(bond)

	if tx.Sequence%snapshotInterval == 0 {
		go tp.autoSnapshot(tx.From, newSenderBal, tx.Sequence)
	}

	return nil
}

// ApplyConfirmedTxBalance: gossip으로 수신한 확정 TX 잔고 반영.
func (tp *TxProcessor) ApplyConfirmedTxBalance(tx *core.Transaction) error {
	if tp.chain.IsConfirmedTxApplied(tx.Hash) {
		return nil
	}
	recvBal, _ := tp.chain.GetBalance(tx.To)
	newBal, err := core.SafeAdd(recvBal, tx.Amount)
	if err != nil {
		return err
	}
	if err := tp.chain.SaveBalance(tx.To, newBal); err != nil {
		return err
	}
	_ = tp.chain.MarkConfirmedTxApplied(tx.Hash)
	return nil
}

func (tp *TxProcessor) autoSnapshot(pk core.PubKey, bal core.Amount, seq uint64) {
	tips, _ := tp.dag.CurrentTips(pk)
	snap := &core.Snapshot{
		PubKey:    pk,
		Sequence:  seq,
		Balance:   bal,
		DAGTips:   tips,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(snapshotTTL),
	}
	_ = tp.snap.SaveSnapshot(snap)
}
