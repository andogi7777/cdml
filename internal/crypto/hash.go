package crypto

import (
	"encoding/binary"

	"cdml/internal/core"
	"golang.org/x/crypto/blake2b"
)

// Sum: Blake2b-256 해시.
func Sum(data []byte) core.Hash32 {
	return blake2b.Sum256(data)
}

// HashTx: 거래 필드로 해시 계산.
func HashTx(seq, nonce uint64, from, to core.PubKey, amount core.Amount, parents []core.Hash32) core.Hash32 {
	h, _ := blake2b.New256(nil)
	var buf [8]byte

	binary.BigEndian.PutUint64(buf[:], seq)
	h.Write(buf[:])
	binary.BigEndian.PutUint64(buf[:], nonce)
	h.Write(buf[:])
	h.Write(from[:])
	h.Write(to[:])
	binary.BigEndian.PutUint64(buf[:], amount)
	h.Write(buf[:])
	for _, p := range parents {
		h.Write(p[:])
	}

	var out core.Hash32
	copy(out[:], h.Sum(nil))
	return out
}
