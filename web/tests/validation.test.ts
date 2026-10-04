import { describe, expect, it, vi } from "vitest";
import { z } from "zod";

/* global console: readonly */

import {
  fieldErrors,
  firstIssueMessage,
  ruleFrom,
  rulesFor,
} from "@/shared/validation/naiveAdapter";
import { parseWith } from "@/shared/validation/parse";
import {
  intInRange,
  nonEmptyString,
  portSchema,
  requiredString,
} from "@/shared/validation/primitives";

const emailSchema = z.string().min(1, "Email is required").email("Enter a valid email address");

function runValidator(
  schema: z.ZodType<unknown>,
  value: unknown,
  opts?: { when?: () => boolean },
): boolean | Error {
  const rule = ruleFrom(schema, opts);
  return rule.validator?.({}, value) as boolean | Error;
}

describe("ruleFrom", () => {
  it("returns true on success", () => {
    expect(runValidator(emailSchema, "a@example.com")).toBe(true);
  });

  it("returns Error(first issue message) on failure", () => {
    expect(runValidator(emailSchema, "")).toEqual(
      new Error("Email is required"),
    );
    expect(runValidator(emailSchema, "not-an-email")).toEqual(
      new Error("Enter a valid email address"),
    );
  });

  it("skips validation when the when predicate is false", () => {
    expect(
      runValidator(emailSchema, "not-an-email", { when: () => false }),
    ).toBe(true);
  });

  it("validates when the when predicate is true", () => {
    expect(
      runValidator(emailSchema, "not-an-email", { when: () => true }),
    ).toEqual(new Error("Enter a valid email address"));
  });
});

describe("rulesFor", () => {
  it("builds one rule per shape key", () => {
    const rules = rulesFor({ email: emailSchema, port: portSchema });
    expect(Object.keys(rules).sort()).toEqual(["email", "port"]);
    expect(rules.email.validator?.({}, "a@example.com")).toBe(true);
    expect(rules.port.validator?.({}, 0)).toEqual(
      new Error("Port must be between 1 and 65535"),
    );
  });
});

describe("fieldErrors", () => {
  it("returns [] on success and every message on failure", () => {
    expect(fieldErrors(emailSchema, "a@example.com")).toEqual([]);
    expect(fieldErrors(emailSchema, "nope")).toEqual([
      "Enter a valid email address",
    ]);
  });

  it("firstIssueMessage falls back on empty issues", () => {
    expect(firstIssueMessage(new z.ZodError([]))).toBe("Invalid value");
  });
});

describe("parseWith", () => {
  const envelope = z.object({ items: z.array(z.string()) });

  it("returns parsed data on success", () => {
    expect(parseWith(envelope, { items: ["a"] })).toEqual({ items: ["a"] });
  });

  it("returns the raw payload reference on success, extras intact", () => {
    const raw = { items: ["a"], extra: 2 };
    const parsed = parseWith(envelope, raw);
    expect(parsed).toBe(raw);
    expect((parsed as { extra?: number }).extra).toBe(2);
  });

  it("warns and returns the raw payload on failure", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => undefined);
    try {
      const raw = { items: "skew" };
      expect(parseWith(envelope, raw, { context: "TestEnvelope" })).toBe(raw);
      expect(warn).toHaveBeenCalledOnce();
      const [message, issues] = warn.mock.calls[0] as [string, unknown];
      expect(message).toContain("TestEnvelope");
      expect(Array.isArray(issues)).toBe(true);
    } finally {
      warn.mockRestore();
    }
  });

  it("strict throws for tests", () => {
    expect(() =>
      parseWith(envelope, { items: 1 }, { context: "TestEnvelope", strict: true }),
    ).toThrowError(/TestEnvelope/);
  });
});

describe("primitives", () => {
  it("portSchema keeps the 1-65535 range message", () => {
    expect(portSchema.safeParse(1).success).toBe(true);
    expect(portSchema.safeParse(65535).success).toBe(true);
    const result = portSchema.safeParse(0);
    expect(result.success).toBe(false);
    if (!result.success) {
      expect(result.error.issues[0]?.message).toBe(
        "Port must be between 1 and 65535",
      );
    }
  });

  it("nonEmptyString trims and uses the caller message", () => {
    const schema = nonEmptyString("Name is required");
    expect(schema.safeParse("  a  ").success).toBe(true);
    expect(fieldErrors(schema, "   ")).toEqual(["Name is required"]);
  });

  it("requiredString reports the caller message for every absent value", () => {
    const schema = requiredString("Name is required");
    expect(schema.safeParse("ok").success).toBe(true);
    for (const value of [undefined, null, "", "   "]) {
      expect(fieldErrors(schema, value)).toEqual(["Name is required"]);
    }
  });

  it("intInRange reports the caller message for null, NaN and range misses", () => {
    const schema = intInRange("Port must be between 1 and 65535", {
      min: 1,
      max: 65535,
    });
    expect(schema.safeParse(80).success).toBe(true);
    for (const value of [undefined, null, Number.NaN, 0, 65536, 1.5]) {
      expect(fieldErrors(schema, value)).toEqual([
        "Port must be between 1 and 65535",
      ]);
    }
  });
});
