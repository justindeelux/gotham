<script setup lang="ts">
import { Archive, File as FileIcon, FileText, Folder, Image as ImageIcon, Link as LinkIcon, Lock, Terminal } from "@lucide/vue";
import { computed } from "vue";
import type { Component } from "vue";

import type { FilesIconKind } from "@/features/files/utils/files";

interface Props {
  kind: FilesIconKind;
}

const props = defineProps<Props>();

// Per-icon imports keep tree-shaking intact (mirrors shared/ui/GothamIcon.vue).
const icons: Record<FilesIconKind, Component> = {
  folder: Folder,
  file: FileIcon,
  doc: FileText,
  image: ImageIcon,
  archive: Archive,
  terminal: Terminal,
  link: LinkIcon,
  lock: Lock,
};

const icon = computed<Component>(() => icons[props.kind]);
</script>

<template>
  <span class="fm-mark" :data-kind="kind" aria-hidden="true">
    <component :is="icon" :size="15" :stroke-width="1.6" />
  </span>
</template>

<style scoped>
/* File-type marks (.fm-mark family). */
.fm-mark {
  position: relative;
  width: calc(var(--space-6) + var(--space-1));
  height: calc(var(--space-6) + var(--space-1));
  flex: 0 0 auto;
  display: grid;
  place-items: center;
  border-radius: var(--radius-md);
  background: var(--surface-warm);
  border: 1px solid var(--border);
  color: var(--fg-2);
}

.fm-mark[data-kind="folder"] {
  color: var(--warn-ink);
  background: var(--warn-soft);
  border-color: transparent;
}

.fm-mark[data-kind="image"] {
  color: var(--success-ink);
}

.fm-mark[data-kind="archive"] {
  color: var(--danger-ink);
}

.fm-mark[data-kind="terminal"] {
  color: var(--warn-ink);
}
</style>
