package core

import "time"

const (
	// 거래
	MaxAmount Amount = 9_000_000_000_000_000

	// P2P
	MaxPeers         = 50
	HandshakeTimeout = 5 * time.Second
	PingInterval     = 30 * time.Second
)
