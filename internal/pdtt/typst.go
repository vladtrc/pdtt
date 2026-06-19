package pdtt

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

var errTypstUnavailable = errors.New("typst not found on PATH")

const typstBaseTextSizePt = 11.0

type Box struct {
	Min   Vec
	Max   Vec
	Valid bool
}

func (b *Box) Include(p Vec) {
	if !b.Valid {
		b.Min = p
		b.Max = p
		b.Valid = true
		return
	}
	if p[0] < b.Min[0] {
		b.Min[0] = p[0]
	}
	if p[1] < b.Min[1] {
		b.Min[1] = p[1]
	}
	if p[0] > b.Max[0] {
		b.Max[0] = p[0]
	}
	if p[1] > b.Max[1] {
		b.Max[1] = p[1]
	}
}

func (b Box) Width() float64 {
	if !b.Valid {
		return 0
	}
	return b.Max[0] - b.Min[0]
}

func (b Box) Height() float64 {
	if !b.Valid {
		return 0
	}
	return b.Max[1] - b.Min[1]
}

type typstCacheKey struct {
	markup string
	math   bool
}

type typstCacheEntry struct {
	contours [][]Vec
	bbox     Box
	err      error
}

var (
	typstPathOnce sync.Once
	typstPath     string
	typstPathErr  error

	typstCacheMu sync.RWMutex
	typstCache   = map[typstCacheKey]typstCacheEntry{}
)

func typstBinary() (string, error) {
	typstPathOnce.Do(func() {
		path, err := exec.LookPath("typst")
		if err != nil {
			typstPathErr = fmt.Errorf("%w", errTypstUnavailable)
			return
		}
		typstPath = path
	})
	if typstPathErr != nil {
		return "", typstPathErr
	}
	return typstPath, nil
}

func typstInstalled() bool {
	_, err := typstBinary()
	return err == nil
}

func typstStringLiteral(s string) string {
	repl := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\n`,
		"\r", `\r`,
		"\t", `\t`,
	)
	return `"` + repl.Replace(s) + `"`
}

func typstSource(markup string, math bool) string {
	body := `#text(` + typstStringLiteral(markup) + `)`
	if math {
		body = "$" + markup + "$"
	}
	return strings.Join([]string{
		"#set page(width: auto, height: auto, margin: 0pt, fill: none)",
		fmt.Sprintf("#set text(fill: black, size: %gpt)", typstBaseTextSizePt),
		body,
		"",
	}, "\n")
}

func typstGlyphs(markup string, math bool) ([][]Vec, Box, error) {
	key := typstCacheKey{markup: markup, math: math}
	typstCacheMu.RLock()
	if cached, ok := typstCache[key]; ok {
		typstCacheMu.RUnlock()
		return cloneContours(cached.contours), cached.bbox, cached.err
	}
	typstCacheMu.RUnlock()

	path, err := typstBinary()
	if err != nil {
		return nil, Box{}, err
	}

	src := typstSource(markup, math)

	cmd := exec.Command(path, "compile", "-f", "svg", "-", "-")
	cmd.Stdin = strings.NewReader(src)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, Box{}, fmt.Errorf("typst compile failed: %s", msg)
	}

	contours, bbox, err := parseTypstSVG(out.String())
	entry := typstCacheEntry{
		contours: cloneContours(contours),
		bbox:     bbox,
		err:      err,
	}
	typstCacheMu.Lock()
	typstCache[key] = entry
	typstCacheMu.Unlock()
	return contours, bbox, err
}

func cloneContours(in [][]Vec) [][]Vec {
	if len(in) == 0 {
		return nil
	}
	out := make([][]Vec, len(in))
	for i := range in {
		if len(in[i]) == 0 {
			continue
		}
		out[i] = append([]Vec(nil), in[i]...)
	}
	return out
}
