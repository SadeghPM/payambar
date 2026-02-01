# Payambar - Minimal Telegram-like Messenger

A minimal 1-to-1 messaging system built with Go 1.25+ (backend), WebSocket (real-time), SQLite (database), and vanilla JavaScript PWA (frontend).

## Features

- ✅ Username-based registration & login (JWT auth)
- ✅ Real-time 1-to-1 messaging via WebSocket
- ✅ One-to-one voice calling (WebRTC)
- ✅ Message status tracking (sent, delivered, read)
- ✅ Public shareable user profiles (`/u/{username}`)
- ✅ Account deletion and conversation cleanup
- ✅ Progressive Web App (PWA) - installable on desktop & mobile
- ✅ RTL layout & Persian (Farsi) language support
- ✅ SQLite database (zero setup)
- ✅ Single Go binary with embedded frontend assets
- ✅ Served behind CDN with SSL termination
- ✅ Auth rate limiting on login/register

## Architecture

### Backend (Go)
- **API**: REST endpoints for auth, messages, profiles
- **WebSocket**: Real-time messaging at `/ws`
- **Database**: SQLite with automatic schema migration
- **Authentication**: JWT tokens (stateless)
- **Embedded Frontend**: Static assets compiled into binary

### Frontend (Vanilla JS PWA)
- **Layout**: Two-panel (chat list, message view)
- **Real-time**: WebSocket client with auto-reconnect
- **Performance**: Optimized conversation list loading
- **Offline**: Service worker with cache-first strategy
- **Responsive**: Mobile-first design (RTL aware)
- **Persian**: Full RTL + Farsi language support
- **Installable**: Web app manifest for desktop/mobile installation

## Project Structure

```
payambar/
├── cmd/payambar/           # Entry point
│   └── main.go
├── internal/
│   ├── auth/               # Authentication service
│   ├── db/                 # Database & migrations
│   ├── handlers/           # HTTP handlers
│   ├── models/             # Data models
│   └── ws/                 # WebSocket handler
├── pkg/
│   └── config/             # Configuration
├── frontend/               # Frontend source
│   ├── index.html
│   ├── app.js
│   ├── styles.css
│   ├── manifest.json       # PWA manifest
│   └── sw.js               # Service worker
├── static/                 # Built frontend (embedded)
├── Makefile                # Build commands
├── Dockerfile              # Container image
└── docker-compose.yml      # Local docker setup
```

## Development

### Prerequisites
- Go 1.25.1 or higher
- Make
- SQLite development libraries (for CGO)

### Local Development
```bash
# Build frontend assets and run backend with hot-reload
make dev

# Navigate to http://localhost:8080
```

### Testing & Quality
```bash
# Run all tests with coverage
make test

# Format code with gofmt
make fmt

# Test WebSocket manually (in browser console)
const ws = new WebSocket('ws://localhost:8080/ws', ['Authorization', 'Bearer <your-token>']);
ws.onmessage = (e) => console.log(JSON.parse(e.data));
ws.send(JSON.stringify({type: 'message', receiver_id: 2, content: 'Hello'}));
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | 8080 | HTTP server port |
| `ENVIRONMENT` | development | "development" or "production" |
| `DATABASE_PATH` | /data/payambar.db | SQLite database file |
| `JWT_SECRET` | (required) | Secret for JWT signing |
| `CORS_ORIGINS` | * | CORS allowed origins |
| `MAX_UPLOAD_SIZE` | 10485760 | Max file size (bytes) |
| `FILE_STORAGE_PATH` | /data/uploads | Directory for uploads |
| `STUN_SERVERS` | stun:stun.l.google.com:19302 | Comma-separated STUN servers |
| `TURN_SERVER` | (optional) | TURN server URL (e.g. turn:domain:3478) |
| `TURN_USERNAME` | (optional) | TURN server username |
| `TURN_PASSWORD` | (optional) | TURN server password |
| `TURN_ENABLED` | false | Enable bundled Coturn server |
| `TURN_EXTERNAL_IP` | (optional) | Public IP for bundled Coturn |
| `TURN_REALM` | (optional) | Realm for bundled Coturn |

## Deployment

### Production Build (Binary)
```bash
# Build binary with embedded frontend
make build-all

# Run with environment variables
PORT=8080 \
  DATABASE_PATH=/data/payambar.db \
  JWT_SECRET=your-secret-key \
  ./bin/payambar
```

### Docker
```bash
# Pull and run pre-built image from GHCR
docker run -p 8080:8080 \
  -e JWT_SECRET=your-secure-key \
  -v payambar_data:/data \
  ghcr.io/sadeghpm/payambar:latest

# Or build locally
make docker-build
docker-compose up -d
```

### VPS Setup
1. **Prepare Server**: Install Docker on your VPS.
2. **Setup Project**: Download `docker-compose.yml` and create a `.env` file with your `JWT_SECRET`.
3. **Run**: `docker-compose up -d`.

### WebRTC & CDN Configuration
If using a CDN (like Cloudflare), WebRTC requires a **direct subdomain** to bypass the CDN for media traffic.

1. **DNS**: Point `turn.example.com` directly to your VPS IP (DNS Only/Unproxied).
2. **Firewall**: Open ports `3478/tcp`, `3478/udp`, and the UDP relay range (default `49152:49252/udp`).
3. **Performance**: It is highly recommended to use `network_mode: "host"` in `docker-compose.yml` to avoid UDP NAT issues.

To use the bundled Coturn server, set `TURN_ENABLED=true` and configure the `TURN_*` environment variables accordingly.

### Backup
```bash
# Cron job to backup SQLite database daily
0 2 * * * cd /path/to/payambar && docker-compose exec -T payambar \
  sh -c 'tar czf /data/backup.tar.gz /data/payambar.db'
```

## Performance & Scaling Notes

- **Current Constraints**: Single VPS backend, SQLite database with WAL mode, In-memory WebSocket hub.
- **Future Scaling**:
    1. Replace SQLite with PostgreSQL.
    2. Add Redis for distributed WebSocket state.
    3. Deploy multiple backend instances with a load balancer.
    4. Use S3 for file storage.

## Security Checklist

- ✅ Password hashing with bcrypt.
- ✅ JWT token validation on protected endpoints.
- ✅ Auth rate limiting on login/register.
- ✅ CORS headers configuration.
- ✅ WebSocket origin validation.
- ⚠️ Input sanitization (basic XSS prevention in frontend).

## Troubleshooting

- **WebSocket Fails**: Check if your CDN supports WebSockets or if a firewall is blocking port 8080.
- **Database Locked**: SQLite single-writer limitation. Upgrade to PostgreSQL for high concurrency.
- **Frontend not loading**: Ensure static files are built and embedded: `make build-all`.

## License

MIT

## Contributing

This is a minimal, purposefully simple messenger. Major features (groups, etc.) are currently out of scope by design.
