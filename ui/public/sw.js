// Homie's service worker. It exists to make the app installable and to speed
// up startup; it deliberately caches very little:
//  - the hashed build files in assets/ (immutable, so safe to keep forever)
//  - nothing else: pages and the API always come from the network, so the
//    dashboard and the service status are never stale and a new release is
//    picked up on the next visit.
// If the server can't be reached, a small "unreachable" page is shown instead
// of the browser's error page.

const CACHE = 'homie-assets'
const MAX_ENTRIES = 60

const OFFLINE_PAGE = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Homie</title>
<style>
  body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: #1c2229; color: #e5e9ed; font: 16px/1.4 system-ui, sans-serif; text-align: center; padding: 0 20px; }
  h1 { margin: 0 0 8px; font-size: 1.6rem; }
  p { margin: 0 0 18px; color: #97a3af; }
  button { font: inherit; padding: 8px 18px; border: 1px solid #3a4450; border-radius: 5px; background: #2c343e; color: inherit; cursor: pointer; }
</style></head>
<body><main><h1>Homie is unreachable</h1><p>Check that the server is running and that you are on the right network.</p>
<button onclick="location.reload()">Try again</button></main></body></html>`

const offline = () => new Response(OFFLINE_PAGE, { status: 503, headers: { 'Content-Type': 'text/html; charset=utf-8' } })

self.addEventListener('install', () => self.skipWaiting())

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim()),
  )
})

async function trim(cache) {
  const keys = await cache.keys()
  for (const k of keys.slice(0, Math.max(0, keys.length - MAX_ENTRIES))) await cache.delete(k)
}

self.addEventListener('fetch', (event) => {
  const req = event.request
  if (req.method !== 'GET') return
  const url = new URL(req.url)
  if (url.origin !== self.location.origin) return

  if (req.mode === 'navigate') {
    event.respondWith(
      fetch(req).then(
        (res) => (res.status >= 500 ? offline() : res),
        () => offline(),
      ),
    )
    return
  }

  if (url.pathname.includes('/assets/')) {
    event.respondWith(
      caches.open(CACHE).then(async (cache) => {
        const hit = await cache.match(req)
        if (hit) return hit
        const res = await fetch(req)
        if (res.ok) {
          await cache.put(req, res.clone())
          trim(cache)
        }
        return res
      }),
    )
  }
})
