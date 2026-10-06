import { describe, expect, it } from "vitest";

import {
  checkCatalogParity,
  hasBalancedSyntax,
  messageParams,
  registerNamespace,
} from "@/shared/i18n/catalog";
import { loadFeatureCatalogs } from "@/shared/i18n";
import { registerDiscoveredCatalogs } from "@/shared/i18n";

/** Catalog fixture proving en/vi key and parameter parity. */
const enFixture = {
  actions: {
    confirmDelete: "Delete {name}?",
    items: "No items | One item | {count} items",
    literal: "Use {{braces}} and @ and 'quotes' and | pipes",
  },
};

const viFixture = {
  actions: {
    confirmDelete: "Xóa {name}?",
    items: "Không có mục nào | Một mục | {count} mục",
    literal: "Dùng {{braces}} và @ và 'nháy' và | gạch",
  },
};

describe("checkCatalogParity", () => {
  it("accepts the en/vi fixture with matching keys and parameters", () => {
    expect(checkCatalogParity(enFixture, viFixture)).toEqual([]);
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
        (issue) => issue.problem === "missing-in-en" && issue.key === "extra.key",
      ),
    ).toBe(true);
  });

  it("reports unbalanced braces on either side", () => {
    expect(hasBalancedSyntax("Delete {name}?")).toBe(true);
    expect(hasBalancedSyntax("Delete {name?")).toBe(false);
    expect(hasBalancedSyntax("Delete name}?")).toBe(false);
    const issues = checkCatalogParity(
      { a: "broken {key" },
      { a: "hỏng {key" },
    );
    expect(issues.some((issue) => issue.problem === "bad-syntax")).toBe(true);
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
    const tree: Record<string, unknown> = { common: {} };
    expect(() => registerNamespace(tree, "common", {})).toThrowError(
      /duplicate i18n namespace/,
    );
    registerNamespace(tree, "servers", { title: "Servers" });
    expect(tree.servers).toEqual({ title: "Servers" });
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

  it("never shadows a shared root", () => {
    const tree: Record<string, unknown> = { common: { actions: {} } };
    loadFeatureCatalogs(tree, {
      "@/features/common/locales/en.ts": { default: { actions: {} } },
    });
    expect(tree.common).toEqual({ actions: {} });
  });

  it("registers the (currently empty) discovered set without throwing", () => {
    expect(() => registerDiscoveredCatalogs()).not.toThrow();
  });
});
