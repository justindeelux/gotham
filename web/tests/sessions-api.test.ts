// Sessions API client (JUS-28): routes, payloads, and envelope handling
// with a mocked axios layer (the PF-2 backend ships in parallel).
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/shared/api/http", () => ({
  http: { get: vi.fn(), delete: vi.fn(), post: vi.fn() },
}));

import {
  listSessions,
  revokeOtherSessions,
  revokeSession,
} from "@/features/profile/api/sessions";
import { profileMessages } from "@/features/profile/schemas/profile";
import {
  sessionsEnvelopeSchema,
  sessionSchema,
} from "@/features/profile/schemas/sessions";
import { parseWith } from "@/shared/validation/parse";
import {
  registerDiscoveredCatalogs,
  resetLocaleState,
  resolveValidationMessage,
  syncComposerLocale,
} from "@/shared/i18n";
import { http } from "@/shared/api/http";

const get = vi.mocked(http.get);
const remove = vi.mocked(http.delete);
const post = vi.mocked(http.post);

function session(overrides = {}) {
  return {
    id: "s-1",
    user_agent:
      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Chrome/126.0 Safari/537.36",
    ip: "203.0.113.7",
    created_at: "2026-09-01T10:00:00Z",
    last_used_at: "2026-10-03T10:00:00Z",
    current: true,
    ...overrides,
  };
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  vi.restoreAllMocks();
});

describe("session messages are keys with byte-exact English display", () => {
  it("pins every new catalog key and its resolved English text", () => {
    const cases: Array<[string, string]> = [
      [profileMessages.sessionsLoadFailed, "Could not load sessions. Try again."],
      [profileMessages.sessionEndFailed, "Could not sign out that session. Try again."],
      [
        profileMessages.revokeOthersFailed,
        "Could not sign out the other sessions. Try again.",
      ],
      [profileMessages.sessionsSignedOut, "Other devices were signed out."],
      [profileMessages.sessionSignedOut, "Session signed out."],
      [profileMessages.sessionsSignedOutHere, "Signed out on this device."],
      [
        profileMessages.needsReauth,
        "Your sign-in predates session management. Sign in again to manage other sessions.",
      ],
      [
        profileMessages.sessionsListStale,
        "Signed out, but the session list may be out of date.",
      ],
      [
        profileMessages.sessionsIntro,
        "Every device signed in to your account. Ending a session signs that device out; ending this device signs you out here.",
      ],
      [profileMessages.sessionsEmpty, "No active sessions."],
      [profileMessages.actionRetry, "Retry"],
      [profileMessages.signInAgain, "Sign in again"],
    ];
    for (const [key, text] of cases) {
      // Keys stay namespaced; behavior preserved: each resolves to the
      // exact previous string in the default locale.
      expect(key.startsWith("profile.sessions.")).toBe(true);
      expect(resolveValidationMessage(key)).toBe(text);
    }
  });
});

describe("sessions envelope", () => {
  it("accepts the GET /auth/me/sessions envelope strictly", () => {
    expect(() =>
      parseWith(sessionsEnvelopeSchema, { sessions: [session()] }, { strict: true }),
    ).not.toThrow();
  });

  it("accepts blank user_agent and ip", () => {
    expect(() =>
      parseWith(
        sessionsEnvelopeSchema,
        { sessions: [session({ user_agent: "", ip: "" })] },
        { strict: true },
      ),
    ).not.toThrow();
  });

  it("rejects a session missing the current flag", () => {
    const { current: _dropped, ...rest } = session();
    expect(sessionSchema.safeParse(rest).success).toBe(false);
  });
});

describe("listSessions", () => {
  it("GETs /auth/me/sessions and returns the list", async () => {
    const sessions = [session(), session({ id: "s-2", current: false })];
    get.mockResolvedValue({ data: { sessions } } as never);
    await expect(listSessions()).resolves.toEqual(sessions);
    expect(get).toHaveBeenCalledWith("/auth/me/sessions");
  });

  it("warns on a skewed shape but still returns the raw payload", async () => {
    const warn = vi.spyOn(globalThis.console, "warn").mockImplementation(() => {});
    const sessions = [{ ...session(), current: "yes" }];
    get.mockResolvedValue({ data: { sessions } } as never);
    await expect(listSessions()).resolves.toEqual(sessions);
    expect(warn).toHaveBeenCalled();
  });
});

describe("revokeSession", () => {
  it("DELETEs the session by id", async () => {
    remove.mockResolvedValue({} as never);
    await revokeSession("s-2");
    expect(remove).toHaveBeenCalledWith("/auth/me/sessions/s-2");
  });

  it("encodes the id in the path", async () => {
    remove.mockResolvedValue({} as never);
    await revokeSession("s/2+x");
    expect(remove).toHaveBeenCalledWith("/auth/me/sessions/s%2F2%2Bx");
  });
});

describe("revokeOtherSessions", () => {
  it("POSTs to revoke-others", async () => {
    post.mockResolvedValue({} as never);
    await revokeOtherSessions();
    expect(post).toHaveBeenCalledWith("/auth/me/sessions/revoke-others");
  });
});
