# which-ports

**See what's running on your ports — instantly.**

`which-ports` is a macOS CLI that gives you a colour-coded table of every dev server, database, and background process listening on your machine. Framework detection, Docker container identification, memory and uptime — all in one command.

## What it looks like

```
$ ports

 ┌─────────────────────────────────────┐
 │  🔊 which-ports                     │
 │  listening to your ports...         │
 └─────────────────────────────────────┘

┌───────┬────────┬───────┬──────────────────────┬─────────┬────────┬───────────┐
│ PORT  │ PROCESS│ PID   │ PROJECT              │FRAMEWORK│ UPTIME │ STATUS    │
├───────┼────────┼───────┼──────────────────────┼─────────┼────────┼───────────┤
│ :8080 │ java   │ 18450 │ api-server           │ Spring  │ 7d 2h  │ ● healthy │
│ :5173 │ node   │ 24167 │ web-app              │ Vite    │ 4h 33m │ ● healthy │
│ :3306 │ mysqld │ 1028  │ docker-mysql-1       │ MySQL   │ 8d 14h │ ● healthy │
│ :6379 │ redis  │ 1089  │ docker-redis-1       │ Redis   │ 8d 14h │ ● healthy │
│ :9000 │ python │ 31945 │ worker-service       │FastAPI  │ 2d 8h  │ ● healthy │
└───────┴────────┴───────┴──────────────────────┴─────────┴────────┴───────────┘

  5 ports active  ·  Run ports <number> for details  ·  --all to show everything
```

Status colours: green = healthy, yellow = orphaned, red = zombie.

## Install

### Using Homebrew (macOS / Linux)

```bash
brew install hnq90/tap/which-ports
```

### From source (Go 1.22+)

```bash
git clone https://github.com/hnq90/which-ports.git
cd which-ports
make build            # builds ./ports binary
make install          # installs to /usr/local/bin/ports
make install-alias    # also installs /usr/local/bin/whoisonport symlink
```

### Uninstall

```bash
make uninstall        # removes from /usr/local/bin
```

## Usage

### Show dev server ports

```bash
ports
```

Displays dev servers, Docker containers, and databases. System apps (Spotify, Slack, browsers, etc.) are filtered out.

### Show all listening ports

```bash
ports --all
```

Includes system services and desktop apps — everything on the machine.

### JSON output

```bash
ports --json
ports ps --json
```

Emit structured JSON instead of a table. Works with `ports` (default list), `ports ps`, or any detail view.

### Inspect a port

```bash
ports 3000
# or
whoisonport 3000
```

Shows the full process tree, project directory, current git branch, memory usage, and an interactive prompt to kill the process.

### Open a port in your browser

```bash
ports open 3000
```

Launches `http://localhost:3000` in your default browser, with framework label in the console.

### Wait for a port to be ready

```bash
ports wait 3000                # blocks until :3000 is listening
ports wait 3000 --timeout 10s  # custom timeout (default 60s)
```

Useful in scripts: exit 0 on success, 1 on timeout. Polls every 500ms.

### Kill a process

```bash
ports kill 3000              # kill by port
ports kill 3000 5173 8080    # kill multiple at once
ports kill 42872             # kill by PID
ports kill -f 3000           # force kill (SIGKILL)
```

Resolves port numbers to PIDs automatically. Use `-f` when a process ignores SIGTERM.

### Show all dev processes

```bash
ports ps
ports ps --all    # include system processes
```

A developer-focused `ps` — shows CPU%, memory, framework, uptime, and a concise description column. Docker processes are collapsed into a single summary row.

```
┌───────┬─────────────┬──────┬──────────┬──────────────────┬───────────┬─────────┬────────────────────────────────┐
│ PID   │ PROCESS     │ CPU% │ MEM      │ PROJECT          │ FRAMEWORK │ UPTIME  │ WHAT                           │
├───────┼─────────────┼──────┼──────────┼──────────────────┼───────────┼─────────┼────────────────────────────────┤
│ 1028  │ mysqld      │ 2.1  │ 589.3 MB │ docker-mysql-1   │ MySQL     │ 8d 14h  │ /usr/sbin/mysqld --datadir=/… │
│ 18450 │ java        │ 1.8  │ 1.2 GB   │ api-server       │ Spring    │ 7d 2h   │ java -jar server.jar          │
│ 24167 │ node        │ 0.3  │ 256.7 MB │ web-app          │ Vite      │ 4h 33m  │ node --loader tsx dev.ts      │
│ 31945 │ python      │ 0.1  │ 92.4 MB  │ worker-service   │ FastAPI   │ 2d 8h   │ uvicorn main:app --reload     │
└───────┴─────────────┴──────┴──────────┴──────────────────┴───────────┴─────────┴────────────────────────────────┘
```

### Clean up orphaned processes

```bash
ports clean
```

Finds and kills orphaned or zombie dev server processes — only targets known dev runtimes, never desktop apps.

### Watch for port changes

```bash
ports watch
```

Real-time monitoring. Notifies you whenever a port opens or closes. Press `Ctrl+C` to stop.

## How it works

Implemented in Go, compiles to a single static binary with no runtime dependencies.

Three shell invocations, runs in ~0.2 s:

1. **`lsof -iTCP -sTCP:LISTEN`** — finds all processes listening on TCP ports
2. **`ps`** (single batched call) — retrieves process details for all PIDs at once: command, uptime, memory, parent PID, status
3. **`lsof -d cwd`** (single batched call) — resolves working directories to detect project roots and frameworks

For Docker ports, a single `docker ps` call maps host ports to container names and images.

Framework detection reads `package.json` dependencies and scans process command lines. Recognises Next.js, Vite, Angular, Remix, Astro, Django, Rails, FastAPI, and many others. Docker images are identified as PostgreSQL, Redis, MongoDB, LocalStack, nginx, etc.

See [`CLAUDE.md`](CLAUDE.md) for technical design details, architecture, and exported API.

## Platform support

| Platform | Status |
|---|---|
| macOS | Supported |
| Linux | Planned |
| Windows | Not planned |

## License

[MIT](LICENSE)
