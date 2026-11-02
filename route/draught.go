package route

import (
	"fmt"
	"math"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

// endpointExemptNM is the radius around each requested point inside which
// shallow edges stay open, so a route can leave and enter a shallow port.
const endpointExemptNM = 15.0

// searchCtx is what a search needs to decide which edges are open.
type searchCtx struct {
	blocked []bool // by passage
	lifted  []bool // by scheme, nil = none
	noTSS   bool
	shallow []bool // per edge: closed by depth; nil = none
}

// Notice tells the caller that the vessel's draught changed the route.
type Notice struct {
	Kind     string     `json:"kind"` // "draught-reroute" | "draught-no-route"
	Text     string     `json:"text"`
	ExtraNM  float64    `json:"extra_nm"`
	Avoided  []Position `json:"avoided"`
	Passages []string   `json:"passages"` // passage tags on the avoided stretch
}

// buildMinDepth fills g.minDepth: the controlling stored depth along each edge,
// +Inf when unknown, without depth data, in a passage whose PassageLimits
// entry fixes a maximum draught (the table value governs there, through the
// passage block) or within passageApproachNM of such a passage. The value is
// segmentDepth: cleaned against OSM land and robust to isolated cells.
func (g *Graph) buildMinDepth() {
	start := time.Now()
	g.minDepth = make([]float32, len(g.to))
	inf := float32(math.Inf(1))
	if g.depth == nil {
		for i := range g.minDepth {
			g.minDepth[i] = inf
		}
		return
	}
	tabled := make([]bool, len(g.passes))
	for i, name := range g.passes {
		tabled[i] = PassageLimits[name].MaxDraughtM > 0
	}
	g.buildApproach()
	nearTabled := make([]bool, len(g.nodes)) // within passageApproachNM of a fixed-limit passage node
	for v, p := range g.nodes {
		nearTabled[v] = g.approachPassage(p) != ""
	}
	const chunk = 2048
	var wg sync.WaitGroup
	var mu sync.Mutex
	next := 0
	for range runtime.NumCPU() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				lo := next
				next += chunk
				mu.Unlock()
				if lo >= len(g.nodes) {
					return
				}
				for v := lo; v < min(lo+chunk, len(g.nodes)); v++ {
					for e := g.first[v]; e < g.first[v+1]; e++ {
						tw := g.twin[e]
						if e > tw {
							continue // the lower index of a pair fills both
						}
						m := inf
						if !tabled[g.pass[e]] && !nearTabled[v] && !nearTabled[g.to[e]] {
							// Only fully stored edges close the network: closing edges that leave
							// the stored tiles too cuts the real Lombok and Torres alternatives
							// (TestDraughtReroutesAroundSingapore, TestRealTorresStraitClosesAt12m).
							// The taut and hazard passes judge the cells that were read.
							if d, known := g.segmentDepth(g.nodes[v], g.nodes[g.to[e]], g.depthRun(e), 255); known {
								m = float32(d)
							}
						}
						g.minDepth[e], g.minDepth[tw] = m, m
					}
				}
			}
		}()
	}
	wg.Wait()
	g.stats.depthBuild = time.Since(start)
}

// shallowEdges returns the edges closed by depth for a vessel needing lim
// metres in open water and limConfined in confined passages, or nil when none
// closes. Edges with an end within endpointExemptNM of a requested point stay open.
func (g *Graph) shallowEdges(lim, limConfined float64, ends []Position) []bool {
	if g.depth == nil {
		return nil
	}
	confined := make([]bool, len(g.passes))
	for i, name := range g.passes {
		confined[i] = PassageLimits[name].Confined
	}
	near := make([]int8, len(g.nodes)) // 0 unknown, 1 near an endpoint, -1 not
	exempt := func(v int32) bool {
		if near[v] == 0 {
			near[v] = -1
			for _, p := range ends {
				if Distance(g.nodes[v], p) <= endpointExemptNM*MetresPerNM {
					near[v] = 1
					break
				}
			}
		}
		return near[v] > 0
	}
	var out []bool
	for v := range g.nodes {
		for e := g.first[v]; e < g.first[v+1]; e++ {
			l := lim
			if confined[g.pass[e]] {
				l = limConfined
			}
			if float64(g.minDepth[e]) >= l {
				continue
			}
			if exempt(int32(v)) || exempt(g.to[e]) {
				continue
			}
			if out == nil {
				out = make([]bool, len(g.to))
			}
			out[e] = true
		}
	}
	return out
}

// draughtBlocked returns blocked plus the passages whose fixed draught limit
// the vessel exceeds, and the names of those added passages.
func (g *Graph) draughtBlocked(blocked []bool, draughtM float64) ([]bool, map[string]bool) {
	out := slices.Clone(blocked)
	closed := map[string]bool{}
	for i, name := range g.passes {
		if lim, ok := PassageLimits[name]; ok && lim.MaxDraughtM > 0 && draughtM > lim.MaxDraughtM {
			if !out[i] {
				closed[name] = true
			}
			out[i] = true
		}
	}
	return out, closed
}

// pathSeq is a search result: the nodes taken and the edge between each pair.
type pathSeq struct {
	nodes, edges []int32
}

// divergence is one stretch where the constrained and unconstrained paths part
// and rejoin. c and u are the stretches of each path.
type divergence struct {
	c, u         pathSeq
	cPass, uPass []string
	extraM       float64
}

func (g *Graph) stretchPassages(edges []int32) []string {
	var out []string
	for _, e := range edges {
		if name := g.passes[g.pass[e]]; name != "" && (len(out) == 0 || out[len(out)-1] != name) {
			out = append(out, name)
		}
	}
	return out
}

func (g *Graph) stretchLen(edges []int32) float64 {
	var d float64
	for _, e := range edges {
		d += g.weight[e]
	}
	return d
}

// divergences walks the constrained path c against the unconstrained path u
// (both from the same start to the same end) and returns each stretch where
// they differ that adds at least 1 NM or changes the passages used.
func (g *Graph) divergences(c, u pathSeq) []divergence {
	posU := make(map[int32]int, len(u.nodes))
	for k, v := range u.nodes {
		posU[v] = k
	}
	var out []divergence
	ia, ja := 0, 0
	for ib := 1; ib < len(c.nodes); ib++ {
		jb, ok := posU[c.nodes[ib]]
		if !ok || jb <= ja {
			continue
		}
		if !slices.Equal(c.nodes[ia:ib+1], u.nodes[ja:jb+1]) {
			d := divergence{
				c: pathSeq{c.nodes[ia : ib+1], c.edges[ia:ib]},
				u: pathSeq{u.nodes[ja : jb+1], u.edges[ja:jb]},
			}
			d.cPass, d.uPass = g.stretchPassages(d.c.edges), g.stretchPassages(d.u.edges)
			d.extraM = g.stretchLen(d.c.edges) - g.stretchLen(d.u.edges)
			if d.extraM >= MetresPerNM || !slices.Equal(d.cPass, d.uPass) {
				out = append(out, d)
			}
		}
		ia, ja = ib, jb
	}
	return out
}

// reroute describes the vessel and closures a notice is written for.
type reroute struct {
	vessel     Vessel
	lim        DepthLimit
	limConfine float64
	shallow    []bool
	closed     map[string]bool // passages closed by draught
}

// notices turns divergences into notices. A divergence whose avoided stretch
// holds no closed edge or passage is left out: something else (a TSS lift)
// caused it.
func (g *Graph) notices(divs []divergence, rr reroute) []Notice {
	var out []Notice
	for _, d := range divs {
		extraNM := d.extraM / MetresPerNM
		hours := extraNM / rr.vessel.SpeedKn
		via := "deeper water"
		if len(d.cPass) > 0 {
			via = strings.Join(d.cPass, ", ")
		}
		var text string
		if k := slices.IndexFunc(d.uPass, func(s string) bool { return rr.closed[s] }); k >= 0 {
			tag := d.uPass[k]
			text = fmt.Sprintf("Draught %.1f m exceeds the %s limit of %.1f m. Routed via %s, +%.0f NM / +%.1f h.",
				rr.vessel.DraughtM, tag, PassageLimits[tag].MaxDraughtM, via, extraNM, hours)
		} else {
			worst, worstM := -1, math.Inf(1)
			for k, e := range d.u.edges {
				if rr.shallow != nil && rr.shallow[e] && float64(g.minDepth[e]) < worstM {
					worst, worstM = k, float64(g.minDepth[e])
				}
			}
			if worst < 0 {
				continue
			}
			avoided := "shallow water near " + func() string {
				a, b := g.nodes[d.u.nodes[worst]], g.nodes[d.u.nodes[worst+1]]
				return fmt.Sprintf("%.2f°, %.2f°", (a[1]+b[1])/2, (a[0]+b[0])/2)
			}()
			if len(d.uPass) > 0 {
				avoided = strings.Join(d.uPass, ", ")
			}
			need := rr.lim.LimitM
			if PassageLimits[g.passes[g.pass[d.u.edges[worst]]]].Confined {
				need = rr.limConfine
			}
			text = fmt.Sprintf("Draught %.1f m: %s too shallow (needs %.1f m incl. squat and margin). Routed via %s, +%.0f NM / +%.1f h.",
				rr.vessel.DraughtM, avoided, need, via, extraNM, hours)
		}
		avoidedPath := make([]Position, len(d.u.nodes))
		for k, v := range d.u.nodes {
			avoidedPath[k] = g.nodes[v]
		}
		out = append(out, Notice{
			Kind: "draught-reroute", Text: text, ExtraNM: extraNM,
			Avoided: avoidedPath, Passages: append([]string{}, d.uPass...),
		})
	}
	return out
}

func noRouteNotice(v Vessel, lim DepthLimit) Notice {
	return Notice{
		Kind:     "draught-no-route",
		Text:     fmt.Sprintf("Draught %.1f m: no route deep enough (needs %.1f m). Showing the shortest route; shallow stretches are flagged.", v.DraughtM, lim.LimitM),
		Avoided:  []Position{},
		Passages: []string{},
	}
}
