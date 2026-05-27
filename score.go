package multicompetitor

import (
	"math"
	"sort"
)

// Score computes the weighted predictive Spearman rank correlation ρ_W
// (Glickman & Hennessy 2015, Equation 22) for candidate tau and sigma0.
//
// periods is the full sequence of rating periods in chronological order.
// Each element periods[t] is the set of contests in that time period,
// using the same slice structure as the variadic argument to RatePeriod.
//
// valStart is the index of the first validation period; periods before it
// warm up the filter only. For periods [valStart, T-1), the posterior means
// at period t are correlated with the actual rankings in period t+1, weighted
// by (m-1) where m is the number of competitors in the contest.
// A typical choice is len(periods)-N for the final N periods.
//
// Returns ρ_W in [-1, 1]; higher is better. Returns 0 if there are no
// scoreable periods.
func Score[K comparable](priors map[K]PeriodRating, tau, sigma0 float64, valStart int, periods [][][]Contest[K]) float64 {
	T := len(periods)
	if T == 0 || valStart >= T-1 {
		return 0
	}

	// Run the filter over all periods, collecting posterior means for use in
	// one-step-ahead prediction.
	posteriorMeans := make([]map[K]float64, T)
	current := priors
	for t, contests := range periods {
		current = RatePeriod(current, tau, sigma0, contests...)
		mus := make(map[K]float64, len(current))
		for id, pr := range current {
			mus[id] = pr.Mu
		}
		posteriorMeans[t] = mus
	}

	// Score validation periods: correlate posterior means at t with actual
	// rankings at t+1, weighted by (m-1) per contest.
	var numerator, denominator float64
	for t := valStart; t < T-1; t++ {
		for _, contest := range periods[t+1] {
			rho, m := contestSpearman(posteriorMeans[t], contest)
			if m < 2 {
				continue
			}
			w := float64(m - 1)
			numerator += w * rho
			denominator += w
		}
	}
	if denominator == 0 {
		return 0
	}
	return numerator / denominator
}

// contestSpearman computes the Spearman rank correlation between the
// posterior means at the previous period and the actual rankings in a contest.
// Only competitors present in both mus and contest contribute.
// Returns the correlation and the number of matched competitors.
func contestSpearman[K comparable](mus map[K]float64, contest []Contest[K]) (float64, int) {
	muVals := make([]float64, 0, len(contest))
	rankVals := make([]float64, 0, len(contest))
	for _, c := range contest {
		mu, ok := mus[c.ID]
		if !ok {
			continue
		}
		muVals = append(muVals, mu)
		rankVals = append(rankVals, -float64(c.Rank)) // negate so higher = better in both axes
	}
	m := len(muVals)
	if m < 2 {
		return 0, m
	}
	return spearmanRho(muVals, rankVals), m
}

func spearmanRho(x, y []float64) float64 {
	return pearsonCorr(rankData(x), rankData(y))
}

// rankData assigns fractional ranks to x in ascending order. Tied values
// receive the average of the ranks they would occupy.
func rankData(x []float64) []float64 {
	n := len(x)
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(i, j int) bool { return x[idx[i]] < x[idx[j]] })
	ranks := make([]float64, n)
	for i := 0; i < n; {
		j := i
		for j < n && x[idx[j]] == x[idx[i]] {
			j++
		}
		avg := float64(i+1+j) / 2.0
		for k := i; k < j; k++ {
			ranks[idx[k]] = avg
		}
		i = j
	}
	return ranks
}

func pearsonCorr(x, y []float64) float64 {
	n := len(x)
	if n == 0 {
		return 0
	}
	var mx, my float64
	for i := range x {
		mx += x[i]
		my += y[i]
	}
	mx /= float64(n)
	my /= float64(n)
	var num, dx2, dy2 float64
	for i := range x {
		dx := x[i] - mx
		dy := y[i] - my
		num += dx * dy
		dx2 += dx * dx
		dy2 += dy * dy
	}
	if dx2 == 0 || dy2 == 0 {
		return 0
	}
	return num / math.Sqrt(dx2*dy2)
}
