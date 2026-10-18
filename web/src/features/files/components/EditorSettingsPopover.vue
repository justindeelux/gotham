<script setup lang="ts">
import { SlidersHorizontal } from "@lucide/vue";
import { NButton, NPopover, NSelect, NSwitch } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import {
  EDITOR_FONT_SIZES,
  EDITOR_TAB_SIZES,
} from "@/features/files/schemas/editorSettings";
import type { EditorSettings } from "@/features/files/schemas/editorSettings";
import { useEditorSettingsStore } from "@/features/files/stores/editorSettings";

const { t } = useI18n();
const store = useEditorSettingsStore();
const settings = store.settings;

/** themeOptions are the two editor surfaces. */
const themeOptions = computed(() => [
  { label: t("files.editor.settings.themeDark"), value: "dark" },
  { label: t("files.editor.settings.themeLight"), value: "light" },
]);

/** familyOptions are the two token-backed font stacks. */
const familyOptions = computed(() => [
  { label: t("files.editor.settings.fontMono"), value: "mono" },
  { label: t("files.editor.settings.fontSans"), value: "sans" },
]);

/** sizeOptions are the offered font sizes, in pixels. */
const sizeOptions = EDITOR_FONT_SIZES.map((size) => ({ label: `${size}px`, value: size }));

/** tabOptions are the offered indentation widths. */
const tabOptions = EDITOR_TAB_SIZES.map((size) => ({ label: String(size), value: size }));

/** indentOptions switch between spaces and tabs. */
const indentOptions = computed(() => [
  { label: t("files.editor.settings.indentSpaces"), value: "spaces" },
  { label: t("files.editor.settings.indentTabs"), value: "tabs" },
]);

/** commit merges one preference change; the store persists and broadcasts it. */
function commit(patch: Partial<EditorSettings>): void {
  store.update(patch);
}
</script>

<template>
  <NPopover trigger="click" placement="bottom-end" class="ed-settings-popover">
    <template #trigger>
      <NButton size="tiny" secondary :aria-label="t('files.editor.settings.label')">
        <template #icon>
          <SlidersHorizontal :size="14" :stroke-width="1.8" aria-hidden="true" />
        </template>
        {{ t("files.editor.settings.label") }}
      </NButton>
    </template>

    <div class="ed-settings" role="group" :aria-label="t('files.editor.settings.title')">
      <p class="ed-settings-title">{{ t("files.editor.settings.title") }}</p>

      <label class="ed-settings-row">
        <span>{{ t("files.editor.settings.theme") }}</span>
        <NSelect
          size="small"
          class="ed-settings-control"
          :value="settings.theme"
          :options="themeOptions"
          @update:value="(value: string) => commit({ theme: value as EditorSettings['theme'] })"
        />
      </label>

      <label class="ed-settings-row">
        <span>{{ t("files.editor.settings.fontSize") }}</span>
        <NSelect
          size="small"
          class="ed-settings-control"
          :value="settings.fontSize"
          :options="sizeOptions"
          @update:value="(value: number) => commit({ fontSize: value })"
        />
      </label>

      <label class="ed-settings-row">
        <span>{{ t("files.editor.settings.fontFamily") }}</span>
        <NSelect
          size="small"
          class="ed-settings-control"
          :value="settings.fontFamily"
          :options="familyOptions"
          @update:value="(value: string) => commit({ fontFamily: value as EditorSettings['fontFamily'] })"
        />
      </label>

      <label class="ed-settings-row">
        <span>{{ t("files.editor.settings.tabSize") }}</span>
        <NSelect
          size="small"
          class="ed-settings-control"
          :value="settings.tabSize"
          :options="tabOptions"
          @update:value="(value: number) => commit({ tabSize: value as EditorSettings['tabSize'] })"
        />
      </label>

      <label class="ed-settings-row">
        <span>{{ t("files.editor.settings.indent") }}</span>
        <NSelect
          size="small"
          class="ed-settings-control"
          :value="settings.insertSpaces ? 'spaces' : 'tabs'"
          :options="indentOptions"
          @update:value="(value: string) => commit({ insertSpaces: value === 'spaces' })"
        />
      </label>

      <label class="ed-settings-row">
        <span>{{ t("files.editor.settings.lineNumbers") }}</span>
        <NSwitch
          size="small"
          :value="settings.lineNumbers"
          @update:value="(value: boolean) => commit({ lineNumbers: value })"
        />
      </label>

      <label class="ed-settings-row">
        <span>{{ t("files.editor.settings.showInvisibles") }}</span>
        <NSwitch
          size="small"
          :value="settings.showInvisibles"
          @update:value="(value: boolean) => commit({ showInvisibles: value })"
        />
      </label>

      <label class="ed-settings-row">
        <span>{{ t("files.editor.settings.indentGuides") }}</span>
        <NSwitch
          size="small"
          :value="settings.indentGuides"
          @update:value="(value: boolean) => commit({ indentGuides: value })"
        />
      </label>

      <label class="ed-settings-row">
        <span>{{ t("files.editor.settings.foldGutter") }}</span>
        <NSwitch
          size="small"
          :value="settings.foldGutter"
          @update:value="(value: boolean) => commit({ foldGutter: value })"
        />
      </label>

      <label class="ed-settings-row">
        <span>{{ t("files.editor.settings.highlightActiveLine") }}</span>
        <NSwitch
          size="small"
          :value="settings.highlightActiveLine"
          @update:value="(value: boolean) => commit({ highlightActiveLine: value })"
        />
      </label>

      <label class="ed-settings-row">
        <span>{{ t("files.editor.settings.wrap") }}</span>
        <NSwitch
          size="small"
          :value="settings.wrap"
          @update:value="(value: boolean) => commit({ wrap: value })"
        />
      </label>
    </div>
  </NPopover>
</template>

<style scoped>
.ed-settings {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  width: 264px;
  max-height: 60vh;
  overflow-y: auto;
  padding: var(--space-1);
}

.ed-settings-title {
  margin: 0 0 var(--space-1);
  font-weight: 600;
  color: var(--fg-2);
}

.ed-settings-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  font-size: var(--text-sm);
  color: var(--fg);
}

.ed-settings-control {
  width: 132px;
}
</style>
