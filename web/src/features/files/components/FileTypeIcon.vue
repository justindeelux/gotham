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
    <component :is="icon" :size="20" :stroke-width="1.6" />
  </span>
</template>

<style scoped>
/* File-type marks (JUS-206): the glyph sits directly in the row — no dark
 * tile. The box is the reference 25px (--fm-mark-size, owned by
 * FilesBrowser); folders are an outline mark in the warn tone. Symlinks keep
 * the link icon until the agent reports the target kind (backend follow-up):
 * the listing cannot tell a folder-link from a file-link, so every symlink
 * draws as a link. */
.fm-mark {
  position: relative;
  width: var(--fm-mark-size);
  height: var(--fm-mark-size);
  flex: 0 0 auto;
  display: grid;
  place-items: center;
  color: var(--fg-2);
}

.fm-mark[data-kind="folder"] {
  color: var(--warn);
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
