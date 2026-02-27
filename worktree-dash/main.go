package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
	"unsafe"

	"github.com/elvisnm/wt/internal/app"
	"github.com/elvisnm/wt/internal/terminal"

	tea "github.com/charmbracelet/bubbletea"
)

//go:embed guide.md
var guideContent string

//go:embed heihei.mp3
var heiHeiAudio []byte

var version = "dev"

var subcommands = map[string]string{
	"init":         "workflow-init.js",
	"create":       "dc-create.js",
	"up":           "dc-worktree-up.js",
	"down":         "dc-worktree-down.js",
	"status":       "dc-status.js",
	"info":         "dc-info.js",
	"logs":         "dc-logs.js",
	"bash":         "dc-bash.js",
	"restart":      "dc-restart.js",
	"seed":         "dc-seed.js",
	"exec":         "dc-exec.js",
	"admin":        "dc-admin.js",
	"lan":            "dc-lan.js",
	"skip-worktree":  "dc-skip-worktree.js",
	"prune":        "dc-prune.js",
	"autostop":     "dc-autostop.js",
	"rebuild-base": "dc-rebuild-base.js",
	"service":      "dc-service.js",
	"build":        "dc-build.js",
	"images-fix":   "dc-images-fix.js",
	"menu":         "dc.js",
}

func main() {
	if len(os.Args) < 2 {
		launchDashboard()
		return
	}

	cmd := os.Args[1]

	switch cmd {
	case "help", "--help", "-h":
		printHelp()
	case "version", "--version":
		fmt.Println("wt", version)
	case "_guide":
		runGuide()
	case "_help":
		runHelp()
	case "_heihei":
		if len(os.Args) < 3 {
			os.Exit(1)
		}
		runHeiHei(os.Args[2])
	default:
		script, ok := subcommands[cmd]
		if !ok {
			fmt.Fprintf(os.Stderr, "wt: unknown command %q\n\nRun 'wt help' for usage.\n", cmd)
			os.Exit(1)
		}
		runScript(script, os.Args[2:])
	}
}

func launchDashboard() {
	if err := terminal.CheckTmux(); err != nil {
		fmt.Fprintf(os.Stderr, "wt: %v\n", err)
		os.Exit(1)
	}

	// Inner mode: the bubbletea app runs inside tmux pane 0
	if os.Getenv("WT_INNER") == "1" {
		launchDashboardInner()
		return
	}

	// Outer mode: create tmux layout and attach the user
	launchDashboardOuter()
}

func launchDashboardOuter() {
	// Show splash immediately — stays visible until dashboard is fully loaded
	stopSplash := showSplash()

	tw, th := termSize()
	ts := terminal.NewTmuxServer()
	if err := ts.EnsureStarted(tw, th); err != nil {
		stopSplash()
		restoreTerm()
		fmt.Fprintf(os.Stderr, "wt: %v\n", err)
		os.Exit(1)
	}

	// Resolve executable path first — needed for both pane layout and inner process
	exe_path, err := os.Executable()
	if err != nil {
		ts.Kill()
		stopSplash()
		restoreTerm()
		fmt.Fprintf(os.Stderr, "wt: cannot determine executable path: %v\n", err)
		os.Exit(1)
	}
	exe_path, err = filepath.EvalSymlinks(exe_path)
	if err != nil {
		ts.Kill()
		stopSplash()
		restoreTerm()
		fmt.Fprintf(os.Stderr, "wt: cannot resolve symlinks: %v\n", err)
		os.Exit(1)
	}

	// Create 2-pane layout (left 20%, right 80%) — right pane shows welcome guide
	pl, err := terminal.SetupPaneLayout(ts, 20, exe_path)
	if err != nil {
		ts.Kill()
		stopSplash()
		restoreTerm()
		fmt.Fprintf(os.Stderr, "wt: %v\n", err)
		os.Exit(1)
	}

	// Configure key bindings (prefix=Ctrl+], prefix+q, prefix+f, prefix+1-9)
	pl.ConfigureBindings()

	// Disable tmux status bar — hints are rendered in the bubbletea status bar
	ts.Run("set-option", "-g", "status", "off")

	// Replace pane 0's shell with the inner process silently (no visible command echo)
	ts.Run("respawn-pane", "-t", "wt:0.0", "-k",
		fmt.Sprintf("WT_INNER=1 WT_SOCKET=%s exec %s", ts.Socket(), exe_path),
	)

	// Block until the inner process signals that discovery is complete.
	// The splash stays visible during this entire wait.
	ts.Run("wait-for", "wt-ready")

	// Stop splash goroutine and wait for it to exit — guarantees no more
	// ANSI cursor-position writes to stdout that could leak into tmux.
	stopSplash()

	// Exit alt screen from splash, then clear. tmux attach will enter
	// its own alt screen cleanly from a known terminal state.
	fmt.Print("\033[?1049l\033[2J\033[H")

	// Attach — tmux takes over the terminal
	cmd := exec.Command("tmux", "-L", ts.Socket(), "attach-session", "-t", "wt")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()

	// Clean up: kill tmux server, then fully reset the terminal.
	// \033c (RIS) resets all modes tmux may have left behind (alternate charset,
	// bracketed paste, mouse reporting, scroll regions, etc.)
	ts.Kill()
	fmt.Print("\033c")
}

func launchDashboardInner() {
	socket := os.Getenv("WT_SOCKET")
	if socket == "" {
		fmt.Fprintf(os.Stderr, "wt: WT_SOCKET not set (inner mode requires it)\n")
		os.Exit(1)
	}

	ts := terminal.ConnectTmuxServer(socket)
	pl := terminal.NewPaneLayout(ts)

	m := app.NewModelWithLayout(ts, pl)
	m.SetHeiHeiAudio(heiHeiAudio)

	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Detach the user's terminal cleanly (avoids "[server exited]" message)
	ts.Run("detach-client", "-s", "wt")
	// Then kill the server (this terminates our own process too)
	ts.Run("kill-server")
}

func findScriptsDir() (string, error) {
	// 1. Explicit env var override
	if dir := os.Getenv("WT_SCRIPTS_DIR"); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir, nil
		}
		return "", fmt.Errorf("WT_SCRIPTS_DIR=%q does not exist or is not a directory", dir)
	}

	// Resolve the binary's real location (follows symlinks)
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot determine executable path: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("cannot resolve symlinks: %w", err)
	}
	binDir := filepath.Dir(exe)

	// 2. Homebrew layout: <prefix>/share/wt/worktree-flow/
	brewPath := filepath.Join(binDir, "..", "share", "wt", "worktree-flow")
	if info, err := os.Stat(brewPath); err == nil && info.IsDir() {
		return brewPath, nil
	}

	// 3. Dev layout: <repo>/worktree-dash/../worktree-flow/
	devPath := filepath.Join(binDir, "..", "worktree-flow")
	if info, err := os.Stat(devPath); err == nil && info.IsDir() {
		return devPath, nil
	}

	return "", fmt.Errorf("cannot find worktree-flow scripts directory\n\nLooked in:\n  %s\n  %s\n\nSet WT_SCRIPTS_DIR to override.", brewPath, devPath)
}

func runScript(script string, args []string) {
	scriptsDir, err := findScriptsDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "wt: %v\n", err)
		os.Exit(1)
	}

	scriptPath := filepath.Join(scriptsDir, script)
	if _, err := os.Stat(scriptPath); err != nil {
		fmt.Fprintf(os.Stderr, "wt: script not found: %s\n", scriptPath)
		os.Exit(1)
	}

	// Find node binary
	nodePath, err := exec.LookPath("node")
	if err != nil {
		fmt.Fprintf(os.Stderr, "wt: node not found in PATH\n")
		os.Exit(1)
	}

	// Build argv: node <script> [args...]
	argv := make([]string, 0, 2+len(args))
	argv = append(argv, "node", scriptPath)
	argv = append(argv, args...)

	// Replace the process with node (no child process overhead)
	if err := syscall.Exec(nodePath, argv, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "wt: exec failed: %v\n", err)
		os.Exit(1)
	}
}

// heiHeiArt is the raw ASCII art of HeiHei from Moana, trimmed to content bounds.
var heiHeiArt = []string{
	`                                                                  .......`,
	`                                                                  .+#-......`,
	`                                                                 ..##.#+#+#+ ...`,
	`                                                                 .###-###+#+..-.`,
	`                                                                 .#####+++###+#....`,
	`                                                                 .#++++########-#.`,
	`                                                                 .##+++++++###+#+`,
	`                                                                  .##+++++++++##......`,
	`                                                                  .###+++++++##++#+..`,
	`                                                                   .##+++++++#+-++-`,
	`                                                                ...###+#++++##+-+#+...`,
	`                                                               .########+#++##-+#####+`,
	`                                                              .##+..#####++###-+##+###.`,
	`                                                             ..........##+++#++#+#+....`,
	`                                                            ............##+#++++#+---...`,
	`                                                           ..............#####++-........`,
	`                                                           .##...........#+++++#++-....##.`,
	`                                                          ..##..........########+-+-...##-`,
	`                                                           ............###+-..##++--+-....`,
	`                                                           ...........+##......+#+------..`,
	`                                                            .-.-++----###.+#--+-#.....-..`,
	`                                                            .##-...+++##+.++++++#+-.....`,
	`                                                            .-######--###########+#####`,
	`                                                             ..-#++######+####+##-##+-`,
	`                                                               ..+++-+###-----+##.+..`,
	`                                                                .-##-#+##-+++-####`,
	`                                                                ...+-#+##++++-#+#+`,
	`                                                                  .-+####-+++-#+##`,
	`                                                                  .+.##+-++++-###+`,
	`                                                                  .-+..-++++-.+#+.`,
	`                                                                   -++-++++++-+--`,
	`                                                                   .+-.++++---+--`,
	`                                                                   .-+.-#++-++-+..`,
	`                                                                  ..#--.+++++++++..`,
	`                               +                                 .-#+-#.++#++-+--+..`,
	`                    -.........--+++-.....                         ---.+.--++-+-.-+-#.`,
	`                 ..-+++++##+#+###++++++++++-...                   -++++++--..+--#+-+.`,
	`               ..-###########################---.                .#+#+.#.-#+-+---+--..`,
	`            ---####+..++---++-.--.+---+-++++###++-             ...+-#..#.-+-----.++++...`,
	`            -###----+##++++++++###++++++++##++###+.             .+#+-+#.-+--##-#++#.#+`,
	`           +##-+-...-..----..-............--++++##-.           ..+++.#+--+--------+++-.`,
	`          .#+-+-.                          .--+++##..         ..#+#++#.#-+--+-+-+++-++.`,
	`         .#++-.                               .++-+#+-.     .-+#+##-#-.#-#.-+-+.#.#++++.`,
	`        .#+.-                                  -++++##-    ..+#-#+.-#+++-#-.+---+#-+#+#+.`,
	`        +#..                                     .--+##-  .+##++#++##.++.#-.#+.+.#++#++#..`,
	`       -#+-                                       +#++#- .-+###+#+++##.#--#-.##.#.##++++++#.`,
	`       .#-                   ..........-     ....--######...-++++-##+-#++#.+#-.#+-#-#+####-##-`,
	`       ++-               ..-+#+######+++++-+###-.....+###...+###-##++#+++##.##--#+-#+++++++.#+.`,
	`       ++             .-+###+++#+-++#++++###++####-.--++-..#####+#+..#-#--#+-#+.+#--##-#+++++##+`,
	`      .+-           .-+#++++-.##    -+####+++++--+##+##..-#########+####+#++.+#+#++-#-+#+#+++.###-+.`,
	`      ...          .++##++.          ++##+++########+...#+###+#######+##+...#+++-#++-#-+#++++--###.#.`,
	`                  .+####.           -+########...+++#######+##++##############.#.+++.#.++++#+.###++#-`,
	`                 .++#++          ..-+#####-..####+-++++#+++.-++##########+#######.+-.#..#.++.###+-#+`,
	`                .-###. ..........-+##+##...##+#+-+#+-+#+++#++++-++########+#++##+###-#.+#+.+###++#+.`,
	`                -+#+-     .-+####+--+##.####-#-+++#+####+######+-++-####+++##+##+++++############+.`,
	`               .++#-        .....-+###.##-#+#-+###+####+##++######+#+-+--+-#####+-++##+###+##+...`,
	`               -#-                 ##.#+..#+#+##..###+##+##+###########++############++##+....`,
	`              .+-.                -##.#...#+##..+##+###+###+#+--++#########++++##+##+##-.`,
	`             .+-.                .+##-+...#+#.+#######+#+##++#-.-..++###+++++##+#####-.`,
	`             +-.                .-##-+-...###+######+###-###++###+-#++#####+#+####+..`,
	`            -+.                .-###+#...+####.-######+########++--+-++##+#######-`,
	`           ...                .+####.#...#####-.###+.#+######+#+#####+#+########+.`,
	`           .                 +#+-- .+..+#####+..#######-+#---+#+--++-++########+.`,
	`                           ..--+  .-..  ####. ####+ +####+   +####+##+  .#####+.`,
	`                                 -...-  ###-  -#++  #####+   -++-++++.  .#####.`,
	`                                ....   -+##+  +###  +##+--   -###+-++   .####+.`,
	`                               ...     ###.         -#...    -###+++     -###-`,
	`                                       ...          .##       #####.     .+++.`,
	`                                                     ..       .--..      .++-.`,
	`                                                              .++#+      .###.`,
	`                                                              .-...      .+++.`,
	`                                                              .###.      .+++.`,
	`                                                              .-.-       .+++.`,
	`                                                              .--+       .+++.`,
	`                                                              .-+#.      .+--.`,
	`                                                              .-++.      .++.+.`,
	`                                                             .-.++.     .-+.-+..`,
	`                                                            ....++-.... .#-.---#..`,
	`                                                           ..-..+#+....---..+-....................`,
	`                                                          ..--.+.+#-+++--......#+-.....-+-+.--+-.--.`,
	`                                                       .......--.+##---.# .-#####...----++###+....-...`,
	`                                       ....................-+--..+++...   ......-.  ....-++...#+....`,
	`                                     ...+-++#######+###++#+++++##+-#       ......      ........`,
	`                                    ....---...++..-++--.... .--..+-.`,
	`                                       ......+..........    ........`,
}

var splashQuotes = []string{
	"It works on my machine.",
	"// TODO: fix this later",
	"git push --force and pray.",
	"There are 10 types of people in the world...",
	"Mass-assign responsibly.",
	"sudo make me a sandwich.",
	"The cloud is just someone else's computer.",
	"Debugging: being the detective in a crime movie where you are also the murderer.",
	"99 little bugs in the code, take one down, patch it around... 127 little bugs in the code.",
	"Works on my container.",
	"Have you tried turning it off and on again?",
	"It's not a bug, it's an undocumented feature.",
	"In case of fire: git commit, git push, leave building.",
	"A SQL query walks into a bar, sees two tables and asks... can I join you?",
	"Weeks of coding can save you hours of planning.",
	"There is no place like 127.0.0.1.",
	"!false -- it's funny because it's true.",
	"I don't always test my code, but when I do, I do it in production.",
	"Documentation is like sex: when it's good, it's very good. When it's bad, it's better than nothing.",
	"Mondays are fine. It's your job that sucks.",
	"Real programmers count from 0.",
	"Deleted code is debugged code.",
	"One does not simply mass-migrate to TypeScript.",
	"The best code is no code at all.",
	"Hire a lazy developer. They'll find the easiest way to do it.",
	"I have a joke about UDP but you might not get it.",
}

// showSplash clears the screen and displays HeiHei (the chicken from Moana)
// scaled to 77% of the terminal, centered. Stays visible until the dashboard is fully loaded.
// Returns a function that stops the spinner and waits for the goroutine to exit.
func showSplash() func() {
	w, h := termSize()

	// Switch to alt screen, hide cursor, clear
	fmt.Print("\033[?1049h\033[?25l\033[2J")

	// Render the art
	art := renderHeiHeiArt(w, h)
	fmt.Print(art.output)

	// Render "Loading worktrees" spinner below the art
	spinFrames := []string{"\u280b", "\u2819", "\u2839", "\u2838", "\u283c", "\u2834", "\u2826", "\u2827", "\u2807", "\u280f"}
	label := "Loading worktrees"
	display := fmt.Sprintf("%s %s", spinFrames[0], label)
	label_row := art.startRow + art.height + 1
	label_col := (w - len(display)) / 2
	if label_col < 1 {
		label_col = 1
	}
	fmt.Printf("\033[%d;%dH\033[38;5;214m%s\033[0m", label_row, label_col, display)

	// Pick a random quote and render it below the spinner
	quote := fmt.Sprintf("\"%s\"", splashQuotes[time.Now().UnixNano()%int64(len(splashQuotes))])
	quote_row := label_row + 2
	quote_col := (w - len(quote)) / 2
	if quote_col < 1 {
		quote_col = 1
	}
	fmt.Printf("\033[%d;%dH\033[3;38;5;245m%s\033[0m", quote_row, quote_col, quote)

	// Animate spinner in a background goroutine
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
				time.Sleep(80 * time.Millisecond)
				i++
				frame := spinFrames[i%len(spinFrames)]
				fmt.Printf("\033[%d;%dH\033[38;5;214m%s\033[0m", label_row, label_col, frame)
			}
		}
	}()

	return func() {
		close(stop)
		<-done // wait for goroutine to actually exit — no more writes to stdout
	}
}

type heiHeiLayout struct {
	output   string
	startRow int
	height   int
}

// renderHeiHeiArt returns the HeiHei ASCII art scaled to 77% of the given
// terminal dimensions and centered, as a string ready to print plus layout metrics.
func renderHeiHeiArt(w, h int) heiHeiLayout {
	// Strip common leading whitespace to reduce effective width
	min_indent := 9999
	for _, line := range heiHeiArt {
		if len(line) == 0 {
			continue
		}
		indent := 0
		for _, ch := range line {
			if ch == ' ' {
				indent++
			} else {
				break
			}
		}
		if indent < len(line) && indent < min_indent {
			min_indent = indent
		}
	}

	trimmed := make([]string, len(heiHeiArt))
	art_w := 0
	for i, line := range heiHeiArt {
		if len(line) > min_indent {
			trimmed[i] = line[min_indent:]
		}
		if len(trimmed[i]) > art_w {
			art_w = len(trimmed[i])
		}
	}
	art_h := len(trimmed)

	// Target 77% of terminal dimensions
	target_w := int(float64(w) * 0.77)
	target_h := int(float64(h) * 0.77)

	// Scale to fit, preserving aspect ratio (no upscale)
	scale_x := float64(target_w) / float64(art_w)
	scale_y := float64(target_h) / float64(art_h)
	scale := scale_x
	if scale_y < scale {
		scale = scale_y
	}
	if scale > 1.0 {
		scale = 1.0
	}

	out_h := int(float64(art_h) * scale)
	out_w := int(float64(art_w) * scale)
	if out_h < 1 {
		out_h = 1
	}
	if out_w < 1 {
		out_w = 1
	}

	// Build scaled art via nearest-neighbor sampling
	scaled := make([]string, out_h)
	for y := 0; y < out_h; y++ {
		src_y := int(float64(y) / scale)
		if src_y >= art_h {
			src_y = art_h - 1
		}
		src_line := trimmed[src_y]
		buf := make([]byte, out_w)
		for x := 0; x < out_w; x++ {
			src_x := int(float64(x) / scale)
			if src_x < len(src_line) {
				buf[x] = src_line[src_x]
			} else {
				buf[x] = ' '
			}
		}
		scaled[y] = string(buf)
	}

	// Center vertically
	start_row := (h - out_h) / 2
	if start_row < 1 {
		start_row = 1
	}

	// Center horizontally
	start_col := (w - out_w) / 2
	if start_col < 1 {
		start_col = 1
	}

	var sb strings.Builder
	for i, line := range scaled {
		row := start_row + i
		if row > h {
			break
		}
		fmt.Fprintf(&sb, "\033[%d;%dH\033[38;5;240m%s\033[0m", row, start_col, line)
	}
	return heiHeiLayout{output: sb.String(), startRow: start_row, height: out_h}
}

// runHeiHei displays the HeiHei art and plays the scream audio clip.
// Exits when playback finishes. Re-renders on terminal resize.
const heiHeiSentinel = "wt-heihei-done"

type heiHeiScreenLayout struct {
	msgRow int
	msgCol int
}

func renderHeiHeiScreen(w, h int) heiHeiScreenLayout {
	art := renderHeiHeiArt(w, h)
	fmt.Print(art.output)

	// Orange message with spinner placeholder below the art
	msg := "  Now that you found the easter egg, you owe me a beer! - @elvisnm"
	msg_row := art.startRow + art.height + 2
	msg_col := (w - len(msg)) / 2
	if msg_col < 1 {
		msg_col = 1
	}
	fmt.Printf("\033[%d;%dH\033[38;5;214m%s\033[0m", msg_row, msg_col, msg)
	return heiHeiScreenLayout{msgRow: msg_row, msgCol: msg_col}
}

func runHeiHei(audioPath string) {
	w, h := termSize()
	fmt.Print("\033[?25l\033[2J") // hide cursor, clear
	sl := renderHeiHeiScreen(w, h)

	// Play audio in background
	player := exec.Command("afplay", audioPath)
	player.Start()

	// Re-render on resize
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGWINCH)

	done := make(chan struct{})
	go func() {
		player.Wait()
		close(done)
	}()

	// Animate spinner before the quote
	spinFrames := []string{"\u280b", "\u2819", "\u2839", "\u2838", "\u283c", "\u2834", "\u2826", "\u2827", "\u2807", "\u280f"}
	tick := time.NewTicker(80 * time.Millisecond)
	defer tick.Stop()
	i := 0

	for {
		select {
		case <-done:
			sentinel := filepath.Join(os.TempDir(), heiHeiSentinel)
			os.WriteFile(sentinel, []byte("0"), 0644)
			fmt.Print("\033[?25h")
			return
		case <-sig:
			w, h = termSize()
			fmt.Print("\033[2J")
			sl = renderHeiHeiScreen(w, h)
		case <-tick.C:
			i++
			frame := spinFrames[i%len(spinFrames)]
			fmt.Printf("\033[%d;%dH\033[38;5;214m%s\033[0m", sl.msgRow, sl.msgCol, frame)
		}
	}
}

// restoreTerm exits alt screen and restores the cursor.
func restoreTerm() {
	fmt.Print("\033[?25h\033[?1049l")
}

type winsize struct {
	Row, Col       uint16
	Xpixel, Ypixel uint16
}

func termSize() (int, int) {
	ws := &winsize{}
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL,
		uintptr(syscall.Stdout),
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(ws)),
	)
	w, h := int(ws.Col), int(ws.Row)
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	return w, h
}

// ANSI escape codes for guide styling — 256-color to match lipgloss palette.
const (
	ansiDim    = "\033[38;5;240m" // matches BorderColor (240)
	ansiBold   = "\033[1m"
	ansiCyan   = "\033[1;38;5;34m" // matches FocusBorderColor (34), bold
	ansiYellow = "\033[38;5;214m"  // matches HintColor (214)
	ansiReset  = "\033[0m"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// visLen returns the visible (display) length of a string, ignoring ANSI escapes.
func visLen(s string) int {
	return utf8.RuneCountInString(ansiRe.ReplaceAllString(s, ""))
}

func guidePadRight(s string, width int) string {
	pad := width - visLen(s)
	if pad < 0 {
		pad = 0
	}
	return s + strings.Repeat(" ", pad)
}

func guideCenterLine(s string, width int) string {
	pad := (width - visLen(s)) / 2
	if pad < 0 {
		pad = 0
	}
	return strings.Repeat(" ", pad) + s
}

func guideKey(k string) string {
	return ansiYellow + k + ansiReset
}

// helpBox renders a compact box (top padding only, no bottom padding).
func helpBox(title string, lines []string, width int) []string {
	// Temporarily wrap guideBox, then remove the second-to-last line (bottom padding)
	full := guideBox(title, lines, width)
	// Remove the empty line just before the bottom border
	if len(full) >= 3 {
		full = append(full[:len(full)-2], full[len(full)-1])
	}
	return full
}

// guideBox renders a bordered box with an optional title. Content is block-centered
// (the widest line determines the offset, all lines share the same left edge).
func guideBox(title string, lines []string, width int) []string {
	inner := width - 2
	var out []string

	if title != "" {
		t := " " + title + " "
		dashes := inner - 1 - len(t)
		if dashes < 1 {
			dashes = 1
		}
		out = append(out, ansiDim+"╭─"+ansiReset+ansiCyan+ansiBold+t+ansiReset+ansiDim+strings.Repeat("─", dashes)+"╮"+ansiReset)
	} else {
		out = append(out, ansiDim+"╭"+strings.Repeat("─", inner)+"╮"+ansiReset)
	}

	// Add padding lines at top and bottom
	lines = append([]string{""}, lines...)
	lines = append(lines, "")

	// Find widest line to center the block as a whole
	maxVis := 0
	for _, l := range lines {
		if vl := visLen(l); vl > maxVis {
			maxVis = vl
		}
	}
	blockPad := (inner - 2 - maxVis) / 2
	if blockPad < 0 {
		blockPad = 0
	}
	prefix := strings.Repeat(" ", blockPad)

	for _, l := range lines {
		padded := prefix + l
		out = append(out, ansiDim+"│"+ansiReset+" "+guidePadRight(padded, inner-2)+" "+ansiDim+"│"+ansiReset)
	}

	out = append(out, ansiDim+"╰"+strings.Repeat("─", inner)+"╯"+ansiReset)
	return out
}

// renderGuide renders the welcome guide with box-drawing characters, centered in the terminal.
func renderGuide() string {
	termWidth, termHeight := termSize()

	w := 56
	if termWidth < w+4 {
		w = termWidth - 4
	}

	var sections [][]string

	// Title
	sections = append(sections, []string{
		guideCenterLine(ansiBold+ansiCyan+"wt — Quick Start"+ansiReset, w),
		"",
	})

	// Worktree actions
	sections = append(sections, guideBox("Worktree", []string{
		"Select a worktree, then press:",
		"",
		guideKey("b") + "  bash shell       " + guideKey("c") + "  claude code",
		guideKey("l") + "  logs             " + guideKey("n") + "  create new",
	}, w))

	// Terminal
	sections = append(sections, guideBox("Terminal", []string{
		"Sessions open in the right pane.",
		"To get back:",
		"",
		guideKey("Ctrl+] q") + "  return to dashboard",
		guideKey("Ctrl+] f") + "  toggle fullscreen",
	}, w))

	// Tabs
	sections = append(sections, guideBox("Tabs", []string{
		"Each session becomes a tab.",
		"",
		guideKey("h") + " / " + guideKey("l") + "     prev / next tab",
		guideKey("1") + "-" + guideKey("9") + "       jump to tab N",
		guideKey("x") + "         close tab",
	}, w))

	// Panels
	sections = append(sections, guideBox("Panels", []string{
		ansiDim + "[" + ansiReset + " Worktrees " + ansiDim + "│" + ansiReset + " Services " + ansiDim + "│" + ansiReset + " Terminal " + ansiDim + "]" + ansiReset,
		"",
		guideKey("Tab") + " / " + guideKey("Shift+Tab") + "  cycle panels",
		"",
		guideKey("w") + " worktrees",
		guideKey("s") + " services      jump directly",
		guideKey("a") + " active tabs",
		guideKey("d") + " details",
	}, w))

	// More
	sections = append(sections, guideBox("More", []string{
		guideKey("Shift+D") + " database    " + guideKey("Shift+A") + " aws keys",
		guideKey("Shift+M") + " maintenance " + guideKey("Shift+X") + " admin",
		guideKey("Shift+L") + " LAN mode    " + guideKey("Shift+K") + " skip-worktree",
		ansiDim + strings.Repeat("─", 42) + ansiReset,
		guideKey("i") + " info  " + guideKey("r") + " restart  " + guideKey("u") + " start  " + guideKey("t") + " stop",
	}, w))

	// Footer
	sections = append(sections, []string{
		"",
		guideCenterLine(ansiDim+"Press "+ansiReset+guideKey("?")+ansiDim+" for all keybindings"+ansiReset, w),
	})

	// Flatten
	var lines []string
	for _, sec := range sections {
		lines = append(lines, sec...)
	}

	// Vertical centering
	contentHeight := len(lines)
	topPad := (termHeight - contentHeight) / 2
	if topPad < 1 {
		topPad = 1
	}

	// Horizontal centering
	leftPad := (termWidth - w) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	prefix := strings.Repeat(" ", leftPad)

	var result []string
	for i := 0; i < topPad; i++ {
		result = append(result, "")
	}
	for _, l := range lines {
		result = append(result, prefix+l)
	}

	return strings.Join(result, "\n")
}

// runGuide renders the welcome guide and re-renders on terminal resize (SIGWINCH).
// Exits when stdin is closed or the process is killed.
func runGuide() {
	// Initial render
	fmt.Print("\033[2J\033[H") // clear screen, cursor home
	fmt.Print(renderGuide())

	// Listen for SIGWINCH to re-render on resize
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGWINCH)

	for range sig {
		fmt.Print("\033[2J\033[H")
		fmt.Print(renderGuide())
	}
}

// renderHelp renders all keybindings with box-drawing characters, centered in the terminal.
func renderHelp() string {
	termWidth, termHeight := termSize()

	w := 56
	if termWidth < w+4 {
		w = termWidth - 4
	}

	var sections [][]string

	// Title
	sections = append(sections, []string{
		guideCenterLine(ansiBold+ansiCyan+"wt — Keybindings"+ansiReset, w),
		"",
	})

	// Navigation
	sections = append(sections, helpBox("Navigation", []string{
		guideKey("j") + " / " + guideKey("k") + "       navigate list",
		guideKey("<") + " / " + guideKey(">") + "       switch panel",
		guideKey("a/w/s/d") + "     jump to panel",
		guideKey("Tab") + "         next panel",
		guideKey("1") + "-" + guideKey("9") + "         jump to tab N",
		guideKey("Esc") + "         back / close",
	}, w))

	// Active Tabs
	sections = append(sections, helpBox("Active Tabs", []string{
		guideKey("Enter") + "       focus terminal",
		guideKey("h") + " / " + guideKey("l") + "       prev / next tab",
		guideKey("f") + "           fullscreen",
		guideKey("x") + "           close tab",
	}, w))

	// Worktrees
	sections = append(sections, helpBox("Worktrees", []string{
		guideKey("Enter") + "       action menu",
		guideKey("b") + "           bash shell",
		guideKey("c") + "           claude code",
		guideKey("z") + "           local shell (zsh)",
		guideKey("l") + "           logs",
		guideKey("n") + "           create worktree",
		guideKey("i") + "           info",
		guideKey("e") + "           esbuild watch",
		guideKey("r") + "           restart container",
		guideKey("u") + "           start container",
		guideKey("t") + "           stop container",
	}, w))

	// Services
	sections = append(sections, helpBox("Services", []string{
		guideKey("Enter") + "       preview logs",
		guideKey("l") + "           pin logs (tab)",
		guideKey("r") + "           restart service",
	}, w))

	// Operations
	sections = append(sections, helpBox("Operations", []string{
		guideKey("Shift+D") + "     database",
		guideKey("Shift+K") + "     skip-worktree toggle",
		guideKey("Shift+M") + "     maintenance",
		guideKey("Shift+A") + "     aws keys",
		guideKey("Shift+X") + "     admin toggle",
		guideKey("Shift+L") + "     LAN toggle",
	}, w))

	// Tmux
	sections = append(sections, helpBox("Tmux  (prefix = Ctrl+])", []string{
		guideKey("prefix+q") + "    return to dashboard",
		guideKey("prefix+f") + "    toggle fullscreen",
		guideKey("prefix+1-9") + "  jump to tab N",
	}, w))

	// Footer
	sections = append(sections, []string{
		"",
		guideCenterLine(ansiDim+"Press "+ansiReset+guideKey("Esc")+ansiDim+" to close"+ansiReset, w),
	})

	// Flatten
	var lines []string
	for _, sec := range sections {
		lines = append(lines, sec...)
	}

	// Vertical centering
	contentHeight := len(lines)
	topPad := (termHeight - contentHeight) / 2
	if topPad < 1 {
		topPad = 1
	}

	// Horizontal centering
	leftPad := (termWidth - w) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	prefix := strings.Repeat(" ", leftPad)

	var result []string
	for i := 0; i < topPad; i++ {
		result = append(result, "")
	}
	for _, l := range lines {
		result = append(result, prefix+l)
	}

	return strings.Join(result, "\n")
}

// runHelp renders keybindings and re-renders on resize. Exits when stdin is closed.
func runHelp() {
	fmt.Print("\033[2J\033[H")
	fmt.Print(renderHelp())

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGWINCH)

	for range sig {
		fmt.Print("\033[2J\033[H")
		fmt.Print(renderHelp())
	}
}

func printHelp() {
	fmt.Printf(`wt — Worktree Toolkit (%s)

Usage:
  wt                    Launch the interactive dashboard
  wt <command> [args]   Run a worktree command

Commands:
  init                  Initialize workflow.config.js for a project
  create                Interactive worktree creation wizard
  up [branch]           Create or start a worktree
  down [branch]         Stop or remove a worktree
  status                Show worktree status table
  info [branch]         Detailed worktree info
  logs [branch]         View Docker compose logs
  bash [branch]         Shell into a container
  restart [branch]      Restart containers
  build [branch]        Build containers
  exec [args]           Run a command in a container
  seed [branch]         Seed database
  admin [args]          Admin operations
  service [args]        Service management
  lan [branch]          LAN access setup
  skip-worktree [args]  Toggle skip-worktree flags
  prune                 Clean up old worktrees
  autostop              Stop idle worktrees
  rebuild-base          Rebuild base Docker image
  images-fix            Fix Docker images
  menu                  Launch interactive menu

Options:
  help, --help, -h      Show this help
  version, --version    Show version
`, version)
}
