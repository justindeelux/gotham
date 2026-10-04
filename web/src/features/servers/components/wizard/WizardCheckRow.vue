<script setup lang="ts">
import type { CheckState } from "@/features/servers/composables/useAddServerWizard";

interface Props {
  state: CheckState;
  label: string;
  detail: string;
}

defineProps<Props>();
</script>

<template>
  <div class="check-row" :class="`is-${state}`">
    <span class="mark">
      <span v-if="state === 'ok'" class="glyph">✓</span>
      <span v-else-if="state === 'fail'" class="glyph">✕</span>
    </span>
    <span class="grow">{{ label }}</span>
    <span class="detail">{{ detail }}</span>
  </div>
</template>

<style scoped>
.check-row {
  display: flex;
  align-items: flex-start;
  flex-wrap: wrap;
  gap: var(--space-3);
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-sm);
  background: var(--surface-warm);
}

.check-row .mark {
  width: 18px;
  height: 18px;
  border-radius: var(--radius-pill);
  display: grid;
  place-items: center;
  flex: 0 0 auto;
  border: 1.5px solid var(--meta);
}

.check-row .mark .glyph {
  font-size: 11px;
  line-height: 1;
  font-weight: 700;
}

.check-row.is-ok .mark {
  background: var(--success);
  border-color: var(--success);
  color: var(--accent-on);
}

.check-row.is-fail .mark {
  background: var(--danger);
  border-color: var(--danger);
  color: var(--accent-on);
}

.check-row.is-running .mark {
  border-color: var(--accent);
  border-top-color: transparent;
  animation: wizard-spin 700ms linear infinite;
}

@keyframes wizard-spin {
  to {
    transform: rotate(360deg);
  }
}

/* The label keeps a readable minimum width and whole words; it wraps onto
 * its own line on narrow rows instead of collapsing to one letter per line.
 * Only the long detail value may break anywhere (JUS-8). */
.check-row > .grow {
  flex: 1 1 11rem;
  min-width: 0;
  word-break: normal;
  overflow-wrap: break-word;
}

.check-row .detail {
  margin-left: auto;
  flex: 1 1 9rem;
  min-width: 0;
  max-width: 100%;
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  color: var(--muted);
  text-align: right;
  word-break: normal;
  overflow-wrap: anywhere;
}
</style>
