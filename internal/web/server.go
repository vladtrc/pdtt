package web

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/vladtrc/pdtt/pkg/render"

	"github.com/vladtrc/pdtt/internal/config"
	"github.com/vladtrc/pdtt/internal/jobs"
)

const (
	renderFPS             = 12.0
	renderSize            = 640
	defaultRenderTimeout  = 3 * time.Minute
	renderTimeoutTemplate = "render timeout after %s (render.Scene is not cancellable; waited for goroutine to finish)"
)

type Server struct {
	cfg         *config.Config
	store       *jobs.Store
	db          *sql.DB
	cancel      context.CancelFunc
	closeOnce   sync.Once
	closeErr    error
	bgWG        sync.WaitGroup
	renderMu    sync.Mutex
	statusMu    sync.RWMutex
	genStatusMu sync.RWMutex

	renderStage    string
	renderDetail   string
	renderStarted  time.Time
	generateStage  string
	generateDetail string

	renderScene   func(scene, workDir string) (*render.Result, error)
	encodeVideo   func(framesDir string, fps float64, size int, destPath string) (int64, error)
	generateScene sceneGenerator
}

func NewServer(cfg *config.Config) (*Server, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data_dir: %w", err)
	}
	for _, sub := range []string{"videos", "work"} {
		if err := os.MkdirAll(filepath.Join(cfg.DataDir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("create %s dir: %w", sub, err)
		}
	}

	db, err := sql.Open("mysql", cfg.MySQL.DSN)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	store, err := jobs.NewStore(db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	s := &Server{
		cfg:   cfg,
		store: store,
		db:    db,
		renderScene: func(scene, workDir string) (*render.Result, error) {
			return render.Scene(scene, workDir, render.Config{
				FPS:    renderFPS,
				Width:  renderSize,
				Height: renderSize,
			})
		},
		encodeVideo: func(framesDir string, fps float64, _ int, destPath string) (int64, error) {
			return EncodeFramesToMP4(framesDir, fps, destPath)
		},
		generateScene: newOpenRouterGenerator(cfg.OpenRouter),
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	cleaner := jobs.NewCleaner(store, cfg)
	s.bgWG.Add(1)
	go func() {
		defer s.bgWG.Done()
		cleaner.Run(ctx)
	}()

	return s, nil
}

// Close initiates graceful shutdown of background cleanup.
//
// In-flight renders are request-scoped and are not interrupted by Close because
// render.Scene is not cancellable. Safe to call multiple times; only the first
// call runs shutdown and later calls return the same result without blocking.
func (s *Server) Close(ctx context.Context) error {
	s.closeOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		waitDone := make(chan struct{})
		go func() {
			s.bgWG.Wait()
			close(waitDone)
		}()
		if ctx == nil {
			ctx = context.Background()
		}
		select {
		case <-waitDone:
		case <-ctx.Done():
			s.closeErr = ctx.Err()
		}
		if s.db != nil {
			if err := s.db.Close(); err != nil && s.closeErr == nil {
				s.closeErr = err
			}
		}
	})
	return s.closeErr
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("GET /render-status", s.handlePublicRenderStatus)
	mux.HandleFunc("GET /generate-status", s.handlePublicGenerateStatus)
	mux.HandleFunc("POST /generate", s.handleGenerate)
	mux.HandleFunc("POST /render", s.handleRender)
	mux.HandleFunc("GET /video/{id}", s.handleVideo)
	mux.HandleFunc("GET /admin/{secret}", s.handleAdmin)
	mux.HandleFunc("GET /admin/{secret}/renders", s.handleAdminRenders)
	mux.HandleFunc("GET /admin/{secret}/generations", s.handleAdminGenerations)
	mux.HandleFunc("GET /admin/{secret}/external", s.handleAdminExternal)
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := IndexPage(s.currentRenderStatus(), s.pdttLLMDocs()).Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handlePublicRenderStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := PublicRenderStatus(s.currentRenderStatus()).Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handlePublicGenerateStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := PublicGenerateStatus(s.currentGenerateStatus()).Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	if err := r.ParseForm(); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeGenerateError(w, http.StatusRequestEntityTooLarge, "request too large")
			return
		}
		writeGenerateError(w, http.StatusBadRequest, "invalid request")
		return
	}

	prompt := strings.TrimSpace(r.FormValue("prompt"))
	if prompt == "" {
		writeGenerateError(w, http.StatusUnprocessableEntity, "prompt is required")
		return
	}
	if s.generateScene == nil {
		writeGenerateError(w, http.StatusInternalServerError, "scene generator is not configured")
		return
	}

	s.setGenerateStatus("preparing request", "Preparing prompt and PDTT rules for OpenRouter.")
	defer s.clearGenerateStatus()

	genID := ""
	genCtx := r.Context()
	if s.store != nil {
		var err error
		genID, err = s.store.CreateGeneration(r.Context(), prompt, s.cfg.OpenRouter.Model)
		if err != nil {
			s.writeLLMFailure(w, r.Context(), http.StatusInternalServerError, prompt, "", "generation setup failed")
			return
		}
		genCtx = withGenerationLog(r.Context(), genID, s.store)
	}

	scene, err := s.generateScene(genCtx, prompt, s.setGenerateStatus)
	if err != nil {
		s.markGenerationFailed(r.Context(), genID, err.Error())
		s.writeLLMFailure(w, r.Context(), http.StatusUnprocessableEntity, prompt, genID, err.Error())
		return
	}
	if int64(len(scene)) > s.cfg.MaxSceneBytes {
		msg := fmt.Sprintf("generated scene exceeds max size (%d bytes)", s.cfg.MaxSceneBytes)
		s.markGenerationFailed(r.Context(), genID, msg)
		s.writeLLMFailure(w, r.Context(), http.StatusUnprocessableEntity, prompt, genID, msg)
		return
	}
	s.markGenerationCompleted(r.Context(), genID, scene)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<textarea id="generated-scene" class="hidden">%s</textarea>
<script>
(() => {
  const generated = document.getElementById("generated-scene");
  const editor = document.getElementById("scene-input");
  if (generated && editor) editor.value = generated.value;
})();
</script>`, html.EscapeString(scene))
}

func (s *Server) handleRender(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxSceneBytes+4096)
	if err := r.ParseForm(); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			s.writeRenderError(w, http.StatusRequestEntityTooLarge, "request too large")
			return
		}
		s.writeRenderError(w, http.StatusBadRequest, "invalid request")
		return
	}

	scene := r.FormValue("scene")
	if scene == "" {
		s.writeRenderError(w, http.StatusUnprocessableEntity, "scene is required")
		return
	}
	if int64(len(scene)) > s.cfg.MaxSceneBytes {
		s.writeRenderError(w, http.StatusUnprocessableEntity, fmt.Sprintf("scene exceeds max size (%d bytes)", s.cfg.MaxSceneBytes))
		return
	}

	if !s.renderMu.TryLock() {
		s.logRejected(r.Context(), scene, "server is busy, try again later")
		s.writeRenderError(w, http.StatusConflict, "server is busy, try again later")
		return
	}
	var releaseOnce sync.Once
	releaseRender := func() {
		releaseOnce.Do(func() {
			s.clearRenderStatus()
			s.renderMu.Unlock()
		})
	}
	releaseInHandler := true
	s.setRenderStatus("preparing", "Starting render")
	defer func() {
		if releaseInHandler {
			releaseRender()
		}
	}()

	job, err := s.createRenderLog(r.Context(), scene)
	if err != nil {
		s.writeRenderError(w, http.StatusInternalServerError, "render setup failed")
		return
	}

	s.setRenderStatus("preparing", "Preparing temporary workspace")
	workParent := filepath.Join(s.cfg.DataDir, "work")
	if err := os.MkdirAll(workParent, 0o755); err != nil {
		s.markRenderFailed(r.Context(), job.ID, "create work dir failed")
		s.writeRenderError(w, http.StatusInternalServerError, "create work dir failed")
		return
	}
	workDir, err := os.MkdirTemp(workParent, job.ID+"-*")
	if err != nil {
		s.markRenderFailed(r.Context(), job.ID, "create work dir failed")
		s.writeRenderError(w, http.StatusInternalServerError, "create work dir failed")
		return
	}
	cleanupWorkDirInHandler := true
	defer func() {
		if cleanupWorkDirInHandler {
			_ = os.RemoveAll(workDir)
		}
	}()

	s.setRenderStatus("rendering frames", "Generating animation frames")
	type renderResult struct {
		result *render.Result
		err    error
	}
	renderDone := make(chan renderResult, 1)
	go func() {
		result, err := s.renderScene(scene, workDir)
		renderDone <- renderResult{result: result, err: err}
	}()

	timeout := s.renderTimeout()
	timer := time.NewTimer(timeout)
	var result *render.Result
	var renderErr error
	select {
	case rr := <-renderDone:
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		result, renderErr = rr.result, rr.err
	case <-timer.C:
		msg := fmt.Sprintf(renderTimeoutTemplate, timeout)
		s.markRenderFailed(context.WithoutCancel(r.Context()), job.ID, msg)
		s.setRenderStatus("render timed out", "Waiting for renderer cleanup")
		cleanupWorkDirInHandler = false
		releaseInHandler = false
		go func() {
			<-renderDone
			_ = os.RemoveAll(workDir)
			releaseRender()
		}()
		s.writeRenderError(w, http.StatusUnprocessableEntity, msg)
		return
	}
	if renderErr != nil {
		s.markRenderFailed(r.Context(), job.ID, renderErr.Error())
		s.writeRenderError(w, http.StatusUnprocessableEntity, renderErr.Error())
		return
	}

	s.markRenderEncoding(r.Context(), job.ID)
	s.setRenderStatus("encoding video", "Encoding frames to MP4")
	videoPath := filepath.Join(s.cfg.DataDir, "videos", job.ID+".mp4")
	videoSize, err := s.encodeVideo(result.FramesDir, renderFPS, renderSize, videoPath)
	if err != nil {
		_ = os.Remove(videoPath)
		s.markRenderFailed(r.Context(), job.ID, "video encode: "+err.Error())
		s.writeRenderError(w, http.StatusUnprocessableEntity, "video encode: "+err.Error())
		return
	}
	s.markRenderCompleted(r.Context(), job.ID, videoPath, videoSize)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = RenderCompleted("/video/"+job.ID).Render(r.Context(), w)
}

func (s *Server) handleVideo(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !isVideoID(id) {
		http.NotFound(w, r)
		return
	}
	videoPath := filepath.Join(s.cfg.DataDir, "videos", id+".mp4")

	videoDir, err := filepath.Abs(filepath.Join(s.cfg.DataDir, "videos"))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	absPath, err := filepath.Abs(videoPath)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	rel, err := filepath.Rel(videoDir, absPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		http.NotFound(w, r)
		return
	}

	f, err := os.Open(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeContent(w, r, id+".mp4", info.ModTime(), f)
}

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	s.handleAdminRenders(w, r)
}

func (s *Server) handleAdminRenders(w http.ResponseWriter, r *http.Request) {
	secret, ok := s.adminSecretFromRequest(w, r)
	if !ok {
		return
	}

	list, err := s.listRenderLogs(r.Context(), 200)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	renderVersion := renderLogsVersion(list)
	if r.URL.Query().Get("render_version") == renderVersion {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	data := adminPageData{
		Secret:        secret,
		Active:        "renders",
		Busy:          s.isBusy(),
		Now:           time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		RenderVersion: renderVersion,
		Logs:          list,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := AdminPage(data).Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleAdminGenerations(w http.ResponseWriter, r *http.Request) {
	secret, ok := s.adminSecretFromRequest(w, r)
	if !ok {
		return
	}
	generations, err := s.listAllGenerationLogs(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	data := adminGenerationsPageData{
		Secret:      secret,
		Active:      "generations",
		Now:         time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Generations: generations,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := AdminGenerationsPage(data).Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleAdminExternal(w http.ResponseWriter, r *http.Request) {
	secret, ok := s.adminSecretFromRequest(w, r)
	if !ok {
		return
	}
	generations, err := s.listExternalGenerationLogs(r.Context(), 50)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	data := adminExternalPageData{
		Secret:      secret,
		Active:      "external",
		Now:         time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Generations: generations,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := AdminExternalPage(data).Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) adminSecretFromRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	secret := r.PathValue("secret")
	if secret != s.cfg.AdminSecret {
		http.NotFound(w, r)
		return "", false
	}
	return secret, true
}

type adminPageData struct {
	Secret        string
	Active        string
	Busy          bool
	Now           string
	RenderVersion string
	Logs          []adminRenderLog
}

type adminRenderLog struct {
	ID             string
	Status         string
	Stage          string
	CreatedAt      string
	StartedAt      string
	DoneAt         string
	Elapsed        string
	Duration       string
	VideoURL       string
	VideoSizeHuman string
	Error          string
}

type adminGenerationLog struct {
	ID              string
	Status          string
	Model           string
	CreatedAt       string
	DoneAt          string
	Duration        string
	Attempts        string
	Prompt          string
	PromptPreview   string
	PromptSize      string
	Scene           string
	ScenePreview    string
	SceneSize       string
	Error           string
	ValidationError string
	OpenRouterError string
}

type adminGenerationsPageData struct {
	Secret      string
	Active      string
	Now         string
	Generations []adminGenerationLog
}

type adminExternalPageData struct {
	Secret      string
	Active      string
	Now         string
	Generations []adminExternalGeneration
}

type adminExternalGeneration struct {
	ID                  string
	Status              string
	Model               string
	CreatedAt           string
	DoneAt              string
	Duration            string
	AttemptsCount       string
	Prompt              string
	Scene               string
	Error               string
	LastValidationError string
	LastOpenRouterError string
	Attempts            []adminGenerationAttempt
}

type adminGenerationAttempt struct {
	Attempt         string
	CreatedAt       string
	RequestMessages string
	ResponseContent string
	ExtractedScene  string
	ValidationError string
	OpenRouterError string
}

type renderStatusData struct {
	Busy    bool
	Stage   string
	Detail  string
	Elapsed string
}

type generateStatusData struct {
	Running bool
	Stage   string
	Detail  string
}

func (s *Server) isBusy() bool {
	if !s.renderMu.TryLock() {
		return true
	}
	s.renderMu.Unlock()
	return false
}

func (s *Server) currentRenderStatus() renderStatusData {
	if !s.isBusy() {
		return renderStatusData{
			Busy:   false,
			Stage:  "ready",
			Detail: "Ready for one render",
		}
	}
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()
	stage := s.renderStage
	if stage == "" {
		stage = "rendering"
	}
	detail := s.renderDetail
	if detail == "" {
		detail = "Render is running"
	}
	elapsed := ""
	if !s.renderStarted.IsZero() {
		elapsed = formatRenderElapsed(time.Since(s.renderStarted))
	}
	return renderStatusData{
		Busy:    true,
		Stage:   stage,
		Detail:  detail,
		Elapsed: elapsed,
	}
}

func (s *Server) setRenderStatus(stage, detail string) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	if s.renderStarted.IsZero() {
		s.renderStarted = time.Now()
	}
	s.renderStage = stage
	s.renderDetail = detail
}

func (s *Server) clearRenderStatus() {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	s.renderStage = ""
	s.renderDetail = ""
	s.renderStarted = time.Time{}
}

func (s *Server) currentGenerateStatus() generateStatusData {
	s.genStatusMu.RLock()
	defer s.genStatusMu.RUnlock()
	if s.generateStage == "" && s.generateDetail == "" {
		return generateStatusData{}
	}
	return generateStatusData{
		Running: true,
		Stage:   s.generateStage,
		Detail:  s.generateDetail,
	}
}

func (s *Server) setGenerateStatus(stage, detail string) {
	s.genStatusMu.Lock()
	defer s.genStatusMu.Unlock()
	s.generateStage = stage
	s.generateDetail = detail
}

func (s *Server) clearGenerateStatus() {
	s.genStatusMu.Lock()
	defer s.genStatusMu.Unlock()
	s.generateStage = ""
	s.generateDetail = ""
}

func alpineRenderPage(status renderStatusData) string {
	return "renderPage(" +
		strconv.FormatBool(status.Busy) + ", " +
		strconv.Quote(status.Stage) + ", " +
		strconv.Quote(status.Detail) + ", " +
		strconv.Quote(status.Elapsed) +
		")"
}

func alpineGenerateStatusInit(status generateStatusData) string {
	return "$dispatch('generation-status', { running: " +
		strconv.FormatBool(status.Running) +
		", stage: " + strconv.Quote(status.Stage) +
		", detail: " + strconv.Quote(status.Detail) +
		" })"
}

func alpineServerStatusInit(status renderStatusData) string {
	return "$dispatch('server-status', { busy: " +
		strconv.FormatBool(status.Busy) +
		", stage: " + strconv.Quote(status.Stage) +
		", detail: " + strconv.Quote(status.Detail) +
		", elapsed: " + strconv.Quote(status.Elapsed) +
		" })"
}

func (s *Server) renderTimeout() time.Duration {
	if s == nil || s.cfg == nil || s.cfg.RenderTimeout <= 0 {
		return defaultRenderTimeout
	}
	if s.cfg.RenderTimeout > defaultRenderTimeout {
		return defaultRenderTimeout
	}
	return s.cfg.RenderTimeout
}

func adminNavClass(active, item string) string {
	if active == item {
		return "tab tab-active"
	}
	return "tab"
}

func isVideoID(id string) bool {
	if len(id) != 12 {
		return false
	}
	for _, r := range id {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func (s *Server) createRenderLog(ctx context.Context, scene string) (*jobs.Job, error) {
	if s.store == nil {
		return &jobs.Job{ID: "abcdef123456"}, nil
	}
	return s.store.CreateRunning(ctx, scene)
}

func (s *Server) logRejected(ctx context.Context, scene, message string) {
	if s.store == nil {
		return
	}
	_ = s.store.CreateFailed(ctx, scene, message)
}

func (s *Server) markRenderEncoding(ctx context.Context, id string) {
	if s.store == nil {
		return
	}
	_ = s.store.SetStage(ctx, id, jobs.StageEncodingVideo)
}

func (s *Server) markRenderCompleted(ctx context.Context, id, videoPath string, videoSize int64) {
	if s.store == nil {
		return
	}
	_ = s.store.MarkCompleted(ctx, id, videoPath, videoSize)
}

func (s *Server) markRenderFailed(ctx context.Context, id, message string) {
	if s.store == nil {
		return
	}
	_ = s.store.MarkFailed(ctx, id, message)
}

func (s *Server) markGenerationCompleted(ctx context.Context, id, scene string) {
	if s.store == nil || id == "" {
		return
	}
	_ = s.store.MarkGenerationCompleted(ctx, id, scene)
}

func (s *Server) markGenerationFailed(ctx context.Context, id, message string) {
	if s.store == nil || id == "" {
		return
	}
	_ = s.store.MarkGenerationFailed(ctx, id, message)
}

func (s *Server) listRenderLogs(ctx context.Context, limit int) ([]adminRenderLog, error) {
	if s.store == nil {
		return nil, nil
	}
	rows, err := s.store.ListRecent(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]adminRenderLog, 0, len(rows))
	for _, job := range rows {
		out = append(out, renderLogRow(job))
	}
	return out, nil
}

func (s *Server) listAllGenerationLogs(ctx context.Context) ([]adminGenerationLog, error) {
	if s.store == nil {
		return nil, nil
	}
	rows, err := s.store.ListAllGenerations(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]adminGenerationLog, 0, len(rows))
	for _, gen := range rows {
		out = append(out, generationLogRow(gen))
	}
	return out, nil
}

func (s *Server) listExternalGenerationLogs(ctx context.Context, limit int) ([]adminExternalGeneration, error) {
	if s.store == nil {
		return nil, nil
	}
	generations, err := s.store.ListRecentGenerations(ctx, limit)
	if err != nil {
		return nil, err
	}
	attempts, err := s.store.ListRecentGenerationAttempts(ctx, limit*maxRepairAttempts+limit)
	if err != nil {
		return nil, err
	}
	attemptsByGeneration := make(map[string][]adminGenerationAttempt)
	for _, attempt := range attempts {
		attemptsByGeneration[attempt.GenerationID] = append(attemptsByGeneration[attempt.GenerationID], generationAttemptRow(attempt))
	}
	out := make([]adminExternalGeneration, 0, len(generations))
	for _, gen := range generations {
		out = append(out, externalGenerationRow(gen, attemptsByGeneration[gen.ID]))
	}
	return out, nil
}

func renderLogRow(job *jobs.Job) adminRenderLog {
	started := "—"
	if job.StartedAt != nil {
		started = job.StartedAt.Format("15:04:05")
	}
	done := "—"
	if job.CompletedAt != nil {
		done = job.CompletedAt.Format("15:04:05")
	}
	duration := "—"
	if dur := job.DurationSeconds(); dur != nil {
		duration = formatSeconds(*dur)
	}
	videoURL := ""
	if job.Status == jobs.StatusCompleted && job.VideoPath != "" {
		videoURL = "/video/" + job.ID
	}
	return adminRenderLog{
		ID:             job.ID,
		Status:         job.Status,
		Stage:          job.Stage,
		CreatedAt:      job.CreatedAt.Format("15:04:05"),
		StartedAt:      started,
		DoneAt:         done,
		Elapsed:        formatSeconds(job.ElapsedSeconds()),
		Duration:       duration,
		VideoURL:       videoURL,
		VideoSizeHuman: formatVideoSize(job.VideoSize),
		Error:          job.ErrorMessage,
	}
}

func renderLogsVersion(logs []adminRenderLog) string {
	var b strings.Builder
	for _, log := range logs {
		b.WriteString(log.ID)
		b.WriteByte('|')
		b.WriteString(log.Status)
		b.WriteByte('|')
		b.WriteString(log.Stage)
		b.WriteByte('|')
		b.WriteString(log.VideoURL)
		b.WriteByte('|')
		b.WriteString(log.VideoSizeHuman)
		b.WriteByte('|')
		b.WriteString(log.Error)
		b.WriteByte('\n')
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(b.String())))
}

func generationLogRow(gen *jobs.GenerationLog) adminGenerationLog {
	done := "—"
	duration := "—"
	if gen.CompletedAt != nil {
		done = gen.CompletedAt.Format("15:04:05")
		duration = formatSeconds(gen.CompletedAt.Sub(gen.CreatedAt).Seconds())
	}
	return adminGenerationLog{
		ID:              gen.ID,
		Status:          gen.Status,
		Model:           gen.Model,
		CreatedAt:       gen.CreatedAt.Format("15:04:05"),
		DoneAt:          done,
		Duration:        duration,
		Attempts:        strconv.Itoa(gen.AttemptCount),
		Prompt:          gen.Prompt,
		PromptPreview:   previewText(gen.Prompt, 180),
		PromptSize:      formatTextSize(gen.Prompt),
		Scene:           gen.Scene,
		ScenePreview:    previewText(gen.Scene, 220),
		SceneSize:       formatTextSize(gen.Scene),
		Error:           gen.ErrorMessage,
		ValidationError: gen.LastValidationError,
		OpenRouterError: gen.LastOpenRouterError,
	}
}

func externalGenerationRow(gen *jobs.GenerationLog, attempts []adminGenerationAttempt) adminExternalGeneration {
	done := "—"
	duration := "—"
	if gen.CompletedAt != nil {
		done = gen.CompletedAt.Format("15:04:05")
		duration = formatSeconds(gen.CompletedAt.Sub(gen.CreatedAt).Seconds())
	}
	return adminExternalGeneration{
		ID:                  gen.ID,
		Status:              gen.Status,
		Model:               gen.Model,
		CreatedAt:           gen.CreatedAt.Format("15:04:05"),
		DoneAt:              done,
		Duration:            duration,
		AttemptsCount:       strconv.Itoa(gen.AttemptCount),
		Prompt:              gen.Prompt,
		Scene:               gen.Scene,
		Error:               gen.ErrorMessage,
		LastValidationError: gen.LastValidationError,
		LastOpenRouterError: gen.LastOpenRouterError,
		Attempts:            attempts,
	}
}

func generationAttemptRow(attempt *jobs.GenerationAttempt) adminGenerationAttempt {
	return adminGenerationAttempt{
		Attempt:         strconv.Itoa(attempt.Attempt),
		CreatedAt:       attempt.CreatedAt.Format("15:04:05"),
		RequestMessages: attempt.RequestMessages,
		ResponseContent: attempt.ResponseContent,
		ExtractedScene:  attempt.ExtractedScene,
		ValidationError: attempt.ValidationError,
		OpenRouterError: attempt.OpenRouterError,
	}
}

func previewText(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return s[:max-1] + "…"
}

func formatVideoSize(size int64) string {
	if size <= 0 {
		return "—"
	}
	if size < 1048576 {
		return fmt.Sprintf("%.0f KB", float64(size)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(size)/1048576)
}

func formatTextSize(s string) string {
	size := len(s)
	if size == 0 {
		return "—"
	}
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	if size < 1048576 {
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(size)/1048576)
}

func formatSeconds(sec float64) string {
	return fmt.Sprintf("%.1fs", sec)
}

func formatRenderElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	seconds := int(d / time.Second)
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}

func renderElapsedSuffix(elapsed string) string {
	if elapsed == "" {
		return ""
	}
	return " · " + elapsed
}
