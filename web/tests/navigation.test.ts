// Shell navigation data: section routing and key uniqueness.
import { describe, expect, it } from "vitest";

import { activeNavKey, navSections } from "../src/app/layouts/navigation";

describe("activeNavKey", () => {
  it("follows the first path segment", () => {
    expect(activeNavKey("/servers/abc")).toBe("servers");
    expect(activeNavKey("/applications/1")).toBe("applications");
    expect(activeNavKey("/dashboard")).toBe("dashboard");
    expect(activeNavKey("/")).toBe("dashboard");
  });

  it("maps aliased sections", () => {
    expect(activeNavKey("/settings/tokens")).toBe("notifications");
  });
});

describe("navSections", () => {
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
