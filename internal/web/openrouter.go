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

	pdttmd "github.com/vladtrc/pdtt/md"
	"github.com/vladtrc/pdtt/pkg/render"
	"github.com/vladtrc/pdtt/internal/config"
)

const (
	maxOpenRouterAttempts = 2
	maxRepairAttempts     = maxOpenRouterAttempts - 1
	defaultMaxTokens      = 6000
	pdttWebHarnessDoc     = `## Web Playground Harness

The web playground renders square 640x640 MP4 videos. The default camera frame is about 14.2 x 8.0 world units, centered at [0, 0]. Keep important content inside roughly x = -6.7..6.7 and y = -3.6..3.6 unless you animate the built-in frame record.

plane/axes have a physical size of about 10 x 6 world units; x_range/y_range change data coordinates, not screen size. The video stays square; use the built-in frame record when a graph-heavy scene needs a tighter camera:

frame frame:
  at: [0, -0.25]
  w: 11.2

This makes a 10 x 6 graph use about 85-90% of the frame width. Put the graph around [0, -0.35] when also showing a title and a formula.

axes are physically 10 x 6, so equal data units need y_span about 0.6 * x_span. Example: x_range [-3, 3, 1] pairs with y_range [-1.8, 1.8, 0.6]. A much larger y_range compresses the curve vertically.`

	pdttWebStyleGuideDoc = `## Web Playground Style Guide

Use the square frame deliberately. For one main graph or diagram, do not leave a small object floating in the middle when there are no other large objects.

Layout suggestions:
- Titles: y = 2.8..3.1
- Main graph with title/formula: at = [0, -0.35]
- Bottom formulas/captions: y = -3.0..-2.6

Convenient scale ranges:
- Title/narration text: 0.48..0.70
- Formulas: 0.65..0.95
- Labels near points: 0.28..0.42
- Small tick/axis labels: 0.24..0.34

Timing rules for readable generated scenes:
- Show one explanatory text card at a time.
- After showing prose text, hold 1.2s..2.8s depending on length.
- Fade old text out before showing the next text card.
- Parallelize related geometry animation, but keep text beats sequential.
- End with a 1.0s..1.8s hold or a gentle fade-out.`
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
		base = "Write valid PDTT source only. No Markdown fences or explanations."
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
	return strings.TrimRight(base, "\n") + "\n\n" + pdttWebHarnessDoc + "\n\n" + pdttWebStyleGuideDoc
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
				"The PDTT scene failed validation without rendering:\n%s\n\nReturn a corrected full PDTT scene only. Keep the user's original intent. No Markdown fences. Never use braces, JSON-like blocks, camera fields, tuple coordinates, or \"time ... ->\" rows; use indented PDTT records and pipe animation rows.",
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
		"\n\nOutput contract: return one complete PDTT scene only. No Markdown, no code fences, no comments outside the code."
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
	defer resp.Body.Close()

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
