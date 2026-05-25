package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	_ "modernc.org/sqlite"

	"github.com/proglottis/multicompetitor"
)

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS drivers (
			id   TEXT PRIMARY KEY,
			name TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS teams (
			id   TEXT PRIMARY KEY,
			name TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS races (
			id     INTEGER PRIMARY KEY,
			season INTEGER NOT NULL,
			round  INTEGER NOT NULL,
			UNIQUE(season, round)
		);
		CREATE TABLE IF NOT EXISTS results (
			race_id   INTEGER NOT NULL REFERENCES races(id),
			driver_id TEXT    NOT NULL REFERENCES drivers(id),
			team_id   TEXT    NOT NULL REFERENCES teams(id),
			position  INTEGER NOT NULL,
			PRIMARY KEY (race_id, driver_id)
		);
		CREATE TABLE IF NOT EXISTS driver_posteriors (
			driver_id    TEXT    NOT NULL REFERENCES drivers(id),
			period_index INTEGER NOT NULL,
			mu           REAL    NOT NULL,
			sigma        REAL    NOT NULL,
			PRIMARY KEY (driver_id, period_index)
		);
		CREATE TABLE IF NOT EXISTS team_posteriors (
			team_id      TEXT    NOT NULL REFERENCES teams(id),
			period_index INTEGER NOT NULL,
			mu           REAL    NOT NULL,
			sigma        REAL    NOT NULL,
			PRIMARY KEY (team_id, period_index)
		);
		CREATE TABLE IF NOT EXISTS driver_smoothed (
			driver_id    TEXT    NOT NULL REFERENCES drivers(id),
			period_index INTEGER NOT NULL,
			mu           REAL    NOT NULL,
			sigma        REAL    NOT NULL,
			PRIMARY KEY (driver_id, period_index)
		);
		CREATE TABLE IF NOT EXISTS team_smoothed (
			team_id      TEXT    NOT NULL REFERENCES teams(id),
			period_index INTEGER NOT NULL,
			mu           REAL    NOT NULL,
			sigma        REAL    NOT NULL,
			PRIMARY KEY (team_id, period_index)
		);
	`)
	return err
}

// storedSeasons returns the set of seasons that have any results in the DB.
func storedSeasons(db *sql.DB) (map[int]bool, error) {
	rows, err := db.Query(`SELECT DISTINCT season FROM races`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() // rows.Err() surfaces any iteration error
	m := make(map[int]bool)
	for rows.Next() {
		var s int
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		m[s] = true
	}
	return m, rows.Err()
}

// storedRounds returns the set of rounds already stored for a given season.
func storedRounds(db *sql.DB, season int) (map[int]bool, error) {
	rows, err := db.Query(`SELECT round FROM races WHERE season = ?`, season)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() // rows.Err() surfaces any iteration error
	m := make(map[int]bool)
	for rows.Next() {
		var r int
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		m[r] = true
	}
	return m, rows.Err()
}

// insertRace inserts a race and its results, upserting drivers and teams.
func insertRace(db *sql.DB, season, round int, results []apiResult) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit; safety net for early returns

	var raceID int64
	err = tx.QueryRow(
		`INSERT INTO races(season, round) VALUES(?,?) RETURNING id`,
		season, round,
	).Scan(&raceID)
	if err != nil {
		return fmt.Errorf("insert race %d/%d: %w", season, round, err)
	}

	for _, r := range results {
		pos := 0
		if p, err2 := strconv.Atoi(r.PositionText); err2 == nil && p > 0 {
			pos = p
		}
		if _, err = tx.Exec(
			`INSERT OR IGNORE INTO drivers(id, name) VALUES(?,?)`,
			r.Driver.DriverId,
			r.Driver.GivenName+" "+r.Driver.FamilyName,
		); err != nil {
			return err
		}
		if _, err = tx.Exec(
			`INSERT OR IGNORE INTO teams(id, name) VALUES(?,?)`,
			r.Constructor.ConstructorId, r.Constructor.Name,
		); err != nil {
			return err
		}
		if _, err = tx.Exec(
			`INSERT OR IGNORE INTO results(race_id, driver_id, team_id, position) VALUES(?,?,?,?)`,
			raceID, r.Driver.DriverId, r.Constructor.ConstructorId, pos,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type raceRow struct {
	id     int64
	season int
	round  int
}

// loadRaces returns all races ordered by season, round.
func loadRaces(db *sql.DB) ([]raceRow, error) {
	rows, err := db.Query(`SELECT id, season, round FROM races ORDER BY season, round`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() // rows.Err() surfaces any iteration error
	var out []raceRow
	for rows.Next() {
		var r raceRow
		if err := rows.Scan(&r.id, &r.season, &r.round); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type resultRow struct {
	driverID string
	teamID   string
	position int
}

// loadResults returns all classified results for a race.
func loadResults(db *sql.DB, raceID int64) ([]resultRow, error) {
	rows, err := db.Query(
		`SELECT driver_id, team_id, position FROM results WHERE race_id = ? AND position > 0`,
		raceID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() // rows.Err() surfaces any iteration error
	var out []resultRow
	for rows.Next() {
		var r resultRow
		if err := rows.Scan(&r.driverID, &r.teamID, &r.position); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// lastProcessedIndex returns the highest period_index stored in driver_posteriors,
// or -1 if none.
func lastProcessedIndex(db *sql.DB) (int, error) {
	var idx sql.NullInt64
	err := db.QueryRow(`SELECT MAX(period_index) FROM driver_posteriors`).Scan(&idx)
	if err != nil {
		return -1, err
	}
	if !idx.Valid {
		return -1, nil
	}
	return int(idx.Int64), nil
}

// loadLastPosteriors returns the most recent (driver_id → {mu, sigma}) from
// driver_posteriors, and equivalently for teams.
func loadLastDriverPosteriors(db *sql.DB) (map[string]multicompetitor.PeriodRating, error) {
	return loadLastPosteriors(db,
		`SELECT d1.driver_id, d1.mu, d1.sigma FROM driver_posteriors d1
		 WHERE d1.period_index = (SELECT MAX(d2.period_index) FROM driver_posteriors d2 WHERE d2.driver_id = d1.driver_id)`)
}

func loadLastTeamPosteriors(db *sql.DB) (map[string]multicompetitor.PeriodRating, error) {
	return loadLastPosteriors(db,
		`SELECT t1.team_id, t1.mu, t1.sigma FROM team_posteriors t1
		 WHERE t1.period_index = (SELECT MAX(t2.period_index) FROM team_posteriors t2 WHERE t2.team_id = t1.team_id)`)
}

func loadLastPosteriors(db *sql.DB, query string) (map[string]multicompetitor.PeriodRating, error) {
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() // rows.Err() surfaces any iteration error
	m := make(map[string]multicompetitor.PeriodRating)
	for rows.Next() {
		var id string
		var pr multicompetitor.PeriodRating
		if err := rows.Scan(&id, &pr.Mu, &pr.Sigma); err != nil {
			return nil, err
		}
		m[id] = pr
	}
	return m, rows.Err()
}

// insertDriverPosteriors inserts or replaces driver posteriors for one period.
func insertDriverPosteriors(tx *sql.Tx, periodIndex int, posteriors map[string]multicompetitor.PeriodRating) (retErr error) {
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO driver_posteriors(driver_id, period_index, mu, sigma) VALUES(?,?,?,?)`)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, stmt.Close()) }()
	for id, p := range posteriors {
		if _, err := stmt.Exec(id, periodIndex, p.Mu, p.Sigma); err != nil {
			return err
		}
	}
	return nil
}

func insertTeamPosteriors(tx *sql.Tx, periodIndex int, posteriors map[string]multicompetitor.PeriodRating) (retErr error) {
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO team_posteriors(team_id, period_index, mu, sigma) VALUES(?,?,?,?)`)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, stmt.Close()) }()
	for id, p := range posteriors {
		if _, err := stmt.Exec(id, periodIndex, p.Mu, p.Sigma); err != nil {
			return err
		}
	}
	return nil
}

// posteriorEntry pairs a global period_index with a PeriodRating.
type posteriorEntry struct {
	Index int
	multicompetitor.PeriodRating
}

// loadAllDriverPosteriors returns the full indexed history per driver_id.
func loadAllDriverPosteriors(db *sql.DB) (map[string][]posteriorEntry, error) {
	return loadAllPosteriors(db,
		`SELECT driver_id, period_index, mu, sigma FROM driver_posteriors ORDER BY driver_id, period_index`)
}

func loadAllTeamPosteriors(db *sql.DB) (map[string][]posteriorEntry, error) {
	return loadAllPosteriors(db,
		`SELECT team_id, period_index, mu, sigma FROM team_posteriors ORDER BY team_id, period_index`)
}

func loadAllPosteriors(db *sql.DB, query string) (map[string][]posteriorEntry, error) {
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() // rows.Err() surfaces any iteration error
	m := make(map[string][]posteriorEntry)
	for rows.Next() {
		var id string
		var e posteriorEntry
		if err := rows.Scan(&id, &e.Index, &e.Mu, &e.Sigma); err != nil {
			return nil, err
		}
		m[id] = append(m[id], e)
	}
	return m, rows.Err()
}

// replaceSmoothed atomically replaces driver_smoothed and team_smoothed.
func replaceSmoothed(
	db *sql.DB,
	driverSmoothed map[string][]posteriorEntry,
	teamSmoothed map[string][]posteriorEntry,
) (retErr error) {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit; safety net for early returns

	if _, err := tx.Exec(`DELETE FROM driver_smoothed`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM team_smoothed`); err != nil {
		return err
	}

	ds, err := tx.Prepare(`INSERT INTO driver_smoothed(driver_id, period_index, mu, sigma) VALUES(?,?,?,?)`)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, ds.Close()) }()
	for id, hist := range driverSmoothed {
		for _, e := range hist {
			if _, err := ds.Exec(id, e.Index, e.Mu, e.Sigma); err != nil {
				return err
			}
		}
	}

	ts, err := tx.Prepare(`INSERT INTO team_smoothed(team_id, period_index, mu, sigma) VALUES(?,?,?,?)`)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, ts.Close()) }()
	for id, hist := range teamSmoothed {
		for _, e := range hist {
			if _, err := ts.Exec(id, e.Index, e.Mu, e.Sigma); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

// latestSmoothedPeriod returns the highest period_index stored in driver_smoothed.
func latestSmoothedPeriod(db *sql.DB) (int, error) {
	var idx sql.NullInt64
	err := db.QueryRow(`SELECT MAX(period_index) FROM driver_smoothed`).Scan(&idx)
	if err != nil || !idx.Valid {
		return -1, err
	}
	return int(idx.Int64), nil
}

type driverSmoothedRow struct {
	driverID string
	name     string
	teamID   string
	mu       float64
	sigma    float64
}

// loadLatestDriverSmoothed returns driver rows at the latest period, with name and last team.
func loadLatestDriverSmoothed(db *sql.DB) ([]driverSmoothedRow, error) {
	rows, err := db.Query(`
		SELECT ds.driver_id, d.name,
		       COALESCE((SELECT r.team_id FROM results r
		                 JOIN races rc ON r.race_id = rc.id
		                 WHERE r.driver_id = ds.driver_id
		                 ORDER BY rc.season DESC, rc.round DESC LIMIT 1), '') AS team_id,
		       ds.mu, ds.sigma
		FROM driver_smoothed ds
		JOIN drivers d ON d.id = ds.driver_id
		WHERE ds.period_index = (SELECT MAX(period_index) FROM driver_smoothed)
		ORDER BY ds.mu DESC
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() // rows.Err() surfaces any iteration error
	var out []driverSmoothedRow
	for rows.Next() {
		var r driverSmoothedRow
		if err := rows.Scan(&r.driverID, &r.name, &r.teamID, &r.mu, &r.sigma); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type teamSmoothedRow struct {
	teamID string
	name   string
	mu     float64
	sigma  float64
}

// loadLatestTeamSmoothed returns team rows at the latest period.
func loadLatestTeamSmoothed(db *sql.DB) ([]teamSmoothedRow, error) {
	rows, err := db.Query(`
		SELECT ts.team_id, t.name, ts.mu, ts.sigma
		FROM team_smoothed ts
		JOIN teams t ON t.id = ts.team_id
		WHERE ts.period_index = (SELECT MAX(period_index) FROM team_smoothed)
		ORDER BY ts.mu DESC
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() // rows.Err() surfaces any iteration error
	var out []teamSmoothedRow
	for rows.Next() {
		var r teamSmoothedRow
		if err := rows.Scan(&r.teamID, &r.name, &r.mu, &r.sigma); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// loadDriverSmoothedHistory returns full smoothed history for named drivers.
func loadDriverSmoothedHistory(db *sql.DB, driverIDs []string) (map[string][]posteriorEntry, error) {
	if len(driverIDs) == 0 {
		return nil, nil
	}
	in := make([]interface{}, len(driverIDs))
	placeholders := ""
	for i, id := range driverIDs {
		in[i] = id
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
	}
	rows, err := db.Query(
		`SELECT driver_id, period_index, mu, sigma
		 FROM driver_smoothed
		 WHERE driver_id IN (`+placeholders+`)
		 ORDER BY driver_id, period_index`,
		in...,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() // rows.Err() surfaces any iteration error
	m := make(map[string][]posteriorEntry)
	for rows.Next() {
		var id string
		var e posteriorEntry
		if err := rows.Scan(&id, &e.Index, &e.Mu, &e.Sigma); err != nil {
			return nil, err
		}
		m[id] = append(m[id], e)
	}
	return m, rows.Err()
}

// loadTeamSmoothedHistory returns full smoothed history for named teams.
func loadTeamSmoothedHistory(db *sql.DB, teamIDs []string) (map[string][]posteriorEntry, error) {
	if len(teamIDs) == 0 {
		return nil, nil
	}
	in := make([]interface{}, len(teamIDs))
	placeholders := ""
	for i, id := range teamIDs {
		in[i] = id
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
	}
	rows, err := db.Query(
		`SELECT team_id, period_index, mu, sigma
		 FROM team_smoothed
		 WHERE team_id IN (`+placeholders+`)
		 ORDER BY team_id, period_index`,
		in...,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() // rows.Err() surfaces any iteration error
	m := make(map[string][]posteriorEntry)
	for rows.Next() {
		var id string
		var e posteriorEntry
		if err := rows.Scan(&id, &e.Index, &e.Mu, &e.Sigma); err != nil {
			return nil, err
		}
		m[id] = append(m[id], e)
	}
	return m, rows.Err()
}

// loadSeasonPeriodStarts returns (seasonYear, periodIndex) pairs for the first
// race of each season — used to draw season boundary lines on the SVG.
func loadSeasonPeriodStarts(db *sql.DB) ([]int, []int, error) {
	rows, err := db.Query(`
		SELECT season, MIN(rownum) FROM (
			SELECT season, ROW_NUMBER() OVER (ORDER BY season, round) - 1 AS rownum
			FROM races
		) GROUP BY season ORDER BY season
	`)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }() // rows.Err() surfaces any iteration error
	var years, starts []int
	for rows.Next() {
		var y, s int
		if err := rows.Scan(&y, &s); err != nil {
			return nil, nil, err
		}
		years = append(years, y)
		starts = append(starts, s)
	}
	return starts, years, rows.Err()
}

type activePeriods struct{ first, last int }

// loadActivePeriods returns the first and last period_index where each entity
// has a classified result (position > 0). idCol must be "driver_id" or "team_id" —
// it is an internal column selector, not user-supplied input.
func loadActivePeriods(db *sql.DB, ids []string, idCol string) (map[string]activePeriods, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	in := make([]interface{}, len(ids))
	placeholders := ""
	for i, id := range ids {
		in[i] = id
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
	}
	query := `
		WITH nr AS (
			SELECT id, ROW_NUMBER() OVER (ORDER BY season, round) - 1 AS period_index
			FROM races
		)
		SELECT r.` + idCol + `,
		       MIN(nr.period_index) AS first_period,
		       MAX(nr.period_index) AS last_period
		FROM results r
		JOIN nr ON nr.id = r.race_id
		WHERE r.` + idCol + ` IN (` + placeholders + `)
		  AND r.position > 0
		GROUP BY r.` + idCol
	rows, err := db.Query(query, in...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() // rows.Err() surfaces any iteration error
	m := make(map[string]activePeriods, len(ids))
	for rows.Next() {
		var id string
		var ap activePeriods
		if err := rows.Scan(&id, &ap.first, &ap.last); err != nil {
			return nil, err
		}
		m[id] = ap
	}
	return m, rows.Err()
}
