import { beforeEach, describe, expect, it } from "vitest";
import { z } from "zod";

import {
  fieldErrors,
  firstIssueMessage,
  ruleFrom,
} from "@/shared/validation/naiveAdapter";
import { resetLocaleState, setLocale, syncComposerLocale } from "@/shared/i18n";

beforeEach(() => {
  resetLocaleState();
  syncComposerLocale("en");
});

/** keyedSchema stores a namespaced message key, resolved at call time. */
const keyedSchema = z.string().min(1, "common.actions.save");

/** legacySchema stores plain English, which must pass through untouched. */
const legacySchema = z.string().min(1, "Email is required");

describe("validation message resolution", () => {
  it("leaves legacy English strings byte-identical in both locales", () => {
    expect(fieldErrors(legacySchema, "")).toEqual(["Email is required"]);
    setLocale("vi", null);
    expect(fieldErrors(legacySchema, "")).toEqual(["Email is required"]);
    const rule = ruleFrom(legacySchema);
    expect(rule.validator?.({}, "")).toEqual(new Error("Email is required"));
  });

  it("resolves a namespaced key at invocation time as the locale changes", () => {
    expect(fieldErrors(keyedSchema, "")).toEqual(["Save"]);
    setLocale("vi", null);
    expect(fieldErrors(keyedSchema, "")).toEqual(["Lưu"]);
    setLocale("en", null);
    expect(fieldErrors(keyedSchema, "")).toEqual(["Save"]);
  });

  it("resolves ruleFrom feedback without clearing form state", () => {
    const rule = ruleFrom(keyedSchema);
    expect(rule.validator?.({}, "")).toEqual(new Error("Save"));
    setLocale("vi", null);
    expect(rule.validator?.({}, "")).toEqual(new Error("Lưu"));
    expect(rule.validator?.({}, "draft kept")).toBe(true);
  });

  it("maps known zod defaults to curated fallbacks", () => {
    expect(firstIssueMessage(new z.ZodError([]))).toBe("Invalid value");
    setLocale("vi", null);
    expect(firstIssueMessage(new z.ZodError([]))).toBe(
      "Giá trị không hợp lệ",
    );
  });

  it("passes provider diagnostics with dots through unchanged", () => {
    const diagnostic = z.string().min(1, "servers: validation failed: bad input");
    setLocale("vi", null);
    expect(fieldErrors(diagnostic, "")).toEqual([
      "servers: validation failed: bad input",
    ]);
  });
});
