package discovery

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var ErrSSONotLoggedIn = errors.New("aws sso token missing or expired")

type tokenCacheRecord struct {
	StartURL    string `json:"startUrl"`
	Region      string `json:"region"`
	AccessToken string `json:"accessToken"`
	ExpiresAt   string `json:"expiresAt"`
}

type tokenInfo struct {
	AccessToken string
	ExpiresAt   time.Time
}

func loadTokenFromCache(startURL, region string, now time.Time) (tokenInfo, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return tokenInfo{}, err
	}
	dir := filepath.Join(home, ".aws", "sso", "cache")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return tokenInfo{}, fmt.Errorf("read sso cache: %w", err)
	}
	startURL = strings.TrimSpace(startURL)
	region = strings.ToLower(strings.TrimSpace(region))

	candidates := make([]tokenInfo, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var rec tokenCacheRecord
		if err := json.Unmarshal(body, &rec); err != nil {
			continue
		}
		if rec.AccessToken == "" || rec.ExpiresAt == "" {
			continue
		}
		if startURL != "" && rec.StartURL != startURL {
			continue
		}
		if region != "" && strings.ToLower(rec.Region) != region {
			continue
		}
		expiresAt, err := parseExpiry(rec.ExpiresAt)
		if err != nil {
			continue
		}
		if !expiresAt.After(now.Add(1 * time.Minute)) {
			continue
		}
		candidates = append(candidates, tokenInfo{AccessToken: rec.AccessToken, ExpiresAt: expiresAt})
	}
	if len(candidates) == 0 {
		return tokenInfo{}, ErrSSONotLoggedIn
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].ExpiresAt.After(candidates[j].ExpiresAt)
	})
	return candidates[0], nil
}

func parseExpiry(value string) (time.Time, error) {
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05UTC",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if ts, err := time.Parse(layout, value); err == nil {
			return ts, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported expiresAt format: %q", value)
}
