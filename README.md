# merge-port

A local reverse proxy that merges your client and server into a single port. Run your frontend and backend on separate ports, then expose them through one — perfect for tunneling.

## Install

Download the latest binary from [Releases](https://github.com/anivaryam/merge-port/releases), or build from source:

```bash
make build
make install  # copies to ~/.local/bin/
```

## Usage

```bash
merge-port --client 3000 --server 3001
```

This starts a proxy on port `8080` (default) that routes:
- `/api/*` → `localhost:3001` (your server)
- Everything else → `localhost:3000` (your client)

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--client` | (required) | Client/frontend port |
| `--server` | (required) | Server/backend port |
| `--port` | `8080` | Port to listen on |
| `--api-prefix` | `/api` | Path prefix to route to server |

### Examples

```bash
# React (5173) + Express (3001), serve on 9000
merge-port --client 5173 --server 3001 --port 9000

# Custom API prefix
merge-port --client 3000 --server 3001 --api-prefix /api/v1

# Then tunnel just one port
tunnel http 8080
```

## How it works

```
Browser / Tunnel → :8080 (merge-port)
                     ├── /api/*  → localhost:3001 (server)
                     └── /*      → localhost:3000 (client)
```

Requests are routed by longest prefix match. WebSocket connections (used by dev server hot-reload) are passed through transparently.
