package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultConfigFile = "config.json"
	defaultDataFile   = "data/stats.json"
)

type fileConfig struct {
	Addr             string   `json:"addr"`
	UpstreamFormat   string   `json:"upstream_format"`
	OpenAIBaseURL    string   `json:"openai_base_url"`
	AnthropicBaseURL string   `json:"anthropic_base_url"`
	KeyFile          string   `json:"key_file"`
	APIKeys          []string `json:"api_keys"`
	HTTPTimeout      string   `json:"http_timeout"`
	KeyCooldown      string   `json:"key_cooldown"`
	DataFile         string   `json:"data_file"`
	AutoSaveInterval string   `json:"auto_save_interval"`
}

func loadConfig() Config {
	configFile := strings.TrimSpace(os.Getenv("CONFIG_FILE"))
	if configFile == "" {
		configFile = defaultConfigFile
	}

	cfg := Config{
		ConfigFile:     configFile,
		Addr:           defaultAddr,
		UpstreamFormat: "openai",
		OpenAIBaseURL:  defaultOpenAIBase,
		AnthropicBase:  defaultAnthropicBase,
		KeyFile:        defaultKeyFile,
		APIKeys:        []string{},
		Timeout:        120 * time.Second,
		Cooldown:       defaultCooldown,
		DataFile:       defaultDataFile,
		AutoSave:       5 * time.Second,
	}

	if err := ensureConfigFile(configFile, cfg); err != nil && appLogger != nil {
		appLogger.Warnf("ensure config file failed: %v", err)
	}

	if fileCfg, err := readFileConfig(configFile); err == nil {
		applyFileConfig(&cfg, fileCfg)
	}

	applyEnvOverrides(&cfg)
	cfg.UpstreamFormat = strings.ToLower(strings.TrimSpace(cfg.UpstreamFormat))
	if cfg.UpstreamFormat == "" {
		cfg.UpstreamFormat = "openai"
	}
	cfg.OpenAIBaseURL = strings.TrimRight(cfg.OpenAIBaseURL, "/")
	cfg.AnthropicBase = strings.TrimRight(cfg.AnthropicBase, "/")
	cfg.KeyFile = normalizePath(cfg.KeyFile)
	cfg.DataFile = normalizePath(cfg.DataFile)
	cfg.APIKeys = normalizeAPIKeys(cfg.APIKeys)
	return cfg
}

func ensureConfigFile(path string, cfg Config) error {
	_, err := os.Stat(filepath.Clean(path))
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	content, err := json.MarshalIndent(defaultFileConfig(cfg), "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return os.WriteFile(filepath.Clean(path), content, 0o644)
}

func readFileConfig(path string) (fileConfig, error) {
	var cfg fileConfig

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return cfg, err
	}
	err = json.Unmarshal(data, &cfg)
	return cfg, err
}

func defaultFileConfig(cfg Config) fileConfig {
	apiKeys := append([]string{}, cfg.APIKeys...)
	return fileConfig{
		Addr:             cfg.Addr,
		UpstreamFormat:   cfg.UpstreamFormat,
		OpenAIBaseURL:    cfg.OpenAIBaseURL,
		AnthropicBaseURL: cfg.AnthropicBase,
		KeyFile:          cfg.KeyFile,
		APIKeys:          apiKeys,
		HTTPTimeout:      cfg.Timeout.String(),
		KeyCooldown:      cfg.Cooldown.String(),
		DataFile:         cfg.DataFile,
		AutoSaveInterval: cfg.AutoSave.String(),
	}
}

func applyFileConfig(cfg *Config, fc fileConfig) {
	if fc.Addr != "" {
		cfg.Addr = fc.Addr
	}
	if fc.UpstreamFormat != "" {
		cfg.UpstreamFormat = fc.UpstreamFormat
	}
	if fc.OpenAIBaseURL != "" {
		cfg.OpenAIBaseURL = fc.OpenAIBaseURL
	}
	if fc.AnthropicBaseURL != "" {
		cfg.AnthropicBase = fc.AnthropicBaseURL
	}
	if fc.KeyFile != "" {
		cfg.KeyFile = fc.KeyFile
	}
	if len(fc.APIKeys) > 0 {
		cfg.APIKeys = append([]string(nil), fc.APIKeys...)
	}
	if v := parseDurationString(fc.HTTPTimeout, 0); v > 0 {
		cfg.Timeout = v
	}
	if v := parseDurationString(fc.KeyCooldown, 0); v > 0 {
		cfg.Cooldown = v
	}
	if fc.DataFile != "" {
		cfg.DataFile = fc.DataFile
	}
	if v := parseDurationString(fc.AutoSaveInterval, 0); v > 0 {
		cfg.AutoSave = v
	}
}

func applyEnvOverrides(cfg *Config) {
	if v := strings.TrimSpace(os.Getenv("ADDR")); v != "" {
		cfg.Addr = v
	}
	if v := strings.TrimSpace(os.Getenv("PORT")); v != "" && strings.TrimSpace(os.Getenv("ADDR")) == "" {
		cfg.Addr = v
	}
	if v := strings.TrimSpace(os.Getenv("LONGCAT_UPSTREAM_FORMAT")); v != "" {
		cfg.UpstreamFormat = v
	}
	if v := strings.TrimSpace(os.Getenv("LONGCAT_OPENAI_BASE")); v != "" {
		cfg.OpenAIBaseURL = v
	}
	if v := strings.TrimSpace(os.Getenv("LONGCAT_ANTHROPIC_BASE")); v != "" {
		cfg.AnthropicBase = v
	}
	if v := strings.TrimSpace(os.Getenv("KEY_FILE")); v != "" {
		cfg.KeyFile = v
	}
	if v := strings.TrimSpace(os.Getenv("CLIENT_API_KEYS")); v != "" {
		cfg.APIKeys = strings.Split(v, ",")
	}
	if v := getenvDuration("HTTP_TIMEOUT", 0); v > 0 {
		cfg.Timeout = v
	}
	if v := getenvDuration("KEY_COOLDOWN", 0); v > 0 {
		cfg.Cooldown = v
	}
	if v := strings.TrimSpace(os.Getenv("DATA_FILE")); v != "" {
		cfg.DataFile = v
	}
	if v := getenvDuration("AUTO_SAVE_INTERVAL", 0); v > 0 {
		cfg.AutoSave = v
	}
}

func normalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	return filepath.Clean(path)
}

func parseDurationString(raw string, fallback time.Duration) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	if d, err := time.ParseDuration(raw); err == nil {
		return d
	}
	return fallback
}

func normalizeAPIKeys(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}

	out := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
