# Payambar Project Structure

## Technology Stack

- **Backend**: Go 1.23+ with Gin router, gorilla/websocket
- **Database**: SQLite (single file, no setup)
- **Frontend**: Vanilla JavaScript (no framework), PWA with service worker
- **Authentication**: JWT tokens with bcrypt password hashing
- **Real-time**: WebSocket with auto-reconnect
- **Deployment**: Docker + Docker Compose, single binary with embedded assets
- **CDN**: Cloudflare (SSL termination, edge caching)

## Project Layout

```
payambar/
├── cmd/
│   └── payambar/
│       └── main.go          # Application entrypoint
├── internal/
│   ├── auth/                # Authentication service (JWT, registration, login)
│   ├── db/
│   │   └── db.go            # SQLite database initialization & migrations
│   ├── handlers/
│   │   ├── auth.go          # Auth HTTP handlers & middleware
│   │   └── messages.go      # Message & conversation HTTP handlers
│   ├── models/
│   │   └── models.go        # Data structures (User, Message, File)
│   └── ws/
│       └── ws.go            # WebSocket hub & connection management
├── pkg/
│   └── config/
│       └── config.go        # Environment configuration loading
├── frontend/                # Frontend source files
│   ├── index.html           # Main HTML template
│   ├── app.js               # Frontend application logic
│   ├── styles.css           # Responsive RTL CSS
│   ├── manifest.json        # PWA manifest
│   ├── sw.js                # Service worker for offline support
│   ├── public/              # Static assets (icons, etc.)
├── static/                  # Built frontend (embedded in binary)
├── go.mod                   # Go module definition
├── go.sum                   # Go dependency checksums
├── Makefile                 # Build targets (build-frontend, build-backend, etc.)
├── Dockerfile               # Multi-stage Docker build
├── docker-compose.yml       # Local development & production setup
├── .env.example             # Environment variables template
└── README.md                # Documentation
```

## Development Workflow

### 1. Backend Development

```bash
# Install dependencies
go mod tidy

# Run with hot-reload (requires reflex or similar)
make dev

# Or run directly
PORT=8080 DATABASE_PATH=/tmp/payambar.db JWT_SECRET=dev-key go run ./cmd/payambar
```

### 2. Frontend Development

Edit frontend files in `frontend/`:
- `index.html` - Page structure
- `app.js` - Business logic, WebSocket client, API calls
- `styles.css` - Responsive RTL styling
- `sw.js` - Offline caching logic
- `manifest.json` - PWA configuration

Frontend is embedded at build time, no separate dev server (unless using Vite/bundler).

### 3. Building for Production

```bash
# Build everything (frontend → static/ → embedded in binary)
make build-all

# Output: bin/payambar
# This is the final deployment artifact
```

### 4. Docker Deployment

```bash
# Build image
make docker-build

# Run locally
docker-compose up -d

# Verify
curl http://localhost:8080/health
```

## Database Schema

### users
```sql
id: INTEGER PRIMARY KEY
username: TEXT UNIQUE NOT NULL
password_hash: TEXT NOT NULL
created_at: TIMESTAMP
updated_at: TIMESTAMP
```

### messages
```sql
id: INTEGER PRIMARY KEY
sender_id: INTEGER FOREIGN KEY (users.id)
receiver_id: INTEGER FOREIGN KEY (users.id)
content: TEXT NOT NULL
status: TEXT (sent|delivered|read)
created_at: TIMESTAMP
delivered_at: TIMESTAMP (nullable)
read_at: TIMESTAMP (nullable)
```

### files
```sql
id: INTEGER PRIMARY KEY
message_id: INTEGER FOREIGN KEY (messages.id)
file_name: TEXT NOT NULL
file_path: TEXT NOT NULL
file_size: INTEGER
content_type: TEXT
created_at: TIMESTAMP
```

## API Routes

### Public Endpoints

- `GET /health` - Server health check
- `GET /users/{username}` - Get public user profile

### Authentication (Unprotected)

- `POST /api/auth/register` - Register (request: {username, password})
- `POST /api/auth/login` - Login (request: {username, password})

### Protected (Require Authorization header: Bearer {token})

- `GET /api/conversations` - List all conversations
- `GET /api/messages?user_id={id}&limit=50&offset=0` - Get message history
- `PUT /api/messages/{id}/delivered` - Mark message as delivered
- `PUT /api/messages/{id}/read` - Mark message as read
- `GET /ws` - WebSocket connection

## WebSocket Protocol

### Client → Server

```json
{
  "type": "message",
  "receiver_id": 2,
  "content": "Hello!"
}
```

```json
{
  "type": "mark_delivered",
  "message_id": 123
}
```

```json
{
  "type": "mark_read",
  "message_id": 123
}
```

### Server → Client

```json
{
  "type": "message",
  "message_id": 123,
  "sender_id": 1,
  "receiver_id": 2,
  "content": "Hello!",
  "status": "sent",
  "created_at": "2024-01-25T10:30:00Z"
}
```

```json
{
  "type": "status_update",
  "message_id": 123,
  "status": "delivered"
}
```

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | 8080 | HTTP server port |
| `ENVIRONMENT` | development | "development" or "production" |
| `DATABASE_PATH` | /data/payambar.db | SQLite database file |
| `JWT_SECRET` | (required) | Secret for JWT signing |
| `CORS_ORIGINS` | * | CORS allowed origins |
| `MAX_UPLOAD_SIZE` | 10485760 | Max file size (bytes) |
| `FILE_STORAGE_PATH` | /data/uploads | Directory for uploads |

### Production Setup

Create `.env` on VPS:

```bash
ENVIRONMENT=production
JWT_SECRET=your-long-random-secret-key
DATABASE_PATH=/data/payambar.db
CORS_ORIGINS=https://yourdomain.com
```

Run:

```bash
docker-compose --env-file .env up -d
```

## Deployment Checklist

- [ ] Generate strong `JWT_SECRET`
- [ ] Set `ENVIRONMENT=production`
- [ ] Configure `CORS_ORIGINS` to CDN domain
- [ ] Enable HTTPS (via CDN)
- [ ] Setup SQLite backups (daily cron)
- [ ] Monitor logs: `docker-compose logs -f payambar`
- [ ] Test WebSocket: Connect and send test message
- [ ] Test PWA: Install from home screen (mobile)
- [ ] Verify CDN caching: Check cache headers on static files

## Performance Considerations

### Current Design
- Single VPS backend (no horizontal scaling)
- SQLite with connection pool (25 max)
- In-memory WebSocket hub (lost on restart)
- Embedded static files (no separate CDN needed, but can use one)

### Bottlenecks
- SQLite single-writer (use PostgreSQL for high concurrency)
- WebSocket connections in memory (use Redis for distributed setup)
- File storage on disk (use S3 for production)

### To Scale
1. PostgreSQL database
2. Redis for session state & WebSocket hub
3. Horizontal backend instances
4. S3 for file storage
5. Message queue (RabbitMQ) for reliability

## Security Notes

- Passwords hashed with bcrypt (default cost)
- JWT tokens expire after 24 hours
- WebSocket origin validation (configurable)
- HTTPS enforced via CDN
- Input validation on registration (username 3-32 chars, password 6+ chars)
- Basic XSS prevention (HTML escaping in frontend)

## Testing

### Manual WebSocket Test

```bash
# Get auth token
curl -X POST http://localhost:8080/api/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"user1","password":"password123"}'

# Response includes "token"
# Use token in WebSocket connection:
# new WebSocket('ws://localhost:8080/ws', ['Authorization', 'Bearer <token>'])
```

### Load Testing (if needed)

```bash
# Install siege or similar
siege -c 10 -r 100 http://localhost:8080/

# Monitor WebSocket
# Use browser DevTools → Network → WS
```

## Troubleshooting

| Issue | Cause | Solution |
|-------|-------|----------|
| WebSocket connection fails | CDN doesn't support WS | Upgrade to Cloudflare Pro or use different CDN |
| Database locked | Multiple writers | Use PostgreSQL for concurrency |
| Static files not loading | Embed failed | Verify `make build-frontend` creates `static/` |
| Auth fails | Wrong JWT_SECRET | Check environment variable on VPS |
| Messages not syncing | WebSocket disconnected | Check browser console for errors |

## References

- [Go language](https://golang.org)
- [Gin web framework](https://github.com/gin-gonic/gin)
- [gorilla/websocket](https://github.com/gorilla/websocket)
- [JWT Go](https://github.com/golang-jwt/jwt)
- [PWA documentation](https://web.dev/progressive-web-apps/)
- [Cloudflare docs](https://developers.cloudflare.com/)
