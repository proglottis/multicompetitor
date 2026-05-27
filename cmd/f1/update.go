package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/proglottis/multicompetitor"
)

func cmdUpdate(db *sql.DB, args []string) {
	// Historical seasons are treated as immutable once stored.
	currentYear := time.Now().Year()

	fs := flag.NewFlagSet("update", flag.ExitOnError)
	from := fs.Int("from", currentYear-10, "first season to fetch")
	to := fs.Int("to", currentYear, "last season to fetch")
	tau := fs.Float64("tau", 0.1, "per-race innovation std dev")
	sigma0 := fs.Float64("sigma0", 1.5, "initial rating uncertainty")
	recompute := fs.Bool("recompute", false, "clear stored posteriors and recompute all ratings from scratch")
	_ = fs.Parse(args) // ExitOnError: calls os.Exit on bad input, never returns an error

	storedSeas, err := storedSeasons(db)
	if err != nil {
		fatalf("load stored seasons: %v", err)
	}

	novelCount := 0
	for year := *from; year <= *to; year++ {
		if year < currentYear && storedSeas[year] {
			continue
		}
		_, _ = fmt.Fprintf(os.Stderr, "fetching %d...\n", year)
		races, err := fetchSeason(year)
		if err != nil {
			fatalf("fetch %d: %v", year, err)
		}
		storedRnds, err := storedRounds(db, year)
		if err != nil {
			fatalf("load stored rounds for %d: %v", year, err)
		}
		for _, race := range races {
			round, _ := strconv.Atoi(race.Round)
			if storedRnds[round] {
				continue
			}
			if err := insertRace(db, year, round, race.Results); err != nil {
				fatalf("insert race %d/%d: %v", year, round, err)
			}
			novelCount++
		}
	}

	if *recompute {
		tx, err := db.Begin()
		if err != nil {
			fatalf("begin recompute tx: %v", err)
		}
		for _, tbl := range []string{"driver_posteriors", "team_posteriors", "driver_smoothed", "team_smoothed"} {
			if _, err := tx.Exec("DELETE FROM " + tbl); err != nil {
				_ = tx.Rollback()
				fatalf("clear %s: %v", tbl, err)
			}
		}
		if err := tx.Commit(); err != nil {
			fatalf("commit recompute clear: %v", err)
		}
		_, _ = fmt.Fprintln(os.Stderr, "cleared stored posteriors; recomputing from scratch...")
	}

	lastSmoothed, err := latestSmoothedPeriod(db)
	if err != nil {
		fatalf("check smoothed: %v", err)
	}
	if novelCount == 0 && lastSmoothed >= 0 && !*recompute {
		_, _ = fmt.Fprintln(os.Stderr, "no new races; ratings up to date")
		return
	}

	allRaces, err := loadRaces(db)
	if err != nil {
		fatalf("load races: %v", err)
	}

	lastProcessed, err := lastProcessedIndex(db)
	if err != nil {
		fatalf("last processed index: %v", err)
	}

	// Restore last posteriors directly — RatePeriod will advance them.
	drvPriors, err := loadLastDriverPosteriors(db)
	if err != nil {
		fatalf("load last driver posteriors: %v", err)
	}
	teamPriors, err := loadLastTeamPosteriors(db)
	if err != nil {
		fatalf("load last team posteriors: %v", err)
	}

	for periodIndex, race := range allRaces {
		if periodIndex <= lastProcessed {
			continue
		}

		results, err := loadResults(db, race.id)
		if err != nil {
			fatalf("load results race %d: %v", race.id, err)
		}

		// Driver contest.
		driverContest := make([]multicompetitor.Contest[string], 0, len(results))
		for _, r := range results {
			driverContest = append(driverContest, multicompetitor.Contest[string]{ID: r.driverID, Rank: r.position})
		}
		drvPriors = multicompetitor.RatePeriod(drvPriors, *tau, *sigma0, driverContest)

		// Team contest: rank teams by F1 points earned this race.
		teamPts := make(map[string]int)
		for _, r := range results {
			if r.position <= len(f1Points) {
				teamPts[r.teamID] += f1Points[r.position-1]
			} else if _, ok := teamPts[r.teamID]; !ok {
				teamPts[r.teamID] = 0
			}
		}
		type teamScore struct {
			id  string
			pts int
		}
		scores := make([]teamScore, 0, len(teamPts))
		for tid, pts := range teamPts {
			scores = append(scores, teamScore{tid, pts})
		}
		sort.Slice(scores, func(i, j int) bool { return scores[i].pts > scores[j].pts })
		teamContest := make([]multicompetitor.Contest[string], 0, len(scores))
		rank := 1
		for i, s := range scores {
			if i > 0 && s.pts < scores[i-1].pts {
				rank = i + 1
			}
			teamContest = append(teamContest, multicompetitor.Contest[string]{ID: s.id, Rank: rank})
		}
		teamPriors = multicompetitor.RatePeriod(teamPriors, *tau, *sigma0, teamContest)

		// Persist posteriors returned by RatePeriod (post-Rate, pre-Advance).
		tx, err := db.Begin()
		if err != nil {
			fatalf("begin tx period %d: %v", periodIndex, err)
		}
		if err := insertDriverPosteriors(tx, periodIndex, drvPriors); err != nil {
			_ = tx.Rollback()
			fatalf("insert driver posteriors period %d: %v", periodIndex, err)
		}
		if err := insertTeamPosteriors(tx, periodIndex, teamPriors); err != nil {
			_ = tx.Rollback()
			fatalf("insert team posteriors period %d: %v", periodIndex, err)
		}
		if err := tx.Commit(); err != nil {
			fatalf("commit posteriors period %d: %v", periodIndex, err)
		}
	}

	// Load full history and smooth.
	drvHist, err := loadAllDriverPosteriors(db)
	if err != nil {
		fatalf("load all driver posteriors: %v", err)
	}
	teamHist, err := loadAllTeamPosteriors(db)
	if err != nil {
		fatalf("load all team posteriors: %v", err)
	}

	smoothEntries := func(entries []posteriorEntry) []posteriorEntry {
		ratings := make([]multicompetitor.PeriodRating, len(entries))
		for i, e := range entries {
			ratings[i] = e.PeriodRating
		}
		smoothed := multicompetitor.Smooth(ratings, *tau)
		out := make([]posteriorEntry, len(entries))
		for i, e := range entries {
			out[i] = posteriorEntry{Index: e.Index, PeriodRating: smoothed[i]}
		}
		return out
	}

	drvSmoothed := make(map[string][]posteriorEntry, len(drvHist))
	teamSmoothed := make(map[string][]posteriorEntry, len(teamHist))
	for id, hist := range drvHist {
		drvSmoothed[id] = smoothEntries(hist)
	}
	for id, hist := range teamHist {
		teamSmoothed[id] = smoothEntries(hist)
	}

	if err := replaceSmoothed(db, drvSmoothed, teamSmoothed); err != nil {
		fatalf("replace smoothed: %v", err)
	}
	_, _ = fmt.Fprintf(os.Stderr, "ratings updated (%d new races processed)\n", novelCount)
}
