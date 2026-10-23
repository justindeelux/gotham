<script setup lang="ts">
import { Pencil, Trash2 } from "@lucide/vue";
import { NButton, NPopconfirm, NTag } from "naive-ui";
import { useI18n } from "vue-i18n";

import type { QuickCommand } from "@/features/terminal/schemas/quickCommands";

interface Props {
  command: QuickCommand;
}

const props = defineProps<Props>();
const emit = defineEmits<{ insert: []; edit: []; delete: [] }>();
const { t } = useI18n();
</script>

<template>
  <li class="term-quick-commands-item">
    <div class="term-quick-commands-main">
      <div class="term-quick-commands-line">
        <span class="term-quick-commands-label">{{ props.command.label }}</span>
        <NTag v-if="props.command.abbreviation" size="tiny" type="info" class="mono">
          {{ props.command.abbreviation }}
        </NTag>
      </div>
      <code class="term-quick-commands-text mono" :title="props.command.command">
        {{ props.command.command }}
      </code>
    </div>
    <div class="term-quick-commands-actions">
      <NButton
        size="tiny"
        secondary
        class="term-quick-commands-insert"
        :aria-label="t('terminal.quickCommands.insertLabel', { label: props.command.label })"
        @click="emit('insert')"
      >
        {{ t("terminal.quickCommands.insert") }}
      </NButton>
      <NButton
        quaternary
        size="tiny"
        :data-term-quick-commands-edit="props.command.id"
        :aria-label="t('terminal.quickCommands.editLabel', { label: props.command.label })"
        @click="emit('edit')"
      >
        <template #icon>
          <Pencil :size="13" :stroke-width="1.8" aria-hidden="true" />
        </template>
      </NButton>
      <NPopconfirm
        :positive-text="t('terminal.quickCommands.delete')"
        :negative-text="t('terminal.quickCommands.cancel')"
        @positive-click="emit('delete')"
      >
        <template #trigger>
          <NButton
            quaternary
            size="tiny"
            :aria-label="t('terminal.quickCommands.deleteLabel', { label: props.command.label })"
          >
            <template #icon>
              <Trash2 :size="13" :stroke-width="1.8" aria-hidden="true" />
            </template>
          </NButton>
        </template>
        <span class="term-quick-commands-confirm">
          {{ t("terminal.quickCommands.deleteBody", { label: props.command.label }) }}
        </span>
      </NPopconfirm>
    </div>
  </li>
</template>

<style scoped>
.term-quick-commands-item {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
}

.term-quick-commands-main {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
  flex: 1 1 auto;
}

.term-quick-commands-line {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
}

.term-quick-commands-label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--text-sm);
  color: var(--fg-2);
}

.term-quick-commands-text {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--muted);
  font-size: var(--text-xs);
}

.term-quick-commands-actions {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  flex: 0 0 auto;
}

.term-quick-commands-confirm {
  display: block;
  max-width: 14rem;
  font-size: var(--text-xs);
}
</style>
