package main

import (
	"database/sql"
	"flag"
	"fmt"
)

func cmdTeams(db *sql.DB, args []string) {
	fs := flag.NewFlagSet("teams", flag.ExitOnError)
	_ = fs.Parse(args) // ExitOnError: calls os.Exit on bad input, never returns an error

	rows, err := loadLatestTeamSmoothed(db)
	if err != nil {
		fatalf("load team ratings: %v", err)
	}

	fmt.Printf(" %4s  %-25s  %6s  %4s\n", "Rank", "Team", "Rating", "RD")
	for i, r := range rows {
		fmt.Printf(" %4d  %-25s  %6d  %4d\n", i+1, r.name, toRating(r.mu), toRD(r.sigma))
	}
}
