import { describe, expect, it } from "vitest";

import {
  checkCatalogParity,
  compileError,
  messageParams,
  pluralSegments,
  registerNamespace,
} from "@/shared/i18n/catalog";
import {
  i18n,
  loadFeatureCatalogs,
  registerDiscoveredCatalogs,
} from "@/shared/i18n";
import en from "@/shared/i18n/locales/en";
import vi from "@/shared/i18n/locales/vi";

/**
 * Catalog fixture proving en/vi key, parameter, plural-form and syntax
 * parity. Literals use the documented vue-i18n escapes, never raw
 * `{{…}}`, bare `@` or bare `|` (all three fail compileError below).
 */
const enFixture = {
  actions: {
    confirmDelete: "Delete {name}?",
    items: "No items | One item | {count} items",
    literal: "Use {'{'}braces{'}'} and {'@'} and 'quotes' and {'|'} pipes",
  },
};

const viFixture = {
  actions: {
    confirmDelete: "Xóa {name}?",
    items: "Không có mục nào | Một mục | {count} mục",
    literal: "Dùng {'{'}ngoặc{'}'} và {'@'} và 'nháy' và {'|'} gạch",
  },
};

describe("checkCatalogParity", () => {
  it("accepts the en/vi fixture with matching keys, params and plurals", () => {
    expect(checkCatalogParity(enFixture, viFixture)).toEqual([]);
  });

  it("passes the real common catalogs", () => {
    expect(checkCatalogParity(en, vi)).toEqual([]);
  });

  it("rejects nested braces and bare @ links the compiler rejects", () => {
    expect(compileError("Use {{braces}}")).toContain("nest placeholder");
    expect(compileError("mail me @ home")).toContain("linked");
    expect(compileError("Use {'{'}braces{'}'} and {'@'}")).toBeNull();
    const issues = checkCatalogParity(
      { a: "broken {{key}}", b: "mail @ home" },
      { a: "hỏng {{key}}", b: "thư @ nhà" },
    );
    expect(
      issues.filter((issue) => issue.problem === "bad-syntax"),
    ).toHaveLength(4);
  });

  it("reports plural-segment mismatches a brace check cannot see", () => {
    expect(pluralSegments("a {'|'} b | c")).toHaveLength(2);
    const issues = checkCatalogParity({ a: "A | B | C" }, { a: "X | Y" });
    expect(issues).toEqual([
      {
        key: "a",
        problem: "plural-mismatch",
        detail: "en 3 vs vi 2 segments",
      },
    ]);
  });

  it("reports a key missing from the Vietnamese catalog", () => {
    const issues = checkCatalogParity(enFixture, {
      actions: { confirmDelete: viFixture.actions.confirmDelete },
    });
    expect(issues.some((issue) => issue.problem === "missing-in-vi")).toBe(
      true,
    );
  });

  it("reports parameter mismatches", () => {
    const issues = checkCatalogParity(enFixture, {
      ...viFixture,
      actions: { ...viFixture.actions, confirmDelete: "Xóa?" },
    });
    expect(
      issues.some(
        (issue) =>
          issue.problem === "param-mismatch" &&
          issue.key === "actions.confirmDelete",
      ),
    ).toBe(true);
  });

  it("reports empty translations and extra keys", () => {
    const issues = checkCatalogParity(enFixture, {
      ...viFixture,
      actions: { ...viFixture.actions, confirmDelete: "" },
      extra: { key: "thừa" },
    });
    expect(issues.some((issue) => issue.problem === "empty")).toBe(true);
    expect(
      issues.some(
        (issue) =>
          issue.problem === "missing-in-en" && issue.key === "extra.key",
      ),
    ).toBe(true);
  });

  it("extracts named parameters", () => {
    expect(messageParams("Delete {name} ({count} items)")).toEqual([
      "count",
      "name",
    ]);
    expect(messageParams("no params")).toEqual([]);
  });
});

describe("registerNamespace", () => {
  it("rejects a duplicate namespace", () => {
    const tree: Record<string, unknown> = { servers: {} };
    expect(() => registerNamespace(tree, "servers", {})).toThrowError(
      /duplicate i18n namespace: servers/,
    );
    registerNamespace(tree, "auth", { title: "Sign in" });
    expect(tree.auth).toEqual({ title: "Sign in" });
  });
});

describe("loadFeatureCatalogs", () => {
  it("registers each file under its module namespace", () => {
    const tree: Record<string, unknown> = {};
    loadFeatureCatalogs(tree, {
      "@/features/servers/locales/en.ts": { default: { title: "Servers" } },
      "@/features/auth/locales/en.ts": { default: { title: "Sign in" } },
    });
    expect(tree).toEqual({
      servers: { title: "Servers" },
      auth: { title: "Sign in" },
    });
  });

  it("throws on a duplicate namespace instead of silently winning", () => {
    const tree: Record<string, unknown> = {};
    loadFeatureCatalogs(tree, {
      "@/features/servers/locales/en.ts": { default: { title: "One" } },
    });
    expect(() =>
      loadFeatureCatalogs(tree, {
        "@/features/servers/locales/vi.ts": { default: { title: "Two" } },
      }),
    ).toThrowError(/duplicate i18n namespace: servers/);
  });

  it("refuses to shadow a reserved shared root", () => {
    expect(() =>
      loadFeatureCatalogs(
        {},
        { "@/features/common/locales/en.ts": { default: { actions: {} } } },
        ["common", "validation", "language", "time"],
      ),
    ).toThrowError(/reserved i18n namespace: common/);
  });
});

describe("registerDiscoveredCatalogs", () => {
  it("registers the temporarily copied feature fixture (real glob path)", () => {
    registerDiscoveredCatalogs();
    expect(i18n.global.te("__i18n_probe__.hello")).toBe(true);
    i18n.global.locale.value = "vi";
    try {
      expect(String(i18n.global.t("__i18n_probe__.hello", { name: "An" }))).toBe(
        "Xin chào An",
      );
    } finally {
      i18n.global.locale.value = "en";
    }
  });
});
