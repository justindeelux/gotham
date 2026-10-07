// Differential tests for the V7 databases zod migration (JUS-23).
// Each row pins the outcome + message the hand-written guard produced
// (captured pre-migration in zz-scratch-old.test.ts): the new schemas must
// produce the identical outcome + message for every row. Messages are
// namespaced i18n keys resolved at invocation time, so the suite registers
// the real feature catalogs and asserts the resolved English display text.
import { beforeEach, describe, expect, it } from "vitest";

import { fieldErrors } from "@/shared/validation/naiveAdapter";
import {
  registerDiscoveredCatalogs,
  resetLocaleState,
  resolveValidationMessage,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";
import {
  cronSchema,
  databaseMessages,
  databaseNameSchema,
  isCronPresent,
  isDatabaseNameValid,
  isWizardConfigureValid,
  validateTargetForm,
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

describe("validateTargetForm keeps guard order and messages", () => {
  const base = {
    name: "t",
    kind: "s3" as const,
    endpoint: "https://s3.example.com",
    bucket: "b",
    accessKey: "a",
    secretKey: "s",
    isNew: true,
  };
  // Keys keep guard order; the resolved English display text stays pinned
  // to the recorded guard strings below.
  const rows: Array<[string, typeof base, string | null]> = [
    ["ok", base, null],
    ["empty name", { ...base, name: "  " }, databaseMessages.targetNameRequired],
    ["blank endpoint", { ...base, endpoint: " " }, databaseMessages.s3LocationRequired],
    ["blank bucket", { ...base, bucket: "" }, databaseMessages.s3LocationRequired],
    ["new missing secret", { ...base, secretKey: "" }, databaseMessages.s3KeysRequired],
    ["new missing access", { ...base, accessKey: "" }, databaseMessages.s3KeysRequired],
    ["new blank-space keys still pass (no trim)", { ...base, accessKey: " ", secretKey: " " }, null],
    ["edit keeps stored keys", { ...base, accessKey: "", secretKey: "", isNew: false }, null],
    ["local needs nothing", { ...base, kind: "local" as const, endpoint: "", bucket: "", accessKey: "", secretKey: "" }, null],
    ["name wins over location", { ...base, name: "", endpoint: "" }, databaseMessages.targetNameRequired],
  ];
  it("returns the recorded first message per row", () => {
    for (const [label, draft, message] of rows) {
      expect(validateTargetForm(draft), label).toBe(message);
    }
  });
  it("resolves the keys to the recorded English display text", () => {
    const resolved = new Map([
      [databaseMessages.targetNameRequired, "Target name is required."],
      [
        databaseMessages.s3LocationRequired,
        "Endpoint and bucket are required for an S3 target.",
      ],
      [
        databaseMessages.s3KeysRequired,
        "Access key and secret key are required for a new S3 target.",
      ],
    ]);
    for (const [, draft, message] of rows) {
      if (message === null) {
        continue;
      }
      expect(validateTargetForm(draft)).toBe(message);
      expect(resolveValidationMessage(message)).toBe(resolved.get(message));
    }
    setLocale("vi", null);
    expect(
      resolveValidationMessage(databaseMessages.targetNameRequired),
    ).toBe("Tên đích lưu trữ là bắt buộc.");
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
