import { createPinia } from "pinia";
import { createApp } from "vue";

import App from "./App.vue";
import { router } from "./router";
import "./styles/main.css";

const app = createApp(App);

app.use(createPinia());
app.use(router);

// Resolve the initial navigation before mounting so route-dependent startup
// (App.vue's session hydration) observes the real route rather than "/" (G3).
await router.isReady();
app.mount("#app");
