// Capture renders 2x Retina PNG screenshots and demo video frames directly from bmux.
package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bmux-dev/bmux/internal/vt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/creack/pty"
)

var allPNGScenes = []string{
	"workspace",
	"session-picker",
	"project-picker",
	"scrollback-search",
	"key-help",
}

type fixture struct {
	root        string
	binDir      string
	bmuxBin     string
	homeDir     string
	configDir   string
	stateDir    string
	runtimeDir  string
	projectsDir string
	logFile     string
	baseEnv     []string
}

func main() {
	repo := flag.String("repo", ".", "bmux repository root")
	scene := flag.String("scene", "all", "scene to capture: all, workspace, session-picker, project-picker, scrollback-search, key-help, demo")
	theme := flag.String("theme", "both", "theme to capture: both, dark, light")
	outScreenshots := flag.String("out-screenshots", "", "output directory for PNG screenshots (default <repo>/site/src/assets/screenshots)")
	outDemo := flag.String("out-demo", "", "output directory for demo video (default <repo>/site/public/demo)")
	renderer := flag.String("renderer", "", "path to render_media.py (default <repo>/.agents/skills/bmux-screenshots/scripts/render_media.py)")
	flag.Parse()

	repoAbs, err := filepath.Abs(*repo)
	must(err)

	if *outScreenshots == "" {
		*outScreenshots = filepath.Join(repoAbs, "site", "src", "assets", "screenshots")
	}
	if *outDemo == "" {
		*outDemo = filepath.Join(repoAbs, "site", "public", "demo")
	}
	if *renderer == "" {
		*renderer = filepath.Join(repoAbs, ".agents", "skills", "bmux-screenshots", "scripts", "render_media.py")
	}

	var themes []string
	switch *theme {
	case "both":
		themes = []string{"dark", "light"}
	case "dark", "light":
		themes = []string{*theme}
	default:
		fatalf("unknown -theme %q", *theme)
	}

	var scenes []string
	if *scene == "all" {
		scenes = allPNGScenes
	} else {
		scenes = []string{*scene}
	}

	workRoot, err := os.MkdirTemp("", "bmux-capture-")
	must(err)
	defer os.RemoveAll(workRoot)

	binDir := filepath.Join(workRoot, "bin")
	must(os.MkdirAll(binDir, 0o755))
	bmuxBin := filepath.Join(binDir, "bmux")

	fmt.Printf("Building bmux binary...\n")
	build := exec.Command("go", "build", "-o", bmuxBin, "./cmd/bmux")
	build.Dir = repoAbs
	if out, err := build.CombinedOutput(); err != nil {
		fatalf("go build failed: %v\n%s", err, out)
	}

	for _, th := range themes {
		fx, err := setupFixture(repoAbs, workRoot, binDir, bmuxBin, th)
		if err != nil {
			fatalf("setup fixture (%s): %v", th, err)
		}
		for _, sc := range scenes {
			if sc == "demo" {
				if err := captureDemo(fx, *renderer, th, *outDemo); err != nil {
					cleanupFixture(fx)
					fatalf("capture demo (%s): %v", th, err)
				}
				continue
			}
			outPath := filepath.Join(*outScreenshots, fmt.Sprintf("%s-%s.png", sc, th))
			if err := captureScenePNG(fx, *renderer, th, sc, outPath); err != nil {
				cleanupFixture(fx)
				fatalf("capture %s (%s): %v", sc, th, err)
			}
		}
		cleanupFixture(fx)
	}
}

func setupFixture(repoAbs, workRoot, binDir, bmuxBin, theme string) (*fixture, error) {
	themeRoot := filepath.Join(workRoot, "env-"+theme)
	homeDir := filepath.Join(themeRoot, "home")
	fx := &fixture{
		root:        themeRoot,
		binDir:      binDir,
		bmuxBin:     bmuxBin,
		homeDir:     homeDir,
		configDir:   filepath.Join(themeRoot, "config"),
		stateDir:    filepath.Join(themeRoot, "state"),
		runtimeDir:  filepath.Join(themeRoot, "runtime"),
		projectsDir: filepath.Join(homeDir, "projects"),
		logFile:     filepath.Join(homeDir, "server.log"),
	}
	for _, d := range []string{
		fx.homeDir,
		filepath.Join(fx.configDir, "bmux"),
		fx.stateDir,
		fx.runtimeDir,
		fx.projectsDir,
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	if err := os.Chmod(fx.runtimeDir, 0o700); err != nil {
		return nil, err
	}

	helperBin := filepath.Join(repoAbs, "site", "screenshots", "bin")
	fx.baseEnv = cleanEnv(map[string]string{
		"PATH":                 helperBin + string(os.PathListSeparator) + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME":                 fx.homeDir,
		"USER":                 "bmux",
		"XDG_CONFIG_HOME":      fx.configDir,
		"XDG_STATE_HOME":       fx.stateDir,
		"BMUX_RUNTIME_DIR":     fx.runtimeDir,
		"BMUX_NO_UPDATE_CHECK": "1",
		"TERM":                 "xterm-256color",
		"COLORTERM":            "truecolor",
		"GIT_AUTHOR_NAME":      "bmux",
		"GIT_AUTHOR_EMAIL":     "bmux@example.com",
		"GIT_COMMITTER_NAME":   "bmux",
		"GIT_COMMITTER_EMAIL":  "bmux@example.com",
	})

	bmuxTheme := "onedark"
	if theme == "light" {
		bmuxTheme = "catppuccin-latte"
	}
	cfg := fmt.Sprintf(`theme = %q
onboarding = false

[ui]
accent = "theme"
theme-panes = true
show-branch = true

[sidebar]
show = true

[update]
check = false
`, bmuxTheme)
	if err := os.WriteFile(filepath.Join(fx.configDir, "bmux", "config.toml"), []byte(cfg), 0o644); err != nil {
		return nil, err
	}

	if err := writeSampleRepos(fx); err != nil {
		return nil, err
	}

	// Write .bmux.toml project layouts for api, api-auth, web, docs, cli using [[windows]] + [[panes]]
	apiLayout := `active = 1

[[windows]]
name = "dev"
focused = 2
[windows.layout]
direction = "horizontal"
ratio = 5200
[windows.layout.first]
pane = 1
[windows.layout.second]
pane = 2

[[panes]]
id = 1
command = "fake-agent working agy"

[[panes]]
id = 2
command = "fake-agent blocked Codex"
`
	if err := os.WriteFile(filepath.Join(fx.projectsDir, "api", ".bmux.toml"), []byte(apiLayout), 0o644); err != nil {
		return nil, err
	}

	apiAuthLayout := `[[windows]]
name = "agy"
[windows.layout]
pane = 1

[[panes]]
id = 1
command = "fake-agent blocked-interactive agy"
`
	if err := os.WriteFile(filepath.Join(fx.projectsDir, "api-auth", ".bmux.toml"), []byte(apiAuthLayout), 0o644); err != nil {
		return nil, err
	}

	webLayout := `[[windows]]
name = "codex"
[windows.layout]
pane = 1

[[panes]]
id = 1
command = "fake-agent blocked Codex"
`
	if err := os.WriteFile(filepath.Join(fx.projectsDir, "web", ".bmux.toml"), []byte(webLayout), 0o644); err != nil {
		return nil, err
	}

	docsLayout := `active = 1

[[windows]]
name = "docs"
focused = 1
[windows.layout]
direction = "vertical"
ratio = 6800
[windows.layout.first]
pane = 1
[windows.layout.second]
pane = 2

[[panes]]
id = 1
command = "clear && tail -n 30 -f ~/server.log"

[[panes]]
id = 2
command = "fake-agent idle OpenCode"
`
	if err := os.WriteFile(filepath.Join(fx.projectsDir, "docs", ".bmux.toml"), []byte(docsLayout), 0o644); err != nil {
		return nil, err
	}

	cliLayout := `[[windows]]
name = "agy"
[windows.layout]
pane = 1

[[panes]]
id = 1
command = "fake-agent idle agy"
`
	if err := os.WriteFile(filepath.Join(fx.projectsDir, "cli", ".bmux.toml"), []byte(cliLayout), 0o644); err != nil {
		return nil, err
	}

	// Trust every generated .bmux.toml so its commands execute cleanly
	for _, name := range []string{"api", "api-auth", "web", "docs", "cli"} {
		if err := fx.runBmux("layout", "trust", filepath.Join(fx.projectsDir, name)); err != nil {
			return nil, fmt.Errorf("trust layout %s: %w", name, err)
		}
	}

	// Create server sessions (leave cli unopened so Ctrl-a n opens it live in the demo)
	for _, s := range []struct {
		name string
		dir  string
	}{
		{"api", filepath.Join(fx.projectsDir, "api")},
		{"api-auth", filepath.Join(fx.projectsDir, "api-auth")},
		{"web", filepath.Join(fx.projectsDir, "web")},
		{"docs", filepath.Join(fx.projectsDir, "docs")},
	} {
		if err := fx.runBmux("new", "--detach", "--dir", s.dir, s.name); err != nil {
			return nil, fmt.Errorf("new session %s: %w", s.name, err)
		}
	}

	// Wait until all 5 fake-agent panes report their expected state
	if err := fx.waitForAgents(5, 6*time.Second); err != nil {
		return nil, err
	}
	return fx, nil
}

func writeSampleRepos(fx *fixture) error {
	for _, name := range []string{"api", "web", "docs", "cli", "infra"} {
		dir := filepath.Join(fx.projectsDir, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		readme := fmt.Sprintf("# %s\n\nService workspace.\n", name)
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0o644); err != nil {
			return err
		}
		if err := runGit(fx.baseEnv, dir, "init", "-q", "-b", "main"); err != nil {
			return err
		}
		if err := runGit(fx.baseEnv, dir, "add", "."); err != nil {
			return err
		}
		if err := runGit(fx.baseEnv, dir, "commit", "-q", "-m", "initial commit"); err != nil {
			return err
		}
	}
	// Create git worktree api-auth on branch fix/auth under api
	apiDir := filepath.Join(fx.projectsDir, "api")
	if err := runGit(fx.baseEnv, apiDir, "worktree", "add", "-q", "-b", "fix/auth", filepath.Join(fx.projectsDir, "api-auth")); err != nil {
		return err
	}

	logLines := strings.Join([]string{
		"2026-11-24T19:41:02Z INFO  http listening addr=:8080 env=dev",
		"2026-11-24T19:41:04Z INFO  pg pool ready conns=8 max=16",
		"2026-11-24T19:41:05Z INFO  GET /v1/health status=200 latency=0.4ms",
		"2026-11-24T19:41:08Z WARN  upstream connect timeout addr=10.0.4.12:5432 retry=1",
		"2026-11-24T19:41:09Z INFO  POST /v1/tokens status=201 latency=3.2ms",
		"2026-11-24T19:41:11Z WARN  upstream read timeout addr=10.0.4.12:5432 deadline=250ms",
		"2026-11-24T19:41:14Z INFO  GET /v1/sessions status=200 latency=1.1ms",
		"2026-11-24T19:41:18Z INFO  worker sync complete updated=14 skipped=2",
	}, "\n") + "\n"
	return os.WriteFile(fx.logFile, []byte(logLines), 0o644)
}

func runGit(env []string, dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %v: %w\n%s", args, err, out)
	}
	return nil
}

func (fx *fixture) runBmux(args ...string) error {
	cmd := exec.Command(fx.bmuxBin, args...)
	cmd.Env = fx.baseEnv
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("bmux %v: %w\n%s", args, err, out)
	}
	return nil
}

func (fx *fixture) waitForAgents(minCount int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		cmd := exec.Command(fx.bmuxBin, "agents")
		cmd.Env = fx.baseEnv
		out, err := cmd.Output()
		if err == nil {
			count := 0
			sc := bufio.NewScanner(strings.NewReader(string(out)))
			for sc.Scan() {
				if strings.TrimSpace(sc.Text()) != "" {
					count++
				}
			}
			if count >= minCount {
				return nil
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for %d agent statuses", minCount)
}

func cleanupFixture(fx *fixture) {
	cmd := exec.Command(fx.bmuxBin, "kill-server")
	cmd.Env = fx.baseEnv
	_ = cmd.Run()
}

type recorder struct {
	ptmx *os.File
	cmd  *exec.Cmd
	emu  *vt.SafeEmulator
	cols int
	rows int
	mu   sync.Mutex
	buf  []byte
}

func startAttachRecorder(fx *fixture, session string, cols, rows int) (*recorder, error) {
	cmd := exec.Command(fx.bmuxBin, "attach", session)
	cmd.Env = fx.baseEnv
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		return nil, err
	}
	emu := vt.NewSafeEmulator(cols, rows)
	rec := &recorder{
		ptmx: ptmx,
		cmd:  cmd,
		emu:  emu,
		cols: cols,
		rows: rows,
	}

	// Drain VT responses back to the PTY
	go func() {
		b := make([]byte, 4096)
		for {
			n, err := emu.Read(b)
			if n > 0 {
				_, _ = ptmx.Write(b[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	// Stream PTY output into the VT emulator
	go func() {
		b := make([]byte, 16384)
		for {
			n, err := ptmx.Read(b)
			if n > 0 {
				rec.mu.Lock()
				_, _ = emu.Write(b[:n])
				rec.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()

	return rec, nil
}

func (r *recorder) send(s string) error {
	_, err := r.ptmx.WriteString(s)
	return err
}

func (r *recorder) appendFrame() {
	r.mu.Lock()
	defer r.mu.Unlock()

	pos := r.emu.CursorPosition()
	var hdr [4]byte
	binary.LittleEndian.PutUint16(hdr[0:2], uint16(int16(pos.X)))
	binary.LittleEndian.PutUint16(hdr[2:4], uint16(int16(pos.Y)))
	r.buf = append(r.buf, hdr[:]...)

	var cellBuf [14]byte
	for y := 0; y < r.rows; y++ {
		for x := 0; x < r.cols; x++ {
			cell := r.emu.CellAt(x, y)
			var cp uint32
			var fg, bg uint32
			var attrs uint8
			var w uint8 = 1
			if cell != nil {
				for _, ch := range cell.Content {
					cp = uint32(ch)
					break
				}
				fg = uint32(vt.ColorFromStd(cell.Style.Fg))
				bg = uint32(vt.ColorFromStd(cell.Style.Bg))
				if cell.Style.Attrs&uv.AttrBold != 0 {
					attrs |= 1 << 0
				}
				if cell.Style.Attrs&uv.AttrFaint != 0 {
					attrs |= 1 << 1
				}
				if cell.Style.Attrs&uv.AttrItalic != 0 {
					attrs |= 1 << 2
				}
				if cell.Style.Underline != uv.UnderlineNone {
					attrs |= 1 << 3
				}
				if cell.Style.Attrs&uv.AttrReverse != 0 {
					attrs |= 1 << 4
				}
				w = uint8(cell.Width)
			}
			binary.LittleEndian.PutUint32(cellBuf[0:4], cp)
			binary.LittleEndian.PutUint32(cellBuf[4:8], fg)
			binary.LittleEndian.PutUint32(cellBuf[8:12], bg)
			cellBuf[12] = attrs
			cellBuf[13] = w
			r.buf = append(r.buf, cellBuf[:]...)
		}
	}
}

func (r *recorder) writeFile(path string, fps uint16) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var hdr [6]byte
	binary.LittleEndian.PutUint16(hdr[0:2], uint16(r.cols))
	binary.LittleEndian.PutUint16(hdr[2:4], uint16(r.rows))
	binary.LittleEndian.PutUint16(hdr[4:6], fps)
	if _, err := f.Write(hdr[:]); err != nil {
		return err
	}
	_, err = f.Write(r.buf)
	return err
}

func (r *recorder) close() {
	_ = r.send("\x01d") // detach cleanly
	time.Sleep(150 * time.Millisecond)
	_ = r.ptmx.Close()
	_ = r.cmd.Process.Kill()
	_, _ = r.cmd.Process.Wait()
}

func captureScenePNG(fx *fixture, renderer, theme, scene, outPath string) error {
	session := "docs"
	if scene == "workspace" || scene == "key-help" {
		session = "api"
	}

	rec, err := startAttachRecorder(fx, session, 104, 32)
	if err != nil {
		return err
	}
	defer rec.close()

	// Allow initial attach + pane render to settle
	time.Sleep(850 * time.Millisecond)

	switch scene {
	case "workspace":
		time.Sleep(350 * time.Millisecond)
	case "session-picker":
		must(rec.send("\x01s"))
		time.Sleep(450 * time.Millisecond)
		must(rec.send("/api\r"))
		time.Sleep(450 * time.Millisecond)
	case "project-picker":
		must(rec.send("\x01n"))
		time.Sleep(550 * time.Millisecond)
	case "scrollback-search":
		must(rec.send("\x01/"))
		time.Sleep(350 * time.Millisecond)
		must(rec.send("timeout\r"))
		time.Sleep(450 * time.Millisecond)
	case "key-help":
		must(rec.send("\x01?"))
		time.Sleep(450 * time.Millisecond)
	default:
		return fmt.Errorf("unknown scene %q", scene)
	}

	rec.appendFrame()
	framesPath := filepath.Join(fx.root, fmt.Sprintf("%s.bin", scene))
	if err := rec.writeFile(framesPath, 30); err != nil {
		return err
	}

	cmd := exec.Command("python3", renderer,
		"--frames", framesPath,
		"--mode", "png",
		"--theme", theme,
		"--out", outPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("render_media.py: %w\n%s", err, out)
	}
	fmt.Print(string(out))
	return nil
}

func captureDemo(fx *fixture, renderer, theme, outDir string) error {
	rec, err := startAttachRecorder(fx, "api", 120, 34)
	if err != nil {
		return err
	}
	defer rec.close()

	time.Sleep(900 * time.Millisecond)

	const fps = 30
	tick := time.Second / fps
	frames := 0
	recordFor := func(d time.Duration) {
		end := time.Now().Add(d)
		for time.Now().Before(end) {
			rec.appendFrame()
			frames++
			time.Sleep(tick)
		}
	}
	markChapter := func(id string) {
		fmt.Printf("CHAPTER %s %.2f\n", id, float64(frames)/float64(fps))
	}

	// Chapter 1: Open project cleanly with Ctrl-a n
	markChapter("projects")
	recordFor(1600 * time.Millisecond)
	must(rec.send("\x01n"))
	recordFor(1400 * time.Millisecond)
	for _, ch := range "cli" {
		must(rec.send(string(ch)))
		recordFor(140 * time.Millisecond)
	}
	recordFor(800 * time.Millisecond)
	must(rec.send("\r"))
	recordFor(1800 * time.Millisecond)

	// Chapter 2: Focus sidebar (Ctrl-h), move into AGENTS and jump to blocked agy in api-auth worktree
	markChapter("sidebar")
	must(rec.send("\x08"))
	recordFor(600 * time.Millisecond)
	for i := 0; i < 3; i++ {
		must(rec.send("j"))
		recordFor(260 * time.Millisecond)
	}
	recordFor(800 * time.Millisecond)
	must(rec.send("\r"))
	recordFor(1200 * time.Millisecond)

	// Chapter 3: Approve blocked prompt (Enter) -> sidebar updates live from blocked to working
	markChapter("approve")
	recordFor(700 * time.Millisecond)
	must(rec.send("\r"))
	recordFor(2500 * time.Millisecond)

	// Chapter 4: Session picker with live preview (Ctrl-a s) back to api
	markChapter("sessions")
	must(rec.send("\x01s"))
	recordFor(900 * time.Millisecond)
	must(rec.send("j"))
	recordFor(900 * time.Millisecond)
	must(rec.send("k"))
	recordFor(1000 * time.Millisecond)
	must(rec.send("\r"))
	recordFor(1000 * time.Millisecond)

	framesPath := filepath.Join(fx.root, "demo.bin")
	if err := rec.writeFile(framesPath, fps); err != nil {
		return err
	}

	cmd := exec.Command("python3", renderer,
		"--frames", framesPath,
		"--mode", "video",
		"--theme", theme,
		"--out-dir", outDir,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("render_media.py video: %w\n%s", err, out)
	}
	fmt.Print(string(out))
	return nil
}

func cleanEnv(overrides map[string]string) []string {
	envMap := make(map[string]string)
	for _, entry := range os.Environ() {
		k, v, ok := strings.Cut(entry, "=")
		if ok && !strings.HasPrefix(k, "BMUX_") {
			envMap[k] = v
		}
	}
	for k, v := range overrides {
		envMap[k] = v
	}
	out := make([]string, 0, len(envMap))
	for k, v := range envMap {
		out = append(out, k+"="+v)
	}
	return out
}

func must(err error) {
	if err != nil {
		fatalf("%v", err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
