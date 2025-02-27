package autopilot

import (
	"math"
	"testing"
	"time"

	rhpv2 "go.sia.tech/core/rhp/v2"
	"go.sia.tech/core/types"
	"go.sia.tech/renterd/api"
	"go.sia.tech/renterd/hostdb"
	"lukechampine.com/frand"
)

func TestHostScore(t *testing.T) {
	cfg := api.DefaultAutopilotConfig()
	day := 24 * time.Hour

	newHost := func(s *rhpv2.HostSettings) hostdb.Host {
		return newTestHost(randomHostKey(), newTestHostPriceTable(), s)
	}
	h1 := newHost(newTestHostSettings())
	h2 := newHost(newTestHostSettings())

	// assert both hosts score equal
	redundancy := 3.0
	if hostScore(cfg, h1, 0, redundancy) != hostScore(cfg, h2, 0, redundancy) {
		t.Fatal("unexpected")
	}

	// assert age affects the score
	h1.KnownSince = time.Now().Add(-1 * day)
	if hostScore(cfg, h1, 0, redundancy) <= hostScore(cfg, h2, 0, redundancy) {
		t.Fatal("unexpected")
	}

	// assert collateral affects the score
	settings := newTestHostSettings()
	settings.Collateral = types.NewCurrency64(1)
	settings.MaxCollateral = types.NewCurrency64(10)
	h1 = newHost(settings) // reset
	if hostScore(cfg, h1, 0, redundancy) <= hostScore(cfg, h2, 0, redundancy) {
		t.Fatal("unexpected")
	}

	// assert interactions affect the score
	h1 = newHost(newTestHostSettings()) // reset
	h1.Interactions.SuccessfulInteractions++
	if hostScore(cfg, h1, 0, redundancy) <= hostScore(cfg, h2, 0, redundancy) {
		t.Fatal("unexpected")
	}

	// assert uptime affects the score
	h2 = newHost(newTestHostSettings()) // reset
	h2.Interactions.SecondToLastScanSuccess = false
	if hostScore(cfg, h1, 0, redundancy) <= hostScore(cfg, h2, 0, redundancy) || ageScore(h1) != ageScore(h2) {
		t.Fatal("unexpected")
	}

	// assert version affects the score
	h2Settings := newTestHostSettings()
	h2Settings.Version = "1.5.6" // lower
	h2 = newHost(h2Settings)     // reset
	if hostScore(cfg, h1, 0, redundancy) <= hostScore(cfg, h2, 0, redundancy) {
		t.Fatal("unexpected")
	}

	// asseret remaining storage affects the score.
	h1 = newHost(newTestHostSettings()) // reset
	h2.Settings.RemainingStorage = 100
	if hostScore(cfg, h1, 0, redundancy) <= hostScore(cfg, h2, 0, redundancy) {
		t.Fatal("unexpected")
	}

	// assert MaxCollateral affects the score.
	h2 = newHost(newTestHostSettings()) // reset
	h2.Settings.MaxCollateral = types.ZeroCurrency
	if hostScore(cfg, h1, 0, redundancy) <= hostScore(cfg, h2, 0, redundancy) {
		t.Fatal("unexpected")
	}
}

func TestRandSelectByWeight(t *testing.T) {
	// assert min float is never selected
	weights := []float64{.1, .2, math.SmallestNonzeroFloat64}
	for i := 0; i < 100; i++ {
		frand.Shuffle(len(weights), func(i, j int) { weights[i], weights[j] = weights[j], weights[i] })
		if weights[randSelectByWeight(weights)] == math.SmallestNonzeroFloat64 {
			t.Fatal("unexpected")
		}
	}

	// assert select is random on equal inputs
	counts := make([]int, 2)
	weights = []float64{.1, .1}
	for i := 0; i < 100; i++ {
		counts[randSelectByWeight(weights)]++
	}
	if diff := absDiffInt(counts[0], counts[1]); diff > 40 {
		t.Fatal("unexpected", counts[0], counts[1], diff)
	}
}

func absDiffInt(x, y int) int {
	if x < y {
		return y - x
	}
	return x - y
}
