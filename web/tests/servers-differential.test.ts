
import { beforeAll, beforeEach, describe, expect, it } from "vitest";
import { z } from "zod";

import {
  requiredField,
  serverHostSchema,
  serverMessages,
  serverNameSchema,
  serverPortSchema,
  serverUserSchema,
} from "@/features/servers/schemas/servers";
import {
  registerDiscoveredCatalogs,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";
import { ruleFrom } from "@/shared/validation/naiveAdapter";
import type { RuleFromOptions } from "@/shared/validation/naiveAdapter";

beforeAll(() => {
  registerDiscoveredCatalogs();
});

beforeEach(() => {
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

describe("server name field", () => {
  it("matches the old rule outcomes and messages", () => {
    checkRows(
      [
        ["empty", "", false, "Enter a node name."],
        ["whitespace", "   ", true, null],
        ["padded", "  build-node-03  ", true, null],
        ["tabbed", "\tnode3\t", true, null],
        ["nbsp padded", "\u00a0node3\u00a0", true, null],
        ["typical", "build-node-03", true, null],
        ["dots", "node_1.prod", true, null],
        ["single", "A", true, null],
        ["digit", "0", true, null],
        ["trailing dash", "trail-", true, null],
        ["63 chars", "a".repeat(63), true, null],
        ["64 chars", "a".repeat(64), false, "Letters, digits, dots, dashes, and underscores only."],
        ["leading dash", "-lead", false, "Letters, digits, dots, dashes, and underscores only."],
        ["space", "has space", false, "Letters, digits, dots, dashes, and underscores only."],
        ["mixed", "UPPER_1-2.x", true, null],
        ["unicode", "nœud", false, "Letters, digits, dots, dashes, and underscores only."],
        ["dot only", ".", false, "Letters, digits, dots, dashes, and underscores only."],
      ],
      (value) => check(serverNameSchema, value),
    );
  });
});

describe("server host field", () => {
  it("matches the old rule outcomes and messages", () => {
    checkRows(
      [
        ["empty", "", false, "Enter an IP address or hostname."],
        ["whitespace", "   ", false, "Enter a valid IPv4 address or hostname."],
        ["ipv4", "203.0.113.90", true, null],
        ["padded ipv4", " 10.0.0.1 ", true, null],
        ["hostname", "node3.internal", true, null],
        ["single label", "a", true, null],
        ["bad-range ipv4", "999.1.1.1", true, null],
        ["bad-range ipv4 b", "256.1.1.1", true, null],
        ["space", "bad host!", false, "Enter a valid IPv4 address or hostname."],
        ["underscore", "bad_host.com", false, "Enter a valid IPv4 address or hostname."],
        ["leading dash", "-bad.com", false, "Enter a valid IPv4 address or hostname."],
        ["empty label", "a..b", false, "Enter a valid IPv4 address or hostname."],
        ["long label", `${"a".repeat(64)}.com`, false, "Enter a valid IPv4 address or hostname."],
        ["punycode", "xn--nxasmq6b.example", true, null],
        ["uppercase", "UPPER.COM", true, null],
      ],
      (value) => check(serverHostSchema, value),
    );
  });
});

describe("server port field", () => {
  it("splits missing/non-numeric from out-of-range exactly as before", () => {
    checkRows(
      [
        ["null", null, false, "Enter an SSH port (1-65535)."],
        ["undefined", undefined, false, "Enter an SSH port (1-65535)."],
        ["nan", Number.NaN, false, "Enter an SSH port (1-65535)."],
        ["string", "22", false, "Enter an SSH port (1-65535)."],
        ["zero", 0, false, "Port must be a number from 1 to 65535."],
        ["negative", -1, false, "Port must be a number from 1 to 65535."],
        ["min", 1, true, null],
        ["usual", 22, true, null],
        ["float int", 22.0, true, null],
        ["fraction", 1.5, false, "Port must be a number from 1 to 65535."],
        ["max", 65535, true, null],
        ["over max", 65536, false, "Port must be a number from 1 to 65535."],
      ],
      (value) => check(serverPortSchema, value),
    );
  });
});

describe("ssh user field", () => {
  it("matches the old rule outcomes and messages", () => {
    checkRows(
      [
        ["empty", "", false, "Enter the SSH user."],
        ["whitespace", "   ", true, null],
        ["padded", "  root  ", true, null],
        ["tabbed", "\troot\t", true, null],
        ["nbsp padded", "\u00a0root\u00a0", true, null],
        ["root", "root", true, null],
        ["underscored", "deploy_2", true, null],
        ["trailing dollar", "depl-oy$", true, null],
        ["uppercase", "Root", false, "Enter a valid Unix username (lowercase, digits, _, -)."],
        ["leading digit", "0root", false, "Enter a valid Unix username (lowercase, digits, _, -)."],
        ["leading underscore", "_ok", true, null],
        ["space", "has space", false, "Enter a valid Unix username (lowercase, digits, _, -)."],
      ],
      (value) => check(serverUserSchema, value),
    );
  });
});

describe("conditional required fields", () => {
  it("matches the old messages per mode", () => {
    checkRows(
      [
        ["key name empty", "", false, "Enter a key name."],
        ["key name blank", "   ", true, null],
        ["key name set", "deploy-key", true, null],
      ],
      (value) => check(requiredField(serverMessages.keyNameRequired), value),
    );
    expect(check(requiredField(serverMessages.privateKeyRequired), "").message).toBe(
      "Paste the PEM-encoded private key.",
    );
    expect(check(requiredField(serverMessages.wizardKeyIdRequired), "").message).toBe(
      "Enter an existing key ID.",
    );
    expect(check(requiredField(serverMessages.editKeyIdRequired), "").message).toBe(
      "Enter a key ID.",
    );
    expect(
      check(requiredField(serverMessages.nodePasswordRequired), "").message,
    ).toBe("Enter the node password.");
  });

  it("passes without touching the schema while its mode is off", () => {
    const off = { when: () => false };
    expect(
      check(requiredField(serverMessages.keyNameRequired), "not-a-key", off),
    ).toEqual({ ok: true, message: null });
  });
});

describe("vietnamese feedback", () => {
  it("keeps acceptance while rendering translated messages", () => {
    setLocale("vi", null);
    try {
      expect(check(serverNameSchema, "").message).toBe("Nhập tên nút.");
      expect(check(serverNameSchema, "-lead").message).toBe(
        "Chỉ dùng chữ cái, chữ số, chấm, gạch ngang và gạch dưới.",
      );
      expect(check(serverNameSchema, "build-node-03").ok).toBe(true);
      expect(check(serverHostSchema, "").message).toBe("Nhập địa chỉ IP hoặc tên máy.");
      expect(check(serverPortSchema, null).message).toBe("Nhập cổng SSH (1-65535).");
      expect(check(serverPortSchema, 0).message).toBe(
        "Cổng phải là số từ 1 đến 65535.",
      );
      expect(check(serverUserSchema, "").message).toBe("Nhập người dùng SSH.");
      expect(
        check(requiredField(serverMessages.nodePasswordRequired), "").message,
      ).toBe("Nhập mật khẩu nút.");
    } finally {
      setLocale("en", null);
    }
  });
});
