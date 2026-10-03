<script setup lang="ts">
import { RouterView } from "vue-router";

import GothamIcon from "../components/GothamIcon.vue";
import type { IconName } from "../components/GothamIcon.vue";

interface ValueProp {
  icon: IconName;
  title: string;
  body: string;
}

// English copy rewritten from the Vietnamese mockup (docs/design/login.html,
// the UI source of truth). Form copy stays with UI-2; only the shell lives here.
const valueProps: ValueProp[] = [
  {
    icon: "server",
    title: "Self-hosted on a VPS with 1 GB of RAM",
    body: "The control plane is a single Go binary with PostgreSQL and Redis; 1 vCPU and 1 GB of RAM is enough to start.",
  },
  {
    icon: "shield",
    title: "Control channel over server-authenticated gRPC TLS",
    body: "The agent drives the Docker Engine on each node; the control plane and the agent speak versioned protobuf over :9442, with certificates from the internal CA.",
  },
  {
    icon: "refresh",
    title: "Signed Ed25519 updates with rollback",
    body: "The control plane and the agent self-update from GitHub Releases, verifying the signature before swapping the binary and keeping the old one for rollback.",
  },
  {
    icon: "rocket",
    title: "4 build engines plus one-click templates",
    body: "Dockerfile, Railpack, Buildpacks, and static; the template library sets up WordPress, Nextcloud, n8n, or Uptime Kuma in one click.",
  },
];
</script>

<template>
  <div class="auth">
    <aside class="auth-aside" data-od-id="auth-aside">
      <div class="stack gap-8">
        <div class="brand-lockup">
          <span class="brand-mark">
            <svg
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="1.8"
              stroke-linecap="round"
              aria-hidden="true"
            >
              <path d="M5 19V11a7 7 0 0114 0v8" />
              <path d="M9.5 19v-6.5a2.5 2.5 0 015 0V19" />
            </svg>
          </span>
          <span class="wordmark">Gotham</span>
        </div>

        <div class="stack gap-4">
          <p class="eyebrow">Self-hosted PaaS</p>
          <h1>The control plane for your own infrastructure</h1>
          <p class="lede">
            One Go binary runs the control plane, a small agent on every node.
            No outside services, no layer whose source you cannot read.
          </p>
        </div>

        <ul class="stack gap-4 auth-values" data-od-id="auth-values">
          <li v-for="prop in valueProps" :key="prop.title" class="auth-value">
            <GothamIcon :name="prop.icon" />
            <div>
              <p class="fg-2">{{ prop.title }}</p>
              <p class="small muted">{{ prop.body }}</p>
            </div>
          </li>
        </ul>
      </div>

      <div class="stack gap-2" data-od-id="auth-aside-foot">
        <p class="small muted">
          <span class="mono">cp.gotham.dev:8000</span> ·
          <span class="mono">v0.9.4</span> · stable channel
        </p>
        <p class="small muted">
          Gotham interface design — every figure on this screen is sample data.
        </p>
      </div>
    </aside>

    <main class="auth-main">
      <RouterView />
    </main>
  </div>
</template>

<style scoped>
/* Two-column shell ported from docs/design/login.html +
   docs/design/assets/gotham-views.css. Tokens come from styles/tokens.css;
   only mockup utilities missing from main.css are re-declared here, scoped. */
.auth {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  min-height: 100vh;
}

.auth-aside {
  background: var(--surface-warm);
  border-right: 1px solid var(--border);
  padding: var(--space-12) clamp(32px, 6vw, 72px);
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  gap: var(--space-8);
}

.auth-main {
  display: grid;
  place-items: center;
  padding: var(--space-8) var(--space-6);
  background: var(--bg);
}

.auth-values {
  max-width: 48ch;
  list-style: none;
  margin: 0;
  padding: 0;
}

.auth-value {
  display: flex;
  gap: var(--space-3);
  align-items: flex-start;
  color: var(--accent-ink);
}

/* The title keeps `.fg-2`; the body keeps `.muted`. Do not force every
   paragraph to `--fg`, which out-specified `.muted` and brightened the copy
   (B3-10). */
.auth-value p.fg-2 {
  color: var(--fg-2);
}

.auth-value p.small {
  margin-top: 2px;
}

.brand-lockup {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.brand-mark {
  width: 40px;
  height: 40px;
  border-radius: var(--radius-md);
  background: var(--accent);
  color: var(--accent-on);
  display: grid;
  place-items: center;
  flex: 0 0 auto;
}

.brand-mark svg {
  width: 24px;
  height: 24px;
}

.wordmark {
  font-family: var(--font-display);
  font-size: var(--text-lg);
  font-weight: 700;
  letter-spacing: -0.01em;
  color: var(--fg-2);
}

.eyebrow {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--muted);
}

.lede {
  font-size: var(--text-lg);
  line-height: 1.5;
  color: var(--muted);
  max-width: 62ch;
}

.stack {
  display: flex;
  flex-direction: column;
}

.gap-2 {
  gap: var(--space-2);
}

.gap-4 {
  gap: var(--space-4);
}

.gap-8 {
  gap: var(--space-8);
}

.small {
  font-size: var(--text-xs);
}

.fg-2 {
  color: var(--fg-2);
}

@media (max-width: 940px) {
  .auth {
    grid-template-columns: minmax(0, 1fr);
  }

  .auth-aside {
    display: none;
  }
}
</style>
