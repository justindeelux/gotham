import { beforeEach, describe, expect, it } from "vitest";
import { z } from "zod";

import {
  fieldErrors,
  firstIssueMessage,
  ruleFrom,
} from "@/shared/validation/naiveAdapter";
import {
  i18n,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";

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

  it("documents the honest Required exception in both locales", () => {
    const absent = z.string({ required_error: "Required" });
    expect(fieldErrors(absent, undefined)).toEqual([
      "This field is required",
    ]);
    setLocale("vi", null);
    expect(fieldErrors(absent, undefined)).toEqual([
      "Trường này là bắt buộc",
    ]);
  });

  it("interpolates params into namespaced keys", () => {
    i18n.global.mergeLocaleMessage("en", {
      testprobe: { length: "At least {min} chars" },
    });
    i18n.global.mergeLocaleMessage("vi", {
      testprobe: { length: "Ít nhất {min} ký tự" },
    });
    const schema = z.string().min(10, "testprobe.length");
    expect(fieldErrors(schema, "short", { min: 10 })).toEqual([
      "At least 10 chars",
    ]);
    setLocale("vi", null);
    expect(fieldErrors(schema, "short", { min: 10 })).toEqual([
      "Ít nhất 10 ký tự",
    ]);
    const rule = ruleFrom(schema, { params: { min: 10 } });
    expect(rule.validator?.({}, "short")).toEqual(
      new Error("Ít nhất 10 ký tự"),
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
