<script setup lang="ts">
import qrcodeGenerator from "qrcode-generator";
import { computed } from "vue";

const props = withDefaults(
  defineProps<{
    value: string;
    size?: number;
    label: string;
  }>(),
  { size: 220 },
);

const grid = computed<{ count: number; path: string }>(() => {
  try {
    const qr = qrcodeGenerator(0, "M");
    qr.addData(props.value);
    qr.make();

    const count = qr.getModuleCount();
    let path = "";
    for (let row = 0; row < count; row += 1) {
      for (let col = 0; col < count; col += 1) {
        if (qr.isDark(row, col)) {
          path += `M${col} ${row}h1v1h-1z`;
        }
      }
    }
    return { count, path };
  } catch {
    return { count: 0, path: "" };
  }
});
</script>

<template>
  <svg
    class="qr-code"
    :viewBox="`0 0 ${grid.count} ${grid.count}`"
    :width="size"
    :height="size"
    role="img"
    :aria-label="label"
    shape-rendering="crispEdges"
  >
    <rect :width="grid.count" :height="grid.count" class="qr-quiet" />
    <path :d="grid.path" class="qr-module" />
  </svg>
</template>

<style scoped>
.qr-quiet {
  fill: #ffffff;
}

.qr-module {
  fill: #000000;
}

.qr-code {
  border-radius: var(--radius-md, 8px);
}
</style>
