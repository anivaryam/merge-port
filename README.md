# merge-port

A local reverse proxy that merges your client and server into a single port. Run your frontend and backend on separate ports, then expose them through one — perfect for tunneling.

## Install

**Requirements:** Go 1.22 or later

**With [brokit](https://github.com/anivaryam/brokit)** (recommended if you use multiple tools from this org — handles install, update, and uninstall):

```sh
brokit install merge-port              # install latest release
brokit update merge-port               # upgrade to latest
brokit list                           # see installed tools and versions
brokit remove merge-port              # uninstall
```

`brokit` reads the GitHub releases for this repo, verifies sha256, and drops the binary into `/usr/local/bin`.

**From release binary (Linux/macOS, single-tool install):**

```sh
curl -sSL https://raw.githubusercontent.com/anivaryam/merge-port/main/install.sh | sh
```

The standalone installer verifies the release checksum before installing. It supports Linux and macOS. On Windows, use `brokit`, `go install`, or download the release zip manually.

Optional installer settings:

```sh
INSTALL_DIR="$HOME/.local/bin" sh install.sh  # install somewhere writable
VERSION=v1.2.3 sh install.sh                  # install a specific release
```

**With Go**:

```sh
go install github.com/anivaryam/merge-port/cmd/mergeport@latest
```

**Build from source**:

```sh
make build
make install  # copies to ~/.local/bin/
```

## Usage

### Simple mode

```bash
merge-port --client 3000 --server 3001
```

This starts a proxy on port `8080` (default) that routes:
- `/api/*` → `localhost:3001` (your server)
- Everything else → `localhost:3000` (your client)

Multiple API prefixes are supported:

```bash
merge-port --client 3000 --server 3001 --api-prefix /api --api-prefix /auth --api-prefix /ws
```

This routes `/api/*`, `/auth/*`, and `/ws/*` to the server, everything else to the client.

### Route mode

For full control over routing — including multiple backends on different ports:

```bash
merge-port --route /api=3001 --route /auth=3002 --route /=3000
```

Targets can be a bare port, host:port, or full URL:

```bash
merge-port --route /api=3001 --route /admin=http://admin.local:4000 --route /=3000
```

Route mode cannot be combined with `--client`, `--server`, or `--api-prefix`.

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--client` | (required) | Client/frontend port (simple mode) |
| `--server` | (required) | Server/backend port (simple mode) |
| `--port` | `8080` | Port to listen on |
| `--api-prefix` | `/api` | Path prefix routed to server (repeatable) |
| `--route` | | Explicit route as `prefix=target` (repeatable, route mode) |
| `--silent` | | Suppress startup banner and request log output |
| `--log-file` | | Write proxy logs to a file instead of stdout |
| `--config` | | Load config from a YAML file |
| `--dry-run` | | Print effective routing config without starting the proxy |
| `--detach` | | Run in the background; request logs go to the file set by `--log-file` |
| `--version` | | Print version and exit |
| `--help` | | Show help and exit |

### Config files

Create a project-local config:

```bash
merge-port init
```

This writes `.merge-port.yaml` in the current directory:

```yaml
port: 8080
client: 3000
server: 3001
api_prefixes:
  - /api
```

Then run:

```bash
merge-port
```

Flags override config values:

```bash
merge-port --config dev.merge-port.yaml --port 9000
```

Preview a generated config without writing it:

```bash
merge-port init --client 5173 --server 3001 --api-prefix /api --api-prefix /auth --dry-run
```

Route-mode config is also supported:

```yaml
port: 8080
routes:
  - prefix: /api
    target: "3001"
  - prefix: /
    target: "5173"
```

Default config lookup is current-directory only. `--config missing.yaml` is an error; a missing default `.merge-port.yaml` falls back to flag-only usage.

### Dry run and validation

Preview routing without binding a port or contacting upstreams:

```bash
merge-port --client 5173 --server 3001 --dry-run
```

Validate route syntax, listen-port availability, and upstream TCP reachability:

```bash
merge-port validate --client 5173 --server 3001
```

Listen-port validation is a point-in-time check. Another process can still bind the port after validation and before startup.

### discover subcommand

Detect the API prefixes exposed by a running server by probing common OpenAPI/Swagger endpoints:

```bash
merge-port discover --server 3001
```

Output:

```
Detected from http://localhost:3001 (http://localhost:3001/openapi.json):

  --api-prefix /api
  --api-prefix /health

proc-compose.yaml:

  api_prefixes:
    - /api
    - /health
```

Use the detected prefixes with `--api-prefix` to configure routing.

For scripts, use JSON output:

```bash
merge-port discover --server 3001 --json
```

Discovery can write a config file from detected prefixes:

```bash
merge-port discover --server 3001 --client 5173 --write-config .merge-port.yaml
```

### Examples

```bash
# React (5173) + Express (3001), serve on 9000
merge-port --client 5173 --server 3001 --port 9000

# Multiple API prefixes to the same server
merge-port --client 3000 --server 3001 --api-prefix /api --api-prefix /auth --api-prefix /graphql

# Full custom routing (different backends)
merge-port --route /api=3001 --route /auth=3002 --route /=3000

# Then tunnel just one port
tunnel http 8080
```

## How it works

```
Simple mode:
  merge-port --client 3000 --server 3001 --api-prefix /api --api-prefix /auth

  Browser / Tunnel → :8080 (merge-port)
                       ├── /auth/* → localhost:3001 (server)
                       ├── /api/*  → localhost:3001 (server)
                       └── /*      → localhost:3000 (client)

Route mode:
  merge-port --route /api=3001 --route /auth=3002 --route /=3000

  Browser / Tunnel → :8080 (merge-port)
                       ├── /auth/* → localhost:3002
                       ├── /api/*  → localhost:3001
                       └── /*      → localhost:3000
```

Requests are routed by longest prefix match. WebSocket connections (used by dev server hot-reload) are passed through transparently.

In route mode, add a `/=client` catch-all route if you want frontend fallback routing. Without `/`, unmatched paths return `404`.

### Detached mode

Start in the background:

```bash
merge-port --client 5173 --server 3001 --port 8080 --detach
```

The detached child suppresses its banner and writes request logs to the effective log file.

Inspect or stop the detached process:

```bash
merge-port status --port 8080
merge-port stop --port 8080
```

### Health endpoint

merge-port exposes a built-in `/_health` endpoint that returns `200 ok`. This is handled locally by merge-port and never proxied to upstream services — useful for cloud platform liveness probes (Railway, Render, Fly.io).

## Windows

`merge-port` works on Windows with the following limitations:

- **`--detach` is not supported.** Windows does not have a `setsid()` equivalent. The proxy will run in the foreground.

### Install on Windows

```sh
# Recommended: brokit
brokit install merge-port

# Or via Go
go install github.com/anivaryam/merge-port/cmd/mergeport@latest

# Or download a release binary from:
# https://github.com/anivaryam/merge-port/releases
```

## Environment Variables

| Variable | Description |
|----------|-------------|
| `NO_COLOR` | Set to any value to disable ANSI color codes in output |

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Error (port in use, upstream unreachable, runtime failure) |
| `2` | Usage or syntactic error (invalid flags, invalid route/config syntax) |

## Troubleshooting

**Port already in use:**
```
merge-port: listen tcp :8080: bind: address already in use
```
Solution: Use `--port` to specify a different port.

**Upstream server not running:**
```
merge-port: upstream unreachable: dial tcp localhost:3001: connection refused
```
Solution: Ensure your client/server are running before starting merge-port.

**Detach on Windows:**
The `--detach` flag is not supported on Windows. The proxy will run in the foreground.

## Contributing

Contributions welcome! Please open an issue first for significant changes.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing`)
3. Commit your changes (`git commit -am 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing`)
5. Open a Pull Request

## License

MIT License - see [LICENSE](LICENSE) file for details.
