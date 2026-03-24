# Neuron Registry — Architecture

## What is this?

The Neuron Registry is the backend server that powers `neuron publish`,
`neuron install`, and `neuron search` commands from the Neuron CLI.
It is a simple Go HTTP server that stores and serves AI tool packages.

---

## API Endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/v1/publish` | Upload a new package (multipart tar.gz + manifest) |
| `GET` | `/v1/packages/:name` | Get latest package metadata |
| `GET` | `/v1/packages/:name/:version` | Get specific version metadata |
| `GET` | `/v1/packages/:name/:version/download` | Download package tarball |
| `GET` | `/v1/packages/:name/versions` | List all versions of a package |
| `GET` | `/v1/search?q=query` | Search packages by name/description |

---

## Request / Response shapes

### POST /v1/publish
Multipart form with two fields:
- `manifest` — the neuron.json contents as JSON string
- `tarball` — the .tar.gz file bytes

Response:
```json
{ "name": "my-agent", "version": "1.0.0", "message": "published successfully" }
```

### GET /v1/packages/:name
Response:
```json
{
  "name": "my-agent",
  "version": "1.0.0",
  "description": "Does something useful",
  "runtime": "python",
  "permissions": ["http"],
  "published_at": "2025-01-01T00:00:00Z"
}
```

### GET /v1/search?q=query
Response:
```json
{
  "results": [
    { "name": "my-agent", "version": "1.0.0", "description": "Does something useful" }
  ]
}
```

---

## Project structure

```
neuron-registry/
├── cmd/
│   └── server/
│       └── main.go           # HTTP server entry point
├── pkg/
│   ├── handlers/
│   │   ├── publish.go        # POST /v1/publish handler
│   │   ├── packages.go       # GET /v1/packages handlers
│   │   └── search.go         # GET /v1/search handler
│   ├── store/
│   │   ├── store.go          # Store interface
│   │   ├── filestore.go      # Local filesystem implementation
│   │   └── index.go          # In-memory search index
│   └── middleware/
│       └── middleware.go     # Logging, CORS, error recovery
├── data/
│   ├── packages/             # Stored tarballs: data/packages/<name>/<version>.tar.gz
│   └── index.json            # Package metadata index
├── ARCHITECTURE.md           # This file — always read before making changes
└── go.mod
```

---

## Key design decisions

**Filesystem-first storage.** No database. Packages are stored as
tar.gz files at `data/packages/<name>/<version>.tar.gz`. Metadata
lives in `data/index.json` as a flat JSON map. Simple to deploy,
simple to back up, simple to migrate later.

**In-memory search index.** On startup, load `index.json` into memory
as a map. Search is a simple string contains check on name and
description. Fast enough for thousands of packages.

**No auth for MVP.** Publishing is open for now. Auth comes in Phase 3
when the verified badge program launches.

**CORS enabled.** The registry should be accessible from any client
including browser-based tools.

---

## Store interface

```go
type Store interface {
    Save(name, version string, manifest []byte, tarball []byte) error
    GetManifest(name, version string) ([]byte, error)
    GetTarball(name, version string) ([]byte, error)
    ListVersions(name string) ([]string, error)
    Search(query string) ([]PackageInfo, error)
    GetLatest(name string) (string, error)
}
```

## PackageInfo struct

```go
type PackageInfo struct {
    Name        string    `json:"name"`
    Version     string    `json:"version"`
    Description string    `json:"description"`
    Runtime     string    `json:"runtime"`
    Permissions []string  `json:"permissions"`
    PublishedAt time.Time `json:"published_at"`
}
```

---

## Tech stack

- **Language:** Go
- **HTTP router:** standard library `net/http` with `ServeMux`
- **Storage:** local filesystem + JSON index file
- **Deployment:** Railway (single binary, no DB needed)

---

## What to always do before making changes

1. Read this file first.
2. Keep changes scoped to one package at a time.
3. Never write to files outside the project directory.
4. Always write actual files — do not describe what you would write.
5. After writing a file, confirm it compiles or passes basic syntax checks.
