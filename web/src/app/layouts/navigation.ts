import type { IconName } from "@/shared/ui/GothamIcon.vue";

export interface NavItem {
  key: string;
  label: string;
  labelKey: string;
  icon: IconName;
  to: string;
  ownerOnly?: boolean;
}

export interface NavSection {
  label: string;
  labelKey: string;
  items: NavItem[];
}

export const navSections: NavSection[] = [
  {
    label: "Operations",
    labelKey: "nav.sections.operations",
    items: [
      { key: "dashboard", label: "Dashboard", labelKey: "nav.items.dashboard", icon: "grid", to: "dashboard" },
      { key: "projects", label: "Projects", labelKey: "nav.items.projects", icon: "layers", to: "projects" },
      { key: "files", label: "File manager", labelKey: "nav.items.files", icon: "folder", to: "files", ownerOnly: true },
      { key: "terminal", label: "Terminal", labelKey: "terminal.page.navLabel", icon: "terminal", to: "terminal", ownerOnly: true },
      { key: "toolchain", label: "App Store", labelKey: "nav.items.toolchain", icon: "box", to: "toolchain" },
      { key: "servers", label: "Servers", labelKey: "nav.items.servers", icon: "server", to: "servers", ownerOnly: true },
      { key: "domains", label: "Domains & SSL", labelKey: "nav.items.domains", icon: "globe", to: "domains", ownerOnly: true },
      { key: "backups", label: "Backups", labelKey: "nav.items.backups", icon: "history", to: "backups" },
    ],
  },
  {
    label: "Access",
    labelKey: "nav.sections.access",
    items: [
      { key: "users", label: "Users", labelKey: "nav.items.users", icon: "users", to: "users", ownerOnly: true },
      {
        key: "notifications",
        label: "Notification channels",
        labelKey: "nav.items.notifications",
        icon: "bell",
        to: "notifications",
      },
      {
        key: "git-sources",
        label: "Git sources",
        labelKey: "nav.items.gitSources",
        icon: "refresh",
        to: "git-sources",
      },
      { key: "mcp", label: "MCP setup", labelKey: "nav.items.mcp", icon: "shield", to: "mcp" },
    ],
  },
  {
    label: "System",
    labelKey: "nav.sections.system",
    items: [
      { key: "instance", label: "Settings", labelKey: "nav.items.settings", icon: "gear", to: "instance", ownerOnly: true },
      { key: "monitoring", label: "Monitoring", labelKey: "nav.items.monitoring", icon: "history", to: "monitoring", ownerOnly: true },
      { key: "updates", label: "Updates", labelKey: "nav.items.updates", icon: "gear", to: "updates", ownerOnly: true },
    ],
  },
];

export const sectionAliases: Record<string, string> = { settings: "notifications" };

export function visibleNavItems(items: NavItem[], isOwner: boolean): NavItem[] {
  return items.filter((item) => !item.ownerOnly || isOwner);
}

export function visibleNavSections(
  sections: NavSection[],
  isOwner: boolean,
): NavSection[] {
  return sections
    .map((section) => ({
      ...section,
      items: visibleNavItems(section.items, isOwner),
    }))
    .filter((section) => section.items.length > 0);
}

export function activeNavKey(path: string): string {
  const parts = path.split("/").filter(Boolean);
  const [segment = "dashboard", section] = parts;
  if (segment === "settings") {
    return section ?? sectionAliases[segment] ?? segment;
  }
  if (segment === "servers" && parts[2] === "files") {
    return "files";
  }
  return sectionAliases[segment] ?? segment;
}
