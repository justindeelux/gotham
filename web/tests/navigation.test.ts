import { describe, expect, it } from "vitest";

import { activeNavKey, navSections, visibleNavSections } from "../src/app/layouts/navigation";
import type { NavSection } from "../src/app/layouts/navigation";

describe("activeNavKey", () => {
  it("follows the first path segment", () => {
    expect(activeNavKey("/servers/abc")).toBe("servers");
    expect(activeNavKey("/projects/abc")).toBe("projects");
    expect(activeNavKey("/projects/abc/environments/def")).toBe("projects");
    expect(activeNavKey("/projects/abc/environments/def/applications/1")).toBe("projects");
    expect(activeNavKey("/dashboard")).toBe("dashboard");
    expect(activeNavKey("/")).toBe("dashboard");
  });

  it("maps aliased sections", () => {
    expect(activeNavKey("/settings")).toBe("notifications");
    expect(activeNavKey("/settings/notifications")).toBe("notifications");
  });

  it("highlights File manager on both file routes (JUS-110)", () => {
    expect(activeNavKey("/files")).toBe("files");
    expect(activeNavKey("/servers/abc/files")).toBe("files");
    expect(activeNavKey("/servers/abc")).toBe("servers");
    expect(activeNavKey("/servers/abc/containers")).toBe("servers");
    const files = navSections
      .flatMap((section) => section.items)
      .find((item) => item.key === "files");
    expect(files?.to).toBe("files");
  });

  it("highlights nothing on profile: no sidebar entry exists", () => {
    expect(activeNavKey("/settings/profile")).toBe("profile");
    const keys = navSections.flatMap((section) =>
      section.items.map((item) => item.key),
    );
    expect(keys).not.toContain("profile");
  });
});

describe("navSections", () => {
  it("lists Projects instead of the flat resource entries", () => {
    const keys = navSections.flatMap((section) =>
      section.items.map((item) => item.key),
    );
    expect(keys).toContain("projects");
    expect(keys).not.toContain("applications");
    expect(keys).not.toContain("services");
    expect(keys).not.toContain("databases");
    expect(keys).not.toContain("templates");
    const projects = navSections
      .flatMap((section) => section.items)
      .find((item) => item.key === "projects");
    expect(projects?.to).toBe("projects");
  });

  it("keeps unique keys with labels and icons", () => {
    const keys = navSections.flatMap((section) =>
      section.items.map((item) => item.key),
    );
    expect(keys.length).toBeGreaterThan(0);
    expect(new Set(keys).size).toBe(keys.length);
    for (const section of navSections) {
      expect(section.label).not.toBe("");
      for (const item of section.items) {
        expect(item.label).not.toBe("");
        expect(item.icon).not.toBe("");
      }
    }
  });

  it("keeps API tokens in the account menu, not the sidebar", () => {
    const items = navSections.flatMap((section) => section.items);
    expect(items.some((item) => item.key === "tokens")).toBe(false);
    for (const item of items) {
      expect(item.to).not.toBe("");
    }
  });

  it("marks exactly the owner-only entries", () => {
    const items = navSections.flatMap((section) => section.items);
    const flagged = new Set(
      items.filter((item) => item.ownerOnly).map((item) => item.key),
    );
    expect(flagged).toEqual(
      new Set(["files", "terminal", "servers", "domains", "users", "instance", "monitoring", "cleanup", "updates"]),
    );
  });

  it("puts Terminal directly below File manager (JUS-246)", () => {
    const keys = navSections.flatMap((section) => section.items.map((item) => item.key));
    expect(keys.indexOf("terminal")).toBe(keys.indexOf("files") + 1);
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
    expect(real.some((section) => section.labelKey === "nav.sections.system")).toBe(false);
    expect(visibleNavSections(navSections, true)).toHaveLength(navSections.length);
  });
});
