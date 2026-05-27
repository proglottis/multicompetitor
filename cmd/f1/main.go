package main

import (
	"fmt"
	"math"
	"os"
	"strings"
)

// Points awarded for positions 1–10 in modern F1 scoring.
var f1Points = [10]int{25, 18, 15, 12, 10, 8, 6, 4, 2, 1}

// Glicko-2 display scale: 400/ln(10). Maps μ=0 → 1500, one μ-unit ≈ 174 pts.
const glickoScale = 173.7178

func toRating(mu float64) int { return int(math.Round(glickoScale*mu + 1500)) }
func toRD(sigma float64) int  { return int(math.Round(glickoScale * sigma)) }
func toDelta(dmu float64) int { return int(math.Round(glickoScale * dmu)) }

func main() {
	if len(os.Args) < 2 {
		usage()
	}

	// Global flag: -db must come before the subcommand.
	dbPath := ""
	args := os.Args[1:]
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		if args[0] == "-db" || args[0] == "--db" {
			if len(args) < 2 {
				fatalf("-db requires a value")
			}
			dbPath = args[1]
			args = args[2:]
		} else if strings.HasPrefix(args[0], "-db=") {
			dbPath = strings.TrimPrefix(args[0], "-db=")
			args = args[1:]
		} else if strings.HasPrefix(args[0], "--db=") {
			dbPath = strings.TrimPrefix(args[0], "--db=")
			args = args[1:]
		} else {
			break
		}
	}

	if len(args) == 0 {
		usage()
	}

	cmd := args[0]
	rest := args[1:]

	if dbPath == "" {
		fatalf("-db is required (e.g. -db f1.db)")
	}

	db, err := openDB(dbPath)
	if err != nil {
		fatalf("open db %s: %v", dbPath, err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "f1: close db: %v\n", err)
		}
	}()

	switch cmd {
	case "update":
		cmdUpdate(db, rest)
	case "drivers":
		cmdDrivers(db, rest)
	case "teams":
		cmdTeams(db, rest)
	case "svg":
		cmdSVG(db, rest)
	case "optimize":
		cmdOptimize(db, rest)
	default:
		fatalf("unknown command %q\n\nRun 'f1 -db <path> <command> -help' for usage.", cmd)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `f1 — F1 driver and team ratings

Usage:
  f1 -db <path> update   [-from YEAR] [-to YEAR] [-tau T] [-sigma0 S] [-recompute]
  f1 -db <path> drivers  [-n 20]
  f1 -db <path> teams
  f1 -db <path> svg      [-out ratings.svg] [-n 20] [-teams]
  f1 -db <path> optimize [-val N]           find optimal tau and sigma0 (N validation seasons, default 3)`)
	os.Exit(1)
}

func fatalf(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(os.Stderr, "f1: "+format+"\n", args...)
	os.Exit(1)
}
