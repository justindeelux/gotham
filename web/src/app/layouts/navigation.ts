import type { IconName } from "@/shared/ui/GothamIcon.vue";

/** Sidebar entry: live route when `to` is set, inert stub otherwise. */
export interface NavItem {
  key: string;
  label: string;
  /** labelKey renders the item label in the active locale (stable key). */
  labelKey: string;
  icon: IconName;
  to?: string;
}

export interface NavSection {
  label: string;
  /** labelKey renders the section label in the active locale. */
  labelKey: string;
  items: NavItem[];
}

// Group order and English labels follow the docs/design shell renderer
// (docs/design/assets/gotham-ui.js SECTIONS). Entries with no route are inert
// and say so — every backend phase has shipped, so no phase number is claimed.
// `label` keeps the English text for source checks; `labelKey` is what the
// sidebar renders, so a language switch updates labels without remounting.
export const navSections: NavSection[] = [
  {
    label: "Operations",
    labelKey: "nav.sections.operations",
    items: [
      { key: "dashboard", label: "Dashboard", labelKey: "nav.items.dashboard", icon: "grid", to: "dashboard" },
      { key: "projects", label: "Projects", labelKey: "nav.items.projects", icon: "layers", to: "projects" },
      { key: "files", label: "File manager", labelKey: "nav.items.files", icon: "folder" },
      { key: "templates", label: "Template library", labelKey: "nav.items.templates", icon: "rocket", to: "templates" },
      { key: "servers", label: "Servers", labelKey: "nav.items.servers", icon: "server", to: "servers" },
      { key: "domains", label: "Domains & SSL", labelKey: "nav.items.domains", icon: "globe", to: "domains" },
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
      { key: "tokens", label: "API tokens", labelKey: "nav.items.tokens", icon: "key" },
    ],
  },
  {
    label: "System",
    labelKey: "nav.sections.system",
    items: [
      { key: "updates", label: "Updates & settings", labelKey: "nav.items.updates", icon: "gear" },
    ],
  },
];

// Section aliases for paths whose first segment is not the sidebar key.
export const sectionAliases: Record<string, string> = { settings: "notifications" };

/**
 * activeNavKey is the sidebar entry for a route path. It follows the first
 * path segment, so detail routes (/servers/:id, /projects/:projectId and
 * the nested /projects/:projectId/environments/:environmentId/... pages)
 * keep their section highlighted (B2-12, B3-5). Projects replaced the
 * Applications, Services and Databases entries; the flat routes were removed
 * in PE-5 and redirect to Projects.
 * Settings pages resolve via the second segment: /settings/notifications
 * highlights Notification channels, while /settings/profile matches no
 * sidebar entry (profile is reached from the MeCard menu) so nothing
 * highlights instead of the wrong item. Bare /settings redirects to
 * notifications, so it keeps that highlight.
 */
export function activeNavKey(path: string): string {
  const [segment = "dashboard", section] = path.split("/").filter(Boolean);
  if (segment === "settings") {
    return section ?? sectionAliases[segment] ?? segment;
  }
  return sectionAliases[segment] ?? segment;
}
