// Shell navigation data: section routing and key uniqueness.
import { describe, expect, it } from "vitest";

import { activeNavKey, navSections } from "../src/app/layouts/navigation";

describe("activeNavKey", () => {
  it("follows the first path segment", () => {
    expect(activeNavKey("/servers/abc")).toBe("servers");
    expect(activeNavKey("/projects/abc")).toBe("projects");
    // PE-5: nested resource pages highlight Projects through its segment.
    expect(activeNavKey("/projects/abc/environments/def")).toBe("projects");
    expect(activeNavKey("/projects/abc/environments/def/applications/1")).toBe("projects");
    expect(activeNavKey("/dashboard")).toBe("dashboard");
    expect(activeNavKey("/")).toBe("dashboard");
  });

  it("maps aliased sections", () => {
    expect(activeNavKey("/settings")).toBe("notifications");
    expect(activeNavKey("/settings/notifications")).toBe("notifications");
  });

  it("highlights nothing on profile: no sidebar entry exists", () => {
    // Profile is reached from the MeCard menu; the key matches no nav item,
    // so AppSidebar renders no is-active entry instead of the wrong one.
    expect(activeNavKey("/settings/profile")).toBe("profile");
    const keys = navSections.flatMap((section) =>
      section.items.map((item) => item.key),
    );
    expect(keys).not.toContain("profile");
  });
});

describe("navSections", () => {
  it("lists Projects instead of the flat resource entries", () => {
    // PE-4 (JUS-33): Projects replaces Applications, Services and Databases.
    // PE-5 removed the flat routes; the sidebar never linked them.
    const keys = navSections.flatMap((section) =>
      section.items.map((item) => item.key),
    );
    expect(keys).toContain("projects");
    expect(keys).not.toContain("applications");
    expect(keys).not.toContain("services");
    expect(keys).not.toContain("databases");
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
});
