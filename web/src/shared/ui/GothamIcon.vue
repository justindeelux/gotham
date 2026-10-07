<script setup lang="ts">
import { computed } from "vue";
import type { Component } from "vue";
import {
  Bell,
  Box,
  ChevronDown,
  Database,
  FileText,
  Folder,
  Globe,
  KeyRound,
  Layers,
  LayoutGrid,
  Plus,
  RefreshCw,
  Rocket,
  Search,
  Server,
  Settings,
  ShieldCheck,
  Trash2,
  Users,
} from "@lucide/vue";

// Thin adapter over @lucide/vue: call sites keep using the 19 Gotham
// names with no changes. Per-icon imports keep tree-shaking intact — never
// import the whole library here.
export type IconName =
  | "server"
  | "shield"
  | "refresh"
  | "rocket"
  | "grid"
  | "box"
  | "layers"
  | "db"
  | "globe"
  | "users"
  | "bell"
  | "search"
  | "doc"
  | "gear"
  | "folder"
  | "key"
  | "plus"
  | "trash"
  | "chevron-down";

interface Props {
  name: IconName;
}

const props = defineProps<Props>();

const components: Record<IconName, Component> = {
  server: Server,
  shield: ShieldCheck,
  refresh: RefreshCw,
  rocket: Rocket,
  grid: LayoutGrid,
  box: Box,
  layers: Layers,
  db: Database,
  globe: Globe,
  users: Users,
  bell: Bell,
  search: Search,
  doc: FileText,
  gear: Settings,
  folder: Folder,
  key: KeyRound,
  plus: Plus,
  trash: Trash2,
  "chevron-down": ChevronDown,
};

const component = computed<Component>(() => components[props.name]);

// @lucide/vue v1 renders stroke-width verbatim (no 24px-grid scaling),
// so :stroke-width="1.6" below matches the old hand-drawn paths exactly.
// Do NOT add absolute-stroke-width: in v1 that flag enables the scaling.
</script>

<template>
  <component :is="component" class="gotham-icon" :size="18" :stroke-width="1.6" />
</template>

<style scoped>
.gotham-icon {
  width: 18px;
  height: 18px;
  flex: 0 0 auto;
}
</style>
