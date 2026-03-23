package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"cdml/internal/core"
	"cdml/internal/crypto"
	"cdml/node"
)

// snapshotTTL: API에서 생성하는 스냅샷 TTL. (내부 상수)
const snapshotTTL = 6 * time.Hour

type Handler struct {
	n *node.Node
}

func NewHandler(n *node.Node) *Handler { return &Handler{n: n} }

func (h *Handler) Healthz(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	pk := h.n.PubKey()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"pubkey":  fmt.Sprintf("%x", pk[:]),
		"peers":   h.n.PeerCount(),
		"version": "1.0.0",
	})
}

func (h *Handler) Balance(w http.ResponseWriter, r *http.Request) {
	pkHex := r.PathValue("pubkey")
	pk, err := crypto.PubKeyFromHex(pkHex)
	if err != nil {
		httpErr(w, http.StatusBadRequest, "INVALID_PUBKEY", err.Error())
		return
	}
	bal, err := h.n.Chain().GetBalance(pk)
	if err != nil {
		httpErr(w, http.StatusInternalServerError, "DB_ERROR", err.Error())
		return
	}
	seq, _ := h.n.Chain().GetLatestSeq(pk)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"pubkey":  pkHex,
		"balance": bal,
		"seq":     seq,
	})
}

func (h *Handler) SendTx(w http.ResponseWriter, r *http.Request) {
	var req struct {
		To     string      `json:"to"`
		Amount core.Amount `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpErr(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	to, err := crypto.PubKeyFromHex(req.To)
	if err != nil {
		httpErr(w, http.StatusBadRequest, "INVALID_PUBKEY", err.Error())
		return
	}
	tx, err := h.n.SendTx(to, req.Amount)
	if err != nil {
		httpErr(w, http.StatusBadRequest, "TX_FAILED", err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"tx_hash": fmt.Sprintf("%x", tx.Hash[:]),
		"seq":     tx.Sequence,
		"nonce":   tx.Nonce,
	})
}

func (h *Handler) GetTx(w http.ResponseWriter, r *http.Request) {
	pkHex := r.PathValue("pubkey")
	seqStr := r.PathValue("seq")

	pk, err := crypto.PubKeyFromHex(pkHex)
	if err != nil {
		httpErr(w, http.StatusBadRequest, "INVALID_PUBKEY", err.Error())
		return
	}
	seq, err := strconv.ParseUint(seqStr, 10, 64)
	if err != nil {
		httpErr(w, http.StatusBadRequest, "INVALID_SEQ", err.Error())
		return
	}
	tx, err := h.n.Chain().GetTx(pk, seq)
	if err != nil || tx == nil {
		httpErr(w, http.StatusNotFound, "NOT_FOUND", "transaction not found")
		return
	}
	json.NewEncoder(w).Encode(tx)
}

func (h *Handler) Peers(w http.ResponseWriter, r *http.Request) {
	peers := h.n.PeerList()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"count": len(peers),
		"peers": peers,
	})
}

func (h *Handler) Faucet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PubKey string      `json:"pubkey"`
		Amount core.Amount `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpErr(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	pk, err := crypto.PubKeyFromHex(req.PubKey)
	if err != nil {
		httpErr(w, http.StatusBadRequest, "INVALID_PUBKEY", err.Error())
		return
	}
	if req.Amount == 0 {
		req.Amount = 10_000 * 1_000_000
	}
	if err := h.n.Chain().SaveBalance(pk, req.Amount); err != nil {
		httpErr(w, http.StatusInternalServerError, "DB_ERROR", err.Error())
		return
	}
	bal, _ := h.n.Chain().GetBalance(pk)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"pubkey":  req.PubKey,
		"amount":  req.Amount,
		"balance": bal,
	})
}

func (h *Handler) ActiveWitnesses(w http.ResponseWriter, r *http.Request) {
	witnesses, err := h.n.Witness().GetActiveWitnesses()
	if err != nil {
		httpErr(w, http.StatusInternalServerError, "DB_ERROR", err.Error())
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"count":  len(witnesses),
		"active": witnesses,
	})
}

func (h *Handler) CreateSnapshot(w http.ResponseWriter, r *http.Request) {
	pk := h.n.PubKey()
	bal, _ := h.n.Chain().GetBalance(pk)
	seq, _ := h.n.Chain().GetLatestSeq(pk)

	snap := &core.Snapshot{
		PubKey:    pk,
		Sequence:  seq,
		Balance:   bal,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(snapshotTTL),
	}
	if err := h.n.Snap().SaveSnapshot(snap); err != nil {
		httpErr(w, http.StatusInternalServerError, "DB_ERROR", err.Error())
		return
	}
	json.NewEncoder(w).Encode(snap)
}

func (h *Handler) RegisterWitnesses(w http.ResponseWriter, r *http.Request) {
	// init.go가 hex 문자열로 pubkey를 전송하므로 별도 구조체로 수신 후 변환
	var raw []struct {
		PubKey string `json:"PubKey"`
		Addr   string `json:"Addr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		httpErr(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	witnesses := make([]core.Witness, 0, len(raw))
	for _, r := range raw {
		pk, err := crypto.PubKeyFromHex(r.PubKey)
		if err != nil {
			httpErr(w, http.StatusBadRequest, "INVALID_PUBKEY", err.Error())
			return
		}
		witnesses = append(witnesses, core.Witness{
			PubKey:  pk,
			Addr:    r.Addr,
			AddedAt: time.Now(),
		})
	}
	if err := h.n.Witness().SaveActiveWitnesses(witnesses); err != nil {
		httpErr(w, http.StatusInternalServerError, "DB_ERROR", err.Error())
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"registered": len(witnesses),
	})
}

func httpErr(w http.ResponseWriter, status int, code, msg string) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"code":    code,
		"message": msg,
	})
}
