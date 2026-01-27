# Payambar - AI Coding Instructions

## Architecture Overview

Payambar is a minimal 1-to-1 Telegram-like messenger with a **single Go binary** that embeds all frontend assets.

**Data Flow:**
1. Frontend (vanilla JS + Vue 3 CDN) → REST API + WebSocket → Go Handlers → SQLite
2. Real-time messages flow through WebSocket Hub (`internal/ws/ws.go`) which maintains per-user client connections
3. Frontend assets in `frontend/` are copied to `cmd/payambar/static/` at build time, then embedded via `//go:embed`

**Key Architectural Decisions:**
- No separate frontend build/bundler - vanilla JS with Vue loaded from CDN
- SQLite with auto-migration on startup (`internal/db/db.go`) - zero database setup
- JWT auth with 24h expiration, stateless validation
- RTL/Persian language as primary UI (Farsi strings in frontend)

## Project Structure

| Path | Purpose |
|------|---------|
| `cmd/payambar/main.go` | Entry point, router setup, embeds `static/` |
| `internal/handlers/` | HTTP handlers (auth.go, messages.go) |
| `internal/ws/ws.go` | WebSocket hub - client registry, message broadcast |
| `internal/auth/auth.go` | JWT generation/validation, bcrypt password hashing |
| `internal/db/db.go` | SQLite connection, schema migrations |
| `internal/models/` | Shared data structures (User, Message, File) |
| `frontend/` | Source frontend files |
| `cmd/payambar/static/` | Build artifact (generated, gitignored) |

## Build & Run Commands

```bash
make dev          # Build frontend + run with go run (hot reload backend only)
make build-all    # Production build → bin/payambar
make docker-build # Docker image
go test ./...     # Run all tests
```

**Important:** Frontend changes require `make build-frontend` or full `make dev` - no hot reload for frontend.

## Code Conventions

### Backend (Go)
- Handlers receive `*gin.Context`, extract `user_id` from context (set by auth middleware)
- Database access via raw SQL with `database/sql` - no ORM
- Error responses: `c.JSON(http.StatusXxx, gin.H{"error": "message"})`
- User ID passed through middleware: `userID, _ := c.Get("user_id"); userID.(int)`

### Frontend (JavaScript)
- Single Vue app in `app.js` using Options API
- API calls use `fetch()` with Authorization header
- WebSocket reconnect with exponential backoff (see `connectWebSocket()`)
- Persian numerals for time display (`formatTime()` method)
- State persisted in localStorage: `token`, `userId`, `username`

### WebSocket Protocol
Messages are JSON with `type` field:
```javascript
// Sending message
{ "type": "message", "receiver_id": 123, "content": "text", "client_message_id": "uuid" }

// Status update
{ "type": "status_update", "message_id": 456, "status": "delivered|read" }
```

## Database Schema

Tables: `users`, `messages`, `conversations`, `files`

Key relationships:
- `messages.sender_id` / `receiver_id` → `users.id`
- `files.message_id` → `messages.id`
- `conversations.participants` stores comma-separated user IDs (not normalized)

## Testing

```bash
go test ./internal/handlers/ -v  # Handler tests
go test ./internal/ws/ -v        # WebSocket tests
```

Tests use in-memory SQLite (`:memory:`). Frontend tests in `frontend/app.test.js`.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | 8080 | HTTP server port |
| `DATABASE_PATH` | `/tmp/payambar.db` | SQLite file path |
| `JWT_SECRET` | required | JWT signing key (must set in production) |
| `FILE_STORAGE_PATH` | `./data/uploads` | Uploaded files directory |

## Common Patterns

**Adding a new API endpoint:**
1. Add handler method in `internal/handlers/`
2. Register route in `cmd/payambar/main.go` under `api` (public) or `protected` (requires auth)

**Adding a new WebSocket event type:**
1. Define struct in `internal/ws/ws.go`
2. Handle in `handleClientMessage()` switch statement
3. Update frontend `handleWsMessage()` in `app.js`

**Modifying database schema:**
Edit the `migrate()` function in `internal/db/db.go` - uses `CREATE TABLE IF NOT EXISTS` pattern.
