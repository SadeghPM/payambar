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
Server re-broadcasts same encrypted fields and existing metadata (`message_id`, `sender_id`, `status`, `created_at`).

## REST conversation payload contract

`GET /api/messages?user_id=:id`

Each message item MUST be one of:

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

## `messages` additions
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

## `user_device_keys` table
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
When `encrypted=true`, reject if any required field missing:
- `ciphertext`, `iv`, `e2ee_v`, `alg`, `sender_device_id`, `key_id`.

Do not attempt decryption on server.

## Client flow (Vue)
1. On startup/login:
   - keep login password in memory briefly,
   - attempt to restore private key by fetching `/keys/devices/self` and decrypting with PBKDF2-derived key,
   - if no backup or decryption fails, generate new P-256 keypair and publish.
2. On send text:
   - fetch/cache recipient device keys (short TTL; retries allowed),
   - if key exists: encrypt plaintext locally and send encrypted envelope,
   - if key missing: warn once and send plaintext (backward-compatible delivery).
3. On receive/history:
   - if `encrypted=true` => decrypt client-side,
   - if decrypt fails => render lock placeholder.

## Migration and rollout
- Additive DB migration only; legacy plaintext rows unchanged.
- New messages are encrypted when both parties have keys; if a recipient key is missing, plaintext is sent with a user warning to keep messages readable.
- Device private keys can be restored on new browsers/devices via password-wrapped backup.

## Security notes
- Require HTTPS/WSS in production.
- Avoid logging ciphertext envelopes at info level.
- Bind AAD to immutable metadata (sender_id, receiver_id, message_id/client_message_id).
- Add replay/duplication checks using `(sender_id, client_message_id)` uniqueness window.
