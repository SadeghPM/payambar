// Service Worker for offline support and caching
// BUILD_HASH is replaced at build time by the Makefile. If it stays as the
// placeholder, the SW still works — it just won't auto-bust cache without a
// manual CACHE_NAME bump.
const BUILD_HASH = '__BUILD_HASH__';
const CACHE_NAME = `payambar-${BUILD_HASH}`;

const PRECACHE_URLS = [
  '/',
  '/index.html',
  '/styles.css',
  '/app.js',
  '/vue.global.prod.js',
  '/manifest.json',
  '/fonts/vazirmatn-arabic.woff2',
  '/fonts/vazirmatn-latin.woff2',
];

// ── Install: precache shell assets ──────────────────────────────────────────
self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => {
      return cache.addAll(PRECACHE_URLS).catch(() => {
        console.warn('Some resources could not be cached');
      });
    })
  );
  // Activate immediately — don't wait for old tabs to close
  self.skipWaiting();
});

// ── Activate: purge old caches ──────────────────────────────────────────────
self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((names) => {
      return Promise.all(
        names
          .filter((name) => name.startsWith('payambar-') && name !== CACHE_NAME)
          .map((name) => caches.delete(name))
      );
    })
  );
  // Take control of all open tabs immediately
  self.clients.claim();
});

// ── Messages from the page ─────────────────────────────────────────────────
self.addEventListener('message', (event) => {
  if (event.data && event.data.type === 'SKIP_WAITING') {
    self.skipWaiting();
  }
});

// ── Fetch strategy ─────────────────────────────────────────────────────────
self.addEventListener('fetch', (event) => {
  // Only handle GET over HTTP(S)
  if (event.request.method !== 'GET') return;
  const url = new URL(event.request.url);
  if (url.protocol !== 'http:' && url.protocol !== 'https:') return;

  // ── API calls: network-first, cache fallback ──────────────────────────
  if (url.pathname.startsWith('/api/')) {
    event.respondWith(
      fetch(event.request)
        .then((response) => {
          if (response && response.status === 200) {
            const clone = response.clone();
            caches.open(CACHE_NAME).then((cache) => cache.put(event.request, clone));
          }
          return response;
        })
        .catch(() => caches.match(event.request))
    );
    return;
  }

  // ── Shell assets: stale-while-revalidate ───────────────────────────────
  // Serve from cache immediately for speed, but fetch a fresh copy in the
  // background. If the response differs, the next page load picks it up.
  // This is strictly better than pure cache-first because it self-heals
  // even if BUILD_HASH was not bumped.
  event.respondWith(
    caches.open(CACHE_NAME).then((cache) => {
      return cache.match(event.request).then((cached) => {
        const networkFetch = fetch(event.request).then((response) => {
          if (response && response.status === 200 && response.type !== 'error') {
            cache.put(event.request, response.clone());
          }
          return response;
        });

        // Return cached version instantly; update in background
        return cached || networkFetch;
      });
    })
  );
});

// ── Push notifications ──────────────────────────────────────────────────────
self.addEventListener('push', (event) => {
  if (!event.data) return;

  let payload;
  try {
    payload = event.data.json();
  } catch (e) {
    payload = { title: 'پیام جدید', body: event.data.text() || 'پیام جدید دارید' };
  }

  const title = payload.title || 'پیام جدید';
  const options = {
    body: payload.body || 'پیام جدید دارید',
    icon: '/favicon-192.png',
    badge: '/favicon-96.png',
    data: { url: payload.url || '/' },
    tag: 'new-message',
    renotify: true,
  };

  event.waitUntil(self.registration.showNotification(title, options));
});

// ── Notification click ──────────────────────────────────────────────────────
self.addEventListener('notificationclick', (event) => {
  event.notification.close();

  const url = event.notification.data?.url || '/';

  event.waitUntil(
    clients.matchAll({ type: 'window', includeUncontrolled: true }).then((windowClients) => {
      // Focus an existing window if available
      for (const client of windowClients) {
        if (client.url.includes(self.location.origin) && 'focus' in client) {
          return client.focus();
        }
      }
      // Otherwise open a new window
      return clients.openWindow(url);
    })
  );
});
