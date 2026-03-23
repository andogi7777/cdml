package node

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"cdml/internal/core"
	"cdml/internal/crypto"
	"cdml/internal/network"
	"cdml/internal/protocol"
	"cdml/internal/storage"
)

// 내부 파라미터 비공개
const (
	quorumK     = 3
	snapshotTTL = 6 * time.Hour
)

// Node: CDML 노드.
type Node struct {
	cfg     *Config
	kp      *crypto.KeyPair
	db      *storage.DB
	chain   *storage.ChainStore
	snap    *storage.SnapshotStore
	witness *storage.WitnessStore
	nonce   *storage.NonceLockStore
	dag     *protocol.DAGManager
	txProc  *protocol.TxProcessor
	gossip  *network.Gossip

	mu        sync.RWMutex
	peers     map[core.PubKey]*network.Peer
	pendingTx map[core.Hash32]*pendingMeta
	listener  net.Listener
}

type pendingMeta struct {
	tx        *core.Transaction
	sigs      []core.WitnessSignature
	sigMu     sync.Mutex
	confirmed bool
}

func New(cfg *Config) (*Node, error) {
	kp, err := loadOrCreateKey(cfg.PrivKeyPath)
	if err != nil {
		return nil, err
	}

	db, err := storage.Open(storage.Options{Path: cfg.DBPath})
	if err != nil {
		return nil, err
	}

	chain := storage.NewChainStore(db)
	snap := storage.NewSnapshotStore(db)
	witness := storage.NewWitnessStore(db)
	nonce := storage.NewNonceLockStore(db)
	dag := protocol.NewDAGManager()
	txProc := protocol.NewTxProcessor(chain, snap, witness, nonce, dag)

	return &Node{
		cfg:       cfg,
		kp:        kp,
		db:        db,
		chain:     chain,
		snap:      snap,
		witness:   witness,
		nonce:     nonce,
		dag:       dag,
		txProc:    txProc,
		gossip:    network.NewGossip(),
		peers:     make(map[core.PubKey]*network.Peer),
		pendingTx: make(map[core.Hash32]*pendingMeta),
	}, nil
}

func (n *Node) Start() error {
	ln, err := net.Listen("tcp", n.cfg.P2PAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", n.cfg.P2PAddr, err)
	}
	n.listener = ln
	log.Printf("[cdml] node started pubkey=%x p2p=%s", n.kp.Public[:4], n.cfg.P2PAddr)

	go n.acceptLoop()

	for _, seed := range n.cfg.SeedPeers {
		go n.connectToSeed(seed)
	}
	return nil
}

func (n *Node) Stop() {
	if n.listener != nil {
		n.listener.Close()
	}
	n.mu.RLock()
	for _, p := range n.peers {
		p.Close()
	}
	n.mu.RUnlock()
	n.db.Close()
}

func (n *Node) acceptLoop() {
	for {
		conn, err := n.listener.Accept()
		if err != nil {
			return
		}
		go n.handleInbound(conn)
	}
}

func (n *Node) handleInbound(conn net.Conn) {
	conn.SetDeadline(time.Now().Add(core.HandshakeTimeout))
	// 상대 pubkey 수신
	pkt, err := network.ReadPacket(conn)
	if err != nil || pkt.Type != core.PacketHandshake {
		conn.Close()
		return
	}
	var theirPub core.PubKey
	if err := json.Unmarshal(pkt.Payload, &theirPub); err != nil {
		conn.Close()
		return
	}
	// 내 pubkey 응답 (양방향 핸드셰이크)
	myPayload, _ := json.Marshal(n.kp.Public)
	replyPkt := &network.Packet{Type: core.PacketHandshake, Payload: myPayload}
	replyData, _ := replyPkt.Encode()
	conn.Write(replyData)
	conn.SetDeadline(time.Time{})

	peer := network.NewPeer(conn, theirPub, conn.RemoteAddr().String(), n.removePeer)
	n.addPeer(peer)
	go peer.StartWritePump()
	go n.readLoop(peer)
}

func (n *Node) connectToSeed(addr string) {
	for {
		conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err != nil {
			time.Sleep(5 * time.Second)
			continue
		}

		payload, _ := json.Marshal(n.kp.Public)
		pkt := &network.Packet{Type: core.PacketHandshake, Payload: payload}
		data, _ := pkt.Encode()
		conn.SetDeadline(time.Now().Add(core.HandshakeTimeout))
		conn.Write(data)
		// 상대 pubkey 수신 (양방향 핸드셰이크)
		replyPkt, rerr := network.ReadPacket(conn)
		if rerr != nil || replyPkt.Type != core.PacketHandshake {
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}
		var theirPub core.PubKey
		if jerr := json.Unmarshal(replyPkt.Payload, &theirPub); jerr != nil {
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}
		conn.SetDeadline(time.Time{})

		peer := network.NewPeer(conn, theirPub, addr, n.removePeer)
		n.addPeer(peer)
		go peer.StartWritePump()
		n.readLoop(peer)

		time.Sleep(3 * time.Second)
	}
}

func (n *Node) readLoop(peer *network.Peer) {
	defer peer.Close()
	for {
		pkt, err := peer.ReadPacket()
		if err != nil {
			return
		}
		n.handlePacket(peer, pkt)
	}
}

func (n *Node) handlePacket(peer *network.Peer, pkt *network.Packet) {
	switch pkt.Type {
	case core.PacketNonceLock:
		n.handleNonceLock(pkt)
	case core.PacketTxVerify:
		n.handleTxVerify(pkt)
	case core.PacketTxWitnessSig:
		n.handleWitnessSig(pkt)
	case core.PacketTxConfirmed:
		n.handleTxConfirmed(pkt)
	case core.PacketPing:
		n.sendToPeer(peer, &network.Packet{Type: core.PacketPong})
	}
}

type nonceLockMsg struct {
	From  core.PubKey
	Nonce uint64
}

func (n *Node) handleNonceLock(pkt *network.Packet) {
	should, _ := n.gossip.ShouldForward(pkt.MsgID)
	if !should {
		return
	}
	var msg nonceLockMsg
	if err := json.Unmarshal(pkt.Payload, &msg); err != nil {
		return
	}
	_ = n.nonce.Lock(msg.From, msg.Nonce)
	n.broadcast(pkt, nil)
}

type txVerifyMsg struct {
	Tx       *core.Transaction
	Snapshot *core.Snapshot
}

func (n *Node) handleTxVerify(pkt *network.Packet) {
	// gossip 중복 방지
	should, _ := n.gossip.ShouldForward(pkt.MsgID)
	if !should {
		return
	}
	var msg txVerifyMsg
	if err := json.Unmarshal(pkt.Payload, &msg); err != nil {
		return
	}
	// 이 노드가 활성 증인인지 확인
	validSet, err := n.witness.GetActiveSet()
	if err != nil || len(validSet) == 0 {
		return
	}
	if _, isWitness := validSet[n.kp.Public]; !isWitness {
		return
	}

	tips, _ := n.dag.CurrentTips(n.kp.Public)
	var tip core.Hash32
	if len(tips) > 0 {
		tip = tips[0]
	}

	verifyPkt := &protocol.VerifyPacket{
		Tx:        msg.Tx,
		Snapshot:  msg.Snapshot,
		KnownTips: map[core.Hash32]struct{}{},
	}
	if err := n.txProc.VerifyTx(verifyPkt); err != nil {
		log.Printf("[cdml] verify failed: %v", err)
		return
	}

	nonceLockSeen := n.nonce.IsLocked(msg.Tx.From, msg.Tx.Nonce)
	sig, err := crypto.SignAsWitness(n.kp, msg.Tx.Hash, tip, nonceLockSeen)
	if err != nil {
		return
	}

	type witSigMsg struct {
		TxHash core.Hash32
		Sig    core.WitnessSignature
	}
	payload, _ := json.Marshal(witSigMsg{TxHash: msg.Tx.Hash, Sig: sig})
	// originator에게 직접 전송, 연결 없으면 broadcast로 전파
	witPkt := &network.Packet{
		Type:    core.PacketTxWitnessSig,
		Payload: payload,
	}
	// MsgID: txHash XOR witness pubkey (중복 gossip 방지)
	var sigMsgID core.Hash32
	for i := 0; i < 32; i++ {
		sigMsgID[i] = msg.Tx.Hash[i] ^ sig.WitnessPubKey[i]
	}
	witPkt.MsgID = sigMsgID
	// originator에게 직접 전송, 연결 없으면 broadcast
	n.mu.RLock()
	_, directConn := n.peers[msg.Tx.From]
	n.mu.RUnlock()
	if directConn {
		n.sendToOriginator(msg.Tx.From, witPkt)
	} else {
		n.broadcast(witPkt, nil)
	}
	// TxVerify gossip 재전파 — 직접 연결 안 된 증인들도 받을 수 있도록
	n.broadcast(pkt, nil)
}

func (n *Node) handleWitnessSig(pkt *network.Packet) {
	type witSigMsg struct {
		TxHash core.Hash32
		Sig    core.WitnessSignature
	}
	var msg witSigMsg
	if err := json.Unmarshal(pkt.Payload, &msg); err != nil {
		return
	}

	n.mu.RLock()
	meta, ok := n.pendingTx[msg.TxHash]
	n.mu.RUnlock()
	if !ok {
		// 이 노드가 originator가 아닌 경우 — gossip으로 전파해서 originator에게 도달시킴
		should, _ := n.gossip.ShouldForward(pkt.MsgID)
		if should {
			n.broadcast(pkt, nil)
		}
		return
	}

	meta.sigMu.Lock()
	meta.sigs = append(meta.sigs, msg.Sig)
	count := len(meta.sigs)
	shouldConfirm := count == quorumK && !meta.confirmed
	if shouldConfirm {
		meta.confirmed = true
	}
	sigsCopy := make([]core.WitnessSignature, len(meta.sigs))
	copy(sigsCopy, meta.sigs)
	meta.sigMu.Unlock()

	log.Printf("[cdml] [quorum] sigs=%d hash=%x", count, msg.TxHash[:4])

	if shouldConfirm {
		n.mu.Lock()
		delete(n.pendingTx, msg.TxHash)
		n.mu.Unlock()
		go n.confirmTx(meta.tx, sigsCopy)
	}
}

type confirmedMsg struct {
	Tx *core.Transaction
}

func (n *Node) handleTxConfirmed(pkt *network.Packet) {
	should, _ := n.gossip.ShouldForward(pkt.MsgID)
	if !should {
		return
	}
	var msg confirmedMsg
	if err := json.Unmarshal(pkt.Payload, &msg); err != nil {
		return
	}
	_ = n.txProc.ApplyConfirmedTxBalance(msg.Tx)
	n.broadcast(pkt, nil)
}

func (n *Node) confirmTx(tx *core.Transaction, sigs []core.WitnessSignature) {
	validSet, err := n.witness.GetActiveSet()
	if err != nil {
		log.Printf("[cdml] confirmTx GetActiveSet: %v", err)
		return
	}
	if err := crypto.VerifyQuorum(sigs, tx.Hash, validSet, quorumK); err != nil {
		log.Printf("[cdml] confirmTx VerifyQuorum: %v", err)
		return
	}
	tx.WitnessSigs = sigs
	if err := n.txProc.ConfirmTx(tx); err != nil {
		log.Printf("[cdml] confirmTx failed: %v", err)
		return
	}
	log.Printf("[cdml] TX confirmed hash=%x seq=%d", tx.Hash[:4], tx.Sequence)

	payload, _ := json.Marshal(confirmedMsg{Tx: tx})
	n.broadcast(&network.Packet{
		Type:    core.PacketTxConfirmed,
		MsgID:   tx.Hash,
		Payload: payload,
	}, nil)
}

func (n *Node) SendTx(to core.PubKey, amount core.Amount) (*core.Transaction, error) {
	tx, err := n.txProc.BuildTx(n.kp, to, amount)
	if err != nil {
		return nil, err
	}
	if err := n.txProc.AcquireNonceLock(tx); err != nil {
		return nil, err
	}

	n.mu.Lock()
	n.pendingTx[tx.Hash] = &pendingMeta{tx: tx}
	n.mu.Unlock()

	nlPayload, _ := json.Marshal(nonceLockMsg{From: tx.From, Nonce: tx.Nonce})
	n.broadcast(&network.Packet{
		Type:    core.PacketNonceLock,
		MsgID:   tx.Hash,
		Payload: nlPayload,
	}, nil)

	bal, _ := n.chain.GetBalance(n.kp.Public)
	tips, _ := n.dag.CurrentTips(n.kp.Public)
	seq, _ := n.chain.GetLatestSeq(n.kp.Public)
	snap := &core.Snapshot{
		PubKey:    n.kp.Public,
		Sequence:  seq,
		Balance:   bal,
		DAGTips:   tips,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(snapshotTTL),
	}

	// TxVerify broadcast — 직접 연결 여부와 무관하게 모든 증인에게 전파
	txVerifyPayload, _ := json.Marshal(txVerifyMsg{Tx: tx, Snapshot: snap})
	n.broadcast(&network.Packet{
		Type:    core.PacketTxVerify,
		MsgID:   tx.Hash,
		Payload: txVerifyPayload,
	}, nil)
	witnesses, _ := n.witness.GetActiveWitnesses()
	log.Printf("[cdml] TxVerify broadcast witnesses=%d tx=%x", len(witnesses), tx.Hash[:4])

	return tx, nil
}

func (n *Node) addPeer(peer *network.Peer) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.peers) >= core.MaxPeers {
		peer.Close()
		return
	}
	n.peers[peer.PubKey] = peer
}

func (n *Node) removePeer(peer *network.Peer) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.peers, peer.PubKey)
}

func (n *Node) broadcast(pkt *network.Packet, exclude *network.Peer) {
	n.mu.RLock()
	peers := make([]*network.Peer, 0, len(n.peers))
	for _, p := range n.peers {
		if p != exclude {
			peers = append(peers, p)
		}
	}
	n.mu.RUnlock()

	selected := network.SelectPeers(peers, network.Fanout())
	for _, p := range selected {
		_ = p.Send(pkt)
	}
}

func (n *Node) sendToPeer(peer *network.Peer, pkt *network.Packet) {
	_ = peer.Send(pkt)
}

func (n *Node) sendToOriginator(from core.PubKey, pkt *network.Packet) {
	n.mu.RLock()
	peer, ok := n.peers[from]
	n.mu.RUnlock()
	if ok {
		_ = peer.Send(pkt)
	}
}

func (n *Node) PubKey() core.PubKey                { return n.kp.Public }
func (n *Node) Chain() *storage.ChainStore          { return n.chain }
func (n *Node) Snap() *storage.SnapshotStore        { return n.snap }
func (n *Node) Witness() *storage.WitnessStore      { return n.witness }
func (n *Node) TxProc() *protocol.TxProcessor       { return n.txProc }

func (n *Node) PeerList() []map[string]string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	result := make([]map[string]string, 0, len(n.peers))
	for _, p := range n.peers {
		result = append(result, map[string]string{
			"pubkey": fmt.Sprintf("%x", p.PubKey[:]),
			"addr":   p.Addr,
		})
	}
	return result
}

func (n *Node) PeerCount() int {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return len(n.peers)
}
