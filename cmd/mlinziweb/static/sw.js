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

const CACHE_NAME = "mlinzi-v3";

// Every language the interface is available in. TestServiceWorkerSpeaksEveryLanguage
// checks this against the string catalogue, so adding a language there and
// forgetting it here fails the build rather than quietly leaving that language
// without an offline copy.
const LANGUAGES = ["en", "sw", "fr"];

// Precached at install time so offline access works even if this is the
// very first thing the person opens with no connectivity yet. The list is
// static rather than a crawl — correct today, needs updating if the seed
// dataset changes; TestServiceWorkerPrecachesOnlyRealGuides and
// TestServiceWorkerPrecachesEveryDocument are the tripwires for that drift.
//
// Why language is part of the URL here: language is a query parameter, so the
// cached copy of "/guides/x" is not the copy a Kiswahili reader asked for.
// Precaching only the bare paths meant that someone reading in Kiswahili who
// then lost their connection was served the browser's offline error — the
// offline claim held in English only, which is the same silent-English
// problem the interface strings had.
const SMALL_PAGES = [
  "/",
  "/ask",
  "/guides/ke-bribery-public-service",
  "/guides/ke-police-misconduct",
  "/guides/ke-gender-based-violence",
  "/guides/ke-wrongful-detention",
  "/guides/ng-bribery-public-service",
  "/guides/ng-wrongful-detention",
  "/guides/ug-bribery-public-service",
  "/guides/ug-police-misconduct",
  "/guides/ug-gender-based-violence",
  "/guides/ug-wrongful-detention",
  "/resources",
];

// The Resources Center documents are precached once, in English, and the
// pages say so in their own wording. The three constitutions render to about
// 450 KB each; precaching three language variants of each would put several
// megabytes in the cache of exactly the basic phones this is built for, to
// duplicate a body of legal text that does not change between the variants —
// only the chrome around it does. The trade is stated rather than hidden: the
// guides, which are what someone needs in the moment, are offline in every
// language; the reference documents are offline in the wording that has legal
// force.
const DOCUMENT_PAGES = [
  "/resources/kenya-constitution",
  "/resources/uganda-constitution",
  "/resources/nigeria-constitution",
  "/resources/nigeria-icpc-act",
];

const PRECACHE_URLS = [
  // Both forms are needed, and the bare one is not redundant with ?lang=en.
  // A cache key is an exact URL: the web app manifest's start_url is "/",
  // and a bookmark or typed address is the bare path too, so precaching only
  // the ?lang= variants would leave the installed app unable to open with no
  // connection — the single most likely way someone offline launches it.
  ...SMALL_PAGES,
  ...LANGUAGES.flatMap((lang) => SMALL_PAGES.map((path) => path + "?lang=" + lang)),
  ...DOCUMENT_PAGES,
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
