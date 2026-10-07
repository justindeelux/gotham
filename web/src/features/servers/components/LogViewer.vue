<script setup lang="ts">
import { NButton } from "naive-ui";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";

import { useLogStream } from "@/features/servers/composables/useLogStream";

/**
 * Monospace log terminal for the realtime drawer.
 *
 * Renders raw frames pushed by the realtime hub; it never fabricates log
 * content. Auto-scroll follows the tail until the reader scrolls up or flips
 * the follow toggle; pause freezes the view and queues incoming lines.
 */

interface Props {
  /** Server whose agent sources the log stream. */
  serverId: string;
  /** Container (or deploy container) being streamed. */
  containerId: string;
  /** Drawer heading; the header renders only when set. */
  title?: string;
  /** Drawer sub-heading. */
  subtitle?: string;
  /** Explicit channel override; defaults to `logs:{serverId}:{containerId}`. */
  channel?: string;
  /**
   * When true, ask the control plane to bridge the agent log stream on mount.
   * Only valid for raw container logs (`logs:{serverId}:{containerId}`); the
   * deploy-log wrapper leaves it off because its channel is already published.
   */
  autoStartStream?: boolean;
  /** Realtime endpoint path. */
  wsPath?: string;
  /** Maximum rendered lines before the oldest are dropped. */
  maxLines?: number;
}

const props = withDefaults(defineProps<Props>(), {
  title: "",
  subtitle: "",
  channel: "",
  autoStartStream: false,
  wsPath: "/api/v1/ws",
  maxLines: 2000,
});

const logBody = ref<HTMLElement | null>(null);

const {
  lines,
  isPaused,
  isFollowing,
  channelName,
  statusLabel,
  statusClasses,
  handleScroll,
  togglePause,
  toggleFollow,
  clearLines,
  downloadLog,
} = useLogStream(props, logBody);

const { t } = useI18n();

/** lineCountText renders the rendered-line count in the display locale. */
const lineCountText = computed<string>(() =>
  lines.value.length === 1
    ? t("servers.logs.lineOne", { count: 1 })
    : t("servers.logs.lineOther", { count: lines.value.length }),
);
</script>

<template>
  <section class="log-viewer">
    <header v-if="title || subtitle" class="log-viewer__head">
      <div class="log-viewer__heading">
        <h3 v-if="title">{{ title }}</h3>
        <p v-if="subtitle">{{ subtitle }}</p>
      </div>
      <span class="realtime" :class="statusClasses">{{ statusLabel }}</span>
    </header>

    <div class="log-viewer__toolbar">
      <!-- The labels already flip (Pause/Resume, Follow/Following), so no
           aria-pressed: a screen reader would otherwise hear "Resume, pressed". -->
      <NButton
        size="small"
        secondary
        @click="togglePause"
      >
        {{ isPaused ? $t("servers.logs.resume") : $t("servers.logs.pause") }}
      </NButton>
      <NButton
        size="small"
        secondary
        :type="isFollowing ? 'primary' : 'default'"
        @click="toggleFollow"
      >
        {{ isFollowing ? $t("servers.logs.following") : $t("servers.logs.follow") }}
      </NButton>
      <NButton size="small" secondary @click="clearLines">{{ $t("servers.logs.clear") }}</NButton>
      <NButton size="small" secondary @click="downloadLog">{{ $t("servers.logs.download") }}</NButton>
      <span class="log-viewer__count">{{ lineCountText }}</span>
    </div>

    <!-- role=log + aria-live announce appended lines; tabindex makes the
         scroll region reachable so it can be scrolled with the keyboard
         (B2-7). -->
    <div
      ref="logBody"
      class="log"
      role="log"
      aria-live="polite"
      aria-relevant="additions"
      :aria-label="$t('servers.logs.logLabel')"
      tabindex="0"
      @scroll="handleScroll"
    >
      <p v-if="lines.length === 0" class="log-viewer__empty">
        {{ $t("servers.logs.waiting") }}
      </p>
      <div
        v-for="line in lines"
        :key="line.id"
        class="log-line"
        :data-kind="line.kind"
      >
        <span class="t">{{ line.ts }}</span>
        <span class="m">{{ line.text }}</span>
      </div>
    </div>

    <p class="log-viewer__channel">
      {{ $t("servers.logs.channel") }} <span class="mono">{{ channelName }}</span>
    </p>
  </section>
</template>

<style scoped>
.log-viewer {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  min-width: 0;
}

.log-viewer__head {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.log-viewer__heading {
  min-width: 0;
  flex: 1 1 auto;
}

.log-viewer__heading h3 {
  margin: 0;
  font-size: var(--text-base);
  font-weight: 600;
  color: var(--fg-2);
}

.log-viewer__heading p {
  margin: 2px 0 0;
  font-size: var(--text-xs);
  color: var(--muted);
}

.realtime {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-family: var(--font-mono);
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--success-ink);
  background: var(--success-soft);
  border-radius: var(--radius-pill);
  padding: 2px 8px;
  white-space: nowrap;
}

.realtime.is-paused {
  color: var(--warn-ink);
  background: var(--warn-soft);
}

.realtime.is-offline {
  color: var(--muted);
  background: var(--surface-warm);
}

.log-viewer__toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.log-viewer__count {
  margin-left: auto;
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  color: var(--muted);
}

.log {
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  line-height: 1.55;
  padding: var(--space-3);
  overflow-y: auto;
  overflow-x: auto;
  min-height: 320px;
  max-height: 56vh;
}

.log-line {
  display: grid;
  grid-template-columns: 78px minmax(0, 1fr);
  gap: var(--space-2);
  white-space: pre-wrap;
  word-break: break-word;
  color: var(--fg);
}

.log-line .t {
  color: var(--muted);
}

.log-line[data-kind="notice"] .m {
  color: var(--warn-ink);
  font-style: italic;
}

.log-viewer__empty {
  margin: 0;
  color: var(--muted);
}

.log-viewer__channel {
  margin: 0;
  font-size: var(--text-xs);
  color: var(--meta);
}

.mono {
  font-family: var(--font-mono);
}
</style>
