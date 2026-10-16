<script setup lang="ts">
import qrcodeGenerator from "qrcode-generator";
import { computed } from "vue";

/**
 * QrCode renders `value` as an inline SVG QR code (JUS-196, TOTP enrollment).
 * The matrix is built with the vetted qrcode-generator library; the dark
 * modules are emitted directly as a single SVG path so no raw markup is
 * injected (v-html stays out of the codebase). The value never appears in the
 * SVG; only the label is exposed, as the image's accessible name.
 */
const props = withDefaults(
  defineProps<{
    /** Text to encode (the otpauth:// URI). */
    value: string;
    /** Rendered edge length in pixels. */
    size?: number;
    /** Accessible name for the image role. */
    label: string;
  }>(),
  { size: 220 },
);

/** grid is the QR module count and the dark modules' coordinates. */
const grid = computed<{ count: number; path: string }>(() => {
  try {
    // Type 0 auto-selects the smallest version; "M" is the standard tolerance.
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
    // An oversized (or otherwise unencodable) value must not break the whole
    // dialog render; fall back to an empty code.
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
/* Tokens come from the design system; the two module colors stay literal so a
   scanner keeps the contrast it needs regardless of theme. */
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
