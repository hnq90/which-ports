# which-ports — CLAUDE.md

## What this project does

`which-ports` is a macOS CLI that shows all TCP listening ports on the local machine, enriched with process metadata: framework detection, Docker container identification, memory/CPU usage, uptime, and git branch. It also supports killing processes, cleaning up orphaned servers, and watching for port changes in real time.

## Build

```bash
make build          # produces ./ports binary
make install        # copies to /usr/local/bin/ports
make install-alias  # also adds /usr/local/bin/whoisonport symlink
```

Requires Go 1.22+. No CGO — produces a fully static macOS binary.

## Project structure

```
cmd/ports/main.go              CLI entry point — arg parsing, command dispatch
internal/scanner/scanner.go    System calls, data types, framework detection
internal/display/display.go    Terminal rendering, colour palette, table builder
go.mod / go.sum                Module: which-ports; deps: fatih/color, go-runewidth
Makefile                       build / install / clean targets
```

## Commands and features

| Command | Purpose |
|---|---|
| `ports` (default) | Show dev server ports |
| `ports --all` / `-a` | Include system processes |
| `ports --json` / `-j` | Emit JSON instead of tables |
| `ports <port-number>` | Inspect a specific port, with kill prompt |
| `ports ps` | All running processes, sorted by CPU% |
| `ports kill <ports/pids>` | Kill by port or PID; `-f` for SIGKILL |
| `ports clean` | Kill orphaned/zombie dev servers |
| `ports watch` | Real-time port change monitor |
| `ports open <port>` | Open port in default browser |
| `ports wait <port>` | Block until port is listening (with `--timeout` option) |
| `ports help` | Show usage |

### JSON output (`--json` / `-j`)

Works with any output context. Emits indented JSON:
- `ports --json` → array of `PortEntry` structs
- `ports ps --json` → array of `ProcessRecord` structs
- `ports <port> --json` → single `PortEntry` struct

All structs include JSON tags with `omitempty` for clean output. Controlled by `jsonOut` flag in `dispatch()`.

### Port opener (`ports open <port>`)

Resolves port, validates a listener is active, then spawns `open http://localhost:<port>` to launch the default browser. Framework label is printed to console for context.

### Port waiter (`ports wait <port> [--timeout <duration>]`)

Polls `LookupPort()` every 500ms until the port is listening or deadline passes. Default timeout is 60 seconds. Exit code 0 on success, 1 on timeout. Useful in shell scripts and build pipelines.

## Key design decisions

### scanner package — exported API

| Function | Purpose |
|---|---|
| `ScanPorts(detailed bool) []PortEntry` | List all TCP listeners; detailed=true adds git branch + process ancestry |
| `LookupPort(port int) *PortEntry` | Detail view for one port |
| `ScanProcesses() []ProcessRecord` | All running processes (for `ports ps`) |
| `ScanStalePorts() []PortEntry` | Orphaned / zombie listeners only |
| `IsDevRuntime(name, cmd string) bool` | Whether a process is a dev tool |
| `SignalProcess(pid int, sig syscall.Signal) error` | Kill wrapper |
| `ResolvePort(n int) *KillTarget` | Resolve number to port-listener or PID |
| `ComparePorts(prev map[int]PortEntry, cb func) map[int]PortEntry` | Diff for watch mode |

### Framework detection is purely data-driven

Rules are slices of structs (`depFrameworks`, `commandKeywords`, `containerImageFrameworks`, `configFileFrameworks`, `runtimeNameFrameworks`). To add a new framework, append a rule — no switch statements to edit.

### display package — exported API

| Function | Purpose |
|---|---|
| `PrintPortTable(ports, filtered)` | Main port list view |
| `PrintProcTable(procs, filtered)` | Process list (`ports ps`) |
| `PrintPortDetail(info)` | Single-port detail box |
| `PrintCleanSummary(stale, killed, failed)` | Cleanup results |
| `PrintPortEvent(event, entry)` | Watch-mode event line |
| `PrintWatchBanner()` | Watch-mode header |

Table rendering uses a custom `buildTable` function (not tablewriter) so that ANSI-coloured cell content pads correctly. Column widths are computed by stripping escape sequences first (`removeEscapes` → `termWidth`).

### ps output parsing

`ps -p <pids> -o pid=,ppid=,stat=,rss=,lstart=,command=` — after `strings.Fields`:
- `f[0..3]` = PID, PPID, STAT, RSS
- `f[4]` = day-of-week (skipped)
- `f[5:9]` = "Apr 6 13:24:00 2026" (joined as lstart)
- `f[9:]` = command

Parsed with `time.ParseInLocation("Jan 2 15:04:05 2006", lstart, time.Local)`.

`lsof` exits non-zero when some PIDs are dead — `runCommand` treats `*exec.ExitError` as partial success and returns whatever stdout was produced.

## macOS specifics

The tool relies on macOS `lsof` and `ps` flag behaviour. Linux uses different flags and is not supported.

## Dependencies

- `github.com/fatih/color` — terminal colours; auto-disables when stdout is not a TTY
- `github.com/mattn/go-runewidth` — accurate display-column width for Unicode / emoji
