package pdtt

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	FrameGlob string
	FrameCnt  int
}

type FrameRenderer struct{}

func NewFrameRenderer() *FrameRenderer {
	return &FrameRenderer{}
}

func (r *FrameRenderer) Render(rt *Runtime, cfg Config) (*RenderResult, error) {
	if err := initFonts(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return nil, err
	}

	renderer := NewRenderer(cfg.Width, cfg.Height)
	nFrames := int(rt.Total*cfg.FPS) + 1
	fmt.Printf("%s: %s - %.1fs, %d frames, %d anims, %d live fields\n",
		filepath.Base(cfg.InputPath), rt.SceneName, rt.Total, nFrames, len(rt.Anims), len(rt.liveFields))

	for k := 0; k < nFrames; k++ {
		t := float64(k) / cfg.FPS
		if err := rt.Step(t); err != nil {
			return nil, fmt.Errorf("t=%.2fs: %w", t, err)
		}
		dc := renderer.Frame(rt)
		path := filepath.Join(cfg.OutputDir, fmt.Sprintf("f%05d.png", k))
		if err := dc.SavePNG(path); err != nil {
			return nil, err
		}
	}

	return &RenderResult{
		FramesDir: cfg.OutputDir,
		FrameGlob: filepath.Join(cfg.OutputDir, "f%05d.png"),
		FrameCnt:  nFrames,
	}, nil
}

type MP4Encoder struct{}

func NewMP4Encoder() *MP4Encoder {
	return &MP4Encoder{}
}

func (e *MP4Encoder) Encode(cfg Config) error {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		fmt.Println("warning: ffmpeg not found; skipping mp4 encoding")
		return nil
	}

	outPath := filepath.Join(cfg.OutputDir, "result.mp4")
	cmd := exec.Command(
		ffmpegPath,
		"-y",
		"-framerate", fmt.Sprintf("%.4f", cfg.FPS),
		"-i", filepath.Join(cfg.OutputDir, "f%05d.png"),
		"-pix_fmt", "yuv420p",
		"-movflags", "+faststart",
		outPath,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg failed: %w", err)
	}
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
	stmts, err := a.parser.ParsePath(a.cfg.InputPath)
	if err != nil {
		return err
	}
	rt, err := a.compiler.CompileStmts(stmts)
	if err != nil {
		return err
	}
	if _, err := a.renderer.Render(rt, a.cfg); err != nil {
		return err
	}
	return a.encoder.Encode(a.cfg)
}
