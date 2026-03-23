package core

import (
	"errors"
	"fmt"
)

var (
	ErrAmountZero         = errors.New("amount is zero")
	ErrAmountOverflow     = errors.New("amount overflow")
	ErrAmountUnderflow    = errors.New("amount underflow")
	ErrAmountExceedMax    = errors.New("amount exceeds maximum")

	ErrTxInvalidSignature = errors.New("invalid transaction signature")
	ErrTxInsufficientBal  = errors.New("insufficient balance")
	ErrTxInvalidSequence  = errors.New("invalid sequence")
	ErrTxAlreadyExists    = errors.New("transaction already exists")
	ErrTxNonceLocked      = errors.New("nonce already locked")

	ErrSnapshotNotFound   = errors.New("snapshot not found")
	ErrSnapshotExpired    = errors.New("snapshot has expired")

	ErrDAGEdgeDuplicate   = errors.New("dag edge already exists")

	ErrWitnessNotFound    = errors.New("witness not found")
	ErrQuorumNotMet       = errors.New("quorum not met")
)

// ErrDAGCycle: DAG 순환 감지 오류.
type ErrDAGCycle struct {
	From string
	To   string
}

func (e *ErrDAGCycle) Error() string {
	return fmt.Sprintf("dag cycle detected: %s -> %s", e.From, e.To)
}

func (e *ErrDAGCycle) Is(target error) bool {
	return target == ErrDAGCycleDetected
}

var ErrDAGCycleDetected = errors.New("dag cycle detected")
