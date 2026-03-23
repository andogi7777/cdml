package network

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"cdml/internal/core"
)

// ─── Packet ──────────────────────────────────────────────────

// Packet: 네트워크 패킷.
type Packet struct {
	Type    core.PacketType
	MsgID   core.Hash32 // gossip 중복 방지용
	Payload []byte
}

func (p *Packet) Encode() ([]byte, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(buf[:4], uint32(len(data)))
	copy(buf[4:], data)
	return buf, nil
}

func ReadPacket(conn net.Conn) (*Packet, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(lenBuf[:])
	if size > 10*1024*1024 {
		return nil, fmt.Errorf("packet too large: %d", size)
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(conn, data); err != nil {
		return nil, err
	}
	var pkt Packet
	if err := json.Unmarshal(data, &pkt); err != nil {
		return nil, err
	}
	return &pkt, nil
}

// ─── PeerState ───────────────────────────────────────────────

type PeerState int

const (
	PeerConnecting PeerState = iota
	PeerActive
	PeerClosed
)

// ─── Peer ────────────────────────────────────────────────────

// Peer: 연결된 피어.
type Peer struct {
	mu      sync.Mutex
	PubKey  core.PubKey
	Addr    string
	conn    net.Conn
	sendCh  chan []byte
	state   PeerState
	onClose func(*Peer)
}

func NewPeer(conn net.Conn, pubKey core.PubKey, addr string, onClose func(*Peer)) *Peer {
	p := &Peer{
		PubKey:  pubKey,
		Addr:    addr,
		conn:    conn,
		sendCh:  make(chan []byte, 64),
		state:   PeerActive,
		onClose: onClose,
	}
	return p
}

// Send: 패킷 전송 (비블로킹).
func (p *Peer) Send(pkt *Packet) error {
	p.mu.Lock()
	if p.state != PeerActive {
		p.mu.Unlock()
		return fmt.Errorf("peer not active")
	}
	p.mu.Unlock()

	data, err := pkt.Encode()
	if err != nil {
		return err
	}
	select {
	case p.sendCh <- data:
		return nil
	default:
		return fmt.Errorf("send channel full")
	}
}

// StartWritePump: 전송 goroutine.
func (p *Peer) StartWritePump() {
	defer p.Close()
	for data := range p.sendCh {
		p.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, err := p.conn.Write(data); err != nil {
			return
		}
	}
}

// Close: 연결 종료.
func (p *Peer) Close() {
	p.mu.Lock()
	if p.state == PeerClosed {
		p.mu.Unlock()
		return
	}
	p.state = PeerClosed
	p.mu.Unlock()

	p.conn.Close()
	close(p.sendCh)
	if p.onClose != nil {
		p.onClose(p)
	}
}

// ReadPacket: 수신 패킷 읽기.
func (p *Peer) ReadPacket() (*Packet, error) {
	p.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	return ReadPacket(p.conn)
}
