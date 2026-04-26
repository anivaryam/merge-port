# merge-port

A local reverse proxy that merges your client and server into a single port. Run your frontend and backend on separate ports, then expose them through one — perfect for tunneling.

## Install

**Requirements:** Go 1.22 or later

**Quick install** (Linux/macOS):

```bash
curl -sSL https://raw.githubusercontent.com/anivaryam/merge-port/main/install.sh | sh
```

**Go install**:

```bash
go install github.com/anivaryam/merge-port/cmd/mergeport@latest
```

**Build from source**:

```bash
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
| `--silent` | | Suppress all proxy log output |
| `--log-file` | | Write proxy logs to a file instead of stdout |
| `--detach` | | Run in the background (implies `--silent`); logs go to the file set by `--log-file` |
| `--version` | | Print version and exit |
| `--help` | | Show help and exit |

### discover subcommand

Detect the API prefixes exposed by a running server by probing common OpenAPI/Swagger endpoints:

```bash
merge-port discover --server 3001
```

Output:

```
Detected API prefixes:
  /api
  /health
```

Use the detected prefixes with `--api-prefix` to configure routing.

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

### Health endpoint

merge-port exposes a built-in `/_health` endpoint that returns `200 ok`. This is handled locally by merge-port and never proxied to upstream services — useful for cloud platform liveness probes (Railway, Render, Fly.io).

## Windows

`merge-port` works on Windows with the following limitations:

- **`--detach` is not supported.** Windows does not have a `setsid()` equivalent. The proxy will run in the foreground.

### Install on Windows

```bash
go install github.com/anivaryam/merge-port/cmd/mergeport@latest
```

Or download a release binary from: https://github.com/anivaryam/merge-port/releases

## Environment Variables

| Variable | Description |
|----------|-------------|
| `NO_COLOR` | Set to any value to disable ANSI color codes in output |

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Error (invalid flags, port in use, upstream unreachable) |

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
