package scanner

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// PortHealth describes the observed state of a listening process.
type PortHealth string

const (
	PortHealthy  PortHealth = "healthy"
	PortOrphaned PortHealth = "orphaned"
	PortZombie   PortHealth = "zombie"
	PortUnknown  PortHealth = "unknown"
)

// PortEntry holds everything known about one TCP listening port.
type PortEntry struct {
	Port        int        `json:"port"`
	PID         int        `json:"pid"`
	ProcessName string     `json:"process"`
	RawName     string     `json:"rawName,omitempty"`
	Command     string     `json:"command,omitempty"`
	WorkDir     string     `json:"workDir,omitempty"`
	ProjectName string     `json:"project,omitempty"`
	Framework   string     `json:"framework,omitempty"`
	Uptime      string     `json:"uptime,omitempty"`
	StartedAt   time.Time  `json:"startedAt,omitempty"`
	Health      PortHealth `json:"health"`
	MemUsage    string     `json:"memory,omitempty"`
	GitBranch   string     `json:"gitBranch,omitempty"`
	Ancestors   []ProcNode `json:"ancestors,omitempty"`
}

// ProcNode is one entry in a process ancestry chain.
type ProcNode struct {
	PID  int    `json:"pid"`
	PPID int    `json:"ppid"`
	Name string `json:"name"`
}

// ProcessRecord holds stats for a running process (used by `ports ps`).
type ProcessRecord struct {
	PID         int     `json:"pid"`
	RSS         int     `json:"-"`
	ProcessName string  `json:"process"`
	Command     string  `json:"command,omitempty"`
	Summary     string  `json:"summary,omitempty"`
	WorkDir     string  `json:"workDir,omitempty"`
	ProjectName string  `json:"project,omitempty"`
	Framework   string  `json:"framework,omitempty"`
	MemUsage    string  `json:"memory,omitempty"`
	Uptime      string  `json:"uptime,omitempty"`
	CPUPct      float64 `json:"cpu"`
	MemPct      float64 `json:"memPct,omitempty"`
}

// ContainerPort maps a Docker host port to its container metadata.
type ContainerPort struct {
	ContainerName string
	ImageName     string
}

// KillTarget is returned by ResolvePort.
type KillTarget struct {
	PID  int
	Via  string // "port" or "pid"
	Port int
	Info *PortEntry
}

// rawStats is the internal result of a batched ps call.
type rawStats struct {
	PPID    int
	Stat    string
	RSS     int
	LStart  string
	Command string
}

// pkgManifest is used to unmarshal the relevant fields of package.json.
type pkgManifest struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

// ---------------------------------------------------------------------------
// Rule tables — framework detection is purely data-driven.
// ---------------------------------------------------------------------------

type depRule struct {
	pkg       string
	framework string
}

// depFrameworks maps npm package names to detected frameworks.
// Ordered: first match wins.
var depFrameworks = []depRule{
	{"next", "Next.js"},
	{"nuxt3", "Nuxt"},
	{"nuxt", "Nuxt"},
	{"@sveltejs/kit", "SvelteKit"},
	{"svelte", "Svelte"},
	{"@remix-run/react", "Remix"},
	{"remix", "Remix"},
	{"astro", "Astro"},
	{"vite", "Vite"},
	{"@angular/core", "Angular"},
	{"vue", "Vue"},
	{"react", "React"},
	{"express", "Express"},
	{"fastify", "Fastify"},
	{"hono", "Hono"},
	{"koa", "Koa"},
	{"@nestjs/core", "NestJS"},
	{"nestjs", "NestJS"},
	{"gatsby", "Gatsby"},
	{"webpack-dev-server", "Webpack"},
	{"esbuild", "esbuild"},
	{"parcel", "Parcel"},
}

type fileRule struct {
	filename  string
	framework string
}

// configFileFrameworks maps config file names to detected frameworks.
var configFileFrameworks = []fileRule{
	{"vite.config.ts", "Vite"},
	{"vite.config.js", "Vite"},
	{"next.config.js", "Next.js"},
	{"next.config.mjs", "Next.js"},
	{"angular.json", "Angular"},
	{"Cargo.toml", "Rust"},
	{"go.mod", "Go"},
	{"manage.py", "Django"},
	{"Gemfile", "Ruby"},
}

type cmdRule struct {
	keyword   string
	framework string
}

// commandKeywords maps substrings found in the process command to frameworks.
var commandKeywords = []cmdRule{
	{"next", "Next.js"},
	{"vite", "Vite"},
	{"nuxt", "Nuxt"},
	{"ng serve", "Angular"},
	{"angular", "Angular"},
	{"webpack", "Webpack"},
	{"remix", "Remix"},
	{"astro", "Astro"},
	{"gatsby", "Gatsby"},
	{"flask", "Flask"},
	{"manage.py", "Django"},
	{"django", "Django"},
	{"uvicorn", "FastAPI"},
	{"rails", "Rails"},
	{"rustc", "Rust"},
	{"cargo", "Rust"},
}

type imageRule struct {
	keyword   string
	framework string
}

// containerImageFrameworks maps Docker image name substrings to tech labels.
var containerImageFrameworks = []imageRule{
	{"postgres", "PostgreSQL"},
	{"redis", "Redis"},
	{"mysql", "MySQL"},
	{"mariadb", "MySQL"},
	{"mongo", "MongoDB"},
	{"nginx", "nginx"},
	{"localstack", "LocalStack"},
	{"rabbitmq", "RabbitMQ"},
	{"kafka", "Kafka"},
	{"elasticsearch", "Elasticsearch"},
	{"opensearch", "Elasticsearch"},
	{"minio", "MinIO"},
}

type nameRule struct {
	name      string
	framework string
}

// runtimeNameFrameworks maps known runtime binary names to tech labels.
var runtimeNameFrameworks = []nameRule{
	{"node", "Node.js"},
	{"python3", "Python"},
	{"python", "Python"},
	{"ruby", "Ruby"},
	{"java", "Java"},
	{"go", "Go"},
}

// ---------------------------------------------------------------------------
// Process classification tables.
// ---------------------------------------------------------------------------

// systemAppPrefixes lists lowercase name prefixes that belong to desktop/system apps.
var systemAppPrefixes = []string{
	"spotify", "raycast", "tableplus", "postman", "linear", "cursor",
	"controlce", "rapportd", "superhuma", "setappage", "slack", "discord",
	"firefox", "chrome", "google", "safari", "figma", "notion", "zoom",
	"teams", "code", "iterm2", "warp", "arc", "loginwindow", "windowserver",
	"systemuise", "kernel_task", "launchd", "mdworker", "mds_stores",
	"cfprefsd", "coreaudio", "corebrightne", "airportd", "bluetoothd",
	"sharingd", "usernoted", "notificationc", "cloudd",
}

// devRuntimes is a fast-lookup set of process names considered dev tools.
var devRuntimes = buildStringSet([]string{
	"node", "python", "python3", "ruby", "java",
	"go", "cargo", "deno", "bun", "php",
	"uvicorn", "gunicorn", "flask", "rails",
	"npm", "npx", "yarn", "pnpm", "tsc",
	"tsx", "esbuild", "rollup", "turbo", "nx",
	"jest", "vitest", "mocha", "pytest",
	"cypress", "playwright", "rustc", "dotnet",
	"gradle", "mvn", "mix", "elixir",
})

func buildStringSet(items []string) map[string]struct{} {
	m := make(map[string]struct{}, len(items))
	for _, s := range items {
		m[s] = struct{}{}
	}
	return m
}

// cmdPatternREs holds pre-compiled patterns for command-line dev detection.
var cmdPatternREs = compilePatterns([]string{
	`\bnode\b`, `\bnext[\s-]`, `\bvite\b`, `\bnuxt\b`, `\bwebpack\b`,
	`\bremix\b`, `\bastro\b`, `\bgulp\b`, `\bng serve\b`, `\bgatsb`,
	`\bflask\b`, `\bdjango\b|manage\.py`, `\buvicorn\b`, `\brails\b`, `\bcargo\b`,
})

func compilePatterns(patterns []string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		if re, err := regexp.Compile(p); err == nil {
			out = append(out, re)
		}
	}
	return out
}

// containerPortRe extracts host ports from Docker port-mapping strings.
var containerPortRe = regexp.MustCompile(`(?:\d+\.\d+\.\d+\.\d+|::):(\d+)->`)

// ---------------------------------------------------------------------------
// Internal batch calls.
// ---------------------------------------------------------------------------

func joinPIDs(pids []int) string {
	parts := make([]string, len(pids))
	for i, p := range pids {
		parts[i] = strconv.Itoa(p)
	}
	return strings.Join(parts, ",")
}

// collectProcStats fetches ps fields for multiple PIDs in a single invocation.
// Output columns: pid ppid stat rss DOW MON DD HH:MM:SS YYYY command...
func collectProcStats(pids []int) map[int]rawStats {
	out := make(map[int]rawStats)
	if len(pids) == 0 {
		return out
	}
	result, err := runCommand("ps", "-p", joinPIDs(pids), "-o", "pid=,ppid=,stat=,rss=,lstart=,command=")
	if err != nil {
		return out
	}
	for _, line := range strings.Split(result, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 9 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		ppid, _ := strconv.Atoi(f[1])
		rss, _ := strconv.Atoi(f[3])
		// f[4]=DOW  f[5]=MON  f[6]=DD  f[7]=TIME  f[8]=YEAR
		lstart := strings.Join(f[5:9], " ")
		cmd := strings.Join(f[9:], " ")
		out[pid] = rawStats{PPID: ppid, Stat: f[2], RSS: rss, LStart: lstart, Command: cmd}
	}
	return out
}

// collectWorkDirs resolves working directories for multiple PIDs via lsof.
func collectWorkDirs(pids []int) map[int]string {
	out := make(map[int]string)
	if len(pids) == 0 {
		return out
	}
	result, err := runCommand("lsof", "-a", "-d", "cwd", "-p", joinPIDs(pids))
	if err != nil {
		return out
	}
	lines := strings.Split(result, "\n")
	if len(lines) < 2 {
		return out
	}
	for _, line := range lines[1:] { // skip header row
		f := strings.Fields(line)
		// COMMAND PID USER FD TYPE DEVICE SIZE/OFF NODE NAME
		if len(f) < 9 {
			continue
		}
		pid, err := strconv.Atoi(f[1])
		if err != nil {
			continue
		}
		path := strings.Join(f[8:], " ")
		if strings.HasPrefix(path, "/") {
			out[pid] = path
		}
	}
	return out
}

// collectContainerPorts fetches Docker container → host-port mappings.
func collectContainerPorts() map[int]ContainerPort {
	out := make(map[int]ContainerPort)
	if _, err := exec.LookPath("docker"); err != nil {
		return out
	}
	result, err := runCommand("docker", "ps", "--format", "{{.Ports}}\t{{.Names}}\t{{.Image}}")
	if err != nil {
		return out
	}
	for _, line := range strings.Split(result, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}
		portsStr, name := parts[0], parts[1]
		image := ""
		if len(parts) == 3 {
			image = parts[2]
		}
		seen := make(map[int]bool)
		for _, m := range containerPortRe.FindAllStringSubmatch(portsStr, -1) {
			port, err := strconv.Atoi(m[1])
			if err == nil && !seen[port] {
				seen[port] = true
				out[port] = ContainerPort{ContainerName: name, ImageName: image}
			}
		}
	}
	return out
}

// runCommand executes a command and returns trimmed stdout.
// An *exec.ExitError is treated as a partial result (lsof exits 1 on missing PIDs).
func runCommand(name string, args ...string) (string, error) {
	data, err := exec.Command(name, args...).Output()
	if err != nil {
		if _, partial := err.(*exec.ExitError); partial {
			return string(data), nil
		}
		return "", err
	}
	return string(data), nil
}

// ---------------------------------------------------------------------------
// Public API.
// ---------------------------------------------------------------------------

// ScanPorts returns all TCP listening ports enriched with process metadata.
func ScanPorts(detailed bool) []PortEntry {
	raw, err := runCommand("lsof", "-iTCP", "-sTCP:LISTEN", "-P", "-n")
	if err != nil {
		return nil
	}

	type stub struct {
		port int
		pid  int
		name string
	}

	portSeen := make(map[int]bool)
	var stubs []stub

	for _, line := range strings.Split(raw, "\n")[1:] { // skip header
		f := strings.Fields(line)
		// COMMAND PID USER FD TYPE DEVICE SIZE/OFF NODE NAME
		if len(f) < 9 {
			continue
		}
		pid, err := strconv.Atoi(f[1])
		if err != nil {
			continue
		}
		nameField := f[8]
		colon := strings.LastIndex(nameField, ":")
		if colon < 0 {
			continue
		}
		port, err := strconv.Atoi(nameField[colon+1:])
		if err != nil || portSeen[port] {
			continue
		}
		portSeen[port] = true
		stubs = append(stubs, stub{port: port, pid: pid, name: f[0]})
	}

	// Gather unique PIDs once for batch calls.
	pidSet := make(map[int]bool, len(stubs))
	for _, s := range stubs {
		pidSet[s.pid] = true
	}
	uniquePIDs := make([]int, 0, len(pidSet))
	for pid := range pidSet {
		uniquePIDs = append(uniquePIDs, pid)
	}

	statsMap := collectProcStats(uniquePIDs)
	workDirs := collectWorkDirs(uniquePIDs)

	needContainers := false
	for _, s := range stubs {
		if strings.HasPrefix(s.name, "com.docke") || s.name == "docker" {
			needContainers = true
			break
		}
	}
	containerMap := make(map[int]ContainerPort)
	if needContainers {
		containerMap = collectContainerPorts()
	}

	entries := make([]PortEntry, 0, len(stubs))
	for _, s := range stubs {
		stats := statsMap[s.pid]
		workDir := workDirs[s.pid]

		e := PortEntry{
			Port:        s.port,
			PID:         s.pid,
			ProcessName: s.name,
			RawName:     s.name,
			Command:     stats.Command,
			Health:      PortHealthy,
		}

		if stats.Stat != "" {
			switch {
			case strings.Contains(stats.Stat, "Z"):
				e.Health = PortZombie
			case stats.PPID == 1 && IsDevRuntime(s.name, stats.Command):
				e.Health = PortOrphaned
			}
			if stats.RSS > 0 {
				e.MemUsage = prettyBytes(stats.RSS)
			}
			if t, err := parseStartTime(stats.LStart); err == nil {
				e.StartedAt = t
				e.Uptime = prettyDuration(time.Since(t))
			}
			if e.Framework == "" {
				e.Framework = matchCommandFramework(stats.Command, s.name)
			}
		}

		if cp, ok := containerMap[s.port]; ok {
			e.ProjectName = cp.ContainerName
			e.Framework = matchContainerFramework(cp.ImageName)
			e.ProcessName = "docker"
		} else if workDir != "" {
			root := walkToProjectRoot(workDir)
			e.WorkDir = root
			e.ProjectName = filepath.Base(root)
			if e.Framework == "" {
				e.Framework = resolveFramework(root)
			}
			if detailed {
				if branch, err := runCommand("git", "-C", root, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
					e.GitBranch = strings.TrimSpace(branch)
				}
			}
		}

		if detailed {
			e.Ancestors = ancestorChain(s.pid)
		}

		entries = append(entries, e)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Port < entries[j].Port
	})
	return entries
}

// IsDevRuntime reports whether a process is a known developer tool (not a system/desktop app).
func IsDevRuntime(processName, command string) bool {
	name := strings.ToLower(processName)
	cmd := strings.ToLower(command)

	for _, prefix := range systemAppPrefixes {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	if _, ok := devRuntimes[name]; ok {
		return true
	}
	if strings.HasPrefix(name, "com.docke") || name == "docker" || name == "docker-sandbox" {
		return true
	}
	for _, re := range cmdPatternREs {
		if re.MatchString(cmd) {
			return true
		}
	}
	return false
}

// LookupPort returns detailed metadata for a single port, or nil if not found.
func LookupPort(port int) *PortEntry {
	for _, e := range ScanPorts(true) {
		if e.Port == port {
			cp := e
			return &cp
		}
	}
	return nil
}

// ScanProcesses returns all running processes with CPU/memory stats.
func ScanProcesses() []ProcessRecord {
	raw, err := runCommand("ps", "-eo", "pid=,pcpu=,pmem=,rss=,lstart=,command=")
	if err != nil {
		return nil
	}

	var records []ProcessRecord
	seen := make(map[int]bool)
	self := os.Getpid()

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		f := strings.Fields(line)
		// PID CPU% MEM% RSS DOW MON DD TIME YEAR command...
		if len(f) < 10 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil || pid <= 1 || pid == self || seen[pid] {
			continue
		}
		seen[pid] = true

		cpu, _ := strconv.ParseFloat(f[1], 64)
		memPct, _ := strconv.ParseFloat(f[2], 64)
		rss, _ := strconv.Atoi(f[3])
		lstart := strings.Join(f[5:9], " ")
		fullCmd := strings.Join(f[9:], " ")

		binName := ""
		if parts := strings.Fields(fullCmd); len(parts) > 0 {
			binName = filepath.Base(parts[0])
		}

		r := ProcessRecord{
			PID:         pid,
			ProcessName: binName,
			Command:     fullCmd,
			Summary:     describeCommand(fullCmd, binName),
			CPUPct:      cpu,
			MemPct:      memPct,
			RSS:         rss,
		}
		if rss > 0 {
			r.MemUsage = prettyBytes(rss)
		}
		if t, err := parseStartTime(lstart); err == nil {
			r.Uptime = prettyDuration(time.Since(t))
		}
		r.Framework = matchCommandFramework(fullCmd, binName)
		records = append(records, r)
	}

	// Resolve working dirs for non-container processes.
	var nonContainerPIDs []int
	for _, r := range records {
		if !isContainerRuntime(r.ProcessName) {
			nonContainerPIDs = append(nonContainerPIDs, r.PID)
		}
	}
	dirs := collectWorkDirs(nonContainerPIDs)

	for i, r := range records {
		if dir, ok := dirs[r.PID]; ok {
			root := walkToProjectRoot(dir)
			records[i].WorkDir = root
			records[i].ProjectName = filepath.Base(root)
			if records[i].Framework == "" {
				records[i].Framework = resolveFramework(root)
			}
		}
	}
	return records
}

// ScanStalePorts returns listening ports whose process is orphaned or zombie.
func ScanStalePorts() []PortEntry {
	var stale []PortEntry
	for _, e := range ScanPorts(false) {
		if e.Health == PortOrphaned || e.Health == PortZombie {
			stale = append(stale, e)
		}
	}
	return stale
}

// IsPidAlive reports whether a process with the given PID is currently running.
func IsPidAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// SignalProcess delivers sig to the given process.
func SignalProcess(pid int, sig syscall.Signal) error {
	return syscall.Kill(pid, sig)
}

// ResolvePort resolves a numeric argument to a kill target.
// Numbers ≤ 65535 are tried as port numbers first; larger values are treated as PIDs directly.
func ResolvePort(n int) *KillTarget {
	if n < 1 {
		return nil
	}
	if n <= 65535 {
		if info := LookupPort(n); info != nil {
			return &KillTarget{PID: info.PID, Via: "port", Port: n, Info: info}
		}
	}
	if IsPidAlive(n) {
		return &KillTarget{PID: n, Via: "pid"}
	}
	return nil
}

// ComparePorts diffs the current listening state against prev, fires callback
// for each change, and returns the updated state map.
func ComparePorts(prev map[int]PortEntry, onChange func(event string, entry PortEntry)) map[int]PortEntry {
	current := ScanPorts(false)
	next := make(map[int]PortEntry, len(current))
	for _, e := range current {
		next[e.Port] = e
		if prev != nil {
			if _, existed := prev[e.Port]; !existed {
				onChange("new", e)
			}
		}
	}
	if prev != nil {
		for port, e := range prev {
			if _, still := next[port]; !still {
				onChange("removed", e)
			}
		}
	}
	return next
}

// ---------------------------------------------------------------------------
// Framework detection helpers.
// ---------------------------------------------------------------------------

func matchContainerFramework(image string) string {
	if image == "" {
		return "Docker"
	}
	img := strings.ToLower(image)
	for _, rule := range containerImageFrameworks {
		if strings.Contains(img, rule.keyword) {
			return rule.framework
		}
	}
	return "Docker"
}

func matchCommandFramework(command, procName string) string {
	if command == "" {
		return matchRuntimeFramework(procName)
	}
	cmd := strings.ToLower(command)
	for _, rule := range commandKeywords {
		if strings.Contains(cmd, rule.keyword) {
			return rule.framework
		}
	}
	return matchRuntimeFramework(procName)
}

func matchRuntimeFramework(procName string) string {
	name := strings.ToLower(procName)
	for _, rule := range runtimeNameFrameworks {
		if name == rule.name {
			return rule.framework
		}
	}
	return ""
}

func resolveFramework(projectRoot string) string {
	data, err := os.ReadFile(filepath.Join(projectRoot, "package.json"))
	if err == nil {
		var manifest pkgManifest
		if json.Unmarshal(data, &manifest) == nil {
			merged := make(map[string]bool, len(manifest.Dependencies)+len(manifest.DevDependencies))
			for k := range manifest.Dependencies {
				merged[k] = true
			}
			for k := range manifest.DevDependencies {
				merged[k] = true
			}
			for _, rule := range depFrameworks {
				if merged[rule.pkg] {
					return rule.framework
				}
			}
		}
	}
	for _, rule := range configFileFrameworks {
		if _, err := os.Stat(filepath.Join(projectRoot, rule.filename)); err == nil {
			return rule.framework
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Project-root detection.
// ---------------------------------------------------------------------------

var projectMarkers = []string{
	"package.json", "Cargo.toml", "go.mod", "pyproject.toml",
	"Gemfile", "pom.xml", "build.gradle",
}

func walkToProjectRoot(startDir string) string {
	cur := startDir
	for depth := 0; depth < 15; depth++ {
		for _, marker := range projectMarkers {
			if _, err := os.Stat(filepath.Join(cur, marker)); err == nil {
				return cur
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return startDir
}

// ---------------------------------------------------------------------------
// Process ancestry.
// ---------------------------------------------------------------------------

func ancestorChain(pid int) []ProcNode {
	raw, err := runCommand("ps", "-eo", "pid=,ppid=,comm=")
	if err != nil {
		return nil
	}
	table := make(map[int]ProcNode)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		p, err1 := strconv.Atoi(f[0])
		pp, err2 := strconv.Atoi(f[1])
		if err1 != nil || err2 != nil {
			continue
		}
		table[p] = ProcNode{PID: p, PPID: pp, Name: strings.Join(f[2:], " ")}
	}
	var chain []ProcNode
	cur := pid
	for depth := 0; cur > 1 && depth < 8; depth++ {
		node, ok := table[cur]
		if !ok {
			break
		}
		chain = append(chain, node)
		cur = node.PPID
	}
	return chain
}

// ---------------------------------------------------------------------------
// Command summarisation.
// ---------------------------------------------------------------------------

func describeCommand(command, procName string) string {
	if command == "" {
		return procName
	}
	tokens := strings.Fields(command)
	var parts []string
	for i, tok := range tokens {
		if i == 0 || strings.HasPrefix(tok, "-") {
			continue
		}
		if strings.Contains(tok, "/") {
			parts = append(parts, filepath.Base(tok))
		} else {
			parts = append(parts, tok)
		}
		if len(parts) == 3 {
			break
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, " ")
	}
	return procName
}

// ---------------------------------------------------------------------------
// Container runtime detection.
// ---------------------------------------------------------------------------

func isContainerRuntime(name string) bool {
	return strings.HasPrefix(name, "com.docke") ||
		strings.HasPrefix(name, "Docker") ||
		name == "docker" ||
		name == "docker-sandbox"
}

// ---------------------------------------------------------------------------
// Formatting helpers.
// ---------------------------------------------------------------------------

func parseStartTime(lstart string) (time.Time, error) {
	return time.ParseInLocation("Jan 2 15:04:05 2006", lstart, time.Local)
}

func prettyDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	secs := int(d.Seconds())
	mins := secs / 60
	hrs := mins / 60
	days := hrs / 24
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hrs%24)
	case hrs > 0:
		return fmt.Sprintf("%dh %dm", hrs, mins%60)
	case mins > 0:
		return fmt.Sprintf("%dm %ds", mins, secs%60)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

func prettyBytes(rssKB int) string {
	switch {
	case rssKB > 1048576:
		return fmt.Sprintf("%.1f GB", float64(rssKB)/1048576)
	case rssKB > 1024:
		return fmt.Sprintf("%.1f MB", float64(rssKB)/1024)
	default:
		return fmt.Sprintf("%d KB", rssKB)
	}
}
