<script setup lang="ts">
import { Copy } from "@lucide/vue";
import { NButton } from "naive-ui";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";

import {
  isSupportedSuggestionShell,
  shellIntegrationSnippet,
} from "@/features/terminal/utils/quickCommandInsertion";

/**
 * Optional shell-integration hint (JUS-249). Shown only while a session has
 * emitted no prompt marks. The snippet is copyable and additive; Gotham never
 * injects it into the shell.
 */
interface Props {
  shell: string;
  alternateScreen: boolean;
}

const props = defineProps<Props>();
const { t } = useI18n();
const copied = ref(false);

const snippet = computed<string>(() => shellIntegrationSnippet(props.shell));
const supported = computed<boolean>(() => isSupportedSuggestionShell(props.shell));

/** copySnippet copies the optional rc snippet. */
async function copySnippet(): Promise<void> {
  const clipboard =
    typeof navigator === "undefined" ? undefined : navigator.clipboard;
  if (!clipboard || snippet.value === "") {
    return;
  }
  try {
    await clipboard.writeText(snippet.value);
    copied.value = true;
  } catch {
    copied.value = false;
  }
}
</script>

<template>
  <section class="qc-hint">
    <p class="qc-hint-title">{{ t("terminal.quickCommands.hintTitle") }}</p>
    <p class="qc-note">{{ t("terminal.quickCommands.hintBody") }}</p>
    <p v-if="props.alternateScreen" class="qc-note">
      {{ t("terminal.quickCommands.hintAlternate") }}
    </p>
    <template v-if="supported">
      <pre class="qc-snippet mono">{{ snippet }}</pre>
      <NButton size="tiny" secondary class="qc-copy" @click="copySnippet">
        <template #icon>
          <Copy :size="13" :stroke-width="1.8" aria-hidden="true" />
        </template>
        {{ copied ? t("terminal.quickCommands.copied") : t("terminal.quickCommands.copySnippet") }}
      </NButton>
    </template>
    <p v-else class="qc-note">{{ t("terminal.quickCommands.hintUnsupported") }}</p>
  </section>
</template>

<style scoped>
.qc-hint {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  padding-top: var(--space-2);
  border-top: 1px solid var(--border);
}

.qc-hint-title {
  margin: 0;
  color: var(--fg-2);
  font-size: var(--text-xs);
  font-weight: 600;
}

.qc-note {
  margin: 0;
  color: var(--muted);
  font-size: var(--text-xs);
  line-height: 1.45;
}

.qc-snippet {
  margin: 0;
  padding: var(--space-2);
  max-height: 12rem;
  overflow: auto;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface-warm);
  color: var(--fg-2);
  font-size: var(--text-xs);
  line-height: 1.4;
  white-space: pre;
}

.qc-copy {
  align-self: flex-start;
}
</style>
