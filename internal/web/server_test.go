package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vladtrc/pdtt/internal/config"
	"github.com/vladtrc/pdtt/pkg/render"
)

func testRenderServer(t *testing.T, maxSceneBytes int64) *Server {
	t.Helper()
	if maxSceneBytes == 0 {
		maxSceneBytes = 1024
	}
	dataDir := t.TempDir()
	srv := &Server{
		cfg: &config.Config{
			DataDir:       dataDir,
			RenderTimeout: 3 * time.Minute,
			MaxSceneBytes: maxSceneBytes,
			Retention:     time.Hour,
			CleanupEvery:  time.Hour,
		},
		renderScene: func(scene, workDir string) (*render.Result, error) {
			framesDir := filepath.Join(workDir, "frames")
			if err := os.MkdirAll(framesDir, 0o755); err != nil {
				return nil, err
			}
			return &render.Result{FramesDir: framesDir}, nil
		},
		encodeVideo: func(_ string, _ float64, _ int, destPath string) (int64, error) {
			if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
				return 0, err
			}
			if err := os.WriteFile(destPath, []byte("mp4"), 0o644); err != nil {
				return 0, err
			}
			return 3, nil
		},
	}
	return srv
}

func postRender(t *testing.T, body string) (*http.Request, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/render", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	return req, rec
}

func TestHandleRenderSemanticErrorsReturn422(t *testing.T) {
	srv := testRenderServer(t, 64)

	req, rec := postRender(t, "scene=")
	srv.handleRender(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty scene status = %d, want 422", rec.Code)
	}

	body := "scene=" + strings.Repeat("x", 128)
	req, rec = postRender(t, body)
	srv.handleRender(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("oversized scene status = %d, want 422", rec.Code)
	}
}

func TestHandleRenderErrorCardEscapesCompilerOutput(t *testing.T) {
	srv := testRenderServer(t, 1024)
	srv.renderScene = func(string, string) (*render.Result, error) {
		return nil, errors.New("<bad & failed>")
	}

	req, rec := postRender(t, "scene=ok")
	srv.handleRender(rec, req)
	body := rec.Body.String()

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if !strings.Contains(body, "Compilation failed") {
		t.Fatalf("body should include render error title: %s", body)
	}
	if !strings.Contains(body, `data-copy-source="error"`) {
		t.Fatalf("body should include compiler-error copy source: %s", body)
	}
	if !strings.Contains(body, "&lt;bad &amp; failed&gt;") {
		t.Fatalf("body should escape compiler output: %s", body)
	}
	if strings.Contains(body, "<bad & failed>") {
		t.Fatalf("body should not include raw compiler output: %s", body)
	}
	if !strings.Contains(body, "copy LLM docs") {
		t.Fatalf("body should include docs copy action: %s", body)
	}
}

func TestHandleRenderMaxBytesReturns413(t *testing.T) {
	srv := testRenderServer(t, 32)
	// Exceed MaxBytesReader limit (max_scene_bytes + 4096), not just max_scene_bytes.
	body := "scene=" + strings.Repeat("a", 5000)
	req, rec := postRender(t, body)
	srv.handleRender(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestHandleRenderReturnsVideoFragment(t *testing.T) {
	srv := testRenderServer(t, 1024)

	req, rec := postRender(t, "scene=ok")
	srv.handleRender(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `data-player`) {
		t.Fatalf("body should include the frame player: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `/dbg/`) {
		t.Fatalf("body should include frame URLs: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `/video/`) {
		t.Fatalf("body should include save-mp4 URL: %s", rec.Body.String())
	}
}

func TestHandleGenerateReturnsSceneWithoutAutoRender(t *testing.T) {
	srv := testRenderServer(t, 1024)
	srv.generateScene = func(ctx context.Context, prompt string, report generateReporter) (string, error) {
		if prompt != "draw parabola" {
			t.Fatalf("prompt = %q", prompt)
		}
		report("calling OpenRouter", "Sending request to test model")
		return "scene generated\n\n| 1s\n", nil
	}

	req := httptest.NewRequest(http.MethodPost, "/generate", strings.NewReader("prompt=draw+parabola"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	srv.handleGenerate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "scene generated") {
		t.Fatalf("body should include generated scene: %s", body)
	}
	if !strings.Contains(body, `pdttSetScene`) {
		t.Fatalf("body should update editor: %s", body)
	}
	if strings.Contains(body, `requestSubmit`) {
		t.Fatalf("body should not auto-submit render: %s", body)
	}
}

func TestHandleGenerateErrors(t *testing.T) {
	srv := testRenderServer(t, 1024)
	srv.generateScene = func(context.Context, string, generateReporter) (string, error) {
		return "", errors.New("model failed")
	}

	req := httptest.NewRequest(http.MethodPost, "/generate", strings.NewReader("prompt=draw"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	srv.handleGenerate(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "model failed") {
		t.Fatalf("body should include error: %s", rec.Body.String())
	}
}

func TestHandleRenderTimeoutReturns422(t *testing.T) {
	srv := testRenderServer(t, 1024)
	srv.cfg.RenderTimeout = 20 * time.Millisecond
	started := make(chan struct{})
	renderFinished := make(chan struct{})
	srv.renderScene = func(scene, workDir string) (*render.Result, error) {
		close(started)
		defer close(renderFinished)
		time.Sleep(60 * time.Millisecond)
		return &render.Result{FramesDir: workDir}, nil
	}

	req, rec := postRender(t, "scene=ok")
	done := make(chan struct{})
	start := time.Now()
	go func() {
		srv.handleRender(rec, req)
		close(done)
	}()
	<-started
	<-done
	if elapsed := time.Since(start); elapsed >= 50*time.Millisecond {
		t.Fatalf("timeout response took %s, want before render function finished", elapsed)
	}

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "render timeout after 20ms") {
		t.Fatalf("body should include timeout: %s", rec.Body.String())
	}

	// Drain the orphaned render goroutine so its workdir cleanup runs.
	<-renderFinished
}

// Renders run concurrently: a second render started while a first is in flight
// must succeed rather than be rejected with 409.
func TestHandleRenderConcurrent(t *testing.T) {
	srv := testRenderServer(t, 1024)
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	srv.renderScene = func(scene, workDir string) (*render.Result, error) {
		// Only the first render blocks; later renders return immediately so the
		// second request can complete while the first is still in flight.
		first := false
		once.Do(func() {
			first = true
			close(started)
		})
		if first {
			<-release
		}
		framesDir := filepath.Join(workDir, "frames")
		if err := os.MkdirAll(framesDir, 0o755); err != nil {
			return nil, err
		}
		return &render.Result{FramesDir: framesDir}, nil
	}

	firstReq, firstRec := postRender(t, "scene=first")
	done := make(chan struct{})
	go func() {
		srv.handleRender(firstRec, firstReq)
		close(done)
	}()
	<-started

	secondReq, secondRec := postRender(t, "scene=second")
	srv.handleRender(secondRec, secondReq)
	if secondRec.Code != http.StatusOK {
		t.Fatalf("concurrent render status = %d, want 200: %s", secondRec.Code, secondRec.Body.String())
	}

	close(release)
	<-done
}

func TestHandleRenderRenderAndEncodeErrors(t *testing.T) {
	t.Run("render", func(t *testing.T) {
		srv := testRenderServer(t, 1024)
		srv.renderScene = func(string, string) (*render.Result, error) {
			return nil, errors.New("bad scene")
		}
		req, rec := postRender(t, "scene=ok")
		srv.handleRender(rec, req)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422", rec.Code)
		}
	})

	t.Run("encode", func(t *testing.T) {
		srv := testRenderServer(t, 1024)
		srv.encodeVideo = func(string, float64, int, string) (int64, error) {
			return 0, errors.New("ffmpeg failed")
		}
		req, rec := postRender(t, "scene=ok")
		srv.handleRender(rec, req)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422", rec.Code)
		}
	})
}

func TestServerCloseWaitsForBackgroundWorkers(t *testing.T) {
	s := &Server{}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel

	started := make(chan struct{})
	s.bgWG.Add(1)
	go func() {
		defer s.bgWG.Done()
		close(started)
		<-ctx.Done()
		time.Sleep(40 * time.Millisecond)
	}()

	<-started
	done := make(chan struct{})
	go func() {
		_ = s.Close(context.Background())
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("Close returned before background worker finished")
	case <-time.After(10 * time.Millisecond):
	}

	<-done
}

func TestServerCloseBoundedWait(t *testing.T) {
	s := &Server{}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel

	s.bgWG.Add(1)
	go func() {
		defer s.bgWG.Done()
		<-ctx.Done()
		time.Sleep(200 * time.Millisecond)
	}()

	closeCtx, closeCancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer closeCancel()

	err := s.Close(closeCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close err = %v, want deadline exceeded", err)
	}
}

func TestServerCloseIsIdempotent(t *testing.T) {
	s := &Server{}
	var once sync.Once
	s.bgWG.Add(1)
	go func() {
		defer s.bgWG.Done()
		once.Do(func() {})
	}()

	if err := s.Close(context.Background()); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestServerCloseNilContextWaits(t *testing.T) {
	s := &Server{}
	finished := make(chan struct{})
	s.bgWG.Add(1)
	go func() {
		defer s.bgWG.Done()
		time.Sleep(20 * time.Millisecond)
		close(finished)
	}()

	if err := s.Close(context.Background()); err != nil {
		t.Fatalf("Close(nil): %v", err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("worker did not finish before Close returned")
	}
}
