import type { IconName } from "@/shared/ui/GothamIcon.vue";

export interface NavItem {
  key: string;
  label: string;
  labelKey: string;
  icon: IconName;
  to: string;
  adminOnly?: boolean;
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
      { key: "files", label: "File manager", labelKey: "nav.items.files", icon: "folder", to: "files", adminOnly: true },
      { key: "terminal", label: "Terminal", labelKey: "terminal.page.navLabel", icon: "terminal", to: "terminal", adminOnly: true },
      { key: "toolchain", label: "App Store", labelKey: "nav.items.toolchain", icon: "box", to: "toolchain" },
      { key: "servers", label: "Servers", labelKey: "nav.items.servers", icon: "server", to: "servers" },
      { key: "domains", label: "Domains & SSL", labelKey: "nav.items.domains", icon: "globe", to: "domains", adminOnly: true },
    ],
  },
  {
    label: "Team",
    labelKey: "nav.sections.team",
    items: [
      { key: "teams", label: "Members & roles", labelKey: "nav.items.teams", icon: "users", to: "teams" },
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
    ],
  },
  {
    label: "System",
    labelKey: "nav.sections.system",
    items: [
      { key: "instance", label: "Instance", labelKey: "nav.items.instance", icon: "gear", to: "instance", adminOnly: true },
      { key: "updates", label: "Updates & settings", labelKey: "nav.items.updates", icon: "gear", to: "updates", adminOnly: true },
    ],
  },
];

export const sectionAliases: Record<string, string> = { settings: "notifications" };

export function visibleNavItems(items: NavItem[], isPlatformAdmin: boolean): NavItem[] {
  return items.filter((item) => !item.adminOnly || isPlatformAdmin);
}

export function visibleNavSections(
  sections: NavSection[],
  isPlatformAdmin: boolean,
): NavSection[] {
  return sections
    .map((section) => ({
      ...section,
      items: visibleNavItems(section.items, isPlatformAdmin),
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
