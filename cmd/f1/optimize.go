package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"

	"gonum.org/v1/gonum/optimize"

	"github.com/proglottis/multicompetitor"
)

func cmdOptimize(db *sql.DB, args []string) {
	fs := flag.NewFlagSet("optimize", flag.ExitOnError)
	valSeasons := fs.Int("val", 3, "number of final seasons used as validation set")
	_ = fs.Parse(args)

	allRaces, err := loadRaces(db)
	if err != nil {
		fatalf("load races: %v", err)
	}
	if len(allRaces) == 0 {
		fatalf("no races in database; run 'update' first")
	}

	// Build one period per race, each containing one driver contest.
	periods := make([][][]multicompetitor.Contest[string], len(allRaces))
	for i, race := range allRaces {
		results, err := loadResults(db, race.id)
		if err != nil {
			fatalf("load results race %d: %v", race.id, err)
		}
		contest := make([]multicompetitor.Contest[string], 0, len(results))
		for _, r := range results {
			contest = append(contest, multicompetitor.Contest[string]{ID: r.driverID, Rank: r.position})
		}
		periods[i] = [][]multicompetitor.Contest[string]{contest}
	}

	// Find valStart: first period index of the last N seasons.
	seasonStarts, seasonYears, err := loadSeasonPeriodStarts(db)
	if err != nil {
		fatalf("load season starts: %v", err)
	}
	valStart := 0
	if *valSeasons > 0 && len(seasonYears) > 0 {
		idx := len(seasonYears) - *valSeasons
		if idx < 0 {
			idx = 0
		}
		valStart = seasonStarts[idx]
	}
	_, _ = fmt.Fprintf(os.Stderr, "optimizing over %d races, validating last %d seasons (from period %d)...\n",
		len(allRaces), *valSeasons, valStart)

	objective := func(x []float64) float64 {
		tau := math.Exp(x[0])
		sigma0 := math.Exp(x[1])
		return -multicompetitor.Score[string](nil, tau, sigma0, valStart, periods)
	}

	result, err := optimize.Minimize(
		optimize.Problem{Func: objective},
		[]float64{math.Log(0.1), math.Log(1.5)},
		nil,
		&optimize.NelderMead{},
	)
	if err != nil && !errors.Is(err, optimize.ErrNoProgress) {
		fatalf("optimize: %v", err)
	}

	tau := math.Exp(result.X[0])
	sigma0 := math.Exp(result.X[1])
	rhoW := -result.F
	fmt.Printf("tau=%.4f  sigma0=%.4f  ρ_W=%.4f\n", tau, sigma0, rhoW)
}
