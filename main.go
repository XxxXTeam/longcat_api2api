package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultAddr          = ":8080"
	defaultKeyFile       = "key.txt"
	defaultOpenAIBase    = "https://api.longcat.chat/openai"
	defaultAnthropicBase = "https://api.longcat.chat/anthropic"
	defaultCooldown      = 90 * time.Second
	clientAuthCookieName = "longcat_dashboard_api_key"
)

var supportedModels = []string{
	"LongCat-Flash-Chat",
	"LongCat-Flash-Thinking",
	"LongCat-Flash-Thinking-2601",
	"LongCat-Flash-Lite",
	"LongCat-Flash-Omni-2603",
	"LongCat-Flash-Chat-2602-Exp",
	"LongCat-2.0-Preview",
}

var appLogger = NewColorLogger(nil)

type Config struct {
	ConfigFile     string
	Addr           string
	UpstreamFormat string
	OpenAIBaseURL  string
	AnthropicBase  string
	KeyFile        string
	APIKeys        []string
	Timeout        time.Duration
	Cooldown       time.Duration
	DataFile       string
	AutoSave       time.Duration
}

type KeyPool struct {
	path      string
	mu        sync.Mutex
	keys      []string
	next      int
	lastLoad  time.Time
	lastMTime time.Time
	stats     *StatsTracker
}

type UpstreamClient struct {
	cfg    Config
	client *http.Client
	pool   *KeyPool
	stats  *StatsTracker
	logger *ColorLogger
}

type ChatCompletionRequest struct {
	Model               string          `json:"model"`
	Messages            []OpenAIMessage `json:"messages"`
	Temperature         *float64        `json:"temperature,omitempty"`
	TopP                *float64        `json:"top_p,omitempty"`
	MaxTokens           *int            `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int            `json:"max_completion_tokens,omitempty"`
	Stream              bool            `json:"stream,omitempty"`
	Stop                any             `json:"stop,omitempty"`
	User                string          `json:"user,omitempty"`
}

type OpenAIMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Name    string          `json:"name,omitempty"`
}

type AnthropicMessageRequest struct {
	Model         string             `json:"model"`
	System        string             `json:"system,omitempty"`
	Messages      []AnthropicMessage `json:"messages"`
	MaxTokens     int                `json:"max_tokens"`
	Temperature   *float64           `json:"temperature,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	Stream        bool               `json:"stream,omitempty"`
}

type AnthropicMessage struct {
	Role    string                  `json:"role"`
	Content []AnthropicContentBlock `json:"content"`
}

type AnthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type AnthropicMessageResponse struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
	Role       string                  `json:"role"`
	Model      string                  `json:"model"`
	Content    []AnthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Usage      AnthropicUsage          `json:"usage"`
}

type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type OpenAIChatCompletionResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   *OpenAIUsage   `json:"usage,omitempty"`
}

type OpenAIChoice struct {
	Index        int                    `json:"index"`
	Message      *OpenAIResponseMessage `json:"message,omitempty"`
	Delta        *OpenAIResponseMessage `json:"delta,omitempty"`
	FinishReason string                 `json:"finish_reason,omitempty"`
}

type OpenAIResponseMessage struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type AnthropicStreamEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message struct {
		ID    string `json:"id"`
		Model string `json:"model"`
	} `json:"message"`
	ContentBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content_block"`
	Delta struct {
		Type       string `json:"type"`
		Text       string `json:"text"`
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage AnthropicUsage `json:"usage"`
}

type OpenAIModelsResponse struct {
	Object string            `json:"object"`
	Data   []OpenAIModelItem `json:"data"`
}

type OpenAIModelItem struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

func main() {
	cfg := loadConfig()
	stats := NewStatsTracker(cfg.UpstreamFormat, cfg.DataFile)
	logger := NewColorLogger(stats)
	appLogger = logger
	if err := stats.Load(); err != nil {
		logger.Warnf("load local stats failed: %v", err)
	}
	stats.StartAutoSave(cfg.AutoSave, logger)
	pool, err := NewKeyPool(cfg.KeyFile)
	if err != nil {
		logger.Errorf("load key pool failed: %v", err)
		os.Exit(1)
	}
	pool.SetStats(stats)

	client := &UpstreamClient{
		cfg: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
		pool:   pool,
		stats:  stats,
		logger: logger,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", dashboardHandler)
	mux.HandleFunc("/dashboard", dashboardHandler)
	mux.HandleFunc("/api/auth/login", loginHandler(cfg))
	mux.HandleFunc("/api/auth/logout", logoutHandler())
	mux.Handle("/api/stats", authMiddleware(cfg, statsHandler(stats)))
	mux.HandleFunc("/healthz", healthzHandler)
	mux.Handle("/v1/models", authMiddleware(cfg, http.HandlerFunc(modelsHandler)))
	mux.Handle("/v1/chat/completions", authMiddleware(cfg, http.HandlerFunc(client.chatCompletionsHandler)))

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           loggingMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	logger.Infof("longcat api2api listening on %s, upstream=%s", cfg.Addr, cfg.UpstreamFormat)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Errorf("server failed: %v", err)
		os.Exit(1)
	}
}

func NewKeyPool(path string) (*KeyPool, error) {
	p := &KeyPool{path: path}
	if err := p.reloadIfNeeded(true); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *KeyPool) SetStats(stats *StatsTracker) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stats = stats
	if p.stats != nil {
		p.stats.SyncKeys(p.keys)
	}
}

func (p *KeyPool) Next() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.reloadLocked(false); err != nil {
		return "", err
	}
	if len(p.keys) == 0 {
		return "", fmt.Errorf("no keys loaded from %s", p.path)
	}

	key := p.keys[p.next%len(p.keys)]
	p.next = (p.next + 1) % len(p.keys)
	return key, nil
}

func (p *KeyPool) Snapshot() ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.reloadLocked(false); err != nil {
		return nil, err
	}

	out := make([]string, len(p.keys))
	copy(out, p.keys)
	return out, nil
}

func (p *KeyPool) NextIndex() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.reloadLocked(false); err != nil || len(p.keys) == 0 {
		return 0
	}

	idx := p.next % len(p.keys)
	p.next = (p.next + 1) % len(p.keys)
	return idx
}

func (p *KeyPool) Candidates() ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.reloadLocked(false); err != nil {
		return nil, err
	}
	if len(p.keys) == 0 {
		return nil, fmt.Errorf("no keys loaded from %s", p.path)
	}

	start := p.next % len(p.keys)
	p.next = (p.next + 1) % len(p.keys)

	ordered := make([]string, 0, len(p.keys))
	for i := 0; i < len(p.keys); i++ {
		ordered = append(ordered, p.keys[(start+i)%len(p.keys)])
	}
	if p.stats == nil {
		return ordered, nil
	}

	ready := make([]string, 0, len(ordered))
	cooling := make([]string, 0, len(ordered))
	for _, key := range ordered {
		switch p.stats.StatusOf(key) {
		case keyStateActive:
			ready = append(ready, key)
		case keyStateCooldown:
			cooling = append(cooling, key)
		}
	}
	if len(ready) > 0 {
		return ready, nil
	}
	if len(cooling) > 0 {
		return cooling, nil
	}
	return ordered, nil
}

func (p *KeyPool) reloadIfNeeded(force bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.reloadLocked(force)
}

func (p *KeyPool) reloadLocked(force bool) error {
	info, err := os.Stat(p.path)
	if err != nil {
		return fmt.Errorf("stat key file: %w", err)
	}

	if !force && !info.ModTime().After(p.lastMTime) && time.Since(p.lastLoad) < 3*time.Second {
		return nil
	}

	file, err := os.Open(filepath.Clean(p.path))
	if err != nil {
		return fmt.Errorf("open key file: %w", err)
	}
	defer file.Close()

	var keys []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		keys = append(keys, line)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read key file: %w", err)
	}
	if len(keys) == 0 {
		return fmt.Errorf("key file %s is empty", p.path)
	}

	p.keys = keys
	p.lastLoad = time.Now()
	p.lastMTime = info.ModTime()
	if p.next >= len(keys) {
		p.next = rand.Intn(len(keys))
	}
	if p.stats != nil {
		p.stats.SyncKeys(keys)
	}
	return nil
}

func (u *UpstreamClient) chatCompletionsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "failed to read request body")
		return
	}

	var req ChatCompletionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "request body is not valid JSON")
		return
	}
	if req.Model == "" {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	if len(req.Messages) == 0 {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "messages is required")
		return
	}
	u.stats.RecordRequest(req.Model)

	keys, err := u.pool.Candidates()
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}

	var lastErr error
	var lastStatus int
	for i := 0; i < len(keys); i++ {
		key := keys[i]
		if key == "" {
			continue
		}

		u.stats.MarkUsed(key)
		retry, usage, statusCode, err := u.forwardRequest(w, r, req, body, key)
		if err == nil {
			u.stats.MarkActive(key)
			u.stats.RecordSuccess(statusCode, usage.PromptTokens, usage.CompletionTokens)
			return
		}
		lastErr = err
		lastStatus = statusCode
		u.applyKeyFailure(key, statusCode, err)
		if !retry {
			break
		}
		u.logger.Warnf("retrying with next key, attempt=%d status=%d reason=%v", i+1, statusCode, err)
	}

	if lastErr == nil {
		lastErr = errors.New("all keys failed")
	}
	u.stats.RecordFailure(lastStatus)
	writeOpenAIError(w, http.StatusBadGateway, "upstream_error", lastErr.Error())
}

func (u *UpstreamClient) applyKeyFailure(key string, statusCode int, err error) {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		u.stats.MarkDisabled(key, err.Error())
	case http.StatusTooManyRequests:
		u.stats.MarkCooldown(key, err.Error(), u.cfg.Cooldown)
	default:
		if statusCode >= http.StatusInternalServerError || statusCode == 0 {
			u.stats.MarkCooldown(key, err.Error(), u.cfg.Cooldown/2)
		}
	}
}

func (u *UpstreamClient) forwardRequest(w http.ResponseWriter, r *http.Request, req ChatCompletionRequest, rawBody []byte, key string) (bool, OpenAIUsage, int, error) {
	switch u.cfg.UpstreamFormat {
	case "openai":
		return u.forwardOpenAI(w, r, rawBody, key, req.Stream)
	case "anthropic":
		return u.forwardAnthropic(w, r, req, key)
	default:
		return false, OpenAIUsage{}, 0, fmt.Errorf("unsupported LONGCAT_UPSTREAM_FORMAT=%s", u.cfg.UpstreamFormat)
	}
}

func (u *UpstreamClient) forwardOpenAI(w http.ResponseWriter, r *http.Request, rawBody []byte, key string, stream bool) (bool, OpenAIUsage, int, error) {
	upstreamURL := u.cfg.OpenAIBaseURL + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(rawBody))
	if err != nil {
		return false, OpenAIUsage{}, 0, err
	}

	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", r.Header.Get("Accept"))

	resp, err := u.client.Do(req)
	if err != nil {
		return true, OpenAIUsage{}, 0, err
	}
	defer resp.Body.Close()

	if shouldRetryStatus(resp.StatusCode) {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return true, OpenAIUsage{}, resp.StatusCode, fmt.Errorf("openai upstream status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	if resp.StatusCode >= 400 {
		copyResponseHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, copyErr := io.Copy(w, resp.Body)
		return false, OpenAIUsage{}, resp.StatusCode, copyErr
	}

	if stream {
		copyResponseHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, err = io.Copy(w, resp.Body)
		return false, OpenAIUsage{}, resp.StatusCode, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, OpenAIUsage{}, resp.StatusCode, err
	}

	var parsed OpenAIChatCompletionResponse
	usage := OpenAIUsage{}
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Usage != nil {
		usage = *parsed.Usage
	}

	copyResponseHeaders(w.Header(), resp.Header)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, err = w.Write(body)
	return false, usage, resp.StatusCode, err
}

func (u *UpstreamClient) forwardAnthropic(w http.ResponseWriter, r *http.Request, req ChatCompletionRequest, key string) (bool, OpenAIUsage, int, error) {
	payload, err := toAnthropicRequest(req)
	if err != nil {
		return false, OpenAIUsage{}, 0, err
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return false, OpenAIUsage{}, 0, err
	}

	upstreamURL := u.cfg.AnthropicBase + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(raw))
	if err != nil {
		return false, OpenAIUsage{}, 0, err
	}

	httpReq.Header.Set("x-api-key", key)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("content-type", "application/json")
	if payload.Stream {
		httpReq.Header.Set("accept", "text/event-stream")
	} else {
		httpReq.Header.Set("accept", "application/json")
	}

	resp, err := u.client.Do(httpReq)
	if err != nil {
		return true, OpenAIUsage{}, 0, err
	}
	defer resp.Body.Close()

	if shouldRetryStatus(resp.StatusCode) {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return true, OpenAIUsage{}, resp.StatusCode, fmt.Errorf("anthropic upstream status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	if resp.StatusCode >= 400 {
		copyResponseHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, copyErr := io.Copy(w, resp.Body)
		return false, OpenAIUsage{}, resp.StatusCode, copyErr
	}

	if payload.Stream {
		usage, err := anthropicStreamToOpenAI(w, resp, req.Model)
		return false, usage, resp.StatusCode, err
	}
	usage, err := anthropicJSONToOpenAI(w, resp, req.Model)
	return false, usage, resp.StatusCode, err
}

func anthropicJSONToOpenAI(w http.ResponseWriter, resp *http.Response, model string) (OpenAIUsage, error) {
	var upstream AnthropicMessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&upstream); err != nil {
		return OpenAIUsage{}, err
	}

	text := anthropicText(upstream.Content)
	out := OpenAIChatCompletionResponse{
		ID:      upstream.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   chooseModel(model, upstream.Model),
		Choices: []OpenAIChoice{
			{
				Index: 0,
				Message: &OpenAIResponseMessage{
					Role:    "assistant",
					Content: text,
				},
				FinishReason: mapFinishReason(upstream.StopReason),
			},
		},
		Usage: &OpenAIUsage{
			PromptTokens:     upstream.Usage.InputTokens,
			CompletionTokens: upstream.Usage.OutputTokens,
			TotalTokens:      upstream.Usage.InputTokens + upstream.Usage.OutputTokens,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	return *out.Usage, json.NewEncoder(w).Encode(out)
}

func anthropicStreamToOpenAI(w http.ResponseWriter, resp *http.Response, model string) (OpenAIUsage, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return OpenAIUsage{}, fmt.Errorf("streaming is not supported by response writer")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	reader := bufio.NewReader(resp.Body)
	var chunkID string
	var chunkModel string
	roleSent := false
	usage := OpenAIUsage{}

	for {
		eventName, data, err := readSSEEvent(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return usage, err
		}
		if len(data) == 0 {
			continue
		}
		if string(data) == "[DONE]" {
			break
		}

		var evt AnthropicStreamEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			continue
		}
		if eventName == "ping" || evt.Type == "ping" {
			continue
		}

		switch evt.Type {
		case "message_start":
			chunkID = evt.Message.ID
			chunkModel = chooseModel(model, evt.Message.Model)
			if err := writeOpenAIChunk(w, chunkID, chunkModel, OpenAIChoice{
				Index: 0,
				Delta: &OpenAIResponseMessage{Role: "assistant"},
			}); err != nil {
				return usage, err
			}
			flusher.Flush()
			roleSent = true
			usage.PromptTokens = evt.Usage.InputTokens
		case "content_block_start":
			if !roleSent {
				chunkID = defaultChunkID(chunkID)
				chunkModel = chooseModel(model, chunkModel)
				if err := writeOpenAIChunk(w, chunkID, chunkModel, OpenAIChoice{
					Index: 0,
					Delta: &OpenAIResponseMessage{Role: "assistant"},
				}); err != nil {
					return usage, err
				}
				roleSent = true
			}
			if evt.ContentBlock.Type == "text" && evt.ContentBlock.Text != "" {
				if err := writeOpenAIChunk(w, defaultChunkID(chunkID), chooseModel(model, chunkModel), OpenAIChoice{
					Index: 0,
					Delta: &OpenAIResponseMessage{Content: evt.ContentBlock.Text},
				}); err != nil {
					return usage, err
				}
				flusher.Flush()
			}
		case "content_block_delta":
			if evt.Delta.Type == "text_delta" && evt.Delta.Text != "" {
				if err := writeOpenAIChunk(w, defaultChunkID(chunkID), chooseModel(model, chunkModel), OpenAIChoice{
					Index: 0,
					Delta: &OpenAIResponseMessage{Content: evt.Delta.Text},
				}); err != nil {
					return usage, err
				}
				flusher.Flush()
			}
			if evt.Usage.OutputTokens > 0 {
				usage.CompletionTokens = evt.Usage.OutputTokens
			}
		case "message_delta":
			if evt.Delta.StopReason != "" {
				if err := writeOpenAIChunk(w, defaultChunkID(chunkID), chooseModel(model, chunkModel), OpenAIChoice{
					Index:        0,
					Delta:        &OpenAIResponseMessage{},
					FinishReason: mapFinishReason(evt.Delta.StopReason),
				}); err != nil {
					return usage, err
				}
				flusher.Flush()
			}
			if evt.Usage.OutputTokens > 0 {
				usage.CompletionTokens = evt.Usage.OutputTokens
			}
		case "message_stop":
			if _, err := io.WriteString(w, "data: [DONE]\n\n"); err != nil {
				return usage, err
			}
			flusher.Flush()
			return usage, nil
		}
	}

	_, err := io.WriteString(w, "data: [DONE]\n\n")
	flusher.Flush()
	return usage, err
}

func writeOpenAIChunk(w io.Writer, id, model string, choice OpenAIChoice) error {
	payload := OpenAIChatCompletionResponse{
		ID:      defaultChunkID(id),
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []OpenAIChoice{choice},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", raw)
	return err
}

func toAnthropicRequest(req ChatCompletionRequest) (AnthropicMessageRequest, error) {
	out := AnthropicMessageRequest{
		Model:       normalizeModel(req.Model),
		Messages:    make([]AnthropicMessage, 0, len(req.Messages)),
		MaxTokens:   chooseMaxTokens(req),
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
	}

	switch v := req.Stop.(type) {
	case string:
		if v != "" {
			out.StopSequences = []string{v}
		}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out.StopSequences = append(out.StopSequences, s)
			}
		}
	}

	var systemParts []string
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			text, err := openAIContentToText(msg.Content)
			if err != nil {
				return AnthropicMessageRequest{}, err
			}
			if strings.TrimSpace(text) != "" {
				systemParts = append(systemParts, text)
			}
			continue
		}

		blocks, err := openAIContentToAnthropicBlocks(msg.Content)
		if err != nil {
			return AnthropicMessageRequest{}, err
		}
		role := msg.Role
		if role != "assistant" {
			role = "user"
		}
		out.Messages = append(out.Messages, AnthropicMessage{
			Role:    role,
			Content: blocks,
		})
	}
	out.System = strings.Join(systemParts, "\n\n")
	if len(out.Messages) == 0 {
		return AnthropicMessageRequest{}, fmt.Errorf("anthropic upstream requires at least one non-system message")
	}
	return out, nil
}

func openAIContentToAnthropicBlocks(raw json.RawMessage) ([]AnthropicContentBlock, error) {
	text, err := openAIContentToText(raw)
	if err != nil {
		return nil, err
	}
	return []AnthropicContentBlock{{Type: "text", Text: text}}, nil
}

func openAIContentToText(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString, nil
	}

	var parts []map[string]any
	if err := json.Unmarshal(raw, &parts); err == nil {
		var chunks []string
		for _, part := range parts {
			t, _ := part["type"].(string)
			switch t {
			case "text", "input_text":
				if txt, ok := part["text"].(string); ok {
					chunks = append(chunks, txt)
				}
			case "image_url", "input_image":
				chunks = append(chunks, "[image omitted]")
			}
		}
		return strings.Join(chunks, "\n"), nil
	}

	return "", fmt.Errorf("unsupported message content format")
}

func anthropicText(blocks []AnthropicContentBlock) string {
	var chunks []string
	for _, block := range blocks {
		if block.Type == "text" {
			chunks = append(chunks, block.Text)
		}
	}
	return strings.Join(chunks, "")
}

func chooseMaxTokens(req ChatCompletionRequest) int {
	if req.MaxCompletionTokens != nil && *req.MaxCompletionTokens > 0 {
		return *req.MaxCompletionTokens
	}
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		return *req.MaxTokens
	}
	return 4096
}

func mapFinishReason(reason string) string {
	switch reason {
	case "end_turn", "stop_sequence":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	default:
		return reason
	}
}

func normalizeModel(model string) string {
	if model == "LongCat-Flash-Thinking" {
		return "LongCat-Flash-Thinking-2601"
	}
	return model
}

func chooseModel(preferred, fallback string) string {
	if preferred != "" {
		return preferred
	}
	return fallback
}

func defaultChunkID(id string) string {
	if id != "" {
		return id
	}
	return fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
}

func readSSEEvent(reader *bufio.Reader) (string, []byte, error) {
	var eventName string
	var dataLines []string

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && (eventName != "" || len(dataLines) > 0) {
				return eventName, []byte(strings.Join(dataLines, "\n")), nil
			}
			return "", nil, err
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			return eventName, []byte(strings.Join(dataLines, "\n")), nil
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
}

func healthzHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func authMiddleware(cfg Config, next http.Handler) http.Handler {
	allowed := allowedAPIKeys(cfg)
	if len(allowed) == 0 {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := extractClientAPIKey(r)
		if _, ok := allowed[key]; !ok {
			writeOpenAIError(w, http.StatusUnauthorized, "authentication_error", "invalid or missing api key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func extractClientAPIKey(r *http.Request) string {
	if cookie, err := r.Cookie(clientAuthCookieName); err == nil {
		if v := strings.TrimSpace(cookie.Value); v != "" {
			return v
		}
	}
	if v := strings.TrimSpace(r.Header.Get("X-API-Key")); v != "" {
		return v
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}

func allowedAPIKeys(cfg Config) map[string]struct{} {
	allowed := make(map[string]struct{}, len(cfg.APIKeys))
	for _, key := range cfg.APIKeys {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			allowed[trimmed] = struct{}{}
		}
	}
	return allowed
}

func loginHandler(cfg Config) http.HandlerFunc {
	allowed := allowedAPIKeys(cfg)

	type loginRequest struct {
		APIKey string `json:"api_key"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeOpenAIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		if len(allowed) == 0 {
			writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "api key auth is not enabled")
			return
		}

		var req loginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "request body is not valid JSON")
			return
		}
		key := strings.TrimSpace(req.APIKey)
		if _, ok := allowed[key]; !ok {
			writeOpenAIError(w, http.StatusUnauthorized, "authentication_error", "invalid or missing api key")
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     clientAuthCookieName,
			Value:    key,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   86400 * 30,
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
		})
	}
}

func logoutHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeOpenAIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     clientAuthCookieName,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   -1,
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
		})
	}
}

func modelsHandler(w http.ResponseWriter, _ *http.Request) {
	items := make([]OpenAIModelItem, 0, len(supportedModels))
	now := time.Now().Unix()
	for _, model := range supportedModels {
		items = append(items, OpenAIModelItem{
			ID:      model,
			Object:  "model",
			Created: now,
			OwnedBy: "longcat",
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(OpenAIModelsResponse{
		Object: "list",
		Data:   items,
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		appLogger.Infof("%s %s from=%s status=%d cost=%s", r.Method, r.URL.Path, r.RemoteAddr, rec.status, time.Since(start))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func writeOpenAIError(w http.ResponseWriter, status int, typ, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": msg,
			"type":    typ,
		},
	})
}

func copyResponseHeaders(dst, src http.Header) {
	for k, values := range src {
		if strings.EqualFold(k, "Content-Length") {
			continue
		}
		dst.Del(k)
		for _, v := range values {
			dst.Add(k, v)
		}
	}
}

func shouldRetryStatus(status int) bool {
	return status == http.StatusUnauthorized ||
		status == http.StatusForbidden ||
		status == http.StatusTooManyRequests ||
		status >= http.StatusInternalServerError
}

func getenv(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	if (key == "PORT" || key == "ADDR") && !strings.HasPrefix(v, ":") {
		return ":" + v
	}
	return v
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	if secs, err := strconv.Atoi(raw); err == nil {
		return time.Duration(secs) * time.Second
	}
	if d, err := time.ParseDuration(raw); err == nil {
		return d
	}
	return fallback
}
