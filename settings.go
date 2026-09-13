package main

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
)

const (
	minStopPercent      = 60
	maxStopPercent      = 99
	defaultSettingsFile = "claude-cache-keepalive.settings.json"
)

type persistedSettings struct {
	StopPercent int   `json:"stop_percent"`
	GuardChat   *bool `json:"guard_chat,omitempty"`
}

var (
	settingsPath    string
	persistSettings = true
)

func clampStopPercent(stop int) int {
	if stop < minStopPercent {
		return minStopPercent
	}
	if stop > maxStopPercent {
		return maxStopPercent
	}
	return stop
}

func stopPercentFromReserve(reserve int) int {
	if reserve <= 0 {
		reserve = defaultReservePct
	}
	return clampStopPercent(100 - reserve)
}

func resolvedSettingsPath() string {
	if settingsPath != "" {
		return settingsPath
	}
	if persistSettings {
		return defaultSettingsFile
	}
	return ""
}

func setStopPercent(stop int) int {
	stop = clampStopPercent(stop)
	mu.Lock()
	cfg.ReservePercent = 100 - stop
	mu.Unlock()
	writePersistedSettings()
	return stop
}

func setGuardChat(on bool) {
	mu.Lock()
	cfg.GuardChat = &on
	mu.Unlock()
	writePersistedSettings()
}

func writePersistedSettings() {
	mu.Lock()
	stop := stopPercentFromReserve(cfg.ReservePercent)
	guard := cfg.guardChat()
	path := resolvedSettingsPath()
	mu.Unlock()
	if path == "" {
		return
	}
	g := guard
	raw, err := json.MarshalIndent(persistedSettings{StopPercent: stop, GuardChat: &g}, "", "  ")
	if err == nil {
		_ = os.WriteFile(path, raw, 0o644)
	}
}

func loadPersistedSettings() (persistedSettings, bool) {
	path := resolvedSettingsPath()
	if path == "" {
		return persistedSettings{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return persistedSettings{}, false
	}
	var saved persistedSettings
	if err := json.Unmarshal(raw, &saved); err != nil {
		return persistedSettings{}, false
	}
	if saved.StopPercent != 0 {
		saved.StopPercent = clampStopPercent(saved.StopPercent)
	}
	return saved, saved.StopPercent != 0 || saved.GuardChat != nil
}

func parseGuardQuery(raw string) (bool, bool) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "1", "true", "on":
		return true, true
	case "0", "false", "off":
		return false, true
	default:
		return false, false
	}
}

func parseStopQuery(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return clampStopPercent(n), true
}
