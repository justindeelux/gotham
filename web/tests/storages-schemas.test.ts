import { beforeEach, describe, expect, it } from "vitest";

import {
  firstStorageFormIssue,
  storageMessages,
} from "@/features/storages/schemas/storages";
import {
  registerDiscoveredCatalogs,
  resetLocaleState,
  resolveValidationMessage,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";

registerDiscoveredCatalogs();

beforeEach(() => {
  resetLocaleState();
  syncComposerLocale("en");
});

describe("storage form schema keeps guard order and messages", () => {
  const base = {
    name: "t",
    kind: "s3" as const,
    endpoint: "https://s3.example.com",
    bucket: "b",
    accessKey: "a",
    secretKey: "s",
    isNew: true,
  };
  const rows: Array<[string, typeof base, string | null]> = [
    ["ok", base, null],
    ["empty name", { ...base, name: "  " }, storageMessages.nameRequired],
    ["blank endpoint", { ...base, endpoint: " " }, storageMessages.s3LocationRequired],
    ["blank bucket", { ...base, bucket: "" }, storageMessages.s3LocationRequired],
    ["new missing secret", { ...base, secretKey: "" }, storageMessages.s3KeysRequired],
    ["new missing access", { ...base, accessKey: "" }, storageMessages.s3KeysRequired],
    ["new blank-space keys still pass (no trim)", { ...base, accessKey: " ", secretKey: " " }, null],
    ["edit keeps stored keys", { ...base, accessKey: "", secretKey: "", isNew: false }, null],
    ["local needs nothing", { ...base, kind: "local" as const, endpoint: "", bucket: "", accessKey: "", secretKey: "" }, null],
    ["name wins over location", { ...base, name: "", endpoint: "" }, storageMessages.nameRequired],
  ];
  it("returns the recorded first message per row", () => {
    for (const [label, draft, message] of rows) {
      expect(firstStorageFormIssue(draft), label).toBe(message);
    }
  });
  it("resolves the keys to the recorded English display text", () => {
    const resolved = new Map([
      [storageMessages.nameRequired, "Enter a storage name."],
      [
        storageMessages.s3LocationRequired,
        "An S3-compatible storage needs an endpoint and a bucket.",
      ],
      [
        storageMessages.s3KeysRequired,
        "Enter both the access key and the secret key.",
      ],
    ]);
    for (const [, draft, message] of rows) {
      if (message === null) {
        continue;
      }
      expect(firstStorageFormIssue(draft)).toBe(message);
      expect(resolveValidationMessage(message)).toBe(resolved.get(message));
    }
    setLocale("vi", null);
    expect(resolveValidationMessage(storageMessages.nameRequired)).toBe(
      "Hãy nhập tên nơi lưu trữ.",
    );
  });
});
