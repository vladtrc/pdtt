package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/vladtrc/pdtt/internal/config"
	pdttmd "github.com/vladtrc/pdtt/md"
	"github.com/vladtrc/pdtt/pkg/render"
)

const (
	maxOpenRouterAttempts = 2
	maxRepairAttempts     = maxOpenRouterAttempts - 1
	defaultMaxTokens      = 6000
)

type generateReporter func(stage, detail string)

type sceneGenerator func(context.Context, string, generateReporter) (string, error)

type generationAttemptRecorder interface {
	RecordGenerationAttempt(ctx context.Context, generationID string, attempt int, requestMessages, responseContent, extractedScene, validationError, openRouterError string) error
}

type generationLogContext struct {
	id       string
	recorder generationAttemptRecorder
}

type generationLogContextKey struct{}

func withGenerationLog(ctx context.Context, id string, recorder generationAttemptRecorder) context.Context {
	if id == "" || recorder == nil {
		return ctx
	}
	return context.WithValue(ctx, generationLogContextKey{}, generationLogContext{id: id, recorder: recorder})
}

func generationLogFromContext(ctx context.Context) (generationLogContext, bool) {
	logCtx, ok := ctx.Value(generationLogContextKey{}).(generationLogContext)
	return logCtx, ok && logCtx.id != "" && logCtx.recorder != nil
}

type openRouterGenerator struct {
	cfg       config.OpenRouter
	client    *http.Client
	rules     string
	validator func(string) error
}

func newOpenRouterGenerator(cfg config.OpenRouter) sceneGenerator {
	g := &openRouterGenerator{
		cfg:       cfg,
		client:    &http.Client{Timeout: cfg.Timeout},
		rules:     loadPDTTLLMDocs(cfg),
		validator: render.Validate,
	}
	return g.Generate
}

func loadPDTTLLMDocs(cfg config.OpenRouter) string {
	base := strings.TrimSpace(pdttmd.Full)
	if base == "" {
		base = "Write valid PDTT source only, wrapped in a single ```pdtt code fence and nothing else."
	}
	if path := strings.TrimSpace(cfg.RulesPath); path != "" {
		if data, err := os.ReadFile(path); err == nil && len(bytes.TrimSpace(data)) > 0 {
			base = strings.TrimSpace(string(data))
		} else if err != nil {
			log.Printf("openrouter rules: could not read %s, using embedded pdtt/md/full.md: %v", path, err)
		} else {
			log.Printf("openrouter rules: %s is empty, using embedded pdtt/md/full.md", path)
		}
	}
	return strings.TrimRight(base, "\n") + "\n\n" + strings.TrimSpace(pdttmd.WebHarness)
}

func (g *openRouterGenerator) Generate(ctx context.Context, prompt string, report generateReporter) (string, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "", errors.New("prompt is required")
	}
	if strings.TrimSpace(g.cfg.Key) == "" {
		return "", errors.New("openrouter key is not configured")
	}

	messages := []openRouterMessage{
		{Role: "system", Content: g.systemPrompt()},
		{Role: "user", Content: "User request:\n" + prompt},
	}

	var lastScene string
	var lastErr error
	for attempt := 1; attempt <= maxOpenRouterAttempts; attempt++ {
		reportGenerateStatus(report, "calling OpenRouter", fmt.Sprintf("Sending request to %s (attempt %d/%d)", modelLabel(g.cfg.Model), attempt, maxOpenRouterAttempts))
		content, err := g.chat(ctx, messages)
		if err != nil {
			g.recordAttempt(ctx, attempt, messages, "", "", nil, err)
			return "", err
		}
		reportGenerateStatus(report, "reading LLM response", "OpenRouter returned a response. Extracting PDTT source.")
		scene := extractPDTT(content)
		lastScene = scene
		reportGenerateStatus(report, "validating PDTT", "Checking generated PDTT before inserting it into the editor.")
		if err := g.validator(scene); err == nil {
			g.recordAttempt(ctx, attempt, messages, content, scene, nil, nil)
			return scene, nil
		} else {
			lastErr = err
			g.recordAttempt(ctx, attempt, messages, content, scene, err, nil)
		}
		if attempt == maxOpenRouterAttempts {
			break
		}
		reportGenerateStatus(report, "repairing PDTT", "Validation failed. Asking OpenRouter to return a corrected scene.")
		messages = append(messages,
			openRouterMessage{Role: "assistant", Content: lastScene},
			openRouterMessage{Role: "user", Content: fmt.Sprintf(
				"The PDTT scene failed validation without rendering:\n%s\n\nReturn a corrected full PDTT scene, wrapped in a single ```pdtt code fence and nothing else. Keep the user's original intent. Never use braces, JSON-like blocks, camera fields, tuple coordinates, or \"time ... ->\" rows; use indented PDTT records and pipe animation rows.",
				lastErr.Error(),
			)},
		)
	}

	return "", fmt.Errorf("generated scene did not validate after %d repair attempts: %w\n\nlast scene:\n%s", maxRepairAttempts, lastErr, lastScene)
}

func (g *openRouterGenerator) recordAttempt(ctx context.Context, attempt int, messages []openRouterMessage, responseContent, extractedScene string, validationErr, openRouterErr error) {
	logCtx, ok := generationLogFromContext(ctx)
	if !ok {
		return
	}
	data, err := json.Marshal(messages)
	if err != nil {
		log.Printf("generation log %s attempt %d: marshal messages: %v", logCtx.id, attempt, err)
		return
	}
	if err := logCtx.recorder.RecordGenerationAttempt(ctx, logCtx.id, attempt, string(data), responseContent, extractedScene, errorString(validationErr), errorString(openRouterErr)); err != nil {
		log.Printf("generation log %s attempt %d: %v", logCtx.id, attempt, err)
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func reportGenerateStatus(report generateReporter, stage, detail string) {
	if report != nil {
		report(stage, detail)
	}
}

func modelLabel(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return "the configured model"
	}
	return model
}

func (g *openRouterGenerator) systemPrompt() string {
	return "You generate concise, readable PDTT animation scenes.\n\n" +
		g.rules +
		"\n\nOutput contract: return exactly one complete PDTT scene, wrapped in a single ```pdtt code fence and nothing else. The fence is required: PDTT is whitespace-sensitive (indented records, leading spaces in pipe rows) and the fence preserves indentation and spaces that would otherwise be lost. Put no prose, no extra fences, and no comments outside the code."
}

func (g *openRouterGenerator) chat(ctx context.Context, messages []openRouterMessage) (string, error) {
	baseURL := strings.TrimRight(g.cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}
	reqBody := openRouterRequest{
		Model:       g.cfg.Model,
		Messages:    messages,
		Temperature: 0.25,
		MaxTokens:   defaultMaxTokens,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+g.cfg.Key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://pdtt.local")
	req.Header.Set("X-Title", "pdtt playground")

	resp, err := g.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("openrouter %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var out openRouterResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("parse openrouter response: %w", err)
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", errors.New("openrouter returned an empty response")
	}
	return out.Choices[0].Message.Content, nil
}

func extractPDTT(content string) string {
	s := strings.TrimSpace(content)
	if i := strings.Index(s, "```"); i >= 0 {
		rest := s[i+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		}
		if j := strings.Index(rest, "```"); j >= 0 {
			s = strings.TrimSpace(rest[:j])
		}
	}
	if i := strings.Index(s, "scene "); i > 0 {
		s = s[i:]
	}
	return strings.TrimSpace(s)
}

type openRouterRequest struct {
	Model       string              `json:"model"`
	Messages    []openRouterMessage `json:"messages"`
	Temperature float64             `json:"temperature"`
	MaxTokens   int                 `json:"max_tokens"`
}

type openRouterMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openRouterResponse struct {
	Choices []struct {
		Message openRouterMessage `json:"message"`
	} `json:"choices"`
}
