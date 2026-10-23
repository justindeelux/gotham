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
          <img class="brand-logo" src="/logo/gotham-logo.svg" alt="Gotham" />
        </div>

        <div class="stack gap-4">
          <p class="eyebrow">{{ t("auth.shell.eyebrow") }}</p>
          <h1>{{ t("auth.shell.headline") }}</h1>
          <p class="lede">
            {{ t("auth.shell.lede") }}
          </p>
        </div>

        <ul class="stack gap-4 auth-values" data-od-id="auth-values">
          <li v-for="prop in valueProps" :key="prop.icon" class="auth-value">
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
      <div class="auth-mobile-brand" aria-hidden="true">
        <img class="brand-wordmark" src="/logo/gotham-wordmark.svg" alt="" />
      </div>
      <RouterView />
    </main>
  </div>
</template>

<style scoped>
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
  align-content: center;
  gap: var(--space-6);
}

.auth-mobile-brand {
  display: none;
  align-items: center;
  gap: var(--space-3);
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

.brand-logo {
  display: block;
  height: var(--brand-logo-h);
  width: auto;
  max-width: 100%;
  aspect-ratio: 946 / 759;
  object-fit: contain;
}

.brand-wordmark {
  display: block;
  height: var(--brand-wordmark-sm-h);
  width: auto;
  aspect-ratio: 946 / 232;
  object-fit: contain;
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

@media (max-width: 640px) {
  .auth-mobile-brand {
    display: flex;
  }
}
</style>
