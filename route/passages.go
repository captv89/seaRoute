package route

// PassageLimit is the draught rule for a tagged passage.
type PassageLimit struct {
	// MaxDraughtM is the deepest draught the passage admits. 0 means no fixed limit.
	MaxDraughtM float64
	// Confined applies the confined-water squat (Squat with confined true).
	Confined bool
	// Source cites where MaxDraughtM and Confined come from.
	Source string
}

// PassageLimits maps passage tags (see Graph.Passages) to their draught rules.
var PassageLimits = map[string]PassageLimit{
	"suez": {
		MaxDraughtM: 20.1, Confined: true,
		Source: "Suez Canal Authority, 66 ft (20.1 m) maximum draft; beam-dependent in SCA tables",
	},
	"panama": {
		MaxDraughtM: 15.24, Confined: true,
		Source: "Panama Canal Authority, Neopanamax locks 50 ft TFW (15.24 m); per plan, unverified: 2026 notices of 49.5 ft not confirmed",
	},
	"kiel": {
		MaxDraughtM: 9.5, Confined: true,
		Source: "Kiel Canal (WSV) 9.5 m; holds only up to about 160 m length, smaller allowance for larger ships; per plan, unverified for the length rule",
	},
	"corinth": {
		MaxDraughtM: 6.5, Confined: true,
		Source: "Corinth Canal S.A. 2026 commercial brochure, 6.50 m maximum permissible draft (some older sources give 7.3 m)",
	},
	"malacca": {
		MaxDraughtM: 0, Confined: true,
		Source: "per plan, unverified: no fixed draught limit; confined squat applied",
	},
	"dover": {
		MaxDraughtM: 0, Confined: true,
		Source: "per plan, unverified: no fixed draught limit; confined squat applied",
	},
}
