package display

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/mattn/go-runewidth"

	"which-ports/internal/scanner"
)

const (
	bannerWidth = 37
	labelWidth  = 16
)

var escSeqRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func visibleWidth(s string) int {
	clean := escSeqRe.ReplaceAllString(s, "")
	return runewidth.StringWidth(clean)
}

func pad(s string, w int) string {
	gap := w - visibleWidth(s)
	if gap <= 0 {
		return s
	}
	return s + strings.Repeat(" ", gap)
}

func trunc(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

var (
	dim    = color.New(color.FgHiBlack)
	accent = color.New(color.FgCyan, color.Bold)
	normal = color.New(color.FgWhite)
	strong = color.New(color.FgWhite, color.Bold)
	link   = color.New(color.FgCyan)
	warn   = color.New(color.FgYellow)
	ok     = color.New(color.FgGreen)
	danger = color.New(color.FgRed)
	loc    = color.New(color.FgBlue)
	branch = color.New(color.FgMagenta)
)

var techColors = map[string]*color.Color{
	"Next.js":      color.New(color.FgWhite, color.Bold),
	"Nuxt":         color.New(color.FgGreen),
	"Vue.js":       color.New(color.FgGreen),
	"React":        color.New(color.FgCyan),
	"Remix":        color.New(color.FgHiBlack, color.BgWhite),
	"Astro":        color.New(color.FgHiBlack),
	"Vite":         color.New(color.FgMagenta, color.Bold),
	"Angular":      color.New(color.FgRed, color.Bold),
	"Svelte":       color.New(color.FgRed),
	"Gatsby":       color.New(color.FgMagenta),
	"Hugo":         color.New(color.FgRed),
	"Jekyll":       color.New(color.FgRed),
	"Rails":        color.New(color.FgRed),
	"Sinatra":      color.New(color.FgRed, color.Bold),
	"Django":       color.New(color.FgGreen),
	"Flask":        color.New(color.FgWhite, color.Bold),
	"FastAPI":      color.New(color.FgGreen),
	"Node.js":      color.New(color.FgGreen, color.Bold),
	"Python":       color.New(color.FgBlue),
	"Ruby":         color.New(color.FgRed),
	"Go":           color.New(color.FgCyan, color.Bold),
	"Rust":         color.New(color.FgHiBlack, color.Bold),
	"Java":         color.New(color.FgRed),
	"Spring":       color.New(color.FgGreen),
	"PostgreSQL":   color.New(color.FgBlue, color.Bold),
	"MongoDB":      color.New(color.FgGreen, color.Bold),
	"Redis":        color.New(color.FgRed, color.Bold),
	"MySQL":        color.New(color.FgBlue),
	"SQLite":       color.New(color.FgBlue),
	"Docker":       color.New(color.FgCyan, color.Bold),
	"LocalStack":   color.New(color.FgYellow),
	"nginx":        color.New(color.FgGreen),
	"Apache":       color.New(color.FgRed),
	"PHP":          color.New(color.FgMagenta),
	"Deno":         color.New(color.FgYellow),
	".NET":         color.New(color.FgBlue),
	"Elixir":       color.New(color.FgMagenta),
	"Erlang":       color.New(color.FgRed),
}

func techLabel(framework string) string {
	if framework == "" {
		return dim.Sprint("—")
	}
	if c, ok := techColors[framework]; ok {
		return c.Sprint(framework)
	}
	return normal.Sprint(framework)
}

func healthLabel(h scanner.PortHealth) string {
	switch h {
	case scanner.PortHealthy:
		return ok.Sprint("●") + " " + ok.Sprint("healthy")
	case scanner.PortOrphaned:
		return warn.Sprint("●") + " " + warn.Sprint("orphaned")
	case scanner.PortZombie:
		return danger.Sprint("●") + " " + danger.Sprint("zombie")
	default:
		return dim.Sprint("●") + " " + dim.Sprint("unknown")
	}
}

func cpuLabel(pct float64) string {
	if pct > 25.0 {
		return danger.Sprintf("%.1f", pct)
	}
	if pct > 5.0 {
		return warn.Sprintf("%.1f", pct)
	}
	return ok.Sprintf("%.1f", pct)
}

func sectionHeader(title string) string {
	return accent.Sprint("  "+title) + "\n" + dim.Sprint(strings.Repeat("  ─", 22))
}

// Table is a builder for rendering ASCII tables with proper ANSI-aware column widths.
type Table struct {
	headers []string
	rows    [][]string
	widths  []int
}

func NewTable() *Table {
	return &Table{}
}

func (t *Table) Headers(cols ...string) *Table {
	t.headers = cols
	for i, h := range cols {
		t.updateWidth(i, visibleWidth(h))
	}
	return t
}

func (t *Table) Row(cells ...string) *Table {
	t.rows = append(t.rows, cells)
	for i, c := range cells {
		t.updateWidth(i, visibleWidth(c))
	}
	return t
}

func (t *Table) updateWidth(i, w int) {
	for len(t.widths) <= i {
		t.widths = append(t.widths, 0)
	}
	if w > t.widths[i] {
		t.widths[i] = w
	}
}

func (t *Table) Render(w io.Writer) {
	cols := len(t.widths)

	hline := func(l, sep, r string) string {
		var parts []string
		for i := 0; i < cols; i++ {
			parts = append(parts, strings.Repeat("─", t.widths[i]+2))
		}
		return l + strings.Join(parts, sep) + r
	}

	rowLine := func(cells []string) string {
		var parts []string
		for i := 0; i < cols; i++ {
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			parts = append(parts, " "+pad(cell, t.widths[i])+" ")
		}
		return dim.Sprint("│") + strings.Join(parts, dim.Sprint("│")) + dim.Sprint("│")
	}

	fmt.Fprintln(w, dim.Sprint(hline("┌", "┬", "┐")))
	fmt.Fprintln(w, rowLine(t.headers))
	fmt.Fprintln(w, dim.Sprint(hline("├", "┼", "┤")))
	for _, row := range t.rows {
		fmt.Fprintln(w, rowLine(row))
	}
	fmt.Fprintln(w, dim.Sprint(hline("└", "┴", "┘")))
}

// Renderer writes output to an io.Writer, enabling testing and writer injection.
type Renderer struct {
	w io.Writer
}

func (r *Renderer) printHeader() {
	title := "  🔊 which-ports"
	sub := "  listening to your ports..."
	border := strings.Repeat("─", bannerWidth)
	fmt.Fprintln(r.w)
	fmt.Fprintln(r.w, accent.Sprintf(" ┌%s┐", border))
	fmt.Fprintln(r.w, accent.Sprint(" │")+strong.Sprint(pad(title, bannerWidth))+accent.Sprint("│"))
	fmt.Fprintln(r.w, accent.Sprint(" │")+dim.Sprint(pad(sub, bannerWidth))+accent.Sprint("│"))
	fmt.Fprintln(r.w, accent.Sprintf(" └%s┘", border))
	fmt.Fprintln(r.w)
}

func (r *Renderer) fieldLine(label, value string) {
	fmt.Fprintf(r.w, "  %s %s\n", dim.Sprint(pad(label, labelWidth)), value)
}

func (r *Renderer) portTable(ports []scanner.PortEntry, filtered bool) {
	r.printHeader()

	if len(ports) == 0 {
		if filtered {
			fmt.Fprintln(r.w, dim.Sprint("  No dev ports found. Run with --all to show system services."))
		} else {
			fmt.Fprintln(r.w, dim.Sprint("  No ports are listening."))
		}
		fmt.Fprintln(r.w)
		return
	}

	table := NewTable().
		Headers("PORT", "PROCESS", "PID", "PROJECT", "FRAMEWORK", "UPTIME", "STATUS")

	for _, p := range ports {
		table.Row(
			link.Sprintf(":%d", p.Port),
			normal.Sprint(trunc(p.ProcessName, 15)),
			normal.Sprintf("%d", p.PID),
			loc.Sprint(trunc(p.ProjectName, 20)),
			techLabel(p.Framework),
			warn.Sprint(p.Uptime),
			healthLabel(p.Health),
		)
	}

	table.Render(r.w)

	fmt.Fprintln(r.w)
	hint := fmt.Sprintf("  %d port%s active", len(ports), plural(len(ports)))
	if filtered {
		hint += "  ·  Run " + link.Sprint("ports <number>") + " for details  ·  " + link.Sprint("--all") + " to show everything"
	} else {
		hint += "  ·  Run " + link.Sprint("ports <number>") + " for details"
	}
	fmt.Fprintln(r.w, hint)
	fmt.Fprintln(r.w)
}

func (r *Renderer) procTable(procs []scanner.ProcessRecord, filtered bool) {
	r.printHeader()

	if len(procs) == 0 {
		if filtered {
			fmt.Fprintln(r.w, dim.Sprint("  No dev processes found. Run with --all to show system processes."))
		} else {
			fmt.Fprintln(r.w, dim.Sprint("  No processes running."))
		}
		fmt.Fprintln(r.w)
		return
	}

	table := NewTable().
		Headers("PID", "PROCESS", "CPU%", "MEM", "PROJECT", "FRAMEWORK", "UPTIME", "WHAT")

	for _, p := range procs {
		table.Row(
			normal.Sprintf("%d", p.PID),
			normal.Sprint(trunc(p.ProcessName, 15)),
			cpuLabel(p.CPUPct),
			normal.Sprint(p.MemUsage),
			loc.Sprint(trunc(p.ProjectName, 20)),
			techLabel(p.Framework),
			warn.Sprint(p.Uptime),
			normal.Sprint(trunc(p.Summary, 30)),
		)
	}

	table.Render(r.w)

	fmt.Fprintln(r.w)
	hint := fmt.Sprintf("  %d process%s", len(procs), plural(len(procs)))
	if filtered {
		hint += "  ·  " + link.Sprint("--all") + " to show everything"
	}
	fmt.Fprintln(r.w, hint)
	fmt.Fprintln(r.w)
}

func (r *Renderer) portDetail(info *scanner.PortEntry) {
	if info == nil {
		fmt.Fprintln(r.w, danger.Sprint("\n  ✕ Port not found\n"))
		return
	}

	fmt.Fprintln(r.w)
	fmt.Fprintln(r.w, strong.Sprintf("  Port %d", info.Port))
	fmt.Fprintln(r.w, dim.Sprint(strings.Repeat("  ─", 22)))
	fmt.Fprintln(r.w)

	r.fieldLine("Process", normal.Sprint(info.ProcessName))
	r.fieldLine("PID", normal.Sprintf("%d", info.PID))
	r.fieldLine("Status", healthLabel(info.Health))
	r.fieldLine("Framework", techLabel(info.Framework))
	r.fieldLine("Memory", normal.Sprint(info.MemUsage))
	r.fieldLine("Uptime", warn.Sprint(info.Uptime))
	r.fieldLine("Started", normal.Sprintf("%s", info.StartedAt.Format("2006-01-02 15:04:05")))

	fmt.Fprintln(r.w)
	fmt.Fprintln(r.w, sectionHeader("Location"))
	fmt.Fprintln(r.w)

	r.fieldLine("Directory", loc.Sprint(info.WorkDir))
	r.fieldLine("Project", loc.Sprint(info.ProjectName))
	if info.GitBranch != "" {
		r.fieldLine("Branch", branch.Sprint(info.GitBranch))
	}

	if len(info.Ancestors) > 0 {
		fmt.Fprintln(r.w)
		fmt.Fprintln(r.w, sectionHeader("Process Tree"))
		fmt.Fprintln(r.w)
		for i, p := range info.Ancestors {
			prefix := strings.Repeat("  ", i)
			sym := "→"
			if i > 0 {
				sym = "└─"
			}
			if p.PID == info.PID {
				fmt.Fprintf(r.w, "%s  %s %s\n", prefix, sym, strong.Sprintf("PID %d — %s", p.PID, p.Name))
			} else {
				fmt.Fprintf(r.w, "%s  %s %s\n", prefix, sym, dim.Sprintf("PID %d — %s", p.PID, p.Name))
			}
		}
	}

	fmt.Fprintln(r.w)
	fmt.Fprintf(r.w, "  %s\n\n", dim.Sprintf("Kill: %s %d", link.Sprint("ports kill"), info.Port))
}

func (r *Renderer) cleanSummary(stale []scanner.PortEntry, killed, failed []int) {
	r.printHeader()

	killedSet := make(map[int]bool)
	for _, p := range killed {
		killedSet[p] = true
	}
	failedSet := make(map[int]bool)
	for _, p := range failed {
		failedSet[p] = true
	}

	if len(stale) == 0 {
		fmt.Fprintln(r.w, ok.Sprint("  ✓ No orphaned processes found\n"))
		return
	}

	fmt.Fprintln(r.w, warn.Sprintf("  Found %d orphaned/zombie process%s:\n", len(stale), plural(len(stale))))
	for _, p := range stale {
		var symbol, status string
		if killedSet[p.PID] {
			symbol, status = ok.Sprint("✓"), ok.Sprint("killed")
		} else if failedSet[p.PID] {
			symbol, status = danger.Sprint("✕"), danger.Sprint("failed")
		} else {
			symbol, status = dim.Sprint("?"), dim.Sprint("not attempted")
		}

		fmt.Fprintf(r.w, "  %s :%d — %s %s [%s]\n",
			symbol,
			p.Port,
			p.ProcessName,
			dim.Sprintf("(PID %d)", p.PID),
			status,
		)
	}
	fmt.Fprintln(r.w)
}

func (r *Renderer) portEvent(event string, entry scanner.PortEntry) {
	ts := time.Now().Format("15:04:05")

	if event == "new" {
		label := entry.ProcessName
		if entry.Framework != "" {
			label += " (" + entry.Framework + ")"
		}
		extra := ""
		if entry.ProjectName != "" && entry.ProjectName != "." && entry.ProjectName != "/" {
			extra = " [" + entry.ProjectName + "]"
		}
		fmt.Fprintf(r.w, "  %s %s  :%d — %s%s\n",
			ok.Sprint("▲ NEW"),
			dim.Sprint(ts),
			entry.Port,
			label,
			extra,
		)
	} else if event == "removed" {
		fmt.Fprintf(r.w, "  %s %s  :%d\n",
			danger.Sprint("▼ CLOSED"),
			dim.Sprint(ts),
			entry.Port,
		)
	}
}

func (r *Renderer) watchBanner() {
	r.printHeader()
	fmt.Fprintln(r.w, link.Sprint("  Watching for port changes...  Press Ctrl+C to stop.\n"))
}

// PrintPortTable renders the main port-listing table.
func PrintPortTable(ports []scanner.PortEntry, filtered bool) {
	(&Renderer{w: os.Stdout}).portTable(ports, filtered)
}

// PrintProcTable renders the process-listing table.
func PrintProcTable(procs []scanner.ProcessRecord, filtered bool) {
	(&Renderer{w: os.Stdout}).procTable(procs, filtered)
}

// PrintPortDetail renders a single-port detail view.
func PrintPortDetail(info *scanner.PortEntry) {
	(&Renderer{w: os.Stdout}).portDetail(info)
}

// PrintCleanSummary renders the results of a cleanup pass.
func PrintCleanSummary(stale []scanner.PortEntry, killed, failed []int) {
	(&Renderer{w: os.Stdout}).cleanSummary(stale, killed, failed)
}

// PrintPortEvent renders a watch-mode event.
func PrintPortEvent(event string, entry scanner.PortEntry) {
	(&Renderer{w: os.Stdout}).portEvent(event, entry)
}

// PrintWatchBanner renders the watch-mode header.
func PrintWatchBanner() {
	(&Renderer{w: os.Stdout}).watchBanner()
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
