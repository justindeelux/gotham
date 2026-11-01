import { beforeEach, describe, expect, it } from "vitest";

import { fieldErrors } from "@/shared/validation/naiveAdapter";
import {
  registerDiscoveredCatalogs,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";
import { cronSchema, isCronPresent } from "@/features/backups/schemas/backups";
import {
  databaseNameSchema,
  isDatabaseNameValid,
  isWizardConfigureValid,
} from "@/features/databases/schemas/databases";

registerDiscoveredCatalogs();

beforeEach(() => {
  resetLocaleState();
  syncComposerLocale("en");
});

const NAME = "Name must be 1-63 characters of letters, digits, ., _ or -.";

describe("databaseNameSchema matches isValidDatabaseName", () => {
  const valid = [
    "pg-orders",
    "  pg-orders  ",
    "a",
    "A1.-_",
    "UPPER",
    "db-1.prod_x",
    "x".repeat(63),
  ];
  const invalid: Array<[unknown, string]> = [
    ["", NAME],
    ["   ", NAME],
    ["no spaces", NAME],
    ["-lead", NAME],
    [".lead", NAME],
    ["_lead", NAME],
    ["db/name", NAME],
    ["ünicode", NAME],
    ["x".repeat(64), NAME],
    [undefined, NAME],
    [null, NAME],
  ];
  it("accepts every name the old pattern accepted", () => {
    for (const name of valid) {
      expect(isDatabaseNameValid(name)).toBe(true);
      expect(fieldErrors(databaseNameSchema, name)).toEqual([]);
    }
  });
  it("rejects the rest with the exact rename toast", () => {
    for (const [name, message] of invalid) {
      expect(isDatabaseNameValid(name)).toBe(false);
      expect(fieldErrors(databaseNameSchema, name)).toEqual([message]);
    }
  });
});

describe("cronSchema keeps non-empty-only, no grammar check", () => {
  it("requires a value with the exact toast", () => {
    for (const value of ["", "   ", undefined, null]) {
      expect(isCronPresent(value)).toBe(false);
      expect(fieldErrors(cronSchema, value)).toEqual([
        "Cron expression is required, e.g. 0 2 * * *.",
      ]);
    }
  });
  it("passes anything non-blank, even nonsense the server will refuse", () => {
    for (const value of ["0 2 * * *", "  0 2 * * *  ", "not a cron"]) {
      expect(isCronPresent(value)).toBe(true);
    }
  });
  it("resolves the same key in Vietnamese without changing the predicate", () => {
    setLocale("vi", null);
    expect(isCronPresent("")).toBe(false);
    expect(fieldErrors(cronSchema, "")).toEqual([
      "Biểu thức cron là bắt buộc, ví dụ 0 2 * * *.",
    ]);
    expect(isCronPresent("0 2 * * *")).toBe(true);
  });
});

describe("isWizardConfigureValid matches configureValid", () => {
  const rows: Array<[string, { name: string; exposePublic: boolean; publicPort: number | null }, boolean]> = [
    ["bad name blocks", { name: "no spaces", exposePublic: false, publicPort: null }, false],
    ["empty name blocks", { name: "", exposePublic: false, publicPort: null }, false],
    ["unexposed needs no port", { name: "db", exposePublic: false, publicPort: null }, true],
    ["null port blocks", { name: "db", exposePublic: true, publicPort: null }, false],
    ["NaN blocks", { name: "db", exposePublic: true, publicPort: Number.NaN }, false],
    ["zero blocks", { name: "db", exposePublic: true, publicPort: 0 }, false],
    ["over max blocks", { name: "db", exposePublic: true, publicPort: 65536 }, false],
    ["fraction blocks", { name: "db", exposePublic: true, publicPort: 1.5 }, false],
    ["min passes", { name: "db", exposePublic: true, publicPort: 1 }, true],
    ["max passes", { name: "db", exposePublic: true, publicPort: 65535 }, true],
    ["mid passes", { name: "db", exposePublic: true, publicPort: 80 }, true],
  ];
  it("gates exactly like the old computed", () => {
    for (const [label, draft, valid] of rows) {
      expect(isWizardConfigureValid(draft), label).toBe(valid);
    }
  });
});
