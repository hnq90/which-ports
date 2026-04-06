package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fatih/color"

	whichports "which-ports"
	"which-ports/internal/display"
	"which-ports/internal/scanner"
)

var version = whichports.Version

func askYN(question string) bool {
	color.New(color.FgYellow).Print(question)
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	return strings.TrimSpace(strings.ToLower(answer)) == "y"
}

func main() {
	args := os.Args[1:]
	showAll := false
	jsonOut := false
	var filtered []string
	for _, a := range args {
		switch a {
		case "--version", "-v":
			fmt.Printf("which-ports %s\n", version)
			os.Exit(0)
		case "--all", "-a":
			showAll = true
		case "--json", "-j":
			jsonOut = true
		default:
			filtered = append(filtered, a)
		}
	}
	command := ""
	if len(filtered) > 0 {
		command = filtered[0]
	}
	os.Exit(dispatch(command, filtered, showAll, jsonOut))
}

func dispatch(command string, args []string, showAll, jsonOut bool) int {
	// Default: list dev ports (or everything with --all).
	if command == "" {
		ports := scanner.ScanPorts(false)
		if !showAll {
			var dev []scanner.PortEntry
			for _, p := range ports {
				if scanner.IsDevRuntime(p.ProcessName, p.Command) {
					dev = append(dev, p)
				}
			}
			ports = dev
		}
		if jsonOut {
			return emitJSON(ports)
		}
		display.PrintPortTable(ports, !showAll)
		return 0
	}

	// Numeric argument: inspect that port.
	if n, err := strconv.Atoi(command); err == nil {
		info := scanner.LookupPort(n)
		if jsonOut {
			return emitJSON(info)
		}
		display.PrintPortDetail(info)
		if info != nil && askYN(fmt.Sprintf("  Kill process on :%d? [y/N] ", n)) {
			if err := scanner.SignalProcess(info.PID, syscall.SIGTERM); err == nil {
				color.New(color.FgGreen).Printf("\n  ✓ Killed PID %d\n\n", info.PID)
			} else {
				color.New(color.FgRed).Printf("\n  ✕ Failed. Try: sudo kill -9 %d\n\n", info.PID)
			}
		}
		return 0
	}

	switch command {
	case "ps":
		procs := scanner.ScanProcesses()
		if !showAll {
			var dev []scanner.ProcessRecord
			for _, p := range procs {
				if scanner.IsDevRuntime(p.ProcessName, p.Command) {
					dev = append(dev, p)
				}
			}
			procs = groupDockerProcs(dev)
		}
		sort.Slice(procs, func(i, j int) bool { return procs[i].CPUPct > procs[j].CPUPct })
		if jsonOut {
			return emitJSON(procs)
		}
		display.PrintProcTable(procs, !showAll)

	case "open":
		if len(args) < 2 {
			color.New(color.FgRed).Print("\n  Usage: ports open <port>\n\n")
			return 1
		}
		n, err := strconv.Atoi(args[1])
		if err != nil {
			color.New(color.FgRed).Printf("\n  %q is not a valid port number\n\n", args[1])
			return 1
		}
		info := scanner.LookupPort(n)
		if info == nil {
			color.New(color.FgRed).Printf("\n  Nothing is listening on :%d\n\n", n)
			return 1
		}
		url := fmt.Sprintf("http://localhost:%d", n)
		label := info.ProcessName
		if info.Framework != "" {
			label = fmt.Sprintf("%s (%s)", info.ProcessName, info.Framework)
		}
		fmt.Printf("\n  Opening %s  %s\n\n",
			color.New(color.FgCyan).Sprint(url),
			color.New(color.FgHiBlack).Sprint(label),
		)
		if err := exec.Command("open", url).Run(); err != nil {
			color.New(color.FgRed).Printf("  Failed to open browser: %v\n\n", err)
			return 1
		}

	case "wait":
		if len(args) < 2 {
			color.New(color.FgRed).Print("\n  Usage: ports wait <port> [--timeout <duration>]\n\n")
			color.New(color.FgHiBlack).Print("  Blocks until the port starts listening. Default timeout: 60s\n")
			color.New(color.FgHiBlack).Print("  Exit 0 on success, 1 on timeout.\n\n")
			color.New(color.FgHiBlack).Print("  Example: ports wait 3000 && open http://localhost:3000\n\n")
			return 1
		}
		n, err := strconv.Atoi(args[1])
		if err != nil {
			color.New(color.FgRed).Printf("\n  %q is not a valid port number\n\n", args[1])
			return 1
		}
		timeout := 60 * time.Second
		for i := 2; i < len(args)-1; i++ {
			if args[i] == "--timeout" || args[i] == "-t" {
				if d, err := time.ParseDuration(args[i+1]); err == nil {
					timeout = d
				}
			}
		}
		return waitForPort(n, timeout)

	case "clean":
		stale := scanner.ScanStalePorts()
		var killed, failed []int

		if len(stale) == 0 {
			display.PrintCleanSummary(stale, killed, failed)
			return 0
		}

		fmt.Println()
		color.New(color.FgYellow, color.Bold).Printf(
			"  Found %d orphaned/zombie process%s:\n\n", len(stale), plural(len(stale)),
		)
		for _, p := range stale {
			fmt.Printf("  %s :%s — %s %s\n",
				color.New(color.FgHiBlack).Sprint("•"),
				color.New(color.FgWhite, color.Bold).Sprintf("%d", p.Port),
				p.ProcessName,
				color.New(color.FgHiBlack).Sprintf("(PID %d)", p.PID),
			)
		}
		fmt.Println()

		if askYN("  Kill all? [y/N] ") {
			for _, p := range stale {
				if err := scanner.SignalProcess(p.PID, syscall.SIGTERM); err == nil {
					killed = append(killed, p.PID)
				} else {
					failed = append(failed, p.PID)
				}
			}
			display.PrintCleanSummary(stale, killed, failed)
		} else {
			color.New(color.FgHiBlack).Print("\n  Aborted.\n\n")
		}

	case "kill":
		force := false
		var targets []string
		for _, a := range args[1:] {
			if a == "--force" || a == "-f" {
				force = true
			} else {
				targets = append(targets, a)
			}
		}
		if len(targets) == 0 {
			color.New(color.FgRed).Print("\n  Usage: ports kill [-f|--force] <port|pid> [port|pid...]\n\n")
			color.New(color.FgHiBlack).Print("  Kills listener on port (1-65535), or process by PID. Use -f for SIGKILL.\n\n")
			return 1
		}
		sig := syscall.SIGTERM
		sigName := "SIGTERM"
		if force {
			sig = syscall.SIGKILL
			sigName = "SIGKILL"
		}
		anyFailed := false
		fmt.Println()
		for _, arg := range targets {
			n, err := strconv.Atoi(strings.TrimSpace(arg))
			if err != nil {
				color.New(color.FgRed).Printf("  ✕ %q is not a valid port/PID\n", arg)
				anyFailed = true
				continue
			}
			t := scanner.ResolvePort(n)
			if t == nil {
				msg := fmt.Sprintf("No listener on :%d and no process with PID %d", n, n)
				if n > 65535 {
					msg = fmt.Sprintf("No process with PID %d", n)
				}
				color.New(color.FgRed).Printf("  ✕ %s\n", msg)
				anyFailed = true
				continue
			}
			label := targetLabel(t)
			color.New(color.FgWhite).Printf("  Killing %s\n", label)
			if err := scanner.SignalProcess(t.PID, sig); err == nil {
				color.New(color.FgGreen).Printf("  ✓ Sent %s to %s\n", sigName, label)
			} else {
				extra := ""
				if force {
					extra = " -9"
				}
				color.New(color.FgRed).Printf("  ✕ Failed. Try: sudo kill%s %d\n", extra, t.PID)
				anyFailed = true
			}
		}
		fmt.Println()
		if anyFailed {
			return 1
		}

	case "watch":
		display.PrintWatchBanner()
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt)
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		state := scanner.ComparePorts(nil, display.PrintPortEvent)
		for {
			select {
			case <-tick.C:
				state = scanner.ComparePorts(state, display.PrintPortEvent)
			case <-sigCh:
				color.New(color.FgHiBlack).Print("\n\n  Stopped watching.\n\n")
				return 0
			}
		}

	case "help", "--help", "-h":
		fmt.Println()
		fmt.Printf("%s%s\n",
			color.New(color.FgCyan, color.Bold).Sprint("  which-ports"),
			color.New(color.FgHiBlack).Sprint(" — listen to your ports"),
		)
		fmt.Println()
		color.New(color.FgWhite).Println("  Usage:")
		helpLine("ports", "Show dev server ports")
		helpLine("ports --all", "Show all listening ports")
		helpLine("ports --json", "JSON output (works with ps too)")
		helpLine("ports ps", "Show all running dev processes")
		helpLine("ports <number>", "Detailed info about a specific port")
		helpLine("ports open <port>", "Open port in browser")
		helpLine("ports wait <port>", "Block until port is listening")
		helpLine("ports kill <n>", "Kill by port or PID (-f for SIGKILL)")
		helpLine("ports clean", "Kill orphaned/zombie dev servers")
		helpLine("ports watch", "Monitor port changes in real-time")
		helpLine("whoisonport <num>", "Alias for ports <number>")
		fmt.Println()
		fmt.Println(color.New(color.FgHiBlack).Sprint("  Flags:"))
		helpLine("--all, -a", "Include system processes")
		helpLine("--json, -j", "Emit JSON instead of a table")
		helpLine("--version, -v", "Print version")
		fmt.Println()

	default:
		color.New(color.FgRed).Printf("\n  Unknown command: %s\n", command)
		fmt.Printf("  Run %s for usage.\n\n", color.New(color.FgCyan).Sprint("ports --help"))
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------
// Feature implementations.
// ---------------------------------------------------------------------------

// waitForPort polls until port n is listening or the deadline passes.
func waitForPort(n int, timeout time.Duration) int {
	deadline := time.Now().Add(timeout)
	fmt.Printf("\n  Waiting for :%d", n)

	for time.Now().Before(deadline) {
		if info := scanner.LookupPort(n); info != nil {
			label := info.ProcessName
			if info.Framework != "" {
				label = fmt.Sprintf("%s · %s", info.ProcessName, info.Framework)
			}
			fmt.Printf("\n  %s :%d is up  %s\n\n",
				color.New(color.FgGreen).Sprint("✓"),
				n,
				color.New(color.FgHiBlack).Sprint(label),
			)
			return 0
		}
		fmt.Print(color.New(color.FgHiBlack).Sprint("."))
		time.Sleep(500 * time.Millisecond)
	}

	fmt.Printf("\n  %s Timed out after %s\n\n",
		color.New(color.FgRed).Sprint("✕"),
		timeout,
	)
	return 1
}

// emitJSON marshals v as indented JSON to stdout.
func emitJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "json encode error: %v\n", err)
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------
// Helpers.
// ---------------------------------------------------------------------------

func helpLine(cmd, desc string) {
	fmt.Printf("    %s  %s\n",
		color.New(color.FgCyan).Sprintf("%-24s", cmd),
		color.New(color.FgHiBlack).Sprint(desc),
	)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func targetLabel(t *scanner.KillTarget) string {
	if t.Via == "port" && t.Info != nil {
		return fmt.Sprintf(":%d — %s (PID %d)", t.Port, t.Info.ProcessName, t.PID)
	}
	if t.Via == "port" {
		return fmt.Sprintf(":%d (PID %d)", t.Port, t.PID)
	}
	return fmt.Sprintf("PID %d", t.PID)
}

// groupDockerProcs collapses Docker internal processes into a single summary row.
func groupDockerProcs(procs []scanner.ProcessRecord) []scanner.ProcessRecord {
	var containers []scanner.ProcessRecord
	var rest []scanner.ProcessRecord
	for _, p := range procs {
		if isDockerApp(p.ProcessName) {
			containers = append(containers, p)
		} else {
			rest = append(rest, p)
		}
	}
	if len(containers) == 0 {
		return rest
	}
	totalCPU, totalKB := 0.0, 0.0
	for _, c := range containers {
		totalCPU += c.CPUPct
		totalKB += memStringToKB(c.MemUsage)
	}
	rest = append(rest, scanner.ProcessRecord{
		PID:         containers[0].PID,
		ProcessName: "Docker",
		Summary:     fmt.Sprintf("%d processes", len(containers)),
		CPUPct:      totalCPU,
		MemUsage:    kbToHuman(totalKB),
		Framework:   "Docker",
		Uptime:      containers[0].Uptime,
	})
	return rest
}

func isDockerApp(name string) bool {
	return strings.HasPrefix(name, "com.docke") ||
		strings.HasPrefix(name, "Docker") ||
		name == "docker" ||
		name == "docker-sandbox"
}

func memStringToKB(mem string) float64 {
	parts := strings.Fields(mem)
	if len(parts) != 2 {
		return 0
	}
	v, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0
	}
	switch parts[1] {
	case "GB":
		return v * 1048576
	case "MB":
		return v * 1024
	case "KB":
		return v
	}
	return 0
}

func kbToHuman(kb float64) string {
	switch {
	case kb > 1048576:
		return fmt.Sprintf("%.1f GB", kb/1048576)
	case kb > 1024:
		return fmt.Sprintf("%.1f MB", kb/1024)
	default:
		return fmt.Sprintf("%.0f KB", kb)
	}
}
