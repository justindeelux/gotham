<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import {
  strengthKindOf,
  strengthLabelOf,
} from "@/features/auth/utils/passwordStrength";
import type { StrengthLocale } from "@/features/auth/utils/passwordStrength";

interface Props {
  /** Meter score 0-4 (0 renders the empty meter). */
  score: number;
}

const props = defineProps<Props>();
const { locale } = useI18n();

const kind = computed<string>(() => strengthKindOf(props.score));
/** label renders the meter score in the active locale, reactively. */
const label = computed<string>(() =>
  strengthLabelOf(props.score, locale.value as StrengthLocale),
);
</script>

<template>
  <div class="strength-row">
    <span class="strength" aria-hidden="true">
      <i :class="score > 0 ? kind : ''" />
      <i :class="score > 1 ? kind : ''" />
      <i :class="score > 2 ? kind : ''" />
      <i :class="score > 3 ? kind : ''" />
    </span>
    <span class="small muted">{{ label }}</span>
  </div>
</template>

<style scoped>
.strength-row {
  display: flex;
  align-items: center;
  gap: 12px;
}

.strength {
  display: flex;
  gap: 4px;
  flex: 1 1 0;
}

.strength i {
  height: 4px;
  flex: 1 1 0;
  border-radius: var(--radius-pill);
  background: var(--surface-warm);
}

.strength i.on {
  background: var(--success);
}

.strength i.mid {
  background: var(--warn);
}

.strength i.weak {
  background: var(--danger);
}

.small {
  font-size: var(--text-xs);
  white-space: nowrap;
}
</style>
