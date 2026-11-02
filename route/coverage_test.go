package route

import "testing"

// TestRouteBackedByData: nothing outside a port approach, canal or flagged
// stretch touches land, by the land data or by the depth grid: every sample
// has water under it in the grid, or lies in an open-ocean tile.
// MARNET edges are long and some cut across land (Basilan, the Bomberai
// peninsula of New Guinea, Isla Guadalupe); a route that trusts them where
// the data is missing runs aground without a hazard.
func TestRouteBackedByData(t *testing.T) {
	g := testFullGraph(t)
	if g.depth == nil {
		t.Skip("no depth data")
	}
	cases := []struct {
		name string
		pts  []Position
	}{
		{"manila-sydney", []Position{{120.97196657895371, 14.631173617326924}, {151.182861328125, -33.8696440161399}}},
		{"newyork-tokyo", []Position{{-74.0, 40.6}, {139.75, 35.65}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := g.Route(tc.pts, Options{})
			if err != nil {
				t.Fatal(err)
			}
			canal := map[int]bool{} // segments of a tagged passage (canal, strait)
			k := 0
			for _, l := range r.Legs {
				for _, code := range l.segs {
					canal[k] = code >= 0 && g.passes[g.pass[code]] != ""
					k++
				}
			}
			flagged := map[int]string{}
			for _, h := range r.Hazards {
				sev := h.Severity
				if h.Passage != "" || h.Kind == "connector" {
					sev = "approach" // canal or port connector: has its own note
				}
				for k := h.From; k < h.To; k++ {
					flagged[k] = sev
				}
			}
			for k := 0; k+1 < len(r.Path); k++ {
				a, b := r.Path[k], r.Path[k+1]
				sev := flagged[k]
				if sev == "approach" || sev == "unchecked" || sev == "aground" && g.approachOf(a, b) != "" {
					continue
				}
				if sev == "aground" {
					t.Errorf("segment %d %v -> %v runs aground", k, a, b)
					continue
				}
				// Independent of the land data: where the land data holds no
				// coast within 3 NM, the depth grid must not show land under a
				// run of 3 NM or more. Shorter blobs are reefs or raster noise,
				// which segmentDepth treats as unknown by design; a crossing
				// the land data cannot see (a peninsula) is far longer.
				if canal[k] {
					continue
				}
				run := 0
			samples:
				for _, pc := range splitAntimeridian(a, b) {
					for _, p := range rhumbSamples(pc[0], pc[1], 0.1) {
						m, known := g.depth.At(p)
						dry := known && m == 0 || !known && g.depth.TileMinDepth(p) < deepTileM
						if !dry || g.land.Within(p, 3*MetresPerNM) {
							run = 0
							continue
						}
						if run++; run >= 30 {
							t.Errorf("segment %d %v -> %v crosses land the land data lacks near %v, not flagged", k, a, b, p)
							break samples
						}
					}
				}
			}
		})
	}
}
