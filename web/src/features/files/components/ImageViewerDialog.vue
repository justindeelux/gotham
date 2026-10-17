<script setup lang="ts">
import { Maximize, ZoomIn, ZoomOut } from "@lucide/vue";
import { NButton, NModal, NSpin } from "naive-ui";
import { onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import {
  describeFilesError,
  fetchDownloadBlob,
  filesErrorKey,
  isCanceledError,
  issueDownloadTicket,
} from "@/features/files/api/files";
import { useImageZoom, WHEEL_FACTOR, zoomPercent } from "@/features/files/composables/useImageZoom";
import type { FilesEntry } from "@/features/files/schemas/files";
import { joinPath } from "@/features/files/utils/files";

interface Props {
  show: boolean;
  serverId: string;
  teamId: string;
  root: string;
  /** directory is the viewer's root-relative folder ("" = root top level). */
  directory: string;
  /** entry is the image to show; null renders an empty closed shell. */
  entry: FilesEntry | null;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  "update:show": [value: boolean];
}>();

const { t } = useI18n();
const { view, transform, zoomAt, pan, reset } = useImageZoom();

const stageEl = ref<HTMLElement | null>(null);
const objectUrl = ref<string | null>(null);
const loading = ref(false);
const error = ref<string | null>(null);

/** loadSeq invalidates a superseded load (a second open or a close). */
let loadSeq = 0;
/** aborter cancels the in-flight blob fetch when the dialog closes. */
let aborter: AbortController | null = null;
/** drag anchor for an active pan gesture; null when not panning. */
let dragAnchor: { x: number; y: number } | null = null;

/** cancelLoad invalidates and aborts any in-flight load. */
function cancelLoad(): void {
  loadSeq += 1;
  aborter?.abort();
  aborter = null;
}

/** releaseUrl revokes the current object URL, if any. */
function releaseUrl(): void {
  if (objectUrl.value !== null) {
    URL.revokeObjectURL(objectUrl.value);
    objectUrl.value = null;
  }
}

/** closeAll drops the load and the transient states. The object URL is
 * released on the modal's leave transition ({@link onAfterLeave}), never
 * mid-close, so a reopen racing the fade cannot lose its URL. */
function closeAll(): void {
  cancelLoad();
  dragAnchor = null;
  loading.value = false;
  error.value = null;
}

/**
 * onAfterLeave revokes the object URL once the dialog is gone. It runs only
 * while closed: a reopen that cancelled the leave keeps its own URL.
 */
function onAfterLeave(): void {
  if (!props.show) {
    releaseUrl();
  }
}

/**
 * load mints a single-use download ticket through the authenticated files
 * API, then fetches the body as a Blob and shows it through an object URL.
 * The ticket is the download route's only credential (no bearer on fetch).
 */
async function load(): Promise<void> {
  const target = props.entry;
  cancelLoad();
  releaseUrl();
  reset();
  error.value = null;
  if (target === null) {
    return;
  }
  const seq = loadSeq;
  aborter = new AbortController();
  loading.value = true;
  try {
    const ticket = await issueDownloadTicket(
      props.serverId,
      props.teamId,
      props.root,
      joinPath(props.directory, target.name),
    );
    if (seq !== loadSeq) {
      return;
    }
    const blob = await fetchDownloadBlob(
      props.serverId,
      ticket,
      target.name,
      aborter.signal,
    );
    if (seq !== loadSeq) {
      return;
    }
    objectUrl.value = URL.createObjectURL(blob);
  } catch (err) {
    if (seq !== loadSeq) {
      return;
    }
    if (err instanceof DOMException && err.name === "AbortError") {
      return;
    }
    if (isCanceledError(err)) {
      return;
    }
    // An unclassified failure (a network TypeError, a ticket 500 with no
    // catalog mapping) gets the viewer copy, never a raw message.
    error.value =
      filesErrorKey(err) === null
        ? String(t("files.viewer.loadError"))
        : describeFilesError(err);
  } finally {
    if (seq === loadSeq) {
      loading.value = false;
    }
  }
}

watch(
  () => props.show,
  (open) => {
    if (open) {
      void load();
    } else {
      closeAll();
    }
  },
);
watch(
  () => props.entry,
  () => {
    if (props.show) {
      void load();
    }
  },
);
onBeforeUnmount(() => {
  cancelLoad();
  releaseUrl();
});

/** close emits the v-model close. */
function close(): void {
  emit("update:show", false);
}

/**
 * onImgError reports an undecodable body (corrupt or unsupported bytes): the
 * spinner is already gone and the stage would otherwise sit blank. The broken
 * URL is released; retry reloads it.
 */
function onImgError(): void {
  releaseUrl();
  error.value = String(t("files.viewer.loadError"));
}

/**
 * onWheel zooms around the cursor by one notch per wheel tick. The point is
 * measured from the stage center, matching the composable's coordinate
 * contract, so the pixel under the cursor stays under it.
 */
function onWheel(event: WheelEvent): void {
  if (objectUrl.value === null) {
    return;
  }
  const rect = stageEl.value?.getBoundingClientRect();
  if (!rect) {
    return;
  }
  zoomAt(
    event.clientX - (rect.left + rect.width / 2),
    event.clientY - (rect.top + rect.height / 2),
    event.deltaY < 0 ? WHEEL_FACTOR : 1 / WHEEL_FACTOR,
  );
}

/**
 * onPointerDown starts a pan drag. Panning needs a zoomed view: at scale 1
 * the image is already fitted, so the press is left alone (text selection
 * included).
 */
function onPointerDown(event: PointerEvent): void {
  if (event.button !== 0 || view.value.scale <= 1 || objectUrl.value === null) {
    return;
  }
  dragAnchor = { x: event.clientX, y: event.clientY };
  stageEl.value?.setPointerCapture(event.pointerId);
}

/** onPointerMove pans the view by the drag delta. */
function onPointerMove(event: PointerEvent): void {
  if (dragAnchor === null) {
    return;
  }
  pan(event.clientX - dragAnchor.x, event.clientY - dragAnchor.y);
  dragAnchor = { x: event.clientX, y: event.clientY };
}

/** onPointerUp ends the pan drag. */
function onPointerUp(): void {
  dragAnchor = null;
}
</script>

<template>
  <NModal
    class="app-modal"
    :show="props.show"
    preset="card"
    style="width: min(1280px, 94vw)"
    :title="props.entry?.name ?? ''"
    :mask-closable="true"
    :close-on-esc="true"
    @update:show="(value: boolean) => emit('update:show', value)"
    @after-leave="onAfterLeave"
  >
    <div
      ref="stageEl"
      class="viewer-stage"
      :class="{ panning: view.scale > 1 }"
      @wheel.prevent="onWheel"
      @pointerdown="onPointerDown"
      @pointermove="onPointerMove"
      @pointerup="onPointerUp"
      @pointercancel="onPointerUp"
      @dblclick="reset()"
    >
      <NSpin v-if="loading" :description="t('files.viewer.loading')" />
      <div v-else-if="error !== null" class="viewer-error">
        <p class="muted">{{ error }}</p>
        <NButton size="small" @click="() => void load()">
          {{ t("files.viewer.retry") }}
        </NButton>
      </div>
      <img
        v-else-if="objectUrl !== null"
        :src="objectUrl"
        :alt="props.entry?.name ?? ''"
        class="viewer-image"
        :style="{ transform: transform }"
        draggable="false"
        @dragstart.prevent
        @error="onImgError"
      />
    </div>

    <template #footer>
      <div class="dialog-foot">
        <span class="hint small muted">{{ t("files.viewer.zoomHint") }}</span>
        <span class="zoom-controls" role="group" :aria-label="t('files.viewer.controls')">
          <NButton
            size="small"
            circle
            :disabled="objectUrl === null"
            :aria-label="t('files.viewer.zoomOut')"
            @click="zoomAt(0, 0, 1 / WHEEL_FACTOR)"
          >
            <template #icon>
              <ZoomOut :size="14" :stroke-width="1.8" aria-hidden="true" />
            </template>
          </NButton>
          <span class="zoom-label small" role="status" aria-live="polite">
            {{ zoomPercent(view.scale) }}
          </span>
          <NButton
            size="small"
            circle
            :disabled="objectUrl === null"
            :aria-label="t('files.viewer.zoomIn')"
            @click="zoomAt(0, 0, WHEEL_FACTOR)"
          >
            <template #icon>
              <ZoomIn :size="14" :stroke-width="1.8" aria-hidden="true" />
            </template>
          </NButton>
          <NButton
            size="small"
            :disabled="objectUrl === null"
            :aria-label="t('files.viewer.reset')"
            @click="reset()"
          >
            <template #icon>
              <Maximize :size="14" :stroke-width="1.8" aria-hidden="true" />
            </template>
            {{ t("files.viewer.reset") }}
          </NButton>
        </span>
        <NButton size="small" @click="close()">
          {{ t("common.actions.close") }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.viewer-stage {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 440px;
  max-height: 72vh;
  overflow: hidden;
  background: var(--surface-warm);
  border-radius: var(--radius-md);
  touch-action: none;
  user-select: none;
}

.viewer-stage.panning {
  cursor: grab;
}

.viewer-stage.panning:active {
  cursor: grabbing;
}

.viewer-image {
  max-width: 100%;
  max-height: 72vh;
  transform-origin: center center;
}

.viewer-error {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-6);
  text-align: center;
}

.viewer-error p {
  margin: 0;
}

.dialog-foot {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.dialog-foot .hint {
  flex: 1 1 auto;
  margin: 0;
}

.zoom-controls {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.zoom-label {
  min-width: 3.5em;
  text-align: center;
  font-variant-numeric: tabular-nums;
}
</style>
