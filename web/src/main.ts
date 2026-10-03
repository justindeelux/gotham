import { createPinia } from "pinia";
import { createApp } from "vue";

import App from "./App.vue";
import { router } from "./router";
import "./styles/main.css";

const app = createApp(App);

app.use(createPinia());
app.use(router);

// A dead session (failed refresh, forced logout) redirects in-app through
// the router instead of a hard location.assign, which would reload the
// document and lose SPA state. Preventing the default tells the HTTP layer
// (see redirectToLogin in api/http) the SPA took over; without a listener
// the HTTP layer falls back to the hard navigation.
window.addEventListener("gotham:session-expired", (event) => {
  event.preventDefault();
  void router.push({ name: "login" }).catch(() => undefined);
});

/** sessionStorage flag marking the one-shot reload after a failed navigation. */
const chunkReloadKey = "gotham-chunk-reload";

/**
 * shouldReloadAfterNavigationFailure decides whether to attempt the one-shot
 * reload. It returns false when the flag is already set (the previous reload
 * did not help) or sessionStorage is unavailable, so the caller shows a
 * fallback instead of reloading in a loop.
 */
function shouldReloadAfterNavigationFailure(): boolean {
  try {
    if (sessionStorage.getItem(chunkReloadKey)) {
      return false;
    }
    sessionStorage.setItem(chunkReloadKey, "1");
    return true;
  } catch {
    return false;
  }
}

/** clearNavigationReloadFlag re-arms the one-shot reload after a good load. */
function clearNavigationReloadFlag(): void {
  try {
    sessionStorage.removeItem(chunkReloadKey);
  } catch {
    // Ignore private-mode failures: the worst case is one extra reload.
  }
}

/**
 * renderReloadFallback overlays a minimal, dependency-free message and reload
 * button, so a user with stale hashed chunks is not stranded. It is appended to
 * the body, outside the mounted app, so the render cannot wipe it.
 */
function renderReloadFallback(): void {
  if (document.getElementById("gotham-reload-fallback")) {
    return;
  }

  const overlay = document.createElement("div");
  overlay.id = "gotham-reload-fallback";
  overlay.setAttribute("role", "alert");
  overlay.style.cssText =
    "position:fixed;inset:0;display:flex;flex-direction:column;gap:12px;" +
    "align-items:center;justify-content:center;padding:24px;" +
    "background:#0f1115;color:#e5e7eb;font:14px system-ui,sans-serif;z-index:9999";

  const message = document.createElement("p");
  message.textContent =
    "The application could not finish loading. Please reload the page.";

  const button = document.createElement("button");
  button.type = "button";
  button.textContent = "Reload";
  button.style.cssText =
    "padding:8px 16px;border-radius:6px;border:1px solid #4b5563;" +
    "background:#1f2937;color:inherit;cursor:pointer";
  button.addEventListener("click", () => window.location.reload());

  overlay.append(message, button);
  document.body.append(overlay);
}

// Mount once the initial navigation resolves so route-dependent startup
// (App.vue's session hydration) observes the real route rather than "/" (G3).
// A top-level `await` here leaves the built entry module pending forever (the
// app never mounts, blank page); a promise chain is equivalent and bundle-safe.
let navigationFailed = false;

router
  .isReady()
  .then(() => {
    // A resolved navigation means the current assets are healthy: re-arm the
    // one-shot reload for a future deploy.
    clearNavigationReloadFlag();
  })
  .catch((error) => {
    console.error("initial navigation failed", error);
    // A lazy chunk that 404s after a deploy with stale hashed assets rejects
    // the initial navigation. Reload once to pull fresh assets; if that did not
    // help, mount and show a reload fallback rather than looping. Redirecting
    // to /login does not work here: it is publicOnly, so an authenticated user
    // bounces to dashboard, which needs the same missing chunks.
    if (shouldReloadAfterNavigationFailure()) {
      window.location.reload();
      return new Promise<void>(() => {});
    }
    navigationFailed = true;
  })
  .then(() => {
    app.mount("#app");
    if (navigationFailed) {
      renderReloadFallback();
    }
  });
