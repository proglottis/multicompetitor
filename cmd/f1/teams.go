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

	fmt.Printf(" %4s  %-25s  %6s  %5s\n", "Rank", "Team", "μ", "σ")
	for i, r := range rows {
		fmt.Printf(" %4d  %-25s  %6.2f  %5.2f\n", i+1, r.name, r.mu, r.sigma)
	}
}
