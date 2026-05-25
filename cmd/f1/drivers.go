package main

import (
	"database/sql"
	"flag"
	"fmt"
)

func cmdDrivers(db *sql.DB, args []string) {
	fs := flag.NewFlagSet("drivers", flag.ExitOnError)
	n := fs.Int("n", 20, "number of drivers to show")
	_ = fs.Parse(args) // ExitOnError: calls os.Exit on bad input, never returns an error

	driverRows, err := loadLatestDriverSmoothed(db)
	if err != nil {
		fatalf("load driver ratings: %v", err)
	}
	teamRows, err := loadLatestTeamSmoothed(db)
	if err != nil {
		fatalf("load team ratings: %v", err)
	}

	teamMu := make(map[string]float64, len(teamRows))
	teamName := make(map[string]string, len(teamRows))
	for _, t := range teamRows {
		teamMu[t.teamID] = t.mu
		teamName[t.teamID] = t.name
	}

	top := driverRows
	if *n < len(top) {
		top = top[:*n]
	}

	fmt.Printf(" %4s  %-25s  %-20s  %6s  %4s  %8s\n",
		"Rank", "Driver", "Team", "Rating", "RD", "Δ vs Car")
	for i, r := range top {
		fmt.Printf(" %4d  %-25s  %-20s  %6d  %4d  %+8d\n",
			i+1, r.name, teamName[r.teamID],
			toRating(r.mu), toRD(r.sigma),
			toDelta(r.mu-teamMu[r.teamID]))
	}
}
