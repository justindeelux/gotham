<script setup lang="ts">
import { NButton } from "naive-ui";
import { useI18n } from "vue-i18n";

import type { McpSnippet } from "@/features/mcp/utils/snippets";
import { useCopyText } from "@/shared/composables/useCopyText";

const props = defineProps<{ snippet: McpSnippet; label: string }>();

const { t } = useI18n();
const { copyText } = useCopyText();
</script>

<template>
  <div class="snippet">
    <div class="snippet-head">
      <span class="snippet-label">{{ props.label }}</span>
      <NButton
        size="tiny"
        quaternary
        :data-testid="`mcp-copy-${props.snippet.id}`"
        @click="copyText(props.snippet.text, props.label)"
      >
        {{ t("mcp.clients.copy") }}
      </NButton>
    </div>
    <pre class="snippet-code" :data-testid="`mcp-snippet-${props.snippet.id}`">{{ props.snippet.text }}</pre>
  </div>
</template>

<style scoped>
.snippet {
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface-warm);
  overflow: hidden;
}

.snippet-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
  padding: var(--space-1) var(--space-2) var(--space-1) var(--space-3);
  border-bottom: 1px solid var(--border);
}

.snippet-label {
  font-size: var(--text-xs);
  color: var(--fg-2);
}

.snippet-code {
  margin: 0;
  padding: var(--space-3);
  overflow-x: auto;
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  line-height: 1.6;
  white-space: pre;
}
</style>
