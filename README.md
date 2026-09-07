<div align="center">

# syncpulse-backend

**The Go service at the center of SyncPulse** — REST API, WebSocket hub, and the gRPC bridge to the AI booking agent.

[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?logo=postgresql&logoColor=white)](https://postgresql.org)
[![gRPC](https://img.shields.io/badge/gRPC-client-4285F4?logo=googlecloud&logoColor=white)](https://grpc.io)
[![WebSocket](https://img.shields.io/badge/WebSocket-coder%2Fwebsocket-black)](https://github.com/coder/websocket)
[![JWT](https://img.shields.io/badge/Auth-JWT%20%2B%20bcrypt-25A162)](https://jwt.io)
[![Tests](https://img.shields.io/badge/tests-passing-brightgreen)](#testing)
[![Status](https://img.shields.io/badge/status-portfolio%20demo-orange)](#security--known-limitations)

Part of the [SyncPulse](#) system · [Flutter client →](#) · [AI Booker service →](#)

[Architecture](#architecture) · [API Reference](#rest-api-reference) · [WebSocket Protocol](#websocket-protocol) · [Database](#database-schema) · [Setup](#setup--installation) · [Security](#security--known-limitations)

</div>

---

## Overview

`syncpulse-backend` is a **Go, framework-free** (`net/http` + Go 1.22 method-pattern routing) service that owns three responsibilities:

1. **REST API** — auth, friends, message history
2. **WebSocket hub** — real-time bidirectional message delivery
3. **gRPC client** — bridges chat messages addressed to the reserved `llm` user to the [Booker AI service](#), and relays the response back over the same WebSocket

It talks to exactly one datastore (`chatdb`) and one downstream service (the Booker, over gRPC). Everything else — the AI logic, the slots table, the LLM provider — is intentionally out of scope for this repo.

---

## Architecture

### Internal layout

```mermaid
graph TB
    subgraph Entry["cmd/server"]
        Main[main.go<br/>wiring · mux · CORS · graceful startup]
    end

    subgraph Internal["internal/"]
        Auth["auth/<br/>JWT · bcrypt · refresh rotation<br/>RequireAuth middleware"]
        Chat["chat/<br/>conversation ID · history"]
        WS["ws/<br/>Hub · Client registry<br/>WS handler · CORS middleware"]
        Booker["booker/<br/>gRPC client wrapper<br/>HTTP /booker adapter"]
        LLM["llm/<br/>reserved 'llm' username<br/>routing constant"]
        Store["store/<br/>pgx/v5 data access<br/>users · messages · friendships · tokens"]
    end

    subgraph External["External"]
        DB[("PostgreSQL<br/>chatdb")]
        BookerSvc["Booker gRPC Service<br/>:50051"]
    end

    Main --> Auth
    Main --> Chat
    Main --> WS
    Main --> Booker

    Auth --> Store
    Chat --> Store
    WS --> Store
    WS --> LLM
    WS --> Booker
    Booker --> Store

    Store --> DB
    Booker -.gRPC.-> BookerSvc

    style Entry fill:#00ADD815,stroke:#00ADD8
    style Internal fill:#f5f5f5,stroke:#999
    style External fill:#fff3e0,stroke:#e65100
```

**Design notes:**
- **Hexagonal-ish layering** — `internal/store` is the only package that imports `pgx`; every other package depends on `*store.PostgresStore`, not on Postgres directly. Swapping the DB driver touches one file.
- **`llm/`** is deliberately tiny — a single constant (`llm.Username = "llm"`) and a case-insensitive matcher. Both the REST `/assistant` handler and the WS router import it, so the reserved username is defined exactly once.
- **`booker.Booker`** is an interface (`ProcessUserMessage(ctx, userID, message) (*Response, error)`), not a concrete type — the WS handler and the REST `/booker` handler both depend on the interface, which is what makes `booker/handler_test.go` able to fake the gRPC client entirely.

### Request flow — REST vs. WebSocket

```mermaid
graph LR
    Client[Flutter Client]

    Client -- "POST /login, /users, /refresh" --> Public["Public routes<br/>(no token)"]
    Client -- "GET /friends, /history<br/>POST /friends, /booker, /logout" --> Protected["RequireAuth middleware<br/>validates Bearer JWT"]
    Client -- "ws://.../ws?token=..." --> WSEntry["WS handler<br/>validates token in query"]

    Public --> Handlers[auth.Service handlers]
    Protected --> Handlers
    WSEntry --> Hub[ws.Hub<br/>per-user Client registry]

    Hub -- "recipient online" --> Push["push over their socket"]
    Hub -- "recipient offline" --> Persist["persist only<br/>(fetched later via /history)"]
    Hub -- "recipient == llm" --> Route["route to Booker gRPC"]

    style Public fill:#e8f5e9,stroke:#2e7d32
    style Protected fill:#fff3e0,stroke:#e65100
    style WSEntry fill:#e3f2fd,stroke:#1565c0
```

All CORS handling happens in **one outer middleware** wrapping the entire mux — `net/http`'s `ServeMux` won't match `OPTIONS /login` against a `POST /login` route, so preflight requests are intercepted before they'd otherwise 405.

---

## REST API Reference

| Method | Path | Auth | Body | Description |
|---|---|:---:|---|---|
| `POST` | `/users` | ❌ | `{username, password}` | Create a new user account |
| `POST` | `/login` | ❌ | `{username, password}` | Returns access token (15 min) + refresh token (30 days) |
| `POST` | `/refresh` | ❌ | `{refresh_token}` | Rotates refresh token, issues a fresh pair |
| `GET` | `/friends` | ✅ | — | List accepted friendships |
| `POST` | `/friends` | ✅ | `{friend_username}` | Add a friend (idempotent upsert) |
| `GET` | `/assistant` | ✅ | — | Returns the reserved `llm` user (id + username) |
| `GET` | `/history?friend_id&limit` | ✅ | — | Message history for a conversation, newest first |
| `POST` | `/booker` | ✅ | `{message}` | Synchronous REST fallback to the AI agent (bypasses WS) |
| `POST` | `/logout` | ✅ | — | Revokes **all** refresh tokens for the user |
| `GET` | `/ws?token=` | ✅ (query param) | — | Upgrades to WebSocket |

**Auth header:** `Authorization: Bearer <access_token>` — enforced by `auth.Service.RequireAuth`, which injects `userID` into the request context for handlers to read via `auth.UserIDFromContext`.

---

## WebSocket Protocol

**Endpoint:** `ws://host:8080/ws?token=<JWT>`

<table>
<tr><th>Outbound (client → server)</th><th>Inbound (server → client)</th></tr>
<tr valign="top">
<td>

```json
{
  "recipient_username": "bob",
  "content": "Hello"
}
```
</td>
<td>

```json
{
  "id": 42,
  "conversation_id": 2000001,
  "sender_id": 2,
  "sender_username": "bob",
  "content": "Hi!",
  "created_at": "2026-09-07T10:30:00Z"
}
```
</td>
</tr>
</table>

If `recipient_username` is the reserved `llm` user, the message is routed to the Booker agent instead of another client — the reply comes back over the **same socket**, tagged with `tool_executed: true/false` so the client can render a "used a tool" indicator.

**Connection lifecycle:**

| Behavior | Value |
|---|---|
| Read timeout (idle) | 60s → connection closed |
| Server ping interval | every 5 minutes |
| Hub cleanup sweep | every 30s — pings all clients, drops dead ones |
| Send buffer per client | 256 messages, then **dropped** (no unbounded queueing) |
| Offline recipient | message persisted only — client fetches via `GET /history` on reconnect |

> Full client-integration guide (reconnection strategy, gap recovery, JS examples) lives in [`ws/README.md`](internal/ws/README.md) inside this repo.

---

## Database Schema

`chatdb` — owned entirely by this service.

```mermaid
erDiagram
    users ||--o{ messages : sends
    users ||--o{ friendships : "user_id"
    users ||--o{ friendships : "friend_id"
    users ||--o{ refresh_tokens : owns

    users {
        bigserial id PK
        text username UK
        text password_hash
        timestamptz created_at
    }
    messages {
        bigserial id PK
        bigint conversation_id "min(a,b)*1e6 + max(a,b)"
        bigint sender_id FK
        text content
        timestamptz created_at
    }
    friendships {
        int8 user_id FK
        int8 friend_id FK
        varchar status "accepted"
        timestamptz created_at
    }
    refresh_tokens {
        bigserial id PK
        bigint user_id FK
        text token_hash UK "SHA-256, raw never stored"
        timestamptz expires_at
        boolean revoked
        timestamptz created_at
    }
```

- `messages` is indexed on `(conversation_id, created_at DESC, id DESC)` — the exact order `GET /history` queries in.
- `friendships` is a composite-PK join table; `AddFriendship` is an upsert (`ON CONFLICT ... DO UPDATE`), so re-adding a friend is safe.
- The reserved `llm` user is seeded via `migrations/0002_llm_user.sql` with an unguessable bcrypt hash — it exists in `users` like anyone else but can never log in.

---

## Auth Flow

```mermaid
sequenceDiagram
    actor C as Client
    participant S as Backend
    participant DB as chatdb

    C->>S: POST /login {username, password}
    S->>DB: SELECT user, bcrypt.Compare
    DB-->>S: user row
    S->>S: sign JWT (HS256, 15 min)
    S->>S: generate 32-byte random refresh token
    S->>DB: INSERT SHA-256(refresh_token)
    S-->>C: {access_token, refresh_token}

    Note over C,S: ...15 minutes later, access token expired...

    C->>S: POST /refresh {refresh_token}
    S->>DB: lookup hash, check !revoked && !expired
    DB-->>S: token row
    S->>DB: revoke old token (rotation)
    S->>DB: insert new token hash
    S-->>C: {new access_token, new refresh_token}
```

Refresh tokens are **single-use** — every refresh revokes the old one and issues a new pair, so a stolen-then-replayed refresh token gets invalidated the moment the legitimate client refreshes again.

---

## Project Structure

```
chat-backend/
├── cmd/
│   └── server/
│       └── main.go                 # entry point, dependency wiring, mux
├── internal/
│   ├── auth/
│   │   ├── auth.go                 # Service, token generation/validation
│   │   ├── handler.go              # HTTP handlers
│   │   └── middleware.go           # RequireAuth, context helpers
│   ├── chat/
│   │   ├── service.go              # ConversationID, GetHistory
│   │   └── handler.go              # GET /history
│   ├── booker/
│   │   ├── client.go                # gRPC client wrapper
│   │   ├── handler.go               # POST /booker
│   │   ├── handler_test.go          # table-driven tests w/ fake Booker
│   │   └── proto/                   # generated protobuf + gRPC stubs
│   ├── ws/
│   │   ├── hub.go                   # client registry, broadcast, cleanup
│   │   ├── handler.go               # WS upgrade, read/write loop, LLM routing
│   │   ├── middleware.go            # CORS
│   │   └── README.md                # client integration guide
│   ├── llm/
│   │   └── llm.go                   # reserved username constant + matcher
│   └── store/
│       ├── postgres.go              # pool connection, AreFriends
│       ├── messages.go              # Message/User models, CRUD
│       ├── friendship.go            # friend graph queries
│       └── refresh_tokens.go        # token CRUD
├── migrations/
│   ├── 0001_init.sql
│   ├── 0002_llm_user.sql
│   └── refresh_tokens.sql
└── bin/                              # compiled binaries (gitignored)
```

---

## Setup & Installation

### Prerequisites
- Go 1.21+
- PostgreSQL 16+
- The [Booker service](#) running and reachable (gRPC)

### 1. Database

```bash
psql -U postgres -c "CREATE DATABASE chatdb;"
psql -U postgres -d chatdb -f migrations/0001_init.sql
psql -U postgres -d chatdb -f migrations/0002_llm_user.sql
psql -U postgres -d chatdb -f migrations/refresh_tokens.sql
```

### 2. Environment

```bash
cat > .env << 'EOF'
DATABASE_URL=postgres://postgres:devpass@localhost:5432/chatdb?sslmode=disable
BOOKER_GRPC_ADDR=localhost:50051
JWT_SECRET=<generate-a-real-secret-here>
EOF
```

| Variable | Required | Default | Notes |
|---|:---:|---|---|
| `DATABASE_URL` | ✅ | — | Fails fast on startup if unset |
| `BOOKER_GRPC_ADDR` | ✅ | — | Fails fast on startup if unset |
| `JWT_SECRET` | ⚠️ | hardcoded fallback | **Set this explicitly** — see [Security](#security--known-limitations) |

### 3. Run

```bash
go mod download
go build -o bin/server ./cmd/server
./bin/server
# Server listening on :8080
```

---

## Testing

```bash
go test ./...
```

`internal/booker/handler_test.go` covers the `/booker` REST handler end-to-end against a **fake `Booker`** (success, missing auth, empty message, invalid JSON, downstream RPC failure) — no live gRPC connection or database needed to run it.

---

## Security & Known Limitations

This service was built in a 5-day sprint as part of a portfolio project — see the [SyncPulse master README](#) for the full picture. Backend-specific items to fix before any real deployment:

| Issue | Where | Fix |
|---|---|---|
| Hardcoded JWT secret fallback | `auth/auth.go` | Fail startup if `JWT_SECRET` is unset — never fall back silently |
| CORS allows any `localhost`/`127.0.0.1` origin | `ws/middleware.go` | Explicit allow-list per environment |
| WS accepts with no origin check beyond the CORS layer | `ws/handler.go` | Add `OriginPatterns` for production |
| No rate limiting | login, `/booker`, WS message loop | Add per-IP/per-user limiting at the edge or via middleware |
| `bcrypt.DefaultCost` | `auth/auth.go` | Fine for a demo; bump for production hardware budgets |
| Backpressure = silent drop | `ws/hub.go`, 256-msg buffer | Acceptable for chat; log/alert on sustained drops in prod |

What's already handled well and worth keeping as-is: refresh token **rotation with single-use semantics**, SHA-256-at-rest token hashing (raw tokens never persisted), and the clean separation between the `Booker` interface and its gRPC implementation (makes the whole AI integration point testable without a live agent).

---

## License

MIT — see [LICENSE](LICENSE).

<div align="center">

[← Back to SyncPulse](#) · [Flutter client →](#) · [AI Booker service →](#)

</div>
