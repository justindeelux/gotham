import { createPinia } from "pinia";
import { createApp } from "vue";

import App from "./App.vue";
import { router } from "./router";
import "./styles/main.css";

const app = createApp(App);

app.use(createPinia());
app.use(router);

// Mount once the initial navigation resolves so route-dependent startup
// (App.vue's session hydration) observes the real route rather than "/" (G3).
// A top-level `await` here leaves the built entry module pending forever (the
// app never mounts, blank page); a promise chain is equivalent and bundle-safe.
// Mount even if the initial navigation rejects (for example a lazy chunk that
// 404s after a deploy with stale hashed assets): the router guard still
// redirects once navigation succeeds, and staying blank is worse.
router.isReady().catch(() => {}).then(() => app.mount("#app"));
