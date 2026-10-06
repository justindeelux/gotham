// Differential contract for the auth zod migration (JUS-23 V1/V2).
//
// Expected outcomes were recorded from the pre-migration Naive UI rules run
// through the real async-validator engine (throwaway script, 2026-10-04)
// and encoded here, so any schema change that alters acceptance or messages
// fails loudly. Preserved quirks (not accidents to fix silently): zod's
// .email() is NOT used because it rejects bracket-IP, quoted-local-part and
// unicode-domain addresses the old rule accepts; whitespace-only passwords
// pass required, exactly as before.

import { beforeEach, describe, expect, it } from "vitest";
import { z } from "zod";

import {
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";
import {
  confirmPasswordSchema,
  emailSchema,
  loginPasswordSchema,
  loginRules,
  registerPasswordSchema,
  registerRules,
  termsSchema,
} from "@/features/auth/schemas/auth";
import { ruleFrom } from "@/shared/validation/naiveAdapter";
import type { RuleFromOptions } from "@/shared/validation/naiveAdapter";

// Schema messages are namespaced keys resolved at validation time; the
// English catalog values stay byte-identical to the recorded outcomes, so
// this contract proves the original behavior in the default locale.
beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
});

interface Outcome {
  ok: boolean;
  message: string | null;
}

function check(
  schema: z.ZodType<unknown, z.ZodTypeDef, unknown>,
  value: unknown,
  opts?: RuleFromOptions,
): Outcome {
  const result = ruleFrom(schema, opts).validator?.({}, value);
  if (result === true) {
    return { ok: true, message: null };
  }
  return { ok: false, message: (result as Error).message };
}

function checkRows(
  rows: Array<[string, unknown, boolean, string | null]>,
  run: (_value: unknown) => Outcome,
): void {
  for (const [label, value, ok, message] of rows) {
    expect({ label, ...run(value) }, label).toEqual({ label, ok, message });
  }
}

const longValidEmail = `${"a".repeat(314)}@b.com`;
const longInvalidEmail = `${"a".repeat(315)}@b.com`;

describe("email field (login + register share one schema)", () => {
  it("matches the old rule outcomes and messages", () => {
    checkRows(
      [
        ["empty", "", false, "Email is required"],
        ["whitespace", "   ", false, "Enter a valid email address"],
        ["simple", "a@example.com", true, null],
        ["uppercase", "USER@EXAMPLE.COM", true, null],
        ["short tld", "a@b.co", true, null],
        ["missing tld", "a@b", false, "Enter a valid email address"],
        ["plain", "not-an-email", false, "Enter a valid email address"],
        ["space in local", "a b@c.com", false, "Enter a valid email address"],
        ["bracket ip", "a@[203.0.113.8]", true, null],
        ["quoted local", '"a b"@example.com', true, null],
        ["unicode local", "üser@example.com", true, null],
        ["missing domain", "a@example", false, "Enter a valid email address"],
        ["empty label", "a@.com", false, "Enter a valid email address"],
        ["missing local", "@example.com", false, "Enter a valid email address"],
        ["double dot", "a..b@example.com", false, "Enter a valid email address"],
        ["padded", " a@example.com ", false, "Enter a valid email address"],
        ["320 chars", longValidEmail, true, null],
        ["321 chars", longInvalidEmail, false, "Enter a valid email address"],
        ["null", null, false, "Email is required"],
        ["undefined", undefined, false, "Email is required"],
      ],
      (value) => check(emailSchema, value),
    );
  });
});

describe("login password field", () => {
  it("is required-only, whitespace passes as before", () => {
    checkRows(
      [
        ["empty", "", false, "Password is required"],
        ["whitespace", "   ", true, null],
        ["value", "x", true, null],
        ["null", null, false, "Password is required"],
        ["undefined", undefined, false, "Password is required"],
      ],
      (value) => check(loginPasswordSchema, value),
    );
  });
});

describe("register password field", () => {
  it("requires 10+ characters with 2+ classes", () => {
    const policy =
      "Use at least 10 characters with 2 character classes (lowercase, uppercase, digits, symbols)";
    checkRows(
      [
        ["empty", "", false, "Password is required"],
        ["short", "short", false, policy],
        ["one class", "abcdefghij", false, policy],
        ["two classes", "Abcdefghij", true, null],
        ["too short", "Abcdefg1", false, policy],
        ["four classes", "aB1!xxxxxx", true, null],
        ["digits only", "1234567890", false, policy],
        ["symbols only", "!!!!!!!!!!", false, policy],
        ["unicode letters", "αβγδεζηθικλμ", false, policy],
        ["long", `${"a".repeat(100)}A`, true, null],
      ],
      (value) => check(registerPasswordSchema, value),
    );
  });
});

describe("confirm password field", () => {
  it("matches the old required-then-equality outcomes", () => {
    const rows: Array<[string, string, string, boolean, string | null]> = [
      ["match", "secret123AB", "secret123AB", true, null],
      ["empty", "secret123AB", "", false, "Please confirm your password"],
      ["mismatch", "secret123AB", "other", false, "Passwords do not match"],
      ["both empty", "", "", false, "Please confirm your password"],
      ["both blank", "   ", "   ", true, null],
    ];
    for (const [label, password, value, ok, message] of rows) {
      const outcome = check(confirmPasswordSchema(() => password), value);
      expect({ label, ...outcome }, label).toEqual({ label, ok, message });
    }
  });
});

describe("terms checkbox", () => {
  it("must be true, trigger stays change", () => {
    const message = "You must accept the terms to create an account";
    checkRows(
      [
        ["checked", true, true, null],
        ["unchecked", false, false, message],
        ["undefined", undefined, false, message],
        ["null", null, false, message],
      ],
      (value) => check(termsSchema, value),
    );
  });
});

describe("login and register rule builders", () => {
  it("cover every field the pages render", () => {
    expect(Object.keys(loginRules()).sort()).toEqual(["email", "password"]);
    expect(Object.keys(registerRules(() => "")).sort()).toEqual([
      "confirmPassword",
      "email",
      "password",
      "terms",
    ]);
  });
});
