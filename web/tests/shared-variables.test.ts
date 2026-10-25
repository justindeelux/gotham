import { describe, expect, it, vi } from "vitest";
import { afterEach, beforeEach } from "vitest";

import projectsEn from "@/features/projects/locales/en";
import projectsVi from "@/features/projects/locales/vi";
import {
  i18n,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";

vi.mock("@/shared/api/http", () => ({
  http: { get: vi.fn(), post: vi.fn(), patch: vi.fn(), delete: vi.fn(), put: vi.fn() }
}));

import { http } from "@/shared/api/http";
import {
  getEnvironmentVariables,
  getProjectVariables,
  replaceEnvironmentVariables,
  replaceProjectVariables,
} from "@/features/projects/api/variables";
import {
  buildVariablesPayload,
  existingSecretKeysOf,
  findDuplicateVariableKey,
  inheritedOriginLabel,
  isOverriddenBy,
  isShadowedByEnvironment,
  isSharedVariableKeyValid,
  isSharedVariableValueValid,
  parseSharedVariables,
  rowKeyFromServerError,
  secretWithoutValueKey,
  toVariableDrafts,
  validateVariableDrafts,
} from "@/features/projects/schemas/variables";

const projectId = "11111111-1111-4111-8111-111111111111";
const environmentId = "22222222-2222-4222-8222-222222222222";

beforeEach(() => {
  i18n.global.mergeLocaleMessage("en", { projects: projectsEn });
  i18n.global.mergeLocaleMessage("vi", { projects: projectsVi });
  resetLocaleState();
  syncComposerLocale("en");
});

afterEach(() => {
  setLocale("en", null);
});

describe("shared variable key gating matches the contract", () => {
  it("accepts shell identifiers", () => {
    for (const key of ["A", "_x", "LOG_LEVEL", "db_url_2", "x".repeat(128)]) {
      expect(isSharedVariableKeyValid(key)).toBe(true);
    }
  });

  it("rejects empty, overlong and non-identifier keys", () => {
    for (const key of ["", "1ABC", "HAS-DASH", "HAS SPACE", "NO=EQUALS", "x".repeat(129)]) {
      expect(isSharedVariableKeyValid(key)).toBe(false);
    }
  });
});

describe("shared variable value gating", () => {
  it("accepts plain values including empty", () => {
    expect(isSharedVariableValueValid("")).toBe(true);
    expect(isSharedVariableValueValid("production")).toBe(true);
  });

  it("rejects NUL for plain and secret rows alike", () => {
    expect(isSharedVariableValueValid("a\0b")).toBe(false);
  });
});

describe("findDuplicateVariableKey", () => {
  it("names the first repeated key, case-sensitively", () => {
    expect(
      findDuplicateVariableKey([
        { key: "A", value: "1", secret: false },
        { key: "B", value: "2", secret: false },
      ]),
    ).toBeNull();
    expect(
      findDuplicateVariableKey([
        { key: "A", value: "1", secret: false },
        { key: "A", value: "2", secret: false },
      ]),
    ).toBe("A");
    expect(
      findDuplicateVariableKey([
        { key: "Ab", value: "1", secret: false },
        { key: "AB", value: "2", secret: false },
      ]),
    ).toBeNull();
  });
});

describe("validateVariableDrafts", () => {
  it("accepts an empty draft (saving it clears the scope)", () => {
    expect(validateVariableDrafts([], new Set())).toEqual([]);
  });

  it("caps the scope at 128 keys", () => {
    const drafts = Array.from({ length: 129 }, (_, index) => ({
      key: `K${index}`,
      value: "v",
      secret: false,
    }));
    expect(validateVariableDrafts(drafts, new Set())).toHaveLength(1);
    expect(
      validateVariableDrafts(drafts.slice(0, 128), new Set()),
    ).toEqual([]);
  });

  it("flags duplicates, bad keys and NUL values", () => {
    const problems = validateVariableDrafts(
      [
        { key: "A", value: "1", secret: false },
        { key: "A", value: "2", secret: false },
        { key: "HAS-DASH", value: "x", secret: false },
        { key: "", value: "x", secret: false },
        { key: "NULLED", value: "a\0b", secret: false },
      ],
      new Set(),
    );
    expect(problems.length).toBeGreaterThanOrEqual(4);
    expect(problems.join("\n")).toContain('Duplicate key "A"');
  });

  it("keeps an untouched stored secret but blocks a new valueless secret", () => {
    expect(
      validateVariableDrafts(
        [{ key: "SENTRY_DSN", value: "", secret: true }],
        new Set(["SENTRY_DSN"]),
      ),
    ).toEqual([]);
    const problems = validateVariableDrafts(
      [{ key: "FRESH", value: "", secret: true }],
      new Set(["SENTRY_DSN"]),
    );
    expect(problems).toHaveLength(1);
    expect(problems[0]).toContain('Secret "FRESH" needs a value');
  });

  it("blocks a secret-to-plain toggle that would silently clear the secret", () => {
    const problems = validateVariableDrafts(
      [{ key: "SENTRY_DSN", value: "", secret: false }],
      new Set(["SENTRY_DSN"]),
    );
    expect(problems).toHaveLength(1);
    expect(problems[0]).toContain('Saving "SENTRY_DSN" as plain');
    expect(
      validateVariableDrafts(
        [{ key: "SENTRY_DSN", value: "rotated", secret: false }],
        new Set(["SENTRY_DSN"]),
      ),
    ).toEqual([]);
    expect(
      validateVariableDrafts(
        [{ key: "EMPTY_PLAIN", value: "", secret: false }],
        new Set(["SENTRY_DSN"]),
      ),
    ).toEqual([]);
  });
});

describe("buildVariablesPayload", () => {
  it("omits the value only for a kept stored secret", () => {
    const stored = new Set(["SENTRY_DSN"]);
    expect(
      buildVariablesPayload(
        [
          { key: "NODE_ENV", value: "production", secret: false },
          { key: "EMPTY_PLAIN", value: "", secret: false },
          { key: "SENTRY_DSN", value: "", secret: true },
          { key: "ROTATED", value: "new-value", secret: true },
        ],
        stored,
      ),
    ).toEqual([
      { key: "NODE_ENV", value: "production", secret: false },
      { key: "EMPTY_PLAIN", value: "", secret: false },
      { key: "SENTRY_DSN", secret: true },
      { key: "ROTATED", value: "new-value", secret: true },
    ]);
  });
});

describe("draft mapping keeps secrets write-only", () => {
  const stored = [
    { key: "NODE_ENV", value: "production", secret: false },
    { key: "SENTRY_DSN", secret: true },
  ];

  it("masks secret values in the draft", () => {
    expect(toVariableDrafts(stored)).toEqual([
      { key: "NODE_ENV", value: "production", secret: false },
      { key: "SENTRY_DSN", value: "", secret: true },
    ]);
  });

  it("collects only the secret keys", () => {
    expect(existingSecretKeysOf(stored)).toEqual(new Set(["SENTRY_DSN"]));
  });
});

describe("inherited display helpers", () => {
  it("labels origins as the brief requires", () => {
    expect(inheritedOriginLabel("project")).toBe("from project");
    expect(inheritedOriginLabel("environment")).toBe("from environment");
  });

  it("marks an application key overriding an inherited one", () => {
    expect(isOverriddenBy("LOG_LEVEL", [{ key: "LOG_LEVEL", value: "debug" }])).toBe(true);
    expect(isOverriddenBy("LOG_LEVEL", [{ key: "OTHER", value: "x" }])).toBe(false);
    expect(isOverriddenBy("LOG_LEVEL", [{ key: "", value: "x" }])).toBe(false);
    expect(isOverriddenBy("LOG_LEVEL", [{ key: "log_level", value: "x" }])).toBe(false);
  });

  it("marks a project row shadowed by the environment set", () => {
    const inherited = [
      { key: "SHARED", origin: "project" as const },
      { key: "SHARED", origin: "environment" as const },
      { key: "SHSEC", origin: "project" as const },
      { key: "SHSEC", origin: "environment" as const },
      { key: "ONLY_PROJ", origin: "project" as const },
    ];
    expect(isShadowedByEnvironment("SHARED", inherited)).toBe(true);
    expect(isShadowedByEnvironment("SHSEC", inherited)).toBe(true);
    expect(isShadowedByEnvironment("ONLY_PROJ", inherited)).toBe(false);
    expect(isShadowedByEnvironment("MISSING", inherited)).toBe(false);
    expect(isShadowedByEnvironment("", inherited)).toBe(false);
  });
});

describe("server error key extraction", () => {
  it("names the row a backend 400 complains about", () => {
    expect(rowKeyFromServerError('secret "ESEC" has no value')).toBe("ESEC");
    expect(rowKeyFromServerError('duplicate variable key "A"')).toBe("A");
    expect(
      rowKeyFromServerError('variable key "1A" must match ^[A-Za-z_][A-Za-z0-9_]*$'),
    ).toBe("1A");
    expect(rowKeyFromServerError("insufficient role")).toBeNull();
    expect(rowKeyFromServerError("")).toBeNull();
  });

  it("spots only the missing-secret refusal for the stored-set drop", () => {
    expect(secretWithoutValueKey('secret "ESEC" has no value')).toBe("ESEC");
    expect(secretWithoutValueKey('duplicate variable key "A"')).toBeNull();
    expect(secretWithoutValueKey("insufficient role")).toBeNull();
  });
});

describe("variables envelope", () => {
  it("accepts masked secrets without a value", () => {
    const parsed = parseSharedVariables({
      variables: [
        { key: "NODE_ENV", value: "production", secret: false },
        { key: "SENTRY_DSN", secret: true },
      ],
    });
    expect(parsed.variables).toHaveLength(2);
    expect(parsed.variables[1]).not.toHaveProperty("value");
  });
});

describe("variables api paths", () => {
  it("reads the project scope", async () => {
    vi.mocked(http.get).mockResolvedValue({
      data: { variables: [{ key: "A", value: "1", secret: false }] },
    });
    const variables = await getProjectVariables(projectId);
    expect(http.get).toHaveBeenCalledWith(`/projects/${projectId}/variables`);
    expect(variables).toHaveLength(1);
  });

  it("replaces the project scope with a whole-set body", async () => {
    vi.mocked(http.put).mockResolvedValue({
      data: { variables: [{ key: "A", value: "1", secret: false }] },
    });
    await replaceProjectVariables(projectId, [
      { key: "A", value: "1", secret: false },
      { key: "S", secret: true },
    ]);
    expect(http.put).toHaveBeenCalledWith(
      `/projects/${projectId}/variables`,
      {
        variables: [
          { key: "A", value: "1", secret: false },
          { key: "S", secret: true },
        ],
      },
    );
  });

  it("reads and replaces the environment scope", async () => {
    vi.mocked(http.get).mockResolvedValue({ data: { variables: [] } });
    await getEnvironmentVariables(environmentId);
    expect(http.get).toHaveBeenCalledWith(
      `/environments/${environmentId}/variables`,
    );
    vi.mocked(http.put).mockResolvedValue({ data: { variables: [] } });
    await replaceEnvironmentVariables(environmentId, []);
    expect(http.put).toHaveBeenCalledWith(
      `/environments/${environmentId}/variables`,
      { variables: [] },
    );
  });
});
