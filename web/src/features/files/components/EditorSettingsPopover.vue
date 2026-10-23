<script setup lang="ts">
import { SlidersHorizontal } from "@lucide/vue";
import { NButton, NPopover, NSelect, NSwitch } from "naive-ui";
import { storeToRefs } from "pinia";
import { computed, nextTick, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import {
  EDITOR_FONT_SIZES,
  EDITOR_TAB_SIZES,
} from "@/features/files/schemas/editorSettings";
import type { EditorSettings } from "@/features/files/schemas/editorSettings";
import { useEditorSettingsStore } from "@/features/files/stores/editorSettings";
import AppTooltip from "@/shared/ui/AppTooltip.vue";

const { t } = useI18n();
const store = useEditorSettingsStore();
const { settings } = storeToRefs(store);

const show = ref(false);
const triggerRef = ref<HTMLElement | null>(null);
const contentRef = ref<HTMLElement | null>(null);

const themeOptions = computed(() => [
  { label: t("files.editor.settings.themeDark"), value: "dark" },
  { label: t("files.editor.settings.themeLight"), value: "light" },
]);

const familyOptions = computed(() => [
  { label: t("files.editor.settings.fontMono"), value: "mono" },
  { label: t("files.editor.settings.fontSans"), value: "sans" },
]);

const sizeOptions = EDITOR_FONT_SIZES.map((size) => ({ label: `${size}px`, value: size }));

const tabOptions = EDITOR_TAB_SIZES.map((size) => ({ label: String(size), value: size }));

const indentOptions = computed(() => [
  { label: t("files.editor.settings.indentSpaces"), value: "spaces" },
  { label: t("files.editor.settings.indentTabs"), value: "tabs" },
]);

function commit(patch: Partial<EditorSettings>): void {
  store.update(patch);
}

function close(): void {
  show.value = false;
}

watch(contentRef, (element) => {
  if (element && show.value) {
    element
      .querySelector<HTMLElement>(".n-base-selection-label, [role='switch']")
      ?.focus();
  }
});

watch(show, async (value) => {
  if (value) {
    return;
  }
  await nextTick();
  triggerRef.value?.querySelector<HTMLElement>("button")?.focus();
});
</script>

<template>
  <NPopover
    v-model:show="show"
    trigger="click"
    placement="bottom-end"
    :duration="0"
    :animated="false"
    class="ed-settings-popover"
  >
    <template #trigger>
      <span ref="triggerRef" class="ed-settings-anchor">
        <AppTooltip :label="t('files.editor.settings.label')">
          <NButton
            size="tiny"
            secondary
            :aria-label="t('files.editor.settings.label')"
            @keydown.esc.stop.prevent="close"
          >
            <template #icon>
              <SlidersHorizontal :size="14" :stroke-width="1.8" aria-hidden="true" />
            </template>
            {{ t("files.editor.settings.label") }}
          </NButton>
        </AppTooltip>
      </span>
    </template>

    <div
      ref="contentRef"
      class="ed-settings"
      role="group"
      :aria-label="t('files.editor.settings.title')"
      @keydown.esc.stop.prevent="close"
    >
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

      <div class="ed-settings-row">
        <span>{{ t("files.editor.settings.autocomplete") }}</span>
        <NSwitch
          size="small"
          :value="settings.autocomplete"
          :aria-label="t('files.editor.settings.autocomplete')"
          @update:value="(value: boolean) => commit({ autocomplete: value })"
        />
      </div>

      <div class="ed-settings-row">
        <span>{{ t("files.editor.settings.snippets") }}</span>
        <NSwitch
          size="small"
          :value="settings.snippets"
          :aria-label="t('files.editor.settings.snippets')"
          @update:value="(value: boolean) => commit({ snippets: value })"
        />
      </div>

      <div class="ed-settings-row">
        <span>{{ t("files.editor.settings.lineNumbers") }}</span>
        <NSwitch
          size="small"
          :value="settings.lineNumbers"
          :aria-label="t('files.editor.settings.lineNumbers')"
          @update:value="(value: boolean) => commit({ lineNumbers: value })"
        />
      </div>

      <div class="ed-settings-row">
        <span>{{ t("files.editor.settings.showInvisibles") }}</span>
        <NSwitch
          size="small"
          :value="settings.showInvisibles"
          :aria-label="t('files.editor.settings.showInvisibles')"
          @update:value="(value: boolean) => commit({ showInvisibles: value })"
        />
      </div>

      <div class="ed-settings-row">
        <span>{{ t("files.editor.settings.indentGuides") }}</span>
        <NSwitch
          size="small"
          :value="settings.indentGuides"
          :aria-label="t('files.editor.settings.indentGuides')"
          @update:value="(value: boolean) => commit({ indentGuides: value })"
        />
      </div>

      <div class="ed-settings-row">
        <span>{{ t("files.editor.settings.foldGutter") }}</span>
        <NSwitch
          size="small"
          :value="settings.foldGutter"
          :aria-label="t('files.editor.settings.foldGutter')"
          @update:value="(value: boolean) => commit({ foldGutter: value })"
        />
      </div>

      <div class="ed-settings-row">
        <span>{{ t("files.editor.settings.highlightActiveLine") }}</span>
        <NSwitch
          size="small"
          :value="settings.highlightActiveLine"
          :aria-label="t('files.editor.settings.highlightActiveLine')"
          @update:value="(value: boolean) => commit({ highlightActiveLine: value })"
        />
      </div>

      <div class="ed-settings-row">
        <span>{{ t("files.editor.settings.wrap") }}</span>
        <NSwitch
          size="small"
          :value="settings.wrap"
          :aria-label="t('files.editor.settings.wrap')"
          @update:value="(value: boolean) => commit({ wrap: value })"
        />
      </div>
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

.ed-settings-anchor {
  display: inline-flex;
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
