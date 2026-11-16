import { describe, expect, it } from "vitest";

import { checkCatalogParity } from "@/shared/i18n/catalog";

import en from "@/features/firewall/locales/en";
import vi from "@/features/firewall/locales/vi";

describe("firewall catalogs", () => {
  it("keeps en and vi in parity", () => {
    expect(checkCatalogParity(en, vi)).toEqual([]);
  });
});
