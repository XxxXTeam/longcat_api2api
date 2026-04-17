package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type KeyState string

const (
	keyStateActive   KeyState = "active"
	keyStateCooldown KeyState = "cooldown"
	keyStateDisabled KeyState = "disabled"
)

type KeyRuntime struct {
	State       KeyState  `json:"state"`
	Reason      string    `json:"reason,omitempty"`
	Until       time.Time `json:"until,omitempty"`
	LastUsedAt  time.Time `json:"last_used_at,omitempty"`
	LastErrorAt time.Time `json:"last_error_at,omitempty"`
}

type LogEntry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}

type DashboardStats struct {
	Timestamp         string       `json:"timestamp"`
	UptimeSeconds     int64        `json:"uptime_seconds"`
	TotalKeys         int          `json:"total_keys"`
	ActiveKeys        int          `json:"active_keys"`
	CooldownKeys      int          `json:"cooldown_keys"`
	DisabledKeys      int          `json:"disabled_keys"`
	RPM               int          `json:"rpm"`
	TotalRequests     int64        `json:"total_requests"`
	SuccessRequests   int64        `json:"success_requests"`
	FailedRequests    int64        `json:"failed_requests"`
	TotalInputTokens  int64        `json:"total_input_tokens"`
	TotalOutputTokens int64        `json:"total_output_tokens"`
	RecentLogs        []LogEntry   `json:"recent_logs"`
	ModelUsage        []NamedValue `json:"model_usage"`
	StatusCodeUsage   []NamedValue `json:"status_code_usage"`
	UpstreamFormat    string       `json:"upstream_format"`
}

type NamedValue struct {
	Name  string `json:"name"`
	Value int64  `json:"value"`
}

type StatsTracker struct {
	mu            sync.RWMutex
	startTime     time.Time
	upstream      string
	dataFile      string
	keyStates     map[string]KeyRuntime
	recentReqs    []time.Time
	totalReqs     int64
	successReqs   int64
	failedReqs    int64
	inTokens      int64
	outTokens     int64
	modelUsage    map[string]int64
	statusUsage   map[string]int64
	recentLogs    []LogEntry
	maxLogEntries int
}

type persistedStats struct {
	UpstreamFormat    string                `json:"upstream_format"`
	KeyStates         map[string]KeyRuntime `json:"key_states"`
	TotalRequests     int64                 `json:"total_requests"`
	SuccessRequests   int64                 `json:"success_requests"`
	FailedRequests    int64                 `json:"failed_requests"`
	TotalInputTokens  int64                 `json:"total_input_tokens"`
	TotalOutputTokens int64                 `json:"total_output_tokens"`
	ModelUsage        map[string]int64      `json:"model_usage"`
	StatusCodeUsage   map[string]int64      `json:"status_code_usage"`
	RecentLogs        []LogEntry            `json:"recent_logs"`
}

func NewStatsTracker(upstream, dataFile string) *StatsTracker {
	return &StatsTracker{
		startTime:     time.Now(),
		upstream:      upstream,
		dataFile:      dataFile,
		keyStates:     make(map[string]KeyRuntime),
		modelUsage:    make(map[string]int64),
		statusUsage:   make(map[string]int64),
		maxLogEntries: 40,
	}
}

func (s *StatsTracker) SyncKeys(keys []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	nextStates := make(map[string]KeyRuntime, len(keys))
	for _, key := range keys {
		state, ok := s.keyStates[key]
		if !ok {
			state = KeyRuntime{State: keyStateActive}
		}
		if state.State == "" {
			state.State = keyStateActive
		}
		if state.State == keyStateCooldown && !state.Until.IsZero() && time.Now().After(state.Until) {
			state.State = keyStateActive
			state.Reason = ""
			state.Until = time.Time{}
		}
		nextStates[key] = state
	}
	s.keyStates = nextStates
}

func (s *StatsTracker) StatusOf(key string) KeyState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refreshKeyStatusLocked(key)
}

func (s *StatsTracker) MarkUsed(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.keyStates[key]
	state.LastUsedAt = time.Now()
	if state.State == keyStateCooldown && !state.Until.IsZero() && time.Now().After(state.Until) {
		state.State = keyStateActive
		state.Reason = ""
		state.Until = time.Time{}
	}
	if state.State == "" {
		state.State = keyStateActive
	}
	s.keyStates[key] = state
}

func (s *StatsTracker) MarkCooldown(key, reason string, duration time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.keyStates[key]
	state.State = keyStateCooldown
	state.Reason = reason
	state.Until = time.Now().Add(duration)
	state.LastErrorAt = time.Now()
	s.keyStates[key] = state
}

func (s *StatsTracker) MarkDisabled(key, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.keyStates[key]
	state.State = keyStateDisabled
	state.Reason = reason
	state.Until = time.Time{}
	state.LastErrorAt = time.Now()
	s.keyStates[key] = state
}

func (s *StatsTracker) MarkActive(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.keyStates[key]
	state.State = keyStateActive
	state.Reason = ""
	state.Until = time.Time{}
	s.keyStates[key] = state
}

func (s *StatsTracker) RecordRequest(model string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	s.totalReqs++
	s.recentReqs = append(s.recentReqs, now)
	s.trimRecentLocked(now)
	if model != "" {
		s.modelUsage[model]++
	}
}

func (s *StatsTracker) RecordSuccess(statusCode int, promptTokens, completionTokens int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.successReqs++
	s.statusUsage[itoa(statusCode)]++
	s.inTokens += int64(promptTokens)
	s.outTokens += int64(completionTokens)
}

func (s *StatsTracker) RecordFailure(statusCode int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.failedReqs++
	if statusCode > 0 {
		s.statusUsage[itoa(statusCode)]++
	}
}

func (s *StatsTracker) RecordLog(entry LogEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.recentLogs = append(s.recentLogs, entry)
	if len(s.recentLogs) > s.maxLogEntries {
		s.recentLogs = append([]LogEntry(nil), s.recentLogs[len(s.recentLogs)-s.maxLogEntries:]...)
	}
}

func (s *StatsTracker) Load() error {
	if strings.TrimSpace(s.dataFile) == "" {
		return nil
	}

	data, err := os.ReadFile(filepath.Clean(s.dataFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var persisted persistedStats
	if err := json.Unmarshal(data, &persisted); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if persisted.KeyStates != nil {
		s.keyStates = persisted.KeyStates
	}
	s.totalReqs = persisted.TotalRequests
	s.successReqs = persisted.SuccessRequests
	s.failedReqs = persisted.FailedRequests
	s.inTokens = persisted.TotalInputTokens
	s.outTokens = persisted.TotalOutputTokens
	if persisted.ModelUsage != nil {
		s.modelUsage = persisted.ModelUsage
	}
	if persisted.StatusCodeUsage != nil {
		s.statusUsage = persisted.StatusCodeUsage
	}
	if persisted.RecentLogs != nil {
		s.recentLogs = persisted.RecentLogs
	}
	return nil
}

func (s *StatsTracker) Save() error {
	s.mu.RLock()
	persisted := persistedStats{
		UpstreamFormat:    s.upstream,
		KeyStates:         cloneKeyStates(s.keyStates),
		TotalRequests:     s.totalReqs,
		SuccessRequests:   s.successReqs,
		FailedRequests:    s.failedReqs,
		TotalInputTokens:  s.inTokens,
		TotalOutputTokens: s.outTokens,
		ModelUsage:        cloneInt64Map(s.modelUsage),
		StatusCodeUsage:   cloneInt64Map(s.statusUsage),
		RecentLogs:        append([]LogEntry(nil), s.recentLogs...),
	}
	s.mu.RUnlock()

	if strings.TrimSpace(s.dataFile) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.dataFile), 0o755); err != nil {
		return err
	}

	raw, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.dataFile + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.dataFile)
}

func (s *StatsTracker) StartAutoSave(interval time.Duration, logger *ColorLogger) {
	if interval <= 0 || strings.TrimSpace(s.dataFile) == "" {
		return
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			if err := s.Save(); err != nil && logger != nil {
				logger.Warnf("save local stats failed: %v", err)
			}
		}
	}()
}

func (s *StatsTracker) Snapshot() DashboardStats {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	s.trimRecentLocked(now)

	active := 0
	cooldown := 0
	disabled := 0
	for key := range s.keyStates {
		switch s.refreshKeyStatusLocked(key) {
		case keyStateActive:
			active++
		case keyStateCooldown:
			cooldown++
		case keyStateDisabled:
			disabled++
		}
	}

	modelUsage := sortNamedValues(s.modelUsage)
	statusUsage := sortNamedValues(s.statusUsage)
	recentLogs := append([]LogEntry(nil), s.recentLogs...)
	reverseLogs(recentLogs)

	return DashboardStats{
		Timestamp:         now.Format(time.RFC3339),
		UptimeSeconds:     int64(now.Sub(s.startTime).Seconds()),
		TotalKeys:         len(s.keyStates),
		ActiveKeys:        active,
		CooldownKeys:      cooldown,
		DisabledKeys:      disabled,
		RPM:               len(s.recentReqs),
		TotalRequests:     s.totalReqs,
		SuccessRequests:   s.successReqs,
		FailedRequests:    s.failedReqs,
		TotalInputTokens:  s.inTokens,
		TotalOutputTokens: s.outTokens,
		RecentLogs:        recentLogs,
		ModelUsage:        modelUsage,
		StatusCodeUsage:   statusUsage,
		UpstreamFormat:    s.upstream,
	}
}

func (s *StatsTracker) refreshKeyStatusLocked(key string) KeyState {
	state := s.keyStates[key]
	if state.State == keyStateCooldown && !state.Until.IsZero() && time.Now().After(state.Until) {
		state.State = keyStateActive
		state.Reason = ""
		state.Until = time.Time{}
		s.keyStates[key] = state
	}
	if state.State == "" {
		state.State = keyStateActive
		s.keyStates[key] = state
	}
	return state.State
}

func (s *StatsTracker) trimRecentLocked(now time.Time) {
	cutoff := now.Add(-time.Minute)
	idx := 0
	for idx < len(s.recentReqs) && s.recentReqs[idx].Before(cutoff) {
		idx++
	}
	if idx > 0 {
		s.recentReqs = append([]time.Time(nil), s.recentReqs[idx:]...)
	}
}

func sortNamedValues(items map[string]int64) []NamedValue {
	out := make([]NamedValue, 0, len(items))
	for k, v := range items {
		out = append(out, NamedValue{Name: k, Value: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Value == out[j].Value {
			return out[i].Name < out[j].Name
		}
		return out[i].Value > out[j].Value
	})
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

func reverseLogs(items []LogEntry) {
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
}

func cloneKeyStates(items map[string]KeyRuntime) map[string]KeyRuntime {
	out := make(map[string]KeyRuntime, len(items))
	for k, v := range items {
		out[k] = v
	}
	return out
}

func cloneInt64Map(items map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(items))
	for k, v := range items {
		out[k] = v
	}
	return out
}

func itoa(v int) string {
	switch {
	case v == 0:
		return "0"
	default:
		return formatInt(int64(v))
	}
}

func formatInt(v int64) string {
	if v == 0 {
		return "0"
	}

	negative := v < 0
	if negative {
		v = -v
	}

	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
