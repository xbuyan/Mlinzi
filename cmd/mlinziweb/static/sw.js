// Mlinzi service worker.
//
// Scope, stated precisely rather than implied: this makes the KNOW layer
// (the home page and every guide's detail page) work with zero network
// connectivity, including on a cold start where the person has never
// visited before. It does NOT make reporting or protection work offline.
//
// That split is deliberate, not a shortcut. A report's value comes from
// being chained into a shared, verifiable ledger — that requires reaching
// the server at submission time. Queuing a write locally and syncing later
// is a real feature with real complexity (local encryption at rest,
// conflict handling); pretending to support it without building it would
// be worse than being direct about the boundary. So: GET requests are
// served cache-first-with-network-fallback; POST requests are never
// intercepted here and simply fail with the browser's normal offline error
// if there's no connection — which is the honest behavior, not a bug.

const CACHE_NAME = "mlinzi-v1";

// Precached at install time so offline access works even if this is the
// very first thing the person opens with no connectivity yet. This list is
// small and static (three guides) rather than dynamically discovered,
// which is a deliberate simplicity trade-off: correct today, needs updating
// if the seed dataset changes.
const PRECACHE_URLS = [
  "/",
  "/guides/ke-bribery-public-service",
  "/guides/ke-police-misconduct",
  "/guides/ke-gender-based-violence",
  "/static/manifest.json",
  "/static/icon.svg",
  "https://cdn.tailwindcss.com",
];

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) =>
      // addAll fails atomically if any single URL fails (e.g. the Tailwind
      // CDN being unreachable during install). Falling back to caching
      // each URL independently means a transient failure on one resource
      // doesn't block the whole precache.
      Promise.allSettled(PRECACHE_URLS.map((url) => cache.add(url)))
    )
  );
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(keys.filter((k) => k !== CACHE_NAME).map((k) => caches.delete(k)))
    )
  );
  self.clients.claim();
});

self.addEventListener("fetch", (event) => {
  // Only GET requests are handled here. Report submission, check-ins,
  // escalation, and guardian share submission are all POSTs and pass
  // through untouched — see the file header for why that's intentional.
  if (event.request.method !== "GET") return;

  event.respondWith(
    fetch(event.request)
      .then((response) => {
        // Network succeeded: serve it, and refresh the cache for next time
        // we're offline. Only cache successful, same-scheme responses.
        const copy = response.clone();
        caches.open(CACHE_NAME).then((cache) => cache.put(event.request, copy));
        return response;
      })
      .catch(() =>
        // Network failed: this is the offline path. Serve whatever we have
        // cached for this exact request; if we have nothing, the fetch
        // simply rejects and the browser shows its own offline page.
        caches.match(event.request)
      )
  );
});
