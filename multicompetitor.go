// Package multicompetitor implements the approximate filter and
// Rauch-Tung-Streibel smoother from Glickman & Hennessy (2015) for rating
// competitors in multi-competitor contests (races, tournaments, etc.).
//
// Each competitor's ability evolves over time as a Gaussian random walk.
// Contest outcomes are modelled as rank-ordered logit (Plackett-Luce);
// ties are handled via the Breslow-Crowley approximation.
package multicompetitor

import "math"

// PeriodRating is a competitor's ability estimate for one time period,
// represented as a Gaussian N(Mu, Sigma²).
type PeriodRating struct {
	Mu    float64
	Sigma float64
}

// Contest specifies a competitor's result in a single contest.
// K is the competitor identifier type — any comparable value (string, int64,
// UUID, etc.) that can serve as a map key.
//
// Rank must be ≥ 1; equal Rank values indicate a tie.
type Contest[K comparable] struct {
	ID   K
	Rank int // 1 = first; equal values indicate a tie
}

// RatePeriod processes one rating period.
//
// priors maps each known competitor to their posterior from the previous
// period (post-Rate, pre-Advance). Competitors absent from priors are
// initialised to N(0, sigma0). All priors are advanced by tau
// (σ → √(σ²+τ²)) before rating.
//
// contests is a variadic list of simultaneous contests within the period
// (e.g. qualifying + race). Pass nothing to advance all priors without rating.
//
// Returns new posteriors (post-Rate, pre-Advance) for all known competitors
// and any newcomers — ready to store and to pass as priors to the next call.
//
// sigma0 must be > 0. Contest Rank values must be ≥ 1.
func RatePeriod[K comparable](priors map[K]PeriodRating, tau, sigma0 float64, contests ...[]Contest[K]) map[K]PeriodRating {
	ratings := make(map[K]*rating, len(priors))
	for id, p := range priors {
		ratings[id] = &rating{mu: p.Mu, sigma: math.Sqrt(p.Sigma*p.Sigma + tau*tau)}
	}
	cslices := make([][]contestant, len(contests))
	for i, cs := range contests {
		cslices[i] = make([]contestant, len(cs))
		for j, c := range cs {
			if _, ok := ratings[c.ID]; !ok {
				ratings[c.ID] = &rating{sigma: sigma0}
			}
			cslices[i][j] = contestant{r: ratings[c.ID], rank: c.Rank}
		}
	}
	runNR(cslices)
	out := make(map[K]PeriodRating, len(ratings))
	for id, r := range ratings {
		out[id] = PeriodRating{Mu: r.mu, Sigma: r.sigma}
	}
	return out
}

// Smooth applies the Rauch-Tung-Streibel backward smoother to a sequence of
// per-period filter posteriors. history[t] must be the posterior collected
// after RatePeriod for period t (post-Rate, pre-Advance).
//
// The smoother reduces uncertainty for interior periods by incorporating
// information from later periods. Returns smoothed estimates of the same
// length as history.
//
// tau must be the same positive value used in the corresponding RatePeriod calls.
func Smooth(history []PeriodRating, tau float64) []PeriodRating {
	T := len(history)
	if T == 0 {
		return nil
	}
	out := make([]PeriodRating, T)
	out[T-1] = history[T-1]
	tau2 := tau * tau
	for t := T - 2; t >= 0; t-- {
		s2 := history[t].Sigma * history[t].Sigma
		gain := s2 / (s2 + tau2)
		out[t].Mu = history[t].Mu + gain*(out[t+1].Mu-history[t].Mu)
		s2smooth := s2 + gain*gain*(out[t+1].Sigma*out[t+1].Sigma-s2-tau2)
		out[t].Sigma = math.Sqrt(math.Max(s2smooth, 0))
	}
	return out
}

// rating is the internal mutable state for one competitor during a period.
type rating struct {
	mu    float64
	sigma float64
}

// contestant is the internal representation of one competitor in a contest.
type contestant struct {
	r        *rating // needed to build the seen map in runNR
	stateIdx int     // index into states[] — set by runNR after states are built
	rank     int
}
