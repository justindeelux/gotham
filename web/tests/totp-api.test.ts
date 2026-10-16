// Two-factor API client and login flow (JUS-196): routes, payloads and the
// half-authenticated state with a mocked axios layer.
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";

vi.mock("@/shared/api/http", () => ({
  http: { get: vi.fn(), post: vi.fn(), patch: vi.fn() },
}));

import { http } from "@/shared/api/http";
import { useAuthStore } from "@/features/auth";
import {
  beginTotpEnrollment,
  disableTotp,
  enableTotp,
  fetchTotpStatus,
  regenerateTotpRecoveryCodes,
} from "@/features/profile/api/totp";

const get = vi.mocked(http.get);
const post = vi.mocked(http.post);

const user = {
  id: "u-1",
  email: "ada@gotham.dev",
  created_at: "2026-01-01T00:00:00Z",
  has_password: true,
};

const pair = {
  user,
  access_token: "access",
  token_type: "Bearer",
  expires_in: 900,
  refresh_token: "refresh",
};

beforeEach(() => {
  vi.restoreAllMocks();
  setActivePinia(createPinia());
  window.localStorage.clear();
});

describe("totp api", () => {
  it("reads the status from GET /auth/me/totp", async () => {
    get.mockResolvedValue({ data: { enabled: true, recovery_codes_remaining: 7 } } as never);
    await expect(fetchTotpStatus()).resolves.toEqual({
      enabled: true,
      recovery_codes_remaining: 7,
    });
    expect(get).toHaveBeenCalledWith("/auth/me/totp");
  });

  it("starts an enrollment with POST /auth/me/totp/enroll", async () => {
    const enrollment = { secret: "JBSWY3DPEHPK3PXP", otpauth_url: "otpauth://totp/Gotham:a" };
    post.mockResolvedValue({ data: enrollment } as never);
    await expect(beginTotpEnrollment()).resolves.toEqual(enrollment);
    expect(post).toHaveBeenCalledWith("/auth/me/totp/enroll", {});
  });

  it("enables with the password and code, returning the recovery codes", async () => {
    post.mockResolvedValue({ data: { recovery_codes: ["ABCDE-FGHJK"] } } as never);
    await expect(enableTotp({ password: "pw", code: "123456" })).resolves.toEqual([
      "ABCDE-FGHJK",
    ]);
    expect(post).toHaveBeenCalledWith("/auth/me/totp/enable", {
      password: "pw",
      code: "123456",
    });
  });

  it("disables and regenerates through their routes", async () => {
    post.mockResolvedValue({ data: { recovery_codes: ["NEW-CODE-01"] } } as never);
    await disableTotp({ code: "123456" });
    expect(post).toHaveBeenCalledWith("/auth/me/totp/disable", { code: "123456" });

    await expect(regenerateTotpRecoveryCodes({ code: "123456" })).resolves.toEqual([
      "NEW-CODE-01",
    ]);
    expect(post).toHaveBeenCalledWith("/auth/me/totp/recovery-codes", {
      code: "123456",
    });
  });
});

describe("two-factor login flow", () => {
  it("does not start a session when the account requires a second factor", async () => {
    const authStore = useAuthStore();
    post.mockResolvedValue({
      data: { ...pair, access_token: "", refresh_token: "", mfa_required: true, mfa_token: "half" },
    } as never);

    await expect(authStore.login("ada@gotham.dev", "pw")).resolves.toEqual({
      mfaRequired: true,
    });
    expect(authStore.isAuthenticated).toBe(false);
    expect(authStore.mfaPending).toBe(true);
    expect(window.localStorage.getItem("gotham.auth.session")).toBeNull();
  });

  it("redeems the challenge and then starts the session", async () => {
    const authStore = useAuthStore();
    post.mockResolvedValueOnce({
      data: { ...pair, access_token: "", refresh_token: "", mfa_required: true, mfa_token: "half" },
    } as never);
    await authStore.login("ada@gotham.dev", "pw");

    post.mockResolvedValueOnce({ data: pair } as never);
    await authStore.completeMfaLogin("123456");

    expect(post).toHaveBeenLastCalledWith("/auth/login/totp", {
      mfa_token: "half",
      code: "123456",
    });
    expect(authStore.isAuthenticated).toBe(true);
    expect(authStore.mfaPending).toBe(false);
  });

  it("refuses to complete without a pending challenge", async () => {
    const authStore = useAuthStore();
    await expect(authStore.completeMfaLogin("123456")).rejects.toThrow();
    expect(post).not.toHaveBeenCalled();
  });

  it("starts the session directly when no second factor is required", async () => {
    const authStore = useAuthStore();
    post.mockResolvedValue({ data: pair } as never);
    await expect(authStore.login("ada@gotham.dev", "pw")).resolves.toEqual({
      mfaRequired: false,
    });
    expect(authStore.isAuthenticated).toBe(true);
    expect(authStore.mfaPending).toBe(false);
  });
});
