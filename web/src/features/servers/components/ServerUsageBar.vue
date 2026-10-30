<script setup lang="ts">
import { NProgress } from "naive-ui";
import { computed } from "vue";

import { usageIndicator } from "@/features/servers/utils/serverDetailView";

interface Props {
  label: string;
  value: number | null;
}

const props = defineProps<Props>();
const indicator = computed(() => usageIndicator(props.value));
</script>

<template>
  <NProgress
    v-if="indicator"
    class="server-usage-bar"
    type="line"
    :percentage="indicator.percentage"
    :color="indicator.color"
    :height="6"
    :rail-style="{ borderRadius: 'var(--radius-pill)' }"
    :aria-label="`${label}: ${indicator.percentage}%`"
  />
  <span v-else class="server-usage-empty">—</span>
</template>

<style scoped>
.server-usage-bar {
  max-width: 16rem;
}

.server-usage-empty {
  color: var(--muted);
}
</style>
