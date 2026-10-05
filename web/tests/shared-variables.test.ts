// PE-6 (JUS-35) shared variables: the contract's key/value rules, the
// keep-ciphertext payload mapping, the override helper, and the API paths
// (warn-only parsing). Pins the backend rules in internal/projects/variables.go:
// key ^[A-Za-z_][A-Za-z0-9_]*$, 128 keys per scope, 128-char keys, NUL
// rejected, secrets write-only, omitted value keeps an existing secret.
import { describe, expect, it, vi } from "vitest";

vi.mock("@/shared/api/http", () => ({
  http: { get: vi.fn(), post: vi.fn(), patch: vi.fn(), delete: vi.fn(), put: vi.fn() },
  teamHeaders: (teamId: string) =>
    teamId ? { "X-Team-Id": teamId } : {},
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
  isSharedVariableKeyValid,
  isSharedVariableValueValid,
  parseSharedVariables,
  toVariableDrafts,
  validateVariableDrafts,
} from "@/features/projects/schemas/variables";

const projectId = "11111111-1111-4111-8111-111111111111";
const environmentId = "22222222-2222-4222-8222-222222222222";

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
    // Lowercase differs from uppercase: no duplicate.
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
    // Stored secret with an empty value: valid, the save omits the value.
    expect(
      validateVariableDrafts(
        [{ key: "SENTRY_DSN", value: "", secret: true }],
        new Set(["SENTRY_DSN"]),
      ),
    ).toEqual([]);
    // Plain-to-secret without a value is a 400 backend-side: blocked up front.
    const problems = validateVariableDrafts(
      [{ key: "FRESH", value: "", secret: true }],
      new Set(["SENTRY_DSN"]),
    );
    expect(problems).toHaveLength(1);
    expect(problems[0]).toContain('Secret "FRESH" needs a value');
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
    // Case-sensitive like the deploy merge.
    expect(isOverriddenBy("LOG_LEVEL", [{ key: "log_level", value: "x" }])).toBe(false);
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
  it("reads the project scope with the team header", async () => {
    vi.mocked(http.get).mockResolvedValue({
      data: { variables: [{ key: "A", value: "1", secret: false }] },
    });
    const variables = await getProjectVariables("team-1", projectId);
    expect(http.get).toHaveBeenCalledWith(`/projects/${projectId}/variables`, {
      headers: { "X-Team-Id": "team-1" },
    });
    expect(variables).toHaveLength(1);
  });

  it("replaces the project scope with a whole-set body", async () => {
    vi.mocked(http.put).mockResolvedValue({
      data: { variables: [{ key: "A", value: "1", secret: false }] },
    });
    await replaceProjectVariables("team-1", projectId, [
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
      { headers: { "X-Team-Id": "team-1" } },
    );
  });

  it("reads and replaces the environment scope", async () => {
    vi.mocked(http.get).mockResolvedValue({ data: { variables: [] } });
    await getEnvironmentVariables("team-1", environmentId);
    expect(http.get).toHaveBeenCalledWith(
      `/environments/${environmentId}/variables`,
      { headers: { "X-Team-Id": "team-1" } },
    );
    vi.mocked(http.put).mockResolvedValue({ data: { variables: [] } });
    await replaceEnvironmentVariables("team-1", environmentId, []);
    expect(http.put).toHaveBeenCalledWith(
      `/environments/${environmentId}/variables`,
      { variables: [] },
      { headers: { "X-Team-Id": "team-1" } },
    );
  });
});
