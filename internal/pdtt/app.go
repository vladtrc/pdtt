package pdtt

import (
	"bufio"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Config struct {
	InputPath string
	OutputDir string
	FPS       float64
	Width     int
	Height    int
}

type SceneParser struct{}

func NewSceneParser() *SceneParser {
	return &SceneParser{}
}

func (p *SceneParser) ParsePath(path string) ([]Stmt, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseFile(string(src))
}

type SceneCompiler struct{}

func NewSceneCompiler() *SceneCompiler {
	return &SceneCompiler{}
}

func (c *SceneCompiler) CompileStmts(stmts []Stmt) (*Runtime, error) {
	return Compile(stmts)
}

type FrameRenderer struct{}

func NewFrameRenderer() *FrameRenderer {
	return &FrameRenderer{}
}

// FrameCount returns how many frames a runtime renders at the given fps.
func FrameCount(rt *Runtime, fps float64) int {
	return int(rt.Total*fps) + 1
}

// FrameSink consumes rendered frames as they are produced. MP4 encoding is one
// sink; tests and public API callers can omit it and still get debug PNG frames.
type FrameSink interface {
	WriteFrame(*image.RGBA) error
}

type RenderResult struct {
	FramesDir string
	FrameCnt  int
	WaitDebug func() error
}

// pngJob is a single debug frame handed off to a background writer. The pixels
// are a private copy because the renderer reuses its draw buffer every frame.
type pngJob struct {
	path string
	pix  []byte
	w, h int
}

// Render renders every frame, optionally streaming each frame into a sink while
// debug PNGs are written by background workers off the critical path.
func (r *FrameRenderer) Render(rt *Runtime, cfg Config, trace *Tracer, sink FrameSink) (*RenderResult, error) {
	if err := initFonts(); err != nil {
		return nil, err
	}
	framesDir := filepath.Join(cfg.OutputDir, "frames")
	if err := os.MkdirAll(framesDir, 0o755); err != nil {
		return nil, err
	}

	renderer := NewRenderer(cfg.Width, cfg.Height)
	nFrames := FrameCount(rt, cfg.FPS)
	fmt.Printf("%s: %s - %.1fs, %d frames, %d anims, %d live fields\n",
		filepath.Base(cfg.InputPath), rt.SceneName, rt.Total, nFrames, len(rt.Anims), len(rt.liveFields))

	trace.Info(
		"render_start",
		"scene", rt.SceneName,
		"total_s", rt.Total,
		"frames", nFrames,
		"anims", len(rt.Anims),
		"live_fields", len(rt.liveFields),
	)

	// Background PNG writers. Buffered so a brief encode stall does not block
	// the render loop (and therefore the mp4).
	jobs := make(chan pngJob, 16)
	var pngErr error
	var pngErrOnce sync.Once
	var wg sync.WaitGroup
	for i := 0; i < pngWorkerCount(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if err := savePNG(j.path, j.pix, j.w, j.h); err != nil {
					pngErrOnce.Do(func() { pngErr = err })
				}
			}
		}()
	}
	pngWait := func() error {
		wg.Wait()
		return pngErr
	}

	var stepTotal, renderTotal, pipeTotal time.Duration
	var slowestFrame int
	var slowestMs float64

	for k := 0; k < nFrames; k++ {
		t := float64(k) / cfg.FPS

		stepStart := time.Now()
		if err := rt.Step(t); err != nil {
			close(jobs)
			_ = pngWait()
			return nil, fmt.Errorf("t=%.2fs: %w", t, err)
		}
		stepDur := time.Since(stepStart)

		renderStart := time.Now()
		dc := renderer.Frame(rt)
		img := dc.Image().(*image.RGBA)
		renderDur := time.Since(renderStart)

		// Feed the encoder immediately: the mp4 grows as we render.
		pipeStart := time.Now()
		if sink != nil {
			if err := sink.WriteFrame(img); err != nil {
				close(jobs)
				_ = pngWait()
				return nil, fmt.Errorf("write frame %d: %w", k, err)
			}
		}
		pipeDur := time.Since(pipeStart)

		// Hand the debug PNG to a worker. Copy first — Frame reuses its buffer.
		pix := make([]byte, len(img.Pix))
		copy(pix, img.Pix)
		jobs <- pngJob{
			path: filepath.Join(framesDir, fmt.Sprintf("f%05d.png", k)),
			pix:  pix,
			w:    cfg.Width,
			h:    cfg.Height,
		}

		stepTotal += stepDur
		renderTotal += renderDur
		pipeTotal += pipeDur

		frameMs := durMs(stepDur + renderDur + pipeDur)
		if frameMs >= slowestMs {
			slowestMs = frameMs
			slowestFrame = k
		}

		trace.Info(
			"frame",
			"index", k,
			"t_s", t,
			"step_ms", durMs(stepDur),
			"render_ms", durMs(renderDur),
			"pipe_ms", durMs(pipeDur),
			"total_ms", frameMs,
		)
	}
	close(jobs)

	trace.Info(
		"render_frames_summary",
		"frames", nFrames,
		"step_total_ms", durMs(stepTotal),
		"render_total_ms", durMs(renderTotal),
		"pipe_total_ms", durMs(pipeTotal),
		"slowest_index", slowestFrame,
		"slowest_ms", slowestMs,
	)

	return &RenderResult{FramesDir: framesDir, FrameCnt: nFrames, WaitDebug: pngWait}, nil
}

// pngWorkerCount sizes the debug-PNG pool. Kept small so that running many
// examples in parallel (make -j render-all) does not oversubscribe the CPU;
// PNGs are off the critical path, so a couple of workers per process suffice.
func pngWorkerCount() int {
	if n := runtime.GOMAXPROCS(0); n < 3 {
		return 1
	}
	return 3
}

// savePNG encodes a copied RGBA buffer to a PNG file on disk.
func savePNG(path string, pix []byte, w, h int) error {
	img := &image.RGBA{
		Pix:    pix,
		Stride: w * 4,
		Rect:   image.Rect(0, 0, w, h),
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

type MP4Encoder struct{}

func NewMP4Encoder() *MP4Encoder {
	return &MP4Encoder{}
}

// MP4Stream is a running ffmpeg process consuming raw RGBA frames on stdin.
// Frames are encoded as they arrive, so the mp4 is produced concurrently with
// rendering instead of in a separate pass over PNGs on disk.
type MP4Stream struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	buf   *bufio.Writer
	out   string
	trace *Tracer
	start time.Time
}

// WriteFrame pushes one frame's raw RGBA bytes to ffmpeg.
func (s *MP4Stream) WriteFrame(img *image.RGBA) error {
	_, err := s.buf.Write(img.Pix)
	return err
}

// Close flushes remaining frames, closes ffmpeg's stdin, and waits for it to
// finalize the mp4. Because every frame was already streamed during rendering,
// this just writes the trailer and returns almost immediately.
func (s *MP4Stream) Close() error {
	flushErr := s.buf.Flush()
	closeErr := s.stdin.Close()
	waitErr := s.cmd.Wait()
	if flushErr != nil {
		return fmt.Errorf("flush frames to ffmpeg: %w", flushErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close ffmpeg stdin: %w", closeErr)
	}
	if waitErr != nil {
		return fmt.Errorf("ffmpeg failed: %w", waitErr)
	}
	s.trace.Info(
		"encode_done",
		"output", s.out,
		"duration_ms", durMs(time.Since(s.start)),
	)
	return nil
}

// Start launches ffmpeg reading raw RGBA frames from stdin. It returns
// (nil, nil) when ffmpeg is unavailable so callers can render frames anyway.
func (e *MP4Encoder) Start(cfg Config, trace *Tracer) (*MP4Stream, error) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		fmt.Println("warning: ffmpeg not found; skipping mp4 encoding")
		trace.Info("encode_skipped", "reason", "ffmpeg not found")
		return nil, nil
	}

	outPath := filepath.Join(cfg.OutputDir, "result.mp4")
	cmd := exec.Command(
		ffmpegPath,
		"-y",
		"-f", "rawvideo",
		"-pixel_format", "rgba",
		"-video_size", fmt.Sprintf("%dx%d", cfg.Width, cfg.Height),
		"-framerate", fmt.Sprintf("%.4f", cfg.FPS),
		"-i", "-",
		"-pix_fmt", "yuv420p",
		"-movflags", "+faststart",
		outPath,
	)
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ffmpeg: %w", err)
	}

	return &MP4Stream{
		cmd:   cmd,
		stdin: stdin,
		buf:   bufio.NewWriterSize(stdin, 1<<20),
		out:   outPath,
		trace: trace,
		start: time.Now(),
	}, nil
}

type App struct {
	cfg      Config
	parser   *SceneParser
	compiler *SceneCompiler
	renderer *FrameRenderer
	encoder  *MP4Encoder
}

func NewApp(
	cfg Config,
	parser *SceneParser,
	compiler *SceneCompiler,
	renderer *FrameRenderer,
	encoder *MP4Encoder,
) *App {
	return &App{
		cfg:      cfg,
		parser:   parser,
		compiler: compiler,
		renderer: renderer,
		encoder:  encoder,
	}
}

func (a *App) Run() error {
	if err := a.run(); err != nil {
		if writeErr := writeErrorOutput(a.cfg.OutputDir, err); writeErr != nil {
			return errors.Join(err, writeErr)
		}
		return err
	}
	return nil
}

func (a *App) run() error {
	if err := prepareOutputDir(a.cfg.OutputDir); err != nil {
		return err
	}
	trace, err := NewTracer(a.cfg.OutputDir)
	if err != nil {
		return err
	}
	defer func() { _ = trace.Close() }()

	pipelineStart := time.Now()
	trace.Info(
		"pipeline_start",
		"input", a.cfg.InputPath,
		"output", a.cfg.OutputDir,
		"fps", a.cfg.FPS,
		"width", a.cfg.Width,
		"height", a.cfg.Height,
	)

	parseStart := time.Now()
	stmts, err := a.parser.ParsePath(a.cfg.InputPath)
	if err != nil {
		return err
	}
	trace.Info(
		"parse_done",
		"duration_ms", durMs(time.Since(parseStart)),
		"stmts", len(stmts),
	)

	compileStart := time.Now()
	rt, err := a.compiler.CompileStmts(stmts)
	if err != nil {
		return err
	}
	nFrames := FrameCount(rt, a.cfg.FPS)
	trace.Info(
		"compile_done",
		"duration_ms", durMs(time.Since(compileStart)),
		"scene", rt.SceneName,
		"total_s", rt.Total,
		"frames", nFrames,
		"anims", len(rt.Anims),
		"live_fields", len(rt.liveFields),
	)

	// Launch ffmpeg up front so it encodes frames as the renderer produces
	// them, rather than in a separate pass once every PNG is on disk.
	stream, err := a.encoder.Start(a.cfg, trace)
	if err != nil {
		return err
	}

	renderStart := time.Now()
	result, err := a.renderer.Render(rt, a.cfg, trace, stream)
	if err != nil {
		if stream != nil {
			_ = stream.Close()
		}
		return err
	}
	trace.Info("render_done", "duration_ms", durMs(time.Since(renderStart)), "frames", nFrames)

	// Finalize the mp4 first — its frames are already encoded, so this returns
	// fast — then wait for the off-path debug PNGs to finish writing.
	if stream != nil {
		if err := stream.Close(); err != nil {
			_ = result.WaitDebug()
			return err
		}
	}
	if err := result.WaitDebug(); err != nil {
		return err
	}

	trace.Info("pipeline_done", "duration_ms", durMs(time.Since(pipelineStart)))
	return nil
}

func writeErrorOutput(outputDir string, renderErr error) error {
	if err := prepareOutputDir(outputDir); err != nil {
		return err
	}

	msg := "render failed\n\n" + renderErr.Error() + "\n"
	if err := os.WriteFile(filepath.Join(outputDir, "error.txt"), []byte(msg), 0o644); err != nil {
		return fmt.Errorf("write error.txt: %w", err)
	}
	return nil
}

func prepareOutputDir(outputDir string) error {
	if isUnsafeOutputDir(outputDir) {
		return fmt.Errorf("refusing to clear unsafe output directory %q", outputDir)
	}

	if err := os.RemoveAll(outputDir); err != nil {
		return fmt.Errorf("clear output dir: %w", err)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	return nil
}

func isUnsafeOutputDir(path string) bool {
	clean := filepath.Clean(strings.TrimSpace(path))
	return clean == "" || clean == "." || clean == string(filepath.Separator)
}
