package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

type apiResponse struct {
	MRData struct {
		Total     string
		RaceTable struct {
			Races []apiRace
		}
	}
}

type apiRace struct {
	Season  string
	Round   string
	Results []apiResult
}

type apiResult struct {
	PositionText string
	Driver       struct {
		DriverId   string
		Code       string
		GivenName  string
		FamilyName string
	}
	Constructor struct {
		ConstructorId string
		Name          string
	}
}

func fetchPage(url string) (*apiResponse, error) {
	const maxRetries = 5
	delay := 500 * time.Millisecond
	var lastErr error
	for attempt := range maxRetries {
		if attempt > 0 {
			time.Sleep(delay)
			delay *= 2
		}
		resp, err := httpClient.Get(url)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("unexpected status %d", resp.StatusCode)
			continue
		}
		var result apiResponse
		err = json.NewDecoder(resp.Body).Decode(&result)
		if closeErr := resp.Body.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			lastErr = err
			continue
		}
		return &result, nil
	}
	return nil, fmt.Errorf("fetch %s: %w", url, lastErr)
}

func fetchSeason(year int) ([]apiRace, error) {
	const pageSize = 100
	byRound := make(map[string]*apiRace)
	total := 0
	for offset := 0; ; offset += pageSize {
		url := fmt.Sprintf("https://api.jolpi.ca/ergast/f1/%d/results.json?limit=%d&offset=%d", year, pageSize, offset)
		result, err := fetchPage(url)
		if err != nil {
			return nil, err
		}
		if total == 0 {
			total, _ = strconv.Atoi(result.MRData.Total)
		}
		for i := range result.MRData.RaceTable.Races {
			r := &result.MRData.RaceTable.Races[i]
			if existing, ok := byRound[r.Round]; ok {
				existing.Results = append(existing.Results, r.Results...)
			} else {
				byRound[r.Round] = r
			}
		}
		if offset+pageSize >= total {
			break
		}
	}
	races := make([]apiRace, 0, len(byRound))
	for _, r := range byRound {
		races = append(races, *r)
	}
	sort.Slice(races, func(i, j int) bool {
		ri, _ := strconv.Atoi(races[i].Round)
		rj, _ := strconv.Atoi(races[j].Round)
		return ri < rj
	})
	return races, nil
}
