const CACHE_NAME = 'juku-pwa-v1';
const STATIC_ASSETS = [
  '/',
  '/manifest.json',
  '/assets/icon.svg',
  '/assets/app.css',
  '/assets/library.css',
  '/assets/downloads.css',
  '/assets/player.css',
  '/assets/history.css',
  '/assets/library-tools.css',
  '/assets/theme.js',
  '/assets/main.js'
];

self.addEventListener('install', event => {
  event.waitUntil(
    caches.open(CACHE_NAME).then(cache => {
      return cache.addAll(STATIC_ASSETS).catch(() => {});
    })
  );
  self.skipWaiting();
});

self.addEventListener('activate', event => {
  event.waitUntil(
    caches.keys().then(keys => {
      return Promise.all(
        keys.filter(k => k !== CACHE_NAME).map(k => caches.delete(k))
      );
    })
  );
  self.clients.claim();
});

self.addEventListener('fetch', event => {
  const url = new URL(event.request.url);

  // Dynamic endpoints, stream segments, and APIs must always be network-first
  if (
    url.pathname.startsWith('/api/') ||
    url.pathname.startsWith('/healthz') ||
    url.pathname.endsWith('.m3u8') ||
    url.pathname.endsWith('.ts') ||
    url.pathname.endsWith('.mp4') ||
    event.request.method !== 'GET'
  ) {
    return;
  }

  // Static assets: cache-first with network background update
  if (url.pathname.startsWith('/assets/') || url.pathname === '/manifest.json') {
    event.respondWith(
      caches.match(event.request).then(cached => {
        const fetchPromise = fetch(event.request)
          .then(networkRes => {
            if (networkRes && networkRes.status === 200) {
              const resClone = networkRes.clone();
              caches.open(CACHE_NAME).then(c => c.put(event.request, resClone));
            }
            return networkRes;
          })
          .catch(() => cached);
        return cached || fetchPromise;
      })
    );
    return;
  }

  // HTML navigation: network first, fallback to cached /
  if (event.request.mode === 'navigate') {
    event.respondWith(
      fetch(event.request).catch(() => {
        return caches.match('/');
      })
    );
  }
});
