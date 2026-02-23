# E2EE Text v1 Protocol (Payambar)

This document defines a minimal end-to-end encryption protocol for **text messages only**.

## Goals
- Encrypt text content on client (Vue) before transport/storage.
- Keep server (Go) as blind relay + ciphertext storage.
- Preserve current delivery/read status and conversation APIs.
- Keep backward compatibility with legacy plaintext messages.

## Non-goals (v1)
- Attachments encryption.
- Voice/video E2EE changes.
- Signal Double Ratchet and prekeys (planned for v2).

## Crypto profile (v1)
- Key agreement: ECDH P-256 (per device identity keypair).
- Content encryption: AES-256-GCM.
- Key backup wrapping: AES-256-GCM with PBKDF2-SHA256 (150,000 iterations, 16-byte salt).
- Encoding: base64url for binary fields.
- Envelope version: `e2ee_v=1`.

## Device key directory APIs

### Publish/rotate own device key
`POST /api/keys/devices`

This is an **upsert** operation. The unique constraint is `(user_id, device_id, key_id)`.

On conflict the server updates the public key and algorithm. Backup fields (`enc_private_key`, `enc_private_key_iv`, `kdf_salt`, `kdf_iterations`, `kdf_alg`, `key_wrap_version`) use `COALESCE` — a new `NULL` value will **not** overwrite an existing backup. This prevents accidental backup loss when the client re-publishes without a password (e.g. on page refresh).

Request:
```json
{
  "device_id": "web-8d0f7f8f",
  "algorithm": "ECDH-P256",
  "public_key": "base64url...",
  "key_id": "k-2026-01",
  "enc_private_key": "base64url...",        // optional, AES-GCM wrapped JWK
  "enc_private_key_iv": "base64url-12bytes",
  "kdf_salt": "base64url-16bytes",
  "kdf_iterations": 150000,
  "kdf_alg": "PBKDF2-SHA256",
  "key_wrap_version": 1
}
```

Response:
```json
{
  "ok": true,
  "device_id": "web-8d0f7f8f",
  "key_id": "k-2026-01"
}
```

### Fetch recipient device keys
`GET /api/keys/users/:id/devices`

Returns only non-revoked keys (`revoked_at IS NULL`), ordered newest first.

Response:
```json
{
  "devices": [
    {
      "device_id": "web-abcd",
      "algorithm": "ECDH-P256",
      "public_key": "base64url...",
      "key_id": "k-2026-01"
    }
  ]
}
```

### Fetch my own device keys (with wrapped private key)
`GET /api/keys/devices/self`

Same as above but also returns the encrypted private key backup fields.

Response:
```json
{
  "devices": [
    {
      "device_id": "web-8d0f7f8f",
      "algorithm": "ECDH-P256",
      "public_key": "base64url...",
      "key_id": "k-2026-01",
      "enc_private_key": "base64url...",
      "enc_private_key_iv": "base64url-12bytes",
      "kdf_salt": "base64url-16bytes",
      "kdf_iterations": 150000,
      "kdf_alg": "PBKDF2-SHA256",
      "key_wrap_version": 1
    }
  ]
}
```

## WebSocket message contract

### Outbound encrypted message event
`type: message`

```json
{
  "type": "message",
  "receiver_id": 42,
  "client_message_id": "client-1738000000",
  "encrypted": true,
  "e2ee_v": 1,
  "alg": "AES-256-GCM",
  "sender_device_id": "web-8d0f7f8f",
  "key_id": "k-2026-01",
  "iv": "base64url-12bytes",
  "ciphertext": "base64url...",
  "aad": "base64url-optional"
}
```

### Inbound/broadcast message event
Server re-broadcasts the same encrypted fields plus existing metadata (`message_id`, `sender_id`, `status`, `created_at`).

## REST conversation payload contract

`GET /api/messages?user_id=:id`

Each message item is one of:

1. Legacy plaintext:
```json
{
  "id": 1,
  "sender_id": 1,
  "receiver_id": 2,
  "content": "legacy plaintext",
  "encrypted": false
}
```

2. E2EE ciphertext:
```json
{
  "id": 2,
  "sender_id": 1,
  "receiver_id": 2,
  "encrypted": true,
  "e2ee_v": 1,
  "alg": "AES-256-GCM",
  "sender_device_id": "web-8d0f7f8f",
  "key_id": "k-2026-01",
  "iv": "base64url-12bytes",
  "ciphertext": "base64url...",
  "aad": "base64url-optional"
}
```

## Database changes

### `messages` additions
- `encrypted INTEGER NOT NULL DEFAULT 0`
- `e2ee_v INTEGER`
- `alg TEXT`
- `sender_device_id TEXT`
- `key_id TEXT`
- `iv TEXT`
- `ciphertext TEXT`
- `aad TEXT`

For legacy rows:
- `encrypted = 0`, `content` remains populated.

For encrypted rows:
- `encrypted = 1`, `ciphertext` fields populated.
- `content` may be empty string or null-equivalent per migration strategy.

### `user_device_keys` table
- `id INTEGER PRIMARY KEY AUTOINCREMENT`
- `user_id INTEGER NOT NULL`
- `device_id TEXT NOT NULL`
- `algorithm TEXT NOT NULL`
- `public_key TEXT NOT NULL`
- `key_id TEXT NOT NULL`
- `enc_private_key TEXT NULL`
- `enc_private_key_iv TEXT NULL`
- `kdf_salt TEXT NULL`
- `kdf_iterations INTEGER NULL`
- `kdf_alg TEXT NULL`
- `key_wrap_version INTEGER NULL`
- `updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP`
- `created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP`
- `revoked_at TIMESTAMP NULL`
- unique `(user_id, device_id, key_id)`

## Validation rules (server)
When `encrypted=true`, reject if any required field is missing:
- `ciphertext`, `iv`, `e2ee_v`, `alg`, `sender_device_id`, `key_id`.

Do not attempt decryption on server.

## Client flow (Vue)

### Key initialization (`ensureE2EEReady`)

On every login or page load the client tries to get a usable ECDH P-256 keypair. The sources are tried in priority order:

```
┌─────────────────────────────────────────────────┐
│  1. localStorage                                │
│     Check for stored private/public JWK,        │
│     device_id and key_id.                        │
│     Key: payambar:e2ee:{userId}:*               │
│     If found → use immediately.                 │
├─────────────────────────────────────────────────┤
│  2. Server backup (requires login password)     │
│     Fetch GET /keys/devices/self.               │
│     Find a device with enc_private_key.         │
│     Decrypt with PBKDF2(password) + AES-GCM.   │
│     If success → use and save to localStorage.  │
│     If fail → warn user, fall through.          │
├─────────────────────────────────────────────────┤
│  3. Generate new keypair (last resort)          │
│     crypto.subtle.generateKey(ECDH, P-256).     │
│     Assign new device_id (UUID) and key_id.     │
│     Save to localStorage.                       │
│     ⚠ Old encrypted messages become unreadable. │
└─────────────────────────────────────────────────┘
```

After obtaining keys, the client publishes them via `POST /api/keys/devices`. If the login password is available, the private key backup is included in the payload.

**Resilience rule:** If the publish POST fails but the keys came from an existing source (localStorage or server backup), the client still marks E2EE as ready. The key was already published in a previous session, so local encryption still works. Only newly generated keys that were never published cause E2EE to be disabled (the recipient would have no way to decrypt).

### Sending a text message

1. Call `ensureE2EEReady()` — returns `true` if local keys are available.
2. Fetch/cache recipient device keys via `GET /api/keys/users/:id/devices` (30 s TTL for populated results, 3 s TTL for empty results).
3. If recipient key exists:
   - Derive shared AES-256 key via ECDH (sender private + recipient public).
   - Encrypt plaintext with AES-256-GCM (random 12-byte IV).
   - Send encrypted envelope over WebSocket.
4. If recipient key is missing or any error occurs:
   - Warn user once per recipient ("secure send not possible").
   - Send message as plaintext (backward-compatible delivery).
   - Errors in encryption never block message delivery.

### Receiving / loading history

- If `encrypted=true` → decrypt client-side using ECDH (own private key + sender public key).
- If decryption fails → show lock placeholder `🔒`.
- If `encrypted=false` → display `content` as-is.

## Migration and rollout
- Additive DB migration only; legacy plaintext rows are unchanged.
- New messages are encrypted when both parties have published keys. If a recipient key is missing, plaintext is sent with a one-time user warning.
- Device private keys can be restored on new browsers/devices via the password-wrapped server backup.

## Security notes
- Require HTTPS/WSS in production.
- Avoid logging ciphertext envelopes at info level.
- Server upsert uses `COALESCE` for backup fields to prevent accidental key backup loss.
- Bind AAD to immutable metadata (sender_id, receiver_id, message_id/client_message_id).
- Add replay/duplication checks using `(sender_id, client_message_id)` uniqueness window.
