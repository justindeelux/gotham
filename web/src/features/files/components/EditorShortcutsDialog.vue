<script setup lang="ts">
import { X } from "@lucide/vue";
import { NButton, NModal } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import {
  EDITOR_SHORTCUT_GROUPS,
  editorShortcuts,
  isMacPlatform,
  shortcutKeysFor,
  shortcutLabel,
} from "@/features/files/editor/shortcuts";
import type { ShortcutGroup, ShortcutPlatform } from "@/features/files/editor/shortcuts";

interface Props {
  show: boolean;
}

const props = defineProps<Props>();
const emit = defineEmits<{ "update:show": [value: boolean] }>();

const { t } = useI18n();
const platform = computed<ShortcutPlatform>(() => (isMacPlatform() ? "mac" : "other"));

function rowsFor(group: ShortcutGroup): Array<{ id: string; label: string; keys: string[] }> {
  return editorShortcuts
    .filter((shortcut) => shortcut.group === group)
    .map((shortcut) => ({
      id: shortcut.id,
      label: String(t(shortcut.label)),
      keys: shortcutKeysFor(shortcut, platform.value).map((key) =>
        shortcutLabel(key, platform.value),
      ),
    }));
}

function close(): void {
  emit("update:show", false);
}
</script>

<template>
  <NModal
    class="app-modal ed-shortcuts-dialog"
    preset="card"
    :show="props.show"
    style="width: 640px; max-width: 94vw"
    :aria-label="t('files.editor.shortcuts.title')"
    :auto-focus="true"
    :trap-focus="true"
    :close-on-esc="true"
    :bordered="false"
    :closable="false"
    @update:show="(value: boolean) => emit('update:show', value)"
  >
    <div class="ed-shortcuts-head">
      <h2 class="ed-shortcuts-title">{{ t("files.editor.shortcuts.title") }}</h2>
      <NButton
        quaternary
        size="small"
        class="ed-shortcuts-close"
        :aria-label="t('files.editor.shortcuts.close')"
        @click="close"
      >
        <template #icon>
          <X :size="16" :stroke-width="1.8" aria-hidden="true" />
        </template>
      </NButton>
    </div>

    <div
      class="ed-shortcuts"
      tabindex="0"
      :aria-label="t('files.editor.shortcuts.title')"
    >
      <section
        v-for="group in EDITOR_SHORTCUT_GROUPS"
        :key="group"
        class="ed-shortcut-group"
        :aria-labelledby="`ed-shortcut-group-${group}`"
      >
        <h3 :id="`ed-shortcut-group-${group}`" class="ed-shortcut-heading">
          {{ t(`files.editor.shortcuts.groups.${group}`) }}
        </h3>
        <dl class="ed-shortcut-list">
          <div v-for="row in rowsFor(group)" :key="row.id" class="ed-shortcut-row">
            <dt>{{ row.label }}</dt>
            <dd>
              <kbd v-for="key in row.keys" :key="key" class="ed-shortcut-key">{{ key }}</kbd>
            </dd>
          </div>
        </dl>
      </section>
    </div>
  </NModal>
</template>

<style scoped>
.ed-shortcuts-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  margin-bottom: var(--space-2);
}

.ed-shortcuts-title {
  margin: 0;
  color: var(--fg-2);
  font-size: var(--text-lg);
  font-weight: 600;
}

.ed-shortcuts {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--space-2) var(--space-5);
  max-height: 60vh;
  overflow-y: auto;
  padding-right: var(--space-1);
}

.ed-shortcut-group {
  min-width: 0;
}

.ed-shortcut-heading {
  margin: 0 0 var(--space-1);
  padding-bottom: var(--space-1);
  border-bottom: 1px solid var(--border);
  color: var(--fg-2);
  font-size: var(--text-sm);
  font-weight: 600;
}

.ed-shortcut-list {
  margin: 0;
}

.ed-shortcut-row {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--space-3);
  padding: var(--space-1) 0;
  border-bottom: 1px solid var(--border-soft);
}

.ed-shortcut-row dt {
  min-width: 0;
  color: var(--fg);
  font-size: var(--text-sm);
}

.ed-shortcut-row dd {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: var(--space-1);
  margin: 0;
}

.ed-shortcut-key {
  padding: 1px var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface-warm);
  color: var(--fg-2);
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  white-space: nowrap;
}

@media (max-width: 640px) {
  .ed-shortcuts {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
