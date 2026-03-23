package protocol

import (
	"fmt"
	"sort"
	"sync"

	"cdml/internal/core"
)

// 내부 파라미터 비공개
const dagCycleCheckDepth = 200

// DAGManager: 노드별 DAG 관리.
type DAGManager struct {
	mu    sync.RWMutex
	edges map[core.Hash32][]core.Hash32
}

func NewDAGManager() *DAGManager {
	return &DAGManager{edges: make(map[core.Hash32][]core.Hash32)}
}

// AddEdge: from → to 방향 엣지 추가. 순환이면 ErrDAGCycleDetected.
func (dm *DAGManager) AddEdge(from, to core.Hash32) error {
	if from == to {
		return &core.ErrDAGCycle{
			From: fmt.Sprintf("%x", from[:4]),
			To:   fmt.Sprintf("%x", to[:4]),
		}
	}
	dm.mu.Lock()
	defer dm.mu.Unlock()

	visited := make(map[core.Hash32]bool)
	if dm.isAncestorLocked(to, from, visited, 0) {
		return &core.ErrDAGCycle{
			From: fmt.Sprintf("%x", from[:4]),
			To:   fmt.Sprintf("%x", to[:4]),
		}
	}
	for _, p := range dm.edges[from] {
		if p == to {
			return core.ErrDAGEdgeDuplicate
		}
	}
	dm.edges[from] = append(dm.edges[from], to)
	return nil
}

func (dm *DAGManager) isAncestorLocked(cur, target core.Hash32, visited map[core.Hash32]bool, depth int) bool {
	if depth > dagCycleCheckDepth || visited[cur] {
		return false
	}
	visited[cur] = true
	for _, parent := range dm.edges[cur] {
		if parent == target {
			return true
		}
		if dm.isAncestorLocked(parent, target, visited, depth+1) {
			return true
		}
	}
	return false
}

// AdvanceTip: 거래 확정 시 DAG 팁 갱신.
func (dm *DAGManager) AdvanceTip(owner core.PubKey, txHash core.Hash32, parents []core.Hash32) error {
	for _, p := range parents {
		if err := dm.AddEdge(txHash, p); err != nil && err != core.ErrDAGEdgeDuplicate {
			return fmt.Errorf("DAGEdge: %w", err)
		}
	}
	return nil
}

// CurrentTips: 현재 팁(자식 없는 노드) 목록.
func (dm *DAGManager) CurrentTips(owner core.PubKey) ([]core.Hash32, error) {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	hasChild := make(map[core.Hash32]bool)
	for _, parents := range dm.edges {
		for _, p := range parents {
			hasChild[p] = true
		}
	}

	var tips []core.Hash32
	for node := range dm.edges {
		if !hasChild[node] {
			tips = append(tips, node)
		}
	}

	sort.Slice(tips, func(i, j int) bool {
		for k := 0; k < 32; k++ {
			if tips[i][k] != tips[j][k] {
				return tips[i][k] < tips[j][k]
			}
		}
		return false
	})
	return tips, nil
}

// Depth: DAG 최대 깊이.
func (dm *DAGManager) Depth(owner core.PubKey) (int, error) {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	if len(dm.edges) == 0 {
		return 0, nil
	}
	max := 0
	for node := range dm.edges {
		d := dm.depthFrom(node, make(map[core.Hash32]bool), 0)
		if d > max {
			max = d
		}
	}
	return max, nil
}

func (dm *DAGManager) depthFrom(cur core.Hash32, visited map[core.Hash32]bool, depth int) int {
	if visited[cur] || len(dm.edges[cur]) == 0 {
		return depth
	}
	visited[cur] = true
	max := depth
	for _, p := range dm.edges[cur] {
		d := dm.depthFrom(p, visited, depth+1)
		if d > max {
			max = d
		}
	}
	return max
}
