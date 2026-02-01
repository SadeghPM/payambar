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

## Quick Start

### Development

```bash
# Build and run locally
make dev

# Navigate to http://localhost:8080
```

### Production Build

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
# Using pre-built image from GitHub Container Registry
docker-compose up -d

# Or run standalone with GHCR image
docker run -p 8080:8080 \
  -e JWT_SECRET=your-secret-key \
  -v payambar_data:/data \
  ghcr.io/sadeghpm/payambar:latest

# Or build locally
make docker-build
docker-compose up -d
```

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

## API Endpoints

### Authentication
- `POST /api/auth/register` - Register new user
- `POST /api/auth/login` - Login & get JWT token

### Messages (Protected)
- `GET /api/conversations` - Get all conversations
- `DELETE /api/conversations/{id}` - Delete a conversation
- `GET /api/messages?user_id={id}` - Get conversation history
- `PUT /api/messages/{id}/delivered` - Mark as delivered
- `PUT /api/messages/{id}/read` - Mark as read
- `DELETE /api/messages/{id}` - Delete a message

### Profile (Protected)
- `DELETE /api/profile` - Delete account and related data

### WebSocket (Protected)
- `GET /ws` - WebSocket connection for real-time messaging

### WebRTC (Protected)
- `GET /api/webrtc/config` - Get STUN/TURN server configuration

### Public
- `GET /users/{username}` - Get public user profile
- `GET /health` - Health check

## Deployment

### GitHub Actions CI/CD

Push to `main` automatically triggers:
- **Binary builds**: Linux (amd64) and macOS (arm64) binaries
- **Docker image**: Published to GitHub Container Registry at `ghcr.io/sadeghpm/payambar:latest`

Download pre-built binaries from GitHub Actions artifacts or pull the Docker image directly.

### VPS Setup

```bash
# SSH into VPS (Hetzner, DigitalOcean, etc.)
ssh root@your-vps-ip

# Install Docker
curl -fsSL https://get.docker.com -o get-docker.sh
sh get-docker.sh

# Create project directory and download docker-compose.yml
mkdir -p payambar && cd payambar
wget https://raw.githubusercontent.com/sadeghpm/payambar/main/docker-compose.yml

# Set environment variables
echo "JWT_SECRET=$(openssl rand -hex 32)" > .env

# Pull and run pre-built image from GHCR
docker-compose up -d

# Enable auto-start on reboot
docker update --restart unless-stopped payambar
```

### 2. CDN & Voice Call Configuration

If your main domain is behind a CDN (like Cloudflare), WebRTC (voice calls) will require a **direct subdomain** to bypass the CDN for media traffic.

1.  **DNS Records**:
    *   `example.com` -> VPS IP (Proxied by CDN)
    *   `turn.example.com` -> VPS IP (**DNS Only / Unproxied**)
2.  **CDN Page Rules**:
    *   Cache Level: Cache Everything for `/`
    *   Browser Cache TTL: 30 minutes for `/`
3.  **SSL/TLS**: Enable Full mode.
4.  **Firewall**: Open ports `3478/tcp`, `3478/udp`, and `49152:65535/udp` (for TURN relay) on your VPS.

### 3. Voice Calling (WebRTC) Setup

The application includes a bundled **Coturn** server. To enable it in production:

1. Set `TURN_ENABLED=true`
2. Set `TURN_EXTERNAL_IP` to your VPS Public IP.
3. Set `TURN_REALM` to your direct subdomain (e.g., `turn.example.com`).
4. Configure `TURN_SERVER`, `TURN_USERNAME`, and `TURN_PASSWORD` in your environment.

### 4. Environment Variables

Create `.env` file in VPS:

```bash
JWT_SECRET=your-secure-random-key
DATABASE_PATH=/data/payambar.db
CORS_ORIGINS=https://yourdomain.com
ENVIRONMENT=production

# Voice Calling (WebRTC)
TURN_ENABLED=true
TURN_EXTERNAL_IP=YOUR_VPS_PUBLIC_IP
TURN_REALM=turn.yourdomain.com
STUN_SERVERS=stun:turn.yourdomain.com:3478
TURN_SERVER=turn:turn.yourdomain.com:3478
TURN_USERNAME=payambar
TURN_PASSWORD=your-strong-password
```

### 4. Backup SQLite Database

```bash
# Cron job to backup daily
0 2 * * * cd /home/ubuntu/payambar && docker-compose exec -T payambar \
  sh -c 'tar czf /data/backup.tar.gz /data/payambar.db' && \
  aws s3 cp /data/backup.tar.gz s3://your-bucket/backups/
```

## Configuration

### Environment Variables

```bash
PORT                # Server port (default: 8080)
ENVIRONMENT         # "development" or "production"
DATABASE_PATH       # SQLite file path (default: /data/payambar.db)
JWT_SECRET          # Secret key for JWT signing (REQUIRED in production)
CORS_ORIGINS        # CORS allowed origins (default: *)
MAX_UPLOAD_SIZE     # Max file upload size in bytes (default: 10485760 = 10MB)
FILE_STORAGE_PATH   # Directory for uploaded files (default: /data/uploads)
STUN_SERVERS        # Comma-separated STUN servers (default: stun:stun.l.google.com:19302)
TURN_SERVER         # TURN server URL
TURN_USERNAME       # TURN username
TURN_PASSWORD       # TURN password
TURN_ENABLED        # Enable bundled Coturn server (default: false)
TURN_EXTERNAL_IP    # Public IP for bundled Coturn
TURN_REALM          # Realm (domain) for bundled Coturn
```

## Development

### Prerequisites
- Go 1.25.1 or higher
- Make
- SQLite development libraries (for CGO)

### Build Frontend
```bash
make build-frontend
# Output: frontend assets → cmd/payambar/static/
```

### Build Backend
```bash
make build-backend
# Output: binary with embedded frontend → bin/payambar
```

### Local Development
```bash
make dev
# Runs backend with hot-reload for Go
# Server starts on http://localhost:8080
```

### Available Make Commands
```bash
make dev              # Development mode with go run
make build-all        # Build for current OS
make docker-build     # Build Docker image locally
make test             # Run all tests with coverage
make fmt              # Format code with gofmt
make clean            # Remove build artifacts
```

### Testing

Frontend WebSocket connection:
```javascript
// In browser console
const ws = new WebSocket('ws://localhost:8080/ws', ['Authorization', 'Bearer <your-token>']);
ws.onmessage = (e) => console.log(JSON.parse(e.data));
ws.send(JSON.stringify({type: 'message', receiver_id: 2, content: 'Hello'}));
```

## Performance & Scaling Notes

### Current Constraints
- Single VPS backend (no horizontal scaling)
- SQLite database with WAL mode (concurrent writes and reads)
- In-memory WebSocket hub (connections lost on restart)

### To Scale in Future
1. Replace SQLite with PostgreSQL
2. Add Redis for WebSocket state
3. Deploy multiple backend instances with load balancer
4. Use message queue (RabbitMQ/Kafka) for reliability

## Security Checklist

- ✅ HTTPS enforced via CDN
- ✅ JWT token validation on protected endpoints
- ✅ Password hashing with bcrypt
- ✅ CORS headers configured
- ✅ WebSocket origin validation
- ✅ Rate limiting on auth endpoints
- ⚠️ Input sanitization (basic XSS prevention in frontend)

## Troubleshooting

### WebSocket connection fails
- Check CDN supports WebSocket (Cloudflare Pro required)
- Verify VPS firewall allows port 8080
- Check JWT token is valid

### Database locks
- SQLite has single-writer limitation
- For high concurrency, upgrade to PostgreSQL

### Frontend not loading
- Check static files are embedded: `go build -v ./cmd/payambar`
- Verify CDN cache headers aren't blocking HTML

## License

MIT

## Contributing

This is a minimal, purposefully simple messenger. Major features (groups, voice, etc.) are out of scope by design.
