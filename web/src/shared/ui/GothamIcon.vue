<script setup lang="ts">
import { computed } from "vue";
import type { Component } from "vue";
import {
  Bell,
  Box,
  ChevronDown,
  Database,
  Folder,
  Globe,
  History,
  Languages,
  Layers,
  LayoutGrid,
  Play,
  Plus,
  RefreshCw,
  Rocket,
  Search,
  Server,
  Settings,
  ShieldCheck,
  Square,
  Terminal as TerminalIcon,
  Trash2,
  Users,
} from "@lucide/vue";

// Thin adapter over @lucide/vue: call sites keep using the Gotham names with
// no changes. Per-icon imports keep tree-shaking intact — never import the
// whole library here.
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
  | "languages"
  | "users"
  | "bell"
  | "search"
  | "gear"
  | "folder"
  | "plus"
  | "trash"
  | "chevron-down"
  | "history"
  | "play"
  | "stop"
  | "terminal";

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
  languages: Languages,
  users: Users,
  bell: Bell,
  search: Search,
  gear: Settings,
  folder: Folder,
  plus: Plus,
  trash: Trash2,
  "chevron-down": ChevronDown,
  history: History,
  play: Play,
  stop: Square,
  terminal: TerminalIcon,
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
