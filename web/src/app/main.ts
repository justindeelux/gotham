import { createPinia } from "pinia";
import { createApp } from "vue";

import App from "./App.vue";
import { router } from "./router";
import { installI18n } from "@/shared/i18n";
import { getLocaleStorage, resolveInitialLocale } from "@/shared/i18n/locale";
import { staleChunkCopy } from "@/shared/i18n/staleFallback";
import "@/shared/styles/main.css";

const app = createApp(App);

app.use(createPinia());
installI18n(app);
app.use(router);

window.addEventListener("gotham:session-expired", (event) => {
  event.preventDefault();
  void router.push({ name: "login" }).catch(() => undefined);
});

const chunkReloadKey = "gotham-chunk-reload";

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

function clearNavigationReloadFlag(): void {
  try {
    sessionStorage.removeItem(chunkReloadKey);
  } catch {
  }
}

function renderReloadFallback(): void {
  if (document.getElementById("gotham-reload-fallback")) {
    return;
  }

  const copy = staleChunkCopy(resolveInitialLocale(getLocaleStorage()));

  const overlay = document.createElement("div");
  overlay.id = "gotham-reload-fallback";
  overlay.setAttribute("role", "alert");
  overlay.style.cssText =
    "position:fixed;inset:0;display:flex;flex-direction:column;gap:12px;" +
    "align-items:center;justify-content:center;padding:24px;" +
    "background:#0f1115;color:#e5e7eb;font:14px system-ui,sans-serif;z-index:9999";

  const message = document.createElement("p");
  message.textContent = copy.message;

  const button = document.createElement("button");
  button.type = "button";
  button.textContent = copy.reload;
  button.style.cssText =
    "padding:8px 16px;border-radius:6px;border:1px solid #4b5563;" +
    "background:#1f2937;color:inherit;cursor:pointer";
  button.addEventListener("click", () => window.location.reload());

  overlay.append(message, button);
  document.body.append(overlay);
}

let navigationFailed = false;

router
  .isReady()
  .then(() => {
    clearNavigationReloadFlag();
  })
  .catch((error) => {
    console.error("initial navigation failed", error);
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
