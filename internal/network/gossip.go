package network

import (
	"math/rand"
	"sync"
	"time"

	"cdml/internal/core"
)

// 내부 파라미터 비공개
const (
	gossipFanout          = 8
	gossipHops            = 6
	gossipMsgTTL          = 10 * time.Minute
	gossipCleanupInterval = time.Minute
)

// GossipMsg: gossip 메시지 캐시 항목.
type GossipMsg struct {
	seenAt time.Time
	hops   int
}

// Gossip: fanout gossip 관리자.
type Gossip struct {
	mu   sync.Mutex
	seen map[core.Hash32]*GossipMsg
}

func NewGossip() *Gossip {
	g := &Gossip{
		seen: make(map[core.Hash32]*GossipMsg),
	}
	go g.cleanup()
	return g
}

// ShouldForward: 이 메시지를 전파해야 하면 true, 이미 본 메시지면 false.
func (g *Gossip) ShouldForward(msgID core.Hash32) (bool, int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if msg, ok := g.seen[msgID]; ok {
		if msg.hops >= gossipHops {
			return false, msg.hops
		}
		msg.hops++
		return true, msg.hops
	}

	g.seen[msgID] = &GossipMsg{seenAt: time.Now(), hops: 1}
	return true, 1
}

func (g *Gossip) cleanup() {
	ticker := time.NewTicker(gossipCleanupInterval)
	defer ticker.Stop()
	for range ticker.C {
		g.mu.Lock()
		for id, msg := range g.seen {
			if time.Since(msg.seenAt) > gossipMsgTTL {
				delete(g.seen, id)
			}
		}
		g.mu.Unlock()
	}
}

// SelectPeers: fanout 수만큼 무작위 피어 선택.
func SelectPeers(peers []*Peer, n int) []*Peer {
	if n >= len(peers) {
		return peers
	}
	shuffled := make([]*Peer, len(peers))
	copy(shuffled, peers)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	return shuffled[:n]
}

// Fanout: gossip fanout 수 반환.
func Fanout() int { return gossipFanout }
