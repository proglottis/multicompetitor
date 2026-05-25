# multicompetitor

Track how competitor ability changes across a series of ranked contests —
races, tournaments, seasons, or any setting where multiple participants finish
in order.

Each competitor gets a Gaussian ability estimate that updates after each
contest, drifts when they sit one out, and can be smoothed retrospectively
once a run of results is complete. Ties are handled natively.

## What it's for

- **Ongoing rating systems** — feed results in as they happen; each call
  returns updated ratings ready to store and display.
- **Retrospective analysis** — smooth a competitor's full history after the
  season ends to get sharper estimates of when they peaked.
- **Flexible identifiers** — competitor IDs can be strings, integers, UUIDs,
  or any comparable Go value.

## Installation

```
go get github.com/proglottis/multicompetitor
```

No external dependencies.

## Quick example

```go
var priors map[string]multicompetitor.PeriodRating

for _, race := range season {
    var contest []multicompetitor.Contest[string]
    for _, r := range race.Results {
        contest = append(contest, multicompetitor.Contest[string]{
            ID:   r.DriverID,
            Rank: r.FinishingPosition,
        })
    }
    priors = multicompetitor.RatePeriod(priors, tau, sigma0, contest)
    // store a copy of priors per competitor if you want to smooth later
}

// Retrospective smoothing for one competitor's career:
smoothed := multicompetitor.Smooth(driverHistory, tau)
```

New competitors are initialised automatically on first appearance. A
competitor who misses a period has their uncertainty widened — the model
acknowledges that their ability may have changed while they were away.

## API

```go
func RatePeriod[K comparable](priors map[K]PeriodRating, tau, sigma0 float64, contests ...[]Contest[K]) map[K]PeriodRating
func Smooth(history []PeriodRating, tau float64) []PeriodRating

type PeriodRating struct {
    Mu    float64 // ability estimate (mean)
    Sigma float64 // uncertainty (std dev)
}

type Contest[K comparable] struct {
    ID   K
    Rank int // 1 = first; equal values are a tie
}
```

Full godoc at [pkg.go.dev/github.com/proglottis/multicompetitor](https://pkg.go.dev/github.com/proglottis/multicompetitor).

## Tuning

| Parameter | What it controls | Starting point |
|-----------|-----------------|----------------|
| `tau` | How quickly ability can change between periods. Lower is more stable; higher reacts faster to recent form. | `0.1` per race |
| `sigma0` | How uncertain you are about a new competitor. Wider priors converge more slowly but avoid overreacting to early results. | `1.5` |

Ratings are on a Glicko-compatible scale: μ=0 corresponds to 1500 and one
unit is roughly 174 rating points.

## F1 driver ratings 2000–2026

![F1 Driver Ratings](docs/ratings.svg)

Smoothed conservative estimates (μ − 2σ) for the top 20 drivers by final
rating, produced by `cmd/f1/` using results from the Jolpica F1 API.

## How it works

The library implements the approximate filter and RTS smoother from
[Glickman & Hennessy (2015)](https://www.glicko.net/research/multicompetitor.pdf).
Ability is modelled as a Gaussian random walk; contest outcomes follow a
rank-ordered logit (Plackett-Luce) model with the Breslow-Crowley
approximation for ties. The per-period posterior mode is found via
Newton-Raphson — a Laplace approximation rather than full Bayesian inference.
