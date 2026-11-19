import {
  Box,
  Cloud,
  Code,
  Database,
  Globe,
  Layers,
  Package,
  Rocket,
  Server,
  Star,
  Terminal,
  Zap,
} from "@lucide/vue";
import type { Component } from "vue";

export interface ProjectIconPreset {
  key: string;
  component: Component;
}

// Mirrors ProjectIconPresets in internal/projects/icons.go.
export const projectIconPresets: ProjectIconPreset[] = [
  { key: "rocket", component: Rocket },
  { key: "box", component: Box },
  { key: "globe", component: Globe },
  { key: "database", component: Database },
  { key: "server", component: Server },
  { key: "terminal", component: Terminal },
  { key: "cloud", component: Cloud },
  { key: "layers", component: Layers },
  { key: "code", component: Code },
  { key: "package", component: Package },
  { key: "zap", component: Zap },
  { key: "star", component: Star },
];

export function projectIconPreset(icon: string): Component | null {
  if (!icon.startsWith("preset:")) {
    return null;
  }
  const key = icon.slice("preset:".length);
  return projectIconPresets.find((preset) => preset.key === key)?.component ?? null;
}
