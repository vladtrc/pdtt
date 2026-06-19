package pdtt

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

type RenderResult struct {
	FramesDir string
	FrameCnt  int
}

type FrameRenderer struct{}

func NewFrameRenderer() *FrameRenderer {
	return &FrameRenderer{}
}

func (r *FrameRenderer) Render(rt *Runtime, cfg Config, trace *Tracer) (*RenderResult, error) {
	if err := initFonts(); err != nil {
		return nil, err
	}
	framesDir := filepath.Join(cfg.OutputDir, "frames")
	if err := os.MkdirAll(framesDir, 0o755); err != nil {
		return nil, err
	}

	renderer := NewRenderer(cfg.Width, cfg.Height)
	nFrames := int(rt.Total*cfg.FPS) + 1
	fmt.Printf("%s: %s - %.1fs, %d frames, %d anims, %d live fields\n",
		filepath.Base(cfg.InputPath), rt.SceneName, rt.Total, nFrames, len(rt.Anims), len(rt.liveFields))

	trace.Info("render_start",
		"scene", rt.SceneName,
		"total_s", rt.Total,
		"frames", nFrames,
		"anims", len(rt.Anims),
		"live_fields", len(rt.liveFields),
	)

	var stepTotal, renderTotal, saveTotal time.Duration
	var slowestFrame int
	var slowestMs float64

	for k := 0; k < nFrames; k++ {
		t := float64(k) / cfg.FPS

		stepStart := time.Now()
		if err := rt.Step(t); err != nil {
			return nil, fmt.Errorf("t=%.2fs: %w", t, err)
		}
		stepDur := time.Since(stepStart)

		renderStart := time.Now()
		dc := renderer.Frame(rt)
		renderDur := time.Since(renderStart)

		path := filepath.Join(framesDir, fmt.Sprintf("f%05d.png", k))
		saveStart := time.Now()
		if err := dc.SavePNG(path); err != nil {
			return nil, err
		}
		saveDur := time.Since(saveStart)

		stepTotal += stepDur
		renderTotal += renderDur
		saveTotal += saveDur

		frameMs := durMs(stepDur + renderDur + saveDur)
		if frameMs >= slowestMs {
			slowestMs = frameMs
			slowestFrame = k
		}

		trace.Info("frame",
			"index", k,
			"t_s", t,
			"step_ms", durMs(stepDur),
			"render_ms", durMs(renderDur),
			"save_ms", durMs(saveDur),
			"total_ms", frameMs,
		)
	}

	trace.Info("render_frames_summary",
		"frames", nFrames,
		"step_total_ms", durMs(stepTotal),
		"render_total_ms", durMs(renderTotal),
		"save_total_ms", durMs(saveTotal),
		"slowest_index", slowestFrame,
		"slowest_ms", slowestMs,
	)

	return &RenderResult{
		FramesDir: framesDir,
		FrameCnt:  nFrames,
	}, nil
}

type MP4Encoder struct{}

func NewMP4Encoder() *MP4Encoder {
	return &MP4Encoder{}
}

func (e *MP4Encoder) Encode(cfg Config, trace *Tracer) error {
	encodeStart := time.Now()
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		fmt.Println("warning: ffmpeg not found; skipping mp4 encoding")
		trace.Info("encode_skipped",
			"reason", "ffmpeg not found",
			"duration_ms", durMs(time.Since(encodeStart)),
		)
		return nil
	}

	outPath := filepath.Join(cfg.OutputDir, "result.mp4")
	cmd := exec.Command(
		ffmpegPath,
		"-y",
		"-framerate", fmt.Sprintf("%.4f", cfg.FPS),
		"-i", filepath.Join(cfg.OutputDir, "frames", "f%05d.png"),
		"-pix_fmt", "yuv420p",
		"-movflags", "+faststart",
		outPath,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg failed: %w", err)
	}
	trace.Info("encode_done",
		"output", outPath,
		"duration_ms", durMs(time.Since(encodeStart)),
	)
	return nil
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
	trace.Info("pipeline_start",
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
	trace.Info("parse_done",
		"duration_ms", durMs(time.Since(parseStart)),
		"stmts", len(stmts),
	)

	compileStart := time.Now()
	rt, err := a.compiler.CompileStmts(stmts)
	if err != nil {
		return err
	}
	nFrames := int(rt.Total*a.cfg.FPS) + 1
	trace.Info("compile_done",
		"duration_ms", durMs(time.Since(compileStart)),
		"scene", rt.SceneName,
		"total_s", rt.Total,
		"frames", nFrames,
		"anims", len(rt.Anims),
		"live_fields", len(rt.liveFields),
	)

	renderStart := time.Now()
	if _, err := a.renderer.Render(rt, a.cfg, trace); err != nil {
		return err
	}
	trace.Info("render_done", "duration_ms", durMs(time.Since(renderStart)), "frames", nFrames)

	if err := a.encoder.Encode(a.cfg, trace); err != nil {
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
