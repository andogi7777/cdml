package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"cdml/internal/core"
)

// KeyPair: Ed25519 키쌍.
type KeyPair struct {
	Public  core.PubKey
	private ed25519.PrivateKey
}

// Generate: 새 키쌍 생성.
func Generate() (*KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	var kp KeyPair
	copy(kp.Public[:], pub)
	kp.private = priv
	return &kp, nil
}

// LoadFromBytes: 64바이트 개인키에서 키쌍 복원.
func LoadFromBytes(privBytes []byte) (*KeyPair, error) {
	if len(privBytes) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid key size: %d", len(privBytes))
	}
	priv := ed25519.PrivateKey(privBytes)
	pub := priv.Public().(ed25519.PublicKey)
	var kp KeyPair
	copy(kp.Public[:], pub)
	kp.private = priv
	return &kp, nil
}

// PrivateBytes: 개인키 바이트 반환.
func (kp *KeyPair) PrivateBytes() []byte {
	return []byte(kp.private)
}

// Sign: 데이터 서명.
func (kp *KeyPair) Sign(data []byte) (core.Signature, error) {
	sig := ed25519.Sign(kp.private, data)
	var out core.Signature
	copy(out[:], sig)
	return out, nil
}

// Verify: 서명 검증.
func Verify(pubKey core.PubKey, data []byte, sig core.Signature) bool {
	return ed25519.Verify(pubKey[:], data, sig[:])
}

// PubKeyFromHex: hex 문자열에서 공개키 파싱.
func PubKeyFromHex(s string) (core.PubKey, error) {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 32 {
		return core.PubKey{}, fmt.Errorf("invalid pubkey hex: %s", s)
	}
	var pk core.PubKey
	copy(pk[:], b)
	return pk, nil
}
