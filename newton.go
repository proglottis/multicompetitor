package multicompetitor

import (
	"math"
	"sort"
)

const (
	maxNRIterations  = 100
	nrConvergenceTol = 1e-6
)

// nrState holds per-competitor working state during Newton-Raphson.
type nrState struct {
	r        *rating
	mu       float64 // prior mean
	sigma2   float64 // prior variance
	theta    float64 // current estimate
	expTheta float64 // cached exp(theta), refreshed each iteration
	grad     float64 // accumulated gradient
	hess     float64 // accumulated diagonal Hessian
}

// preparedContest holds a contest's pre-processed rank structure so it does
// not need to be recomputed on every Newton-Raphson iteration.
type preparedContest struct {
	contestants  []contestant
	maxRank      int
	nonLastRanks []int // unique ranks < maxRank
}

// runNR finds the posterior mode of the log-posterior via Newton-Raphson and
// writes the result back to each rating. Only competitors appearing in at
// least one contest are updated; all others are untouched.
func runNR(contests [][]contestant) {
	// Collect unique ratings across all contests.
	seen := make(map[*rating]*nrState)
	for _, contest := range contests {
		for _, c := range contest {
			if _, ok := seen[c.r]; !ok {
				seen[c.r] = &nrState{
					r:      c.r,
					mu:     c.r.mu,
					sigma2: c.r.sigma * c.r.sigma,
				}
			}
		}
	}
	if len(seen) == 0 {
		return
	}

	states := make([]*nrState, 0, len(seen))
	for _, s := range seen {
		states = append(states, s)
	}
	idx := make(map[*rating]int, len(states))
	for i, s := range states {
		idx[s.r] = i
	}

	// Populate stateIdx in each contestant and pre-process rank structure.
	pcs := make([]preparedContest, len(contests))
	for i, contest := range contests {
		for j := range contest {
			contests[i][j].stateIdx = idx[contest[j].r]
		}
		pcs[i] = prepareContest(contests[i])
	}

	// Initialise theta using the procedure from Appendix A, step 1.
	initTheta(states, pcs)

	// Newton-Raphson iterations (Appendix A, step 2).
	for range maxNRIterations {
		// Cache exp(theta) and reset to prior contributions.
		for _, s := range states {
			s.expTheta = math.Exp(s.theta)
			s.grad = -(s.theta - s.mu) / s.sigma2
			s.hess = -1.0 / s.sigma2
		}
		// Add likelihood contributions from all contests.
		for i := range pcs {
			addGradHess(&pcs[i], states)
		}
		// Simultaneous update step and convergence check.
		maxDelta := 0.0
		for _, s := range states {
			delta := s.grad / s.hess // hess < 0, so this moves toward the maximum
			s.theta -= delta
			if d := math.Abs(delta); d > maxDelta {
				maxDelta = d
			}
		}
		if maxDelta < nrConvergenceTol {
			break
		}
	}

	// Recompute Hessian at the converged θ* to get accurate posterior variances.
	for _, s := range states {
		s.expTheta = math.Exp(s.theta)
		s.hess = -1.0 / s.sigma2
	}
	for i := range pcs {
		addGradHess(&pcs[i], states)
	}

	// Write posterior mean and standard deviation back to the rating.
	for _, s := range states {
		s.r.mu = s.theta
		s.r.sigma = math.Sqrt(-1.0 / s.hess)
	}
}

// prepareContest computes the pre-processed rank structure for a contest once,
// before the NR loop.
func prepareContest(contest []contestant) preparedContest {
	maxRank := 0
	for _, c := range contest {
		if c.rank > maxRank {
			maxRank = c.rank
		}
	}
	// Collect non-last ranks, sort, and deduplicate.
	ranks := make([]int, 0, len(contest))
	for _, c := range contest {
		if c.rank < maxRank {
			ranks = append(ranks, c.rank)
		}
	}
	sort.Ints(ranks)
	out := ranks[:0]
	for i, r := range ranks {
		if i == 0 || r != ranks[i-1] {
			out = append(out, r)
		}
	}
	return preparedContest{contestants: contest, maxRank: maxRank, nonLastRanks: out}
}

// initTheta sets the starting values for Newton-Raphson using the
// initialisation procedure described in Appendix A, steps 1a–1c.
func initTheta(states []*nrState, pcs []preparedContest) {
	appearances := make([]int, len(states))
	totalFactors := 0

	for _, pc := range pcs {
		if pc.maxRank == 0 {
			continue
		}
		for _, ci := range pc.contestants {
			if ci.rank < pc.maxRank {
				totalFactors++
			}
		}
		// Competitor i appears in the choice set of factor j iff rank(j) <= rank(i)
		// and factor j is non-last.
		for _, ci := range pc.contestants {
			for _, cj := range pc.contestants {
				if cj.rank <= ci.rank && cj.rank < pc.maxRank {
					appearances[ci.stateIdx]++
				}
			}
		}
	}

	for i, s := range states {
		pi := 0.0
		if totalFactors > 0 {
			pi = math.Min(float64(appearances[i])/float64(totalFactors), 1)
		}
		// q is the logistic quantile of the (clamped) outperformance rate.
		p := 0.01 + 0.98*(1-pi)
		q := math.Log(p / (1 - p))
		// Weighted average of q with the prior mean (Appendix A, step 1c).
		s.theta = (q + s.mu/s.sigma2) / (1 + 1/s.sigma2)
	}
}

// addGradHess accumulates the gradient and diagonal Hessian contributions
// from a single contest into states, using the Breslow-Crowley approximation
// for ties and cached exp(theta) values.
func addGradHess(pc *preparedContest, states []*nrState) {
	for _, r := range pc.nonLastRanks {
		// Denominator S = Σ exp(θ) for all contestants with rank ≥ r.
		// nFactors = number of tied competitors at rank r (one factor each).
		S := 0.0
		nFactors := 0
		for _, c := range pc.contestants {
			if c.rank >= r {
				S += states[c.stateIdx].expTheta
			}
			if c.rank == r {
				nFactors++
			}
		}

		fn := float64(nFactors)
		for _, c := range pc.contestants {
			i := c.stateIdx
			// Winner (W) contribution: +1 for each factor this contestant wins.
			if c.rank == r {
				states[i].grad += 1.0
			}
			// Denominator contribution: scaled by the number of parallel factors.
			if c.rank >= r {
				p := states[i].expTheta / S
				states[i].grad -= fn * p
				states[i].hess -= fn * p * (1 - p)
			}
		}
	}
}
