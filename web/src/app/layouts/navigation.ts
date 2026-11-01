import type { IconName } from "@/shared/ui/GothamIcon.vue";

export interface NavItem {
  key: string;
  label: string;
  labelKey: string;
  icon: IconName;
  to?: string;
  ownerOnly?: boolean;
  children?: NavItem[];
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
      { key: "servers", label: "Servers", labelKey: "nav.items.servers", icon: "server", to: "servers", ownerOnly: true },
      { key: "terminal", label: "Terminal", labelKey: "terminal.page.navLabel", icon: "terminal", to: "terminal", ownerOnly: true },
      { key: "files", label: "File manager", labelKey: "nav.items.files", icon: "folder", to: "files", ownerOnly: true },
      { key: "toolchain", label: "App store", labelKey: "nav.items.toolchain", icon: "box", to: "toolchain" },
      { key: "domains", label: "Domains & SSL", labelKey: "nav.items.domains", icon: "globe", to: "domains", ownerOnly: true },
    ],
  },
  {
    label: "Manage",
    labelKey: "nav.sections.manage",
    items: [
      { key: "users", label: "Users", labelKey: "nav.items.users", icon: "users", to: "users", ownerOnly: true },
      {
        key: "notifications",
        label: "Notifications",
        labelKey: "nav.items.notifications",
        icon: "bell",
        to: "notifications",
      },
      {
        key: "security",
        label: "Security",
        labelKey: "nav.items.security",
        icon: "shield",
        children: [
          {
            key: "private-keys",
            label: "Private keys",
            labelKey: "nav.items.privateKeys",
            icon: "shield",
            to: "private-keys",
            ownerOnly: true,
          },
          {
            key: "api-tokens",
            label: "API tokens",
            labelKey: "nav.items.apiTokens",
            icon: "key",
            to: "api-tokens",
          },
        ],
      },
      {
        key: "integrations",
        label: "Integrations",
        labelKey: "nav.items.integrations",
        icon: "refresh",
        children: [
          {
            key: "git-sources",
            label: "Git sources",
            labelKey: "nav.items.gitSources",
            icon: "refresh",
            to: "git-sources",
          },
          { key: "mcp", label: "MCP", labelKey: "nav.items.mcp", icon: "shield", to: "mcp" },
          {
            key: "storages",
            label: "Storages",
            labelKey: "nav.items.storages",
            icon: "db",
            to: "storages",
          },
        ],
      },
    ],
  },
  {
    label: "System",
    labelKey: "nav.sections.system",
    items: [
      {
        key: "settings",
        label: "Settings",
        labelKey: "nav.items.settings",
        icon: "gear",
        children: [
          {
            key: "instance",
            label: "General",
            labelKey: "nav.items.general",
            icon: "gear",
            to: "instance",
            ownerOnly: true,
          },
          {
            key: "monitoring",
            label: "Monitoring",
            labelKey: "nav.items.monitoring",
            icon: "history",
            to: "monitoring",
            ownerOnly: true,
          },
        ],
      },
      { key: "backups", label: "Backups", labelKey: "nav.items.backups", icon: "history", to: "backups" },
      { key: "cleanup", label: "Cleanup", labelKey: "nav.items.cleanup", icon: "trash", to: "cleanup", ownerOnly: true },
      { key: "updates", label: "Updates", labelKey: "nav.items.updates", icon: "gear", to: "updates", ownerOnly: true },
    ],
  },
];

/** Old paths kept working by the router; activeNavKey also reads them. */
const routeKeys: Record<string, string> = {
  notifications: "notifications",
  updates: "updates",
  "security/private-keys": "private-keys",
  "security/api-tokens": "api-tokens",
  "integrations/git-sources": "git-sources",
  "integrations/mcp": "mcp",
  "integrations/storages": "storages",
  "settings/general": "instance",
  "settings/monitoring": "monitoring",
  "settings/profile": "profile",
  "settings/notifications": "notifications",
  "settings/git-sources": "git-sources",
  "settings/instance": "instance",
  "settings/mcp": "mcp",
  "settings/updates": "updates",
  "settings/api-tokens": "api-tokens",
};

export const sectionAliases: Record<string, string> = { settings: "notifications" };

export function visibleNavItems(items: NavItem[], isOwner: boolean): NavItem[] {
  return items
    .map((item) =>
      item.children ? { ...item, children: visibleNavItems(item.children, isOwner) } : item,
    )
    .filter((item) => !item.ownerOnly || isOwner)
    .filter((item) => !item.children || item.children.length > 0);
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
  if (segment === "servers" && parts[2] === "files") {
    return "files";
  }
  const pair = section ? `${segment}/${section}` : "";
  if (segment === "settings" || segment === "security" || segment === "integrations") {
    return routeKeys[pair] ?? section ?? sectionAliases[segment] ?? segment;
  }
  return routeKeys[segment] ?? sectionAliases[segment] ?? segment;
}
