<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { RouterView } from "vue-router";

import GothamIcon from "@/shared/ui/GothamIcon.vue";
import type { IconName } from "@/shared/ui/GothamIcon.vue";
import LanguageSelect from "@/shared/ui/LanguageSelect.vue";

const { t } = useI18n();

interface ValueProp {
  icon: IconName;
  title: string;
  body: string;
}

// Value props rewritten from the Vietnamese mockup (docs/design/login.html,
// the UI source of truth). Copy renders through the auth catalog so a
// language switch updates the shell without navigating; form copy stays with
// UI-2 pages, only the shell lives here.
const valueProps = computed<ValueProp[]>(() => [
  {
    icon: "server",
    title: t("auth.shell.selfHostedTitle"),
    body: t("auth.shell.selfHostedBody"),
  },
  {
    icon: "shield",
    title: t("auth.shell.tlsTitle"),
    body: t("auth.shell.tlsBody"),
  },
  {
    icon: "refresh",
    title: t("auth.shell.updatesTitle"),
    body: t("auth.shell.updatesBody"),
  },
  {
    icon: "rocket",
    title: t("auth.shell.enginesTitle"),
    body: t("auth.shell.enginesBody"),
  },
]);
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
          <p class="eyebrow">{{ t("auth.shell.eyebrow") }}</p>
          <h1>{{ t("auth.shell.headline") }}</h1>
          <p class="lede">
            {{ t("auth.shell.lede") }}
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
          {{ t("auth.shell.footnote") }}
        </p>
      </div>
    </aside>

    <main class="auth-main">
      <div class="auth-lang">
        <LanguageSelect />
      </div>
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
  position: relative;
}

.auth-lang {
  position: absolute;
  top: var(--space-4);
  right: var(--space-4);
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
