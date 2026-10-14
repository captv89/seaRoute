package route

import (
	"container/heap"
	"math"
	"slices"
)

type queueItem struct {
	node int32
	g    float64 // distance from the start
	f    float64 // g plus the estimate to the target
}

type queue []queueItem

func (q queue) Len() int           { return len(q) }
func (q queue) Less(i, j int) bool { return q[i].f < q[j].f }
func (q queue) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *queue) Push(x any)        { *q = append(*q, x.(queueItem)) }
func (q *queue) Pop() any {
	old := *q
	it := old[len(old)-1]
	*q = old[:len(old)-1]
	return it
}

// search returns the node path s -> t, the edges taken, and the length in
// metres. Edges whose passage is blocked are skipped.
//
// A* uses Graph.estimate, a lower bound on the remaining distance that
// never overestimates and grows by at most each edge's length, so A*
// returns the same shortest path as Dijkstra while visiting fewer nodes.
func (g *Graph) search(s, t int32, blocked []bool, alg Algorithm) ([]int32, []int32, float64, bool) {
	n := len(g.nodes)
	dist := make([]float64, n)
	for i := range dist {
		dist[i] = math.Inf(1)
	}
	prevEdge := make([]int32, n)
	prevNode := make([]int32, n)
	estimate := func(v int32) float64 {
		if alg == Dijkstra {
			return 0
		}
		return g.estimate(v, t)
	}

	dist[s] = 0
	prevNode[s] = -1
	q := &queue{{node: s, g: 0, f: estimate(s)}}
	for q.Len() > 0 {
		it := heap.Pop(q).(queueItem)
		v := it.node
		if it.g > dist[v] {
			continue // stale entry
		}
		if v == t {
			break
		}
		for e := g.first[v]; e < g.first[v+1]; e++ {
			if blocked[g.pass[e]] {
				continue
			}
			u := g.to[e]
			if d := dist[v] + g.weight[e]; d < dist[u] {
				dist[u] = d
				prevNode[u], prevEdge[u] = v, e
				heap.Push(q, queueItem{node: u, g: d, f: d + estimate(u)})
			}
		}
	}
	if math.IsInf(dist[t], 1) {
		return nil, nil, 0, false
	}

	var nodes, edges []int32
	for v := t; v != s; v = prevNode[v] {
		nodes = append(nodes, v)
		edges = append(edges, prevEdge[v])
	}
	nodes = append(nodes, s)
	slices.Reverse(nodes)
	slices.Reverse(edges)
	return nodes, edges, dist[t], true
}
