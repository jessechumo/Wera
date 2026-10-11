package resume

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

//go:embed template.typ
var template []byte

// ErrUnavailable means the typst binary is missing on this server.
var ErrUnavailable = errors.New("resume rendering is not available on this server (typst is not installed)")

// Renderer turns resumes into PDF and SVG with the typst binary. Each
// compile runs in its own temporary folder (its --root, so the template
// can read nothing else), with no system fonts, a time limit and a cap on
// concurrent compiles.
type Renderer struct {
	bin  string
	slot chan struct{}
}

// NewRenderer finds typst (bin, or "typst" on PATH when empty). It returns
// a Renderer whose calls fail with ErrUnavailable when there is none.
func NewRenderer(bin string) *Renderer {
	if bin == "" {
		bin = "typst"
	}
	if p, err := exec.LookPath(bin); err == nil {
		bin = p
	} else {
		bin = ""
	}
	return &Renderer{bin: bin, slot: make(chan struct{}, 4)}
}

// Available reports whether typst was found.
func (rd *Renderer) Available() bool { return rd != nil && rd.bin != "" }

type workdir struct{ dir string }

func (rd *Renderer) prepare(r *Resume, l Layout) (*workdir, error) {
	dir, err := os.MkdirTemp("", "wera-resume-*")
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(r)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	lay, _ := json.Marshal(l.Clamp())
	for name, b := range map[string][]byte{"main.typ": template, "resume.json": data, "layout.json": lay} {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			os.RemoveAll(dir)
			return nil, err
		}
	}
	return &workdir{dir}, nil
}

func (rd *Renderer) run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if !rd.Available() {
		return nil, ErrUnavailable
	}
	select {
	case rd.slot <- struct{}{}:
		defer func() { <-rd.slot }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	full := append([]string{args[0], "--root", dir, "--ignore-system-fonts"}, args[1:]...)
	cmd := exec.CommandContext(ctx, rd.bin, full...) //nolint:gosec // fixed binary; arguments are file names we chose
	cmd.Dir = dir
	cmd.Env = []string{"HOME=" + dir, "XDG_CACHE_HOME=" + dir, "TYPST_PACKAGE_CACHE_PATH=" + dir}
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 400 {
			msg = msg[:400]
		}
		return nil, fmt.Errorf("typst %s: %w: %s", args[0], err, msg)
	}
	return out.Bytes(), nil
}

// PDF renders the resume.
func (rd *Renderer) PDF(ctx context.Context, r *Resume, l Layout) ([]byte, error) {
	w, err := rd.prepare(r, l)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(w.dir)
	if _, err := rd.run(ctx, w.dir, "compile", "main.typ", "out.pdf"); err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(w.dir, "out.pdf"))
}

// SVG renders every page as SVG, for previews (an SVG shown as an image
// cannot run scripts).
func (rd *Renderer) SVG(ctx context.Context, r *Resume, l Layout) ([][]byte, error) {
	w, err := rd.prepare(r, l)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(w.dir)
	if _, err := rd.run(ctx, w.dir, "compile", "--format", "svg", "main.typ", "page-{0p}.svg"); err != nil {
		return nil, err
	}
	names, _ := filepath.Glob(filepath.Join(w.dir, "page-*.svg"))
	sort.Strings(names)
	pages := make([][]byte, 0, len(names))
	for _, n := range names {
		b, err := os.ReadFile(n) //nolint:gosec // our own temp folder's output files
		if err != nil {
			return nil, err
		}
		pages = append(pages, b)
	}
	return pages, nil
}

// Measure is where the content ends: the page count and how full the last
// page is (0 to 1).
type Measure struct {
	Pages int     `json:"pages"`
	Fill  float64 `json:"fill"`
}

// Measure lays the resume out without producing output.
func (rd *Renderer) Measure(ctx context.Context, r *Resume, l Layout) (Measure, error) {
	w, err := rd.prepare(r, l)
	if err != nil {
		return Measure{}, err
	}
	defer os.RemoveAll(w.dir)
	out, err := rd.run(ctx, w.dir, "query", "main.typ", "<wera-end>", "--field", "value", "--one")
	if err != nil {
		return Measure{}, err
	}
	var end struct {
		Page int     `json:"page"`
		Y    float64 `json:"y"`
	}
	if err := json.Unmarshal(out, &end); err != nil {
		return Measure{}, fmt.Errorf("reading the layout: %w", err)
	}
	l = l.Clamp()
	top, bottom := l.Margin*0.85*72, l.Margin*72
	fill := (end.Y - top) / (792 - top - bottom)
	return Measure{Pages: end.Page, Fill: max(0, min(1, fill))}, nil
}
