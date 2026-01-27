# Quick Start Guide - Payambar

## Local Development

### Prerequisites
- Go 1.23+
- Make (optional, but recommended)

### Build & Run

```bash
# Clone/navigate to project
cd /Users/sadegh/Desktop/projects/payambar

# RECOMMENDED: Dev mode (copies frontend → runs with Go)
make dev
# This copies all frontend files to cmd/payambar/static/ and runs with go run
# Frontend changes will be picked up on next `make dev` run

# Alternative: Build everything (frontend → binary with embedded assets)
make build-all

# Run the compiled binary
./bin/payambar

# Or run directly after building
make run
```

Navigate to: **http://localhost:8080**

### Frontend Workflow
- **During development**: Use `make dev` to run the server
  - Changes to `frontend/` files are copied to `cmd/payambar/static/` automatically
  - Stop (Ctrl+C) and run `make dev` again to pick up changes
  
- **For production**: Use `make build-all` to create the binary
  - Frontend assets are embedded in the binary

### First Steps

1. **Register a new account**
   - Username: `alice` (3-32 chars, alphanumeric + underscore)
   - Password: `password123` (6+ chars)

2. **Register second account**
   - Username: `bob`
   - Password: `password123`

3. **Send a message**
   - Login as alice, search/select bob
   - Type a message and click send
   - Bob should see it in real-time
   - Alice should see "delivered" status

4. **Test delivery & read status**
   - Bob marks message as delivered/read
   - Alice sees status update in real-time

### Troubleshooting Local Dev

```bash
# Port already in use
lsof -i :8080
kill -9 <PID>

# Reset database
rm /tmp/payambar.db

# View logs
# Server logs appear in console where you ran ./bin/payambar
```

---

## Docker Deployment

### Build Docker Image

```bash
make docker-build

# Or manually
docker build -t payambar:latest .
```

### Run with Docker Compose

```bash
# Create .env file
cp .env.example .env
# Edit .env with your settings (especially JWT_SECRET)

# Run
docker-compose up -d

# View logs
docker-compose logs -f payambar

# Stop
docker-compose down

# Clean everything (including data)
docker-compose down -v
```

### Run Docker Standalone

```bash
docker run -p 8080:8080 \
  -e JWT_SECRET=your-secret-key \
  -e DATABASE_PATH=/data/payambar.db \
  -v payambar_data:/data \
  payambar:latest
```

---

## VPS Deployment (with CDN)

### 1. Prepare on Your Machine

```bash
# Build the binary
make build-all

# Test locally to ensure it works
./bin/payambar
```
### 2. VPS BInary
PORT=8082 JWT_SECRET=5lYDZorjiC7TSMJahcjwitjuwKbuWvoMoP38VmiqZw0 DATABASE_PATH=/opt/payambar/data/payambar.db FILE_STORAGE_PATH=/opt/payambar/data/uploads ./bin/payambar
### 2. Deploy to VPS

```bash
# Option A: Use Docker Compose (recommended)
scp -r . root@your-vps:/opt/payambar
ssh root@your-vps

# On VPS:
cd /opt/payambar
# Edit .env with strong JWT_SECRET
cp .env.example .env
nano .env

# Run
docker-compose up -d
docker-compose logs -f payambar

# Option B: Deploy binary directly
scp ./bin/payambar root@your-vps:/opt/payambar/
scp .env.example root@your-vps:/opt/payambar/.env
ssh root@your-vps

# On VPS:
cd /opt/payambar
# Edit .env
nano .env
# Run
./payambar
```

### 3. Configure CDN (Cloudflare Example)

1. **DNS Records**
   - Add A record pointing to VPS IP: `payambar.yourdomain.com`

2. **Cloudflare Settings**
   - SSL/TLS: Full (recommended)
   - Page Rules:
     - URL: `payambar.yourdomain.com/api*` → Cache Level: Bypass
     - URL: `payambar.yourdomain.com/ws` → Cache Level: Bypass
     - URL: `payambar.yourdomain.com/*` → Cache Level: Cache Everything

3. **VPS Firewall**
   - Allow port 8080 from Cloudflare IPs only
   - Or: firewall rules to accept from CDN

### 4. Backend Configuration on VPS

Create `.env` file:

```bash
ENVIRONMENT=production
PORT=8080
DATABASE_PATH=/data/payambar.db
JWT_SECRET=your-very-long-random-secret-key-here
CORS_ORIGINS=https://payambar.yourdomain.com
FILE_STORAGE_PATH=/data/uploads
```

### 5. Enable HTTPS

CDN handles SSL for frontend users automatically.
Backend communicates with CDN over HTTPS, then CDN forwards HTTP to VPS.

### 6. Backup SQLite Database

```bash
# Cron job to backup daily (add to crontab)
0 2 * * * cd /opt/payambar && docker-compose exec -T payambar \
  sh -c 'tar czf /data/backup.tar.gz /data/payambar.db' && \
  # Upload to S3 or backup service
```

---

## API Testing

### Register & Login

```bash
# Register
curl -X POST http://localhost:8080/api/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "username": "alice",
    "password": "password123"
  }'

# Response includes "token"

# Login
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "username": "alice",
    "password": "password123"
  }'
```

### Get Conversations

```bash
curl -X GET http://localhost:8080/api/conversations \
  -H "Authorization: Bearer <your-token>"
```

### Get Public Profile

```bash
curl -X GET http://localhost:8080/users/bob
```

### WebSocket Test (Browser Console)

```javascript
// Get a token first (via login API above)
const token = "your-jwt-token";

const ws = new WebSocket(
  'ws://localhost:8080/ws',
  ['Authorization', `Bearer ${token}`]
);

ws.onmessage = (event) => {
  console.log("Message:", JSON.parse(event.data));
};

// Send a message
ws.send(JSON.stringify({
  type: 'message',
  receiver_id: 2,  // bob's user_id
  content: 'Hello Bob!'
}));

// Mark as delivered
ws.send(JSON.stringify({
  type: 'mark_delivered',
  message_id: 1
}));

// Mark as read
ws.send(JSON.stringify({
  type: 'mark_read',
  message_id: 1
}));
```

---

## Performance Tuning

### For Small Scale (< 100 concurrent users)

Current setup is fine. No changes needed.

### For Medium Scale (100-1000 users)

1. Increase SQLite connection pool in `internal/db/db.go`:
   ```go
   conn.SetMaxOpenConns(50)  // from 25
   ```

2. Upgrade to PostgreSQL:
   - Allows true concurrent writers
   - Better query optimization
   - Can scale horizontally

### For Large Scale (1000+ users)

1. **Database**: PostgreSQL with read replicas
2. **Cache**: Redis for session state
3. **Message Queue**: RabbitMQ/Kafka for reliability
4. **WebSocket**: Redis pub/sub for distributed hub
5. **File Storage**: S3 or object storage
6. **Load Balancer**: Multiple backend instances

---

## Monitoring & Logs

### Docker Logs

```bash
# Real-time logs
docker-compose logs -f payambar

# Last 100 lines
docker-compose logs --tail 100 payambar

# Since specific time
docker-compose logs --since 1h payambar
```

### Check Health

```bash
curl http://localhost:8080/health
# Response: {"status":"ok"}
```

### Database Size

```bash
# SSH into VPS or container
du -h /data/payambar.db
```

---

## Common Issues

| Issue | Solution |
|-------|----------|
| WebSocket connection fails | Check CDN supports WS (Cloudflare Pro required) |
| Messages not syncing | Reload page, check browser console for errors |
| Port 8080 already in use | Change PORT in .env or kill existing process |
| Database locked | Stop server, delete /data/payambar.db, restart |
| Login fails | Check JWT_SECRET matches between instances |
| Frontend shows 404 | Ensure `make build-frontend` ran before build |

---

## Features & Status

### ✅ Implemented
- User registration & login (JWT)
- 1-to-1 messaging
- Message status (sent/delivered/read)
- Real-time WebSocket
- Public profiles
- PWA (installable)
- RTL/Persian support
- SQLite database
- Docker deployment

### ⚠️ Not Implemented (Out of Scope)
- Groups
- Voice/video
- File uploads (API structure ready, frontend not connected)
- Encryption beyond HTTPS
- Rate limiting
- End-to-end encryption
- Stickers/emojis (basic text only)

---

## Next Steps

1. **Local testing**: Run `make run` and test with multiple browser windows
2. **Deploy to VPS**: Use Docker Compose for easiest setup
3. **Configure CDN**: Point domain to CDN, CDN to VPS
4. **Backup strategy**: Set up daily database backups
5. **Monitor**: Watch logs and user activity

---

## Support & Debugging

### Enable Debug Logging

Change `ENVIRONMENT` to `development` in .env:

```bash
ENVIRONMENT=development
```

Restart and check console output for detailed logs.

### Check Dependencies

```bash
go mod verify
go mod tidy
```

### Test Database Connection

```bash
# From within container
sqlite3 /data/payambar.db ".tables"
```

---

For more details, see [README.md](README.md) and [DEVELOPMENT.md](DEVELOPMENT.md)
