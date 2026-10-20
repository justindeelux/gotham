<script setup lang="ts">
import { X } from "@lucide/vue";
import { NButton } from "naive-ui";
import { useI18n } from "vue-i18n";

/** Inline status notice above the terminal viewport (JUS-249). */
interface Props {
  message: string;
}

const props = defineProps<Props>();
const emit = defineEmits<{ dismiss: [] }>();
const { t } = useI18n();
</script>

<template>
  <p class="term-command-notice" role="status">
    {{ props.message }}
    <NButton
      quaternary
      size="tiny"
      :aria-label="t('terminal.quickCommands.dismissNotice')"
      @click.stop="emit('dismiss')"
    >
      <template #icon>
        <X :size="12" :stroke-width="1.8" aria-hidden="true" />
      </template>
    </NButton>
  </p>
</template>

<style scoped>
.term-command-notice {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-1) var(--space-3);
  border-bottom: 1px solid var(--border);
  background: var(--surface);
  color: var(--fg-2);
  font-size: var(--text-xs);
}
</style>
