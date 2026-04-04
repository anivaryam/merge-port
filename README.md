# merge-port

A local reverse proxy that merges your client and server into a single port. Run your frontend and backend on separate ports, then expose them through one — perfect for tunneling.

## Install

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
