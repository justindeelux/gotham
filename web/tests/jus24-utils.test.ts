// JUS-24: extracted pure helpers keep their contracts after the split.
import { describe, expect, it } from "vitest";

import { durationText } from "../src/features/services/utils/deployDuration";
import { cardMark } from "../src/features/services/utils/serviceDisplay";
import { acceptLink, registerLink } from "../src/features/teams/utils/teamLinks";
import type { ServiceDeploy } from "../src/features/services/api/services";

function deploy(overrides: Partial<ServiceDeploy> = {}): ServiceDeploy {
  return {
    id: "d".repeat(16),
    state: "succeeded",
    error: null,
    created_at: "2026-01-01T00:00:00Z",
    finished_at: "2026-01-01T00:01:30Z",
    ...overrides,
  } as ServiceDeploy;
}

describe("teamLinks", () => {
  it("builds the accept and register links with an encoded token", () => {
    const token = "a+b/c=d";
    expect(acceptLink(token)).toBe(
      `${globalThis.location.origin}/invite/accept?token=${encodeURIComponent(token)}`,
    );
    expect(registerLink(token)).toBe(
      `${globalThis.location.origin}/register?invite=${encodeURIComponent(token)}`,
    );
  });
});

describe("cardMark", () => {
  it("uppercases the first non-blank character", () => {
    expect(cardMark("blog")).toBe("B");
    expect(cardMark("  api")).toBe("A");
  });

  it("falls back to a placeholder for blank names", () => {
    expect(cardMark("")).toBe("?");
    expect(cardMark("   ")).toBe("?");
  });
});

describe("durationText", () => {
  it("renders seconds, minutes and hours", () => {
    expect(durationText(deploy({ finished_at: "2026-01-01T00:00:45Z" }))).toBe("45s");
    expect(durationText(deploy())).toBe("1m30s");
    expect(
      durationText(deploy({ finished_at: "2026-01-01T02:05:00Z" })),
    ).toBe("2h5m");
  });

  it("renders an em dash for invalid ranges", () => {
    expect(durationText(deploy({ created_at: "nope" }))).toBe("—");
    expect(
      durationText(
        deploy({
          created_at: "2026-01-02T00:00:00Z",
          finished_at: "2026-01-01T00:00:00Z",
        }),
      ),
    ).toBe("—");
  });
});
