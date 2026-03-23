package crypto

import (
	"fmt"

	"cdml/internal/core"
)

// SignTx: 거래에 발신자 서명.
func SignTx(kp *KeyPair, tx *core.Transaction) error {
	tx.Hash = HashTx(tx.Sequence, tx.Nonce, tx.From, tx.To, tx.Amount, tx.ParentHashes)
	sig, err := kp.Sign(tx.Hash[:])
	if err != nil {
		return fmt.Errorf("sign tx: %w", err)
	}
	tx.SenderSig = sig
	return nil
}

// VerifyTx: 거래 발신자 서명 검증.
func VerifyTx(tx *core.Transaction) error {
	expected := HashTx(tx.Sequence, tx.Nonce, tx.From, tx.To, tx.Amount, tx.ParentHashes)
	if expected != tx.Hash {
		return core.ErrTxInvalidSignature
	}
	if !Verify(tx.From, tx.Hash[:], tx.SenderSig) {
		return core.ErrTxInvalidSignature
	}
	return nil
}

// SignAsWitness: 증인으로서 거래 서명.
func SignAsWitness(kp *KeyPair, txHash core.Hash32, dagTip core.Hash32, nonceLockSeen bool) (core.WitnessSignature, error) {
	msg := witnessMsg(txHash, dagTip, nonceLockSeen)
	sig, err := kp.Sign(msg)
	if err != nil {
		return core.WitnessSignature{}, fmt.Errorf("sign as witness: %w", err)
	}
	return core.WitnessSignature{
		WitnessPubKey: kp.Public,
		DAGTipAtSign:  dagTip,
		NonceLockSeen: nonceLockSeen,
		Sig:           sig,
	}, nil
}

// VerifyWitnessSig: 증인 서명 검증.
func VerifyWitnessSig(ws core.WitnessSignature, txHash core.Hash32) error {
	msg := witnessMsg(txHash, ws.DAGTipAtSign, ws.NonceLockSeen)
	if !Verify(ws.WitnessPubKey, msg, ws.Sig) {
		return fmt.Errorf("invalid witness signature from %x", ws.WitnessPubKey[:4])
	}
	return nil
}

// VerifyQuorum: 쿼럼 충족 여부 확인. (내부 검증 로직 비공개)
func VerifyQuorum(
	sigs     []core.WitnessSignature,
	txHash   core.Hash32,
	validSet map[core.PubKey]struct{},
	k        int,
) error {
	valid := countValidSigs(sigs, txHash, validSet)
	if valid < k {
		return fmt.Errorf("%w: required %d, got %d", core.ErrQuorumNotMet, k, valid)
	}
	return nil
}

// countValidSigs: 유효 서명 수 계산. (내부 구현)
func countValidSigs(sigs []core.WitnessSignature, txHash core.Hash32, validSet map[core.PubKey]struct{}) int {
	counted := make(map[core.PubKey]struct{}, len(sigs))
	valid := 0
	for _, ws := range sigs {
		if _, dup := counted[ws.WitnessPubKey]; dup {
			continue
		}
		if _, ok := validSet[ws.WitnessPubKey]; !ok {
			continue
		}
		if !ws.NonceLockSeen {
			continue
		}
		if err := VerifyWitnessSig(ws, txHash); err != nil {
			continue
		}
		counted[ws.WitnessPubKey] = struct{}{}
		valid++
	}
	return valid
}

func witnessMsg(txHash, dagTip core.Hash32, nonceLockSeen bool) []byte {
	msg := make([]byte, 65)
	copy(msg[:32], txHash[:])
	copy(msg[32:64], dagTip[:])
	if nonceLockSeen {
		msg[64] = 1
	}
	return msg
}
