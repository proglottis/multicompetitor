package main

import (
	"bufio"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"strings"
)

// chartEntry is a generic label used by writeSVG for both drivers and teams.
type chartEntry struct {
	id          string
	name        string
	firstPeriod int
	lastPeriod  int
}

func cmdSVG(db *sql.DB, args []string) {
	fs := flag.NewFlagSet("svg", flag.ExitOnError)
	out := fs.String("out", "ratings.svg", "output SVG file path")
	n := fs.Int("n", 20, "number of entries to chart")
	teams := fs.Bool("teams", false, "chart team trajectories instead of drivers")
	_ = fs.Parse(args) // ExitOnError: calls os.Exit on bad input, never returns an error

	seasonStarts, seasonYears, err := loadSeasonPeriodStarts(db)
	if err != nil {
		fatalf("load season starts: %v", err)
	}

	var entries []chartEntry
	var histories map[string][]posteriorEntry
	var title string

	fromYear, toYear := 0, 0
	if len(seasonYears) > 0 {
		fromYear = seasonYears[0]
		toYear = seasonYears[len(seasonYears)-1]
	}

	if *teams {
		teamRows, err := loadLatestTeamSmoothed(db)
		if err != nil {
			fatalf("load team ratings: %v", err)
		}
		if *n < len(teamRows) {
			teamRows = teamRows[:*n]
		}
		ids := make([]string, len(teamRows))
		nameByID := make(map[string]string, len(teamRows))
		for i, r := range teamRows {
			ids[i] = r.teamID
			nameByID[r.teamID] = r.name
			entries = append(entries, chartEntry{id: r.teamID, name: r.name})
		}
		histories, err = loadTeamSmoothedHistory(db, ids)
		if err != nil {
			fatalf("load team history: %v", err)
		}
		activeMap, err := loadActivePeriods(db, ids, "team_id")
		if err != nil {
			fatalf("load team active periods: %v", err)
		}
		for i := range entries {
			if ap, ok := activeMap[entries[i].id]; ok {
				entries[i].firstPeriod = ap.first
				entries[i].lastPeriod = ap.last
			} else if hist := histories[entries[i].id]; len(hist) > 0 {
				entries[i].lastPeriod = hist[len(hist)-1].Index
			}
		}
		// Rekey histories by name for writeSVG.
		byName := make(map[string][]posteriorEntry, len(histories))
		for id, hist := range histories {
			byName[nameByID[id]] = hist
		}
		histories = byName
		title = fmt.Sprintf("%d–%d F1 Team Ratings — μ − 2σ smoothed", fromYear, toYear)
	} else {
		driverRows, err := loadLatestDriverSmoothed(db)
		if err != nil {
			fatalf("load driver ratings: %v", err)
		}
		if *n < len(driverRows) {
			driverRows = driverRows[:*n]
		}
		ids := make([]string, len(driverRows))
		nameByID := make(map[string]string, len(driverRows))
		for i, r := range driverRows {
			ids[i] = r.driverID
			nameByID[r.driverID] = r.name
			entries = append(entries, chartEntry{id: r.driverID, name: r.name})
		}
		drvHistories, err := loadDriverSmoothedHistory(db, ids)
		if err != nil {
			fatalf("load driver history: %v", err)
		}
		byName := make(map[string][]posteriorEntry, len(drvHistories))
		for id, hist := range drvHistories {
			byName[nameByID[id]] = hist
		}
		histories = byName
		activeMap, err := loadActivePeriods(db, ids, "driver_id")
		if err != nil {
			fatalf("load driver active periods: %v", err)
		}
		for i := range entries {
			if ap, ok := activeMap[entries[i].id]; ok {
				entries[i].firstPeriod = ap.first
				entries[i].lastPeriod = ap.last
			} else if hist := histories[entries[i].name]; len(hist) > 0 {
				entries[i].lastPeriod = hist[len(hist)-1].Index
			}
		}
		title = fmt.Sprintf("%d–%d F1 Driver Ratings — μ − 2σ smoothed", fromYear, toYear)
	}

	if err := writeSVG(*out, title, entries, histories, seasonStarts, seasonYears); err != nil {
		fatalf("write svg: %v", err)
	}
	_, _ = fmt.Fprintf(os.Stderr, "chart written to %s\n", *out)
}

// conservativeEstimate returns the conservative ability estimate: μ − 2σ.
func conservativeEstimate(mu, sigma float64) float64 {
	return mu - 2*sigma
}

func writeSVG(
	path string,
	title string,
	entries []chartEntry,
	histories map[string][]posteriorEntry,
	seasonStarts, seasonYears []int,
) (retErr error) {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			retErr = errors.Join(retErr, err)
		}
	}()
	w := bufio.NewWriter(file)

	const (
		svgW    = 1400
		svgH    = 700
		marginL = 70
		marginR = 220
		marginT = 50
		marginB = 55
		plotW   = svgW - marginL - marginR
		plotH   = svgH - marginT - marginB
	)

	totalPeriods := 0
	yMin, yMax := math.MaxFloat64, -math.MaxFloat64
	for _, e := range entries {
		hist := histories[e.name]
		if len(hist) > 0 {
			if last := hist[len(hist)-1].Index + 1; last > totalPeriods {
				totalPeriods = last
			}
		}
		for _, p := range hist {
			if p.Index < e.firstPeriod || p.Index > e.lastPeriod {
				continue
			}
			v := conservativeEstimate(p.Mu, p.Sigma)
			if v < yMin {
				yMin = v
			}
			if v > yMax {
				yMax = v
			}
		}
	}
	if yMin == math.MaxFloat64 {
		yMin, yMax = -1, 1
	}
	pad := (yMax - yMin) * 0.06
	yMin -= pad
	yMax += pad

	xOf := func(i int) float64 {
		if totalPeriods <= 1 {
			return float64(marginL)
		}
		return float64(marginL) + float64(i)/float64(totalPeriods-1)*float64(plotW)
	}
	yOf := func(v float64) float64 {
		return float64(marginT+plotH) - (v-yMin)/(yMax-yMin)*float64(plotH)
	}

	palette := []string{
		"#e6194b", "#3cb44b", "#4363d8", "#f58231", "#911eb4",
		"#42d4f4", "#f032e6", "#bfef45", "#ff6b6b", "#469990",
		"#9a6324", "#ffd700", "#800000", "#aaffc3", "#808000",
		"#ffd8b1", "#000075", "#a9a9a9", "#ff1493", "#e6beff",
	}
	xmlEsc := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

	// w is a bufio.Writer; write errors accumulate and are returned by Flush.
	_, _ = fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" font-family="sans-serif">`+"\n", svgW, svgH)
	_, _ = fmt.Fprintf(w, `<rect width="%d" height="%d" fill="#1a1a2e"/>`+"\n", svgW, svgH)
	_, _ = fmt.Fprintf(w, `<text x="%d" y="28" fill="#ffffff" font-size="15" text-anchor="middle">%s</text>`+"\n",
		marginL+plotW/2, xmlEsc.Replace(title))

	// Y-axis grid lines and integer tick labels.
	for _, t := range niceTicks(yMin, yMax, 8) {
		y := yOf(t)
		_, _ = fmt.Fprintf(w, `<line x1="%d" y1="%.1f" x2="%d" y2="%.1f" stroke="#ffffff" stroke-opacity="0.08"/>`+"\n",
			marginL, y, marginL+plotW, y)
		_, _ = fmt.Fprintf(w, `<text x="%d" y="%.1f" fill="#777777" font-size="11" text-anchor="end">%.4g</text>`+"\n",
			marginL-6, y+4, t)
	}

	// μ=0 baseline (average ability).
	if yMin < 0 && yMax > 0 {
		y0 := yOf(0)
		_, _ = fmt.Fprintf(w, `<line x1="%d" y1="%.1f" x2="%d" y2="%.1f" stroke="#ffffff" stroke-opacity="0.25" stroke-dasharray="4,4"/>`+"\n",
			marginL, y0, marginL+plotW, y0)
	}

	// Season boundary lines + year labels.
	for i, start := range seasonStarts {
		x := xOf(start)
		_, _ = fmt.Fprintf(w, `<line x1="%.1f" y1="%d" x2="%.1f" y2="%d" stroke="#ffffff" stroke-opacity="0.2"/>`+"\n",
			x, marginT, x, marginT+plotH)
		_, _ = fmt.Fprintf(w, `<text x="%.1f" y="%d" fill="#aaaaaa" font-size="12" text-anchor="middle">%d</text>`+"\n",
			x, marginT+plotH+20, seasonYears[i])
	}

	// Polylines — conservative estimate μ − 2σ.
	for i, e := range entries {
		hist := histories[e.name]
		if len(hist) == 0 {
			continue
		}
		color := palette[i%len(palette)]
		var sb strings.Builder
		for _, p := range hist {
			if p.Index < e.firstPeriod || p.Index > e.lastPeriod {
				continue
			}
			if sb.Len() > 0 {
				sb.WriteByte(' ')
			}
			_, _ = fmt.Fprintf(&sb, "%.1f,%.1f", xOf(p.Index), yOf(conservativeEstimate(p.Mu, p.Sigma)))
		}
		if sb.Len() == 0 {
			continue
		}
		_, _ = fmt.Fprintf(w, `<polyline points="%s" fill="none" stroke="%s" stroke-width="2" stroke-linejoin="round"/>`+"\n",
			sb.String(), color)
	}

	// Axes.
	_, _ = fmt.Fprintf(w, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#555555"/>`+"\n",
		marginL, marginT, marginL, marginT+plotH)
	_, _ = fmt.Fprintf(w, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#555555"/>`+"\n",
		marginL, marginT+plotH, marginL+plotW, marginT+plotH)

	// Legend.
	lx := marginL + plotW + 20
	for i, e := range entries {
		color := palette[i%len(palette)]
		y := marginT + i*22
		_, _ = fmt.Fprintf(w, `<rect x="%d" y="%d" width="16" height="3" fill="%s"/>`+"\n", lx, y+7, color)
		_, _ = fmt.Fprintf(w, `<text x="%d" y="%d" fill="#cccccc" font-size="12">%s</text>`+"\n",
			lx+22, y+12, xmlEsc.Replace(e.name))
	}

	_, _ = fmt.Fprintf(w, `</svg>`)
	return w.Flush()
}

func niceTicks(min, max float64, targetN int) []float64 {
	if max <= min || targetN <= 0 {
		return nil
	}
	rawStep := (max - min) / float64(targetN)
	pow := math.Pow(10, math.Floor(math.Log10(rawStep)))
	norm := rawStep / pow
	var step float64
	switch {
	case norm < 1.5:
		step = pow
	case norm < 3.5:
		step = 2 * pow
	case norm < 7.5:
		step = 5 * pow
	default:
		step = 10 * pow
	}
	start := math.Ceil(min/step) * step
	var ticks []float64
	for t := start; t <= max+step*1e-9; t += step {
		ticks = append(ticks, math.Round(t/step)*step)
	}
	return ticks
}
