import { describe, expect, it } from "vitest";

import { activeNavKey, navSections, visibleNavSections } from "../src/app/layouts/navigation";
import type { NavItem, NavSection } from "../src/app/layouts/navigation";

function flatten(items: NavItem[]): NavItem[] {
  return items.flatMap((item) => [item, ...(item.children ? flatten(item.children) : [])]);
}

const allItems = (): NavItem[] =>
  navSections.flatMap((section) => flatten(section.items));

describe("activeNavKey", () => {
  it("follows the first path segment", () => {
    expect(activeNavKey("/servers/abc")).toBe("servers");
    expect(activeNavKey("/projects/abc")).toBe("projects");
    expect(activeNavKey("/projects/abc/environments/def")).toBe("projects");
    expect(activeNavKey("/projects/abc/environments/def/applications/1")).toBe("projects");
    expect(activeNavKey("/dashboard")).toBe("dashboard");
    expect(activeNavKey("/")).toBe("dashboard");
  });

  it("maps the grouped and aliased sections", () => {
    expect(activeNavKey("/settings")).toBe("notifications");
    expect(activeNavKey("/notifications")).toBe("notifications");
    expect(activeNavKey("/updates")).toBe("updates");
    expect(activeNavKey("/settings/general")).toBe("instance");
    expect(activeNavKey("/settings/monitoring")).toBe("monitoring");
    expect(activeNavKey("/security/private-keys")).toBe("private-keys");
    expect(activeNavKey("/security/api-tokens")).toBe("api-tokens");
    expect(activeNavKey("/integrations/git-sources")).toBe("git-sources");
    expect(activeNavKey("/integrations/mcp")).toBe("mcp");
    expect(activeNavKey("/integrations/storages")).toBe("storages");
  });

  it("still maps the old URLs that redirect", () => {
    expect(activeNavKey("/settings/notifications")).toBe("notifications");
    expect(activeNavKey("/settings/git-sources")).toBe("git-sources");
    expect(activeNavKey("/settings/instance")).toBe("instance");
    expect(activeNavKey("/settings/mcp")).toBe("mcp");
    expect(activeNavKey("/settings/updates")).toBe("updates");
    expect(activeNavKey("/settings/api-tokens")).toBe("api-tokens");
  });

  it("highlights File manager on both file routes (JUS-110)", () => {
    expect(activeNavKey("/files")).toBe("files");
    expect(activeNavKey("/servers/abc/files")).toBe("files");
    expect(activeNavKey("/servers/abc")).toBe("servers");
    expect(activeNavKey("/servers/abc/containers")).toBe("servers");
    const files = allItems().find((item) => item.key === "files");
    expect(files?.to).toBe("files");
  });

  it("highlights nothing on profile: no sidebar entry exists", () => {
    expect(activeNavKey("/settings/profile")).toBe("profile");
    expect(allItems().map((item) => item.key)).not.toContain("profile");
  });
});

describe("navSections", () => {
  it("orders sections and items exactly as the grouped sidebar", () => {
    expect(navSections.map((section) => section.label)).toEqual([
      "Operations",
      "Manage",
      "System",
    ]);
    const manage = navSections[1]!;
    expect(manage.items.map((item) => item.key)).toEqual([
      "users",
      "notifications",
      "security",
      "integrations",
    ]);
    expect(
      manage.items.find((item) => item.key === "security")?.children?.map((item) => item.key),
    ).toEqual(["private-keys", "api-tokens"]);
    expect(
      manage.items.find((item) => item.key === "integrations")?.children?.map((item) => item.key),
    ).toEqual(["git-sources", "mcp", "storages"]);
    const system = navSections[2]!;
    expect(
      system.items.find((item) => item.key === "settings")?.children?.map((item) => item.key),
    ).toEqual(["instance", "monitoring"]);
    expect(system.items.map((item) => item.key)).toEqual([
      "settings",
      "backups",
      "cleanup",
      "updates",
    ]);
  });

  it("lists Projects instead of the flat resource entries", () => {
    const keys = allItems().map((item) => item.key);
    expect(keys).toContain("projects");
    expect(keys).not.toContain("applications");
    expect(keys).not.toContain("services");
    expect(keys).not.toContain("databases");
    expect(keys).not.toContain("templates");
    const projects = allItems().find((item) => item.key === "projects");
    expect(projects?.to).toBe("projects");
  });

  it("keeps unique keys with labels and icons", () => {
    const keys = allItems().map((item) => item.key);
    expect(keys.length).toBeGreaterThan(0);
    expect(new Set(keys).size).toBe(keys.length);
    for (const section of navSections) {
      expect(section.label).not.toBe("");
      for (const item of flatten(section.items)) {
        expect(item.label).not.toBe("");
        expect(item.icon).not.toBe("");
        if (!item.children) {
          expect(item.to).toBeTruthy();
        }
      }
    }
  });

  it("puts private keys and API tokens in the Security group", () => {
    const security = navSections[1]!.items.find((item) => item.key === "security");
    expect(security?.children?.map((item) => item.key)).toEqual([
      "private-keys",
      "api-tokens",
    ]);
    expect(
      security?.children?.find((item) => item.key === "private-keys")?.ownerOnly,
    ).toBe(true);
    expect(
      security?.children?.find((item) => item.key === "api-tokens")?.ownerOnly,
    ).toBeUndefined();
  });

  it("marks exactly the owner-only entries", () => {
    const flagged = new Set(
      allItems().filter((item) => item.ownerOnly).map((item) => item.key),
    );
    expect(flagged).toEqual(
      new Set([
        "files",
        "terminal",
        "servers",
        "domains",
        "users",
        "private-keys",
        "instance",
        "monitoring",
        "cleanup",
        "updates",
      ]),
    );
  });

  it("puts Terminal directly above File manager (JUS-246)", () => {
    const keys = navSections[0]!.items.map((item) => item.key);
    expect(keys.indexOf("terminal")).toBe(keys.indexOf("files") - 1);
  });

  it("drops groups whose items are all hidden from members", () => {
    const mixed: NavSection[] = [
      {
        label: "Open",
        labelKey: "nav.sections.open",
        items: [
          { key: "a", label: "A", labelKey: "nav.items.a", icon: "grid", to: "a" },
          { key: "b", label: "B", labelKey: "nav.items.b", icon: "grid", to: "b", ownerOnly: true },
        ],
      },
      {
        label: "Locked",
        labelKey: "nav.sections.locked",
        items: [
          { key: "c", label: "C", labelKey: "nav.items.c", icon: "gear", to: "c", ownerOnly: true },
        ],
      },
    ];
    const hidden = visibleNavSections(mixed, false);
    expect(hidden.map((section) => section.label)).toEqual(["Open"]);
    expect(hidden[0]!.items.map((item) => item.key)).toEqual(["a"]);
    const shown = visibleNavSections(mixed, true);
    expect(shown.map((section) => section.label)).toEqual(["Open", "Locked"]);
    expect(shown[1]!.items.map((item) => item.key)).toEqual(["c"]);
    const real = visibleNavSections(navSections, false);
    const system = real.find(
      (section) => section.labelKey === "nav.sections.system",
    );
    expect(system?.items.map((item) => item.key)).toEqual(["backups"]);
    expect(visibleNavSections(navSections, true)).toHaveLength(navSections.length);
  });

  it("keeps submenu groups whose visible children survive for members", () => {
    const manage = visibleNavSections(navSections, false)[1]!;
    const security = manage.items.find((item) => item.key === "security");
    expect(security?.children?.map((item) => item.key)).toEqual(["api-tokens"]);
    const integrations = manage.items.find((item) => item.key === "integrations");
    expect(integrations?.children?.map((item) => item.key)).toEqual([
      "git-sources",
      "mcp",
      "storages",
    ]);
  });
});
