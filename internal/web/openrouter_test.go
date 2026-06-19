package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vladtrc/pdtt/internal/config"
)

func TestOpenRouterGeneratorRepairsValidationErrors(t *testing.T) {
	calls := 0
	recorder := &recordingGenerationAttempts{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("missing auth header")
		}
		var req openRouterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		calls++
		content := "scene bad\n\n| 1s\n"
		if calls == 2 {
			if len(req.Messages) < 4 || !strings.Contains(req.Messages[len(req.Messages)-1].Content, "failed validation") {
				t.Fatalf("repair request should include validation error: %#v", req.Messages)
			}
			content = "```pdtt\nscene ok\n\n| 1s\n```"
		}
		_ = json.NewEncoder(w).Encode(openRouterResponse{
			Choices: []struct {
				Message openRouterMessage `json:"message"`
			}{
				{Message: openRouterMessage{Role: "assistant", Content: content}},
			},
		})
	}))
	defer ts.Close()

	g := &openRouterGenerator{
		cfg:    config.OpenRouter{Key: "test-key", BaseURL: ts.URL, Model: "test-model"},
		client: ts.Client(),
		rules:  "rules",
		validator: func(scene string) error {
			if strings.Contains(scene, "scene ok") {
				return nil
			}
			return errors.New("line 1: invalid scene")
		},
	}

	var statuses []string
	ctx := withGenerationLog(context.Background(), "gen1", recorder)
	scene, err := g.Generate(ctx, "draw", func(stage, detail string) {
		statuses = append(statuses, stage+": "+detail)
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if scene != "scene ok\n\n| 1s" {
		t.Fatalf("scene = %q", scene)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if len(statuses) == 0 || !strings.Contains(strings.Join(statuses, "\n"), "calling OpenRouter") {
		t.Fatalf("expected generation status reports, got %#v", statuses)
	}
	if len(recorder.attempts) != 2 {
		t.Fatalf("logged attempts = %d, want 2", len(recorder.attempts))
	}
	if !strings.Contains(recorder.attempts[0].validationError, "invalid scene") {
		t.Fatalf("first attempt should log validation error: %#v", recorder.attempts[0])
	}
	if !strings.Contains(recorder.attempts[1].responseContent, "scene ok") {
		t.Fatalf("second attempt should log raw response: %#v", recorder.attempts[1])
	}
	if recorder.attempts[1].extractedScene != "scene ok\n\n| 1s" {
		t.Fatalf("second attempt extracted scene = %q", recorder.attempts[1].extractedScene)
	}
}

func TestOpenRouterGeneratorStopsAfterOneRepair(t *testing.T) {
	calls := 0
	recorder := &recordingGenerationAttempts{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(openRouterResponse{
			Choices: []struct {
				Message openRouterMessage `json:"message"`
			}{
				{Message: openRouterMessage{Role: "assistant", Content: "scene still bad\n\n| 1s\n"}},
			},
		})
	}))
	defer ts.Close()

	g := &openRouterGenerator{
		cfg:    config.OpenRouter{Key: "test-key", BaseURL: ts.URL, Model: "test-model"},
		client: ts.Client(),
		rules:  "rules",
		validator: func(scene string) error {
			return errors.New("line 1: invalid scene")
		},
	}

	ctx := withGenerationLog(context.Background(), "gen1", recorder)
	_, err := g.Generate(ctx, "draw", nil)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if calls != maxOpenRouterAttempts {
		t.Fatalf("calls = %d, want %d", calls, maxOpenRouterAttempts)
	}
	if len(recorder.attempts) != maxOpenRouterAttempts {
		t.Fatalf("logged attempts = %d, want %d", len(recorder.attempts), maxOpenRouterAttempts)
	}
	if recorder.attempts[len(recorder.attempts)-1].attempt != maxOpenRouterAttempts {
		t.Fatalf("last logged attempt = %d, want %d", recorder.attempts[len(recorder.attempts)-1].attempt, maxOpenRouterAttempts)
	}
}

func TestLoadPDTTLLMDocsUsesEmbeddedFullDocsAndWebAddendum(t *testing.T) {
	docs := loadPDTTLLMDocs(config.OpenRouter{})
	for _, want := range []string{
		"# PDTT full LLM reference",
		"## Web Playground Harness",
		"## Web Playground Style Guide",
		"Title/narration text: 0.48..0.70",
		"Formulas: 0.65..0.95",
		"Labels near points: 0.28..0.42",
	} {
		if !strings.Contains(docs, want) {
			t.Fatalf("docs missing %q:\n%s", want, docs)
		}
	}
}

func TestLoadPDTTLLMDocsFallsBackToEmbeddedWhenOverrideMissing(t *testing.T) {
	docs := loadPDTTLLMDocs(config.OpenRouter{RulesPath: filepath.Join(t.TempDir(), "missing.md")})
	if !strings.Contains(docs, "# PDTT full LLM reference") {
		t.Fatalf("docs should use embedded full reference, got:\n%s", docs)
	}
	if strings.Contains(docs, "Critical syntax rules:") {
		t.Fatalf("docs should not use old short fallback:\n%s", docs)
	}
}

func TestLoadPDTTLLMDocsAllowsFileOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.md")
	if err := os.WriteFile(path, []byte("custom pdtt rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	docs := loadPDTTLLMDocs(config.OpenRouter{RulesPath: path})
	if !strings.Contains(docs, "custom pdtt rules") {
		t.Fatalf("docs should include override file:\n%s", docs)
	}
	if !strings.Contains(docs, "## Web Playground Harness") || !strings.Contains(docs, "## Web Playground Style Guide") {
		t.Fatalf("docs should still include web docs:\n%s", docs)
	}
}

type recordedGenerationAttempt struct {
	attempt         int
	requestMessages string
	responseContent string
	extractedScene  string
	validationError string
	openRouterError string
}

type recordingGenerationAttempts struct {
	attempts []recordedGenerationAttempt
}

func (r *recordingGenerationAttempts) RecordGenerationAttempt(ctx context.Context, generationID string, attempt int, requestMessages, responseContent, extractedScene, validationError, openRouterError string) error {
	r.attempts = append(r.attempts, recordedGenerationAttempt{
		attempt:         attempt,
		requestMessages: requestMessages,
		responseContent: responseContent,
		extractedScene:  extractedScene,
		validationError: validationError,
		openRouterError: openRouterError,
	})
	return nil
}

func TestExtractPDTT(t *testing.T) {
	got := extractPDTT("text before\n```pdtt\nscene demo\n\n| 1s\n```\ntext after")
	if got != "scene demo\n\n| 1s" {
		t.Fatalf("extractPDTT = %q", got)
	}
}
