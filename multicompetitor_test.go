package multicompetitor_test

import (
	"math"
	"strconv"
	"testing"

	"github.com/proglottis/multicompetitor"
)

func contests[K comparable](cs ...multicompetitor.Contest[K]) []multicompetitor.Contest[K] {
	return cs
}

func TestRatePeriod_WinnerLoser(t *testing.T) {
	posts := multicompetitor.RatePeriod(
		nil, 0.3, 1.5,
		contests(
			multicompetitor.Contest[string]{ID: "r1", Rank: 1},
			multicompetitor.Contest[string]{ID: "r2", Rank: 2},
		),
	)

	r1, r2 := posts["r1"], posts["r2"]
	if r1.Mu <= 0 {
		t.Errorf("winner Mu = %v, want > 0", r1.Mu)
	}
	if r2.Mu >= 0 {
		t.Errorf("loser Mu = %v, want < 0", r2.Mu)
	}
	if r1.Sigma >= 1.5 {
		t.Errorf("winner Sigma = %v, want < 1.5", r1.Sigma)
	}
	if r2.Sigma >= 1.5 {
		t.Errorf("loser Sigma = %v, want < 1.5", r2.Sigma)
	}
	if math.Abs(r1.Mu+r2.Mu) > 1e-5 {
		t.Errorf("ratings should be antisymmetric: r1.Mu=%v r2.Mu=%v", r1.Mu, r2.Mu)
	}
}

func TestRatePeriod_OrderingPreserved(t *testing.T) {
	ids := []string{"a", "b", "c", "d", "e"}
	cs := make([]multicompetitor.Contest[string], len(ids))
	for i, id := range ids {
		cs[i] = multicompetitor.Contest[string]{ID: id, Rank: i + 1}
	}

	posts := multicompetitor.RatePeriod(nil, 0.3, 1.5, cs)

	for i := 1; i < len(ids); i++ {
		prev, cur := posts[ids[i-1]], posts[ids[i]]
		if prev.Mu <= cur.Mu {
			t.Errorf("rank %d Mu (%v) should be > rank %d Mu (%v)", i, prev.Mu, i+1, cur.Mu)
		}
	}
}

func TestRatePeriod_TiedWinnersSymmetric(t *testing.T) {
	posts := multicompetitor.RatePeriod(
		nil, 0.3, 1.5,
		contests(
			multicompetitor.Contest[string]{ID: "r1", Rank: 1},
			multicompetitor.Contest[string]{ID: "r2", Rank: 1},
			multicompetitor.Contest[string]{ID: "r3", Rank: 2},
		),
	)

	r1, r2, r3 := posts["r1"], posts["r2"], posts["r3"]
	if math.Abs(r1.Mu-r2.Mu) > 1e-10 {
		t.Errorf("tied winners should have equal Mu: r1=%v r2=%v", r1.Mu, r2.Mu)
	}
	if math.Abs(r1.Sigma-r2.Sigma) > 1e-10 {
		t.Errorf("tied winners should have equal Sigma")
	}
	if r1.Mu <= r3.Mu {
		t.Errorf("winners should have higher Mu than loser: winners=%v loser=%v", r1.Mu, r3.Mu)
	}
}

func TestRatePeriod_TiedLastPlace(t *testing.T) {
	posts := multicompetitor.RatePeriod(
		nil, 0.3, 1.5,
		contests(
			multicompetitor.Contest[string]{ID: "r1", Rank: 1},
			multicompetitor.Contest[string]{ID: "r2", Rank: 2},
			multicompetitor.Contest[string]{ID: "r3", Rank: 2},
			multicompetitor.Contest[string]{ID: "r4", Rank: 2},
		),
	)

	r1, r2, r3, r4 := posts["r1"], posts["r2"], posts["r3"], posts["r4"]
	if r1.Mu <= r2.Mu {
		t.Errorf("winner should have higher Mu: winner=%v loser=%v", r1.Mu, r2.Mu)
	}
	if math.Abs(r2.Mu-r3.Mu) > 1e-10 || math.Abs(r3.Mu-r4.Mu) > 1e-10 {
		t.Errorf("tied losers should have equal Mu: %v %v %v", r2.Mu, r3.Mu, r4.Mu)
	}
}

func TestRatePeriod_MultipleContests(t *testing.T) {
	posts := multicompetitor.RatePeriod(
		nil, 0.3, 1.5,
		contests(multicompetitor.Contest[string]{ID: "r1", Rank: 1}, multicompetitor.Contest[string]{ID: "r2", Rank: 2}),
		contests(multicompetitor.Contest[string]{ID: "r1", Rank: 1}, multicompetitor.Contest[string]{ID: "r3", Rank: 2}),
	)

	r1, r2, r3 := posts["r1"], posts["r2"], posts["r3"]
	if r1.Mu <= r2.Mu {
		t.Errorf("r1.Mu=%v should be > r2.Mu=%v", r1.Mu, r2.Mu)
	}
	if r1.Mu <= r3.Mu {
		t.Errorf("r1.Mu=%v should be > r3.Mu=%v", r1.Mu, r3.Mu)
	}
}

func TestRatePeriod_EmptyContests(t *testing.T) {
	posts := multicompetitor.RatePeriod[string](nil, 0.3, 1.5)
	if len(posts) != 0 {
		t.Errorf("empty RatePeriod should return empty map, got %v", posts)
	}
}

func TestRatePeriod_NonParticipantAdvances(t *testing.T) {
	tau, sigma0 := 0.3, 1.5
	priors := map[string]multicompetitor.PeriodRating{
		"r1": {Mu: 0, Sigma: sigma0},
		"r2": {Mu: 0, Sigma: sigma0},
		"r3": {Mu: 0, Sigma: sigma0},
	}

	// Two periods where only r1 and r2 race; r3 is carried but never rates.
	cs := contests(
		multicompetitor.Contest[string]{ID: "r1", Rank: 1},
		multicompetitor.Contest[string]{ID: "r2", Rank: 2},
	)
	posts1 := multicompetitor.RatePeriod(priors, tau, sigma0, cs)
	posts2 := multicompetitor.RatePeriod(posts1, tau, sigma0, cs)

	// r3 advances once per period: sigma = sqrt(sigma0² + 2*tau²) after 2 periods.
	want := math.Sqrt(sigma0*sigma0 + 2*tau*tau)
	if math.Abs(posts2["r3"].Sigma-want) > 1e-10 {
		t.Errorf("non-participant Sigma = %v, want %v", posts2["r3"].Sigma, want)
	}
	if posts2["r3"].Mu != 0 {
		t.Errorf("non-participant Mu should be 0, got %v", posts2["r3"].Mu)
	}
}

func TestRatePeriod_MultiPeriod(t *testing.T) {
	tau, sigma0 := 0.3, 1.5
	cs := contests(
		multicompetitor.Contest[string]{ID: "r1", Rank: 1},
		multicompetitor.Contest[string]{ID: "r2", Rank: 2},
	)
	var priors map[string]multicompetitor.PeriodRating
	for range 3 {
		priors = multicompetitor.RatePeriod(priors, tau, sigma0, cs)
	}

	if priors["r1"].Mu <= priors["r2"].Mu {
		t.Errorf("consistent winner should have higher rating: r1=%v r2=%v", priors["r1"].Mu, priors["r2"].Mu)
	}
}

func TestSmooth_Length(t *testing.T) {
	history := []multicompetitor.PeriodRating{
		{Mu: 0.5, Sigma: 0.8},
		{Mu: 1.0, Sigma: 0.7},
		{Mu: 0.8, Sigma: 0.9},
	}
	out := multicompetitor.Smooth(history, 0.3)
	if len(out) != len(history) {
		t.Fatalf("Smooth returned %d entries, want %d", len(out), len(history))
	}
}

func TestSmooth_LastPeriodUnchanged(t *testing.T) {
	history := []multicompetitor.PeriodRating{
		{Mu: 0.5, Sigma: 0.8},
		{Mu: 1.0, Sigma: 0.7},
		{Mu: 0.8, Sigma: 0.9},
	}
	out := multicompetitor.Smooth(history, 0.3)
	last := len(history) - 1
	if out[last].Mu != history[last].Mu || out[last].Sigma != history[last].Sigma {
		t.Errorf("last period should be unchanged: got {%v,%v} want {%v,%v}",
			out[last].Mu, out[last].Sigma, history[last].Mu, history[last].Sigma)
	}
}

func TestSmooth_ReducesInteriorUncertainty(t *testing.T) {
	history := []multicompetitor.PeriodRating{
		{Mu: 0.0, Sigma: 1.0},
		{Mu: 0.0, Sigma: 1.0},
		{Mu: 0.0, Sigma: 1.0},
	}
	out := multicompetitor.Smooth(history, 0.3)
	if out[1].Sigma >= history[1].Sigma {
		t.Errorf("smoother should reduce interior sigma: got %v, filter %v",
			out[1].Sigma, history[1].Sigma)
	}
}

func TestSmooth_Empty(t *testing.T) {
	out := multicompetitor.Smooth(nil, 0.3)
	if out != nil {
		t.Errorf("Smooth(nil) = %v, want nil", out)
	}
}

func makeContest(ids []string) []multicompetitor.Contest[string] {
	cs := make([]multicompetitor.Contest[string], len(ids))
	for i, id := range ids {
		cs[i] = multicompetitor.Contest[string]{ID: id, Rank: i + 1}
	}
	return cs
}

// BenchmarkRatePeriod_20Drivers benchmarks the NR hot path: 20 participants
// with established priors, all unique ranks.
func BenchmarkRatePeriod_20Drivers(b *testing.B) {
	const n = 20
	ids := make([]string, n)
	for i := range ids {
		ids[i] = strconv.Itoa(i)
	}
	cs := makeContest(ids)
	priors := multicompetitor.RatePeriod(nil, 0.1, 1.5, makeContest(ids))
	for range 10 {
		priors = multicompetitor.RatePeriod(priors, 0.1, 1.5, cs)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		priors = multicompetitor.RatePeriod(priors, 0.1, 1.5, cs)
	}
}

// BenchmarkRatePeriod_20of130 benchmarks the steady-state F1 workload: 130
// drivers in priors, 20 racing per period — advancing 110 non-participants.
func BenchmarkRatePeriod_20of130(b *testing.B) {
	const total, active = 130, 20
	allIDs := make([]string, total)
	for i := range allIDs {
		allIDs[i] = strconv.Itoa(i)
	}
	// Seed all 130 into priors via a single seeding contest, then warm up.
	priors := multicompetitor.RatePeriod(nil, 0.1, 1.5, makeContest(allIDs))
	cs := makeContest(allIDs[:active])
	for range 10 {
		priors = multicompetitor.RatePeriod(priors, 0.1, 1.5, cs)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		priors = multicompetitor.RatePeriod(priors, 0.1, 1.5, cs)
	}
}

// BenchmarkSmooth_500Periods benchmarks smoothing a full career history.
func BenchmarkSmooth_500Periods(b *testing.B) {
	const T = 500
	history := make([]multicompetitor.PeriodRating, T)
	for i := range history {
		history[i] = multicompetitor.PeriodRating{Mu: float64(i%10) * 0.1, Sigma: 1.0}
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		_ = multicompetitor.Smooth(history, 0.1)
	}
}

// makePeriods builds a [][][]Contest slice for Score tests.
// Each inner call is one contest (one period = one race).
func makePeriods(contestsPerPeriod ...[]multicompetitor.Contest[string]) [][][]multicompetitor.Contest[string] {
	out := make([][][]multicompetitor.Contest[string], len(contestsPerPeriod))
	for i, c := range contestsPerPeriod {
		out[i] = [][]multicompetitor.Contest[string]{c}
	}
	return out
}

func TestScore_EmptyPeriods(t *testing.T) {
	s := multicompetitor.Score[string](nil, 0.1, 1.5, 0, nil)
	if s != 0 {
		t.Errorf("Score(empty) = %v, want 0", s)
	}
}

func TestScore_ValStartPastEnd(t *testing.T) {
	periods := makePeriods(
		contests(multicompetitor.Contest[string]{ID: "a", Rank: 1}, multicompetitor.Contest[string]{ID: "b", Rank: 2}),
	)
	s := multicompetitor.Score[string](nil, 0.1, 1.5, 1, periods) // valStart >= T-1
	if s != 0 {
		t.Errorf("Score with no validation periods = %v, want 0", s)
	}
}

func TestScore_ConsistentWinnerPositive(t *testing.T) {
	// r1 always wins, r2 always loses. After warm-up, r1 should have higher mu,
	// and Score should be positive.
	race := contests(
		multicompetitor.Contest[string]{ID: "r1", Rank: 1},
		multicompetitor.Contest[string]{ID: "r2", Rank: 2},
	)
	// 5 periods, validate on last 3.
	periods := makePeriods(race, race, race, race, race)
	s := multicompetitor.Score[string](nil, 0.3, 1.5, 2, periods)
	if s <= 0 {
		t.Errorf("Score for consistent winner = %v, want > 0", s)
	}
}

func TestScore_InRange(t *testing.T) {
	race := contests(
		multicompetitor.Contest[string]{ID: "r1", Rank: 1},
		multicompetitor.Contest[string]{ID: "r2", Rank: 2},
	)
	periods := makePeriods(race, race, race, race, race)
	s := multicompetitor.Score[string](nil, 0.3, 1.5, 2, periods)
	if s < -1 || s > 1+1e-9 {
		t.Errorf("Score = %v, want in [-1, 1]", s)
	}
}

func TestScore_ConsistentConvergesToOne(t *testing.T) {
	// After sufficient warm-up, a perfect predictor should score 1.0.
	race := contests(
		multicompetitor.Contest[string]{ID: "r1", Rank: 1},
		multicompetitor.Contest[string]{ID: "r2", Rank: 2},
		multicompetitor.Contest[string]{ID: "r3", Rank: 3},
	)
	var periods [][][]multicompetitor.Contest[string]
	for range 10 {
		periods = append(periods, [][]multicompetitor.Contest[string]{race})
	}
	s := multicompetitor.Score[string](nil, 0.3, 1.5, 7, periods)
	if math.Abs(s-1.0) > 1e-9 {
		t.Errorf("fully warmed-up consistent model score = %v, want 1.0", s)
	}
}
