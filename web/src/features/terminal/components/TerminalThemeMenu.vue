<script setup lang="ts">
import { Palette } from "@lucide/vue";
import { NButton, NPopover } from "naive-ui";
import {
  computed,
  nextTick,
  ref,
  useId,
  watch,
} from "vue";
import type { ComponentPublicInstance } from "vue";
import { useI18n } from "vue-i18n";

import {
  TERMINAL_THEMES,
  isTerminalThemeName,
  type TerminalThemeName,
} from "@/features/terminal/schemas/terminalAppearance";
import { useTerminalStore } from "@/features/terminal/stores/terminal";

const { t } = useI18n();
const terminal = useTerminalStore();
const groupName = useId();
const show = ref(false);
const triggerRef = ref<ComponentPublicInstance | null>(null);
const contentRef = ref<HTMLElement | null>(null);

const THEME_LABEL_KEYS: Record<TerminalThemeName, string> = {
  "gotham-dark": "terminal.appearance.themes.gothamDark",
  "gotham-light": "terminal.appearance.themes.gothamLight",
};

const themes = computed(() =>
  TERMINAL_THEMES.map((name) => ({
    name,
    label: t(THEME_LABEL_KEYS[name]),
    swatchClass: name === "gotham-light" ? "is-light" : "is-dark",
  })),
);

function choose(name: TerminalThemeName): void {
  terminal.setTheme(name);
}

function closeMenu(): void {
  show.value = false;
  void nextTick(() => {
    const trigger = triggerRef.value?.$el;
    if (trigger instanceof HTMLElement) {
      trigger.focus();
    }
  });
}

function onMenuKeydown(event: KeyboardEvent): void {
  event.stopPropagation();
  if (event.key === "Escape") {
    event.preventDefault();
    closeMenu();
    return;
  }
  if (event.key !== "Enter") {
    return;
  }
  const target = event.target;
  if (target instanceof HTMLInputElement && target.type === "radio") {
    event.preventDefault();
    if (isTerminalThemeName(target.value)) {
      choose(target.value);
    }
  }
}

watch(show, async (open) => {
  if (!open) {
    return;
  }
  await nextTick();
  const content = contentRef.value;
  const checked = content?.querySelector<HTMLInputElement>(
    "input[type='radio']:checked",
  );
  const first = content?.querySelector<HTMLInputElement>("input[type='radio']");
  (checked ?? first)?.focus();
});
</script>

<template>
  <NPopover
    v-model:show="show"
    trigger="click"
    placement="bottom-end"
    :duration="0"
    :animated="false"
  >
    <template #trigger>
      <NButton
        ref="triggerRef"
        quaternary
        size="tiny"
        class="term-theme-btn"
        :aria-label="t('terminal.appearance.label')"
        aria-haspopup="true"
        :aria-expanded="show"
        @keydown.esc.stop.prevent="closeMenu"
      >
        <template #icon>
          <Palette :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
      </NButton>
    </template>

    <div
      ref="contentRef"
      class="term-themes"
      role="radiogroup"
      :aria-label="t('terminal.appearance.title')"
      @keydown.stop="onMenuKeydown"
    >
      <p class="term-themes-title">{{ t("terminal.appearance.title") }}</p>
      <label
        v-for="theme in themes"
        :key="theme.name"
        class="term-theme-option"
        :class="{ 'is-active': terminal.theme === theme.name }"
      >
        <input
          class="term-theme-radio"
          type="radio"
          :name="groupName"
          :value="theme.name"
          :checked="terminal.theme === theme.name"
          @change="choose(theme.name)"
        />
        <span class="term-theme-label">{{ theme.label }}</span>
        <span
          class="term-theme-swatch"
          :class="theme.swatchClass"
          aria-hidden="true"
        />
      </label>
    </div>
  </NPopover>
</template>

<style scoped>
.term-themes {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  min-width: min(13rem, 70vw);
  padding: var(--space-1);
}

.term-themes-title {
  margin: 0 0 var(--space-1);
  font-weight: 600;
  color: var(--fg-2);
  font-size: var(--text-sm);
}

.term-theme-option {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-1) var(--space-2);
  border-radius: var(--radius-sm);
  color: var(--fg);
  font-size: var(--text-sm);
  cursor: pointer;
}

.term-theme-option:hover,
.term-theme-option.is-active {
  background: var(--hover-row);
}

.term-theme-radio {
  margin: 0;
  accent-color: var(--accent);
}

.term-theme-radio:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}

.term-theme-label {
  flex: 1 1 auto;
}

.term-theme-swatch {
  width: var(--space-4);
  height: var(--space-4);
  border-radius: var(--radius-pill);
  border: 1px solid var(--border-soft);
}

.term-theme-swatch.is-dark {
  background: linear-gradient(
    135deg,
    var(--term-dark-bg) 50%,
    var(--term-dark-ansi-blue) 50%
  );
}

.term-theme-swatch.is-light {
  background: linear-gradient(
    135deg,
    var(--term-light-bg) 50%,
    var(--term-light-ansi-blue) 50%
  );
}
</style>
