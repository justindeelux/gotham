// Profile API client (JUS-27): routes, payloads, and envelope handling
// with a mocked axios layer (the PF-1 backend ships in parallel).
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/shared/api/http", () => ({
  http: { get: vi.fn(), patch: vi.fn(), post: vi.fn() },
}));

import { http } from "@/shared/api/http";
import { changePassword, patchDisplayName } from "@/features/profile/api/profile";

const patch = vi.mocked(http.patch);
const post = vi.mocked(http.post);

const user = {
  id: "u-1",
  email: "ada@gotham.dev",
  created_at: "2026-01-01T00:00:00Z",
};

beforeEach(() => {
  vi.restoreAllMocks();
});

describe("patchDisplayName", () => {
  it("PATCHes /auth/me with the trimmed name and returns the user", async () => {
    const updated = { ...user, display_name: "Ada" };
    patch.mockResolvedValue({ data: { user: updated } } as never);
    await expect(patchDisplayName("Ada")).resolves.toEqual(updated);
    expect(patch).toHaveBeenCalledWith("/auth/me", { display_name: "Ada" });
  });

  it("sends null to clear the name", async () => {
    patch.mockResolvedValue({ data: { user } } as never);
    await patchDisplayName(null);
    expect(patch).toHaveBeenCalledWith("/auth/me", { display_name: null });
  });

  it("warns on a skewed shape but still returns the raw payload", async () => {
    const warn = vi.spyOn(globalThis.console, "warn").mockImplementation(() => {});
    const updated = { ...user, display_name: 42 };
    patch.mockResolvedValue({ data: { user: updated } } as never);
    await expect(patchDisplayName("Ada")).resolves.toEqual(updated);
    expect(warn).toHaveBeenCalled();
  });
});

describe("changePassword", () => {
  const pair = {
    user,
    access_token: "new-access",
    token_type: "Bearer",
    expires_in: 900,
    refresh_token: "new-refresh",
  };

  it("POSTs the password pair and returns it", async () => {
    post.mockResolvedValue({ data: pair } as never);
    await expect(
      changePassword({ current_password: "old", new_password: "new-secret-123" }),
    ).resolves.toEqual(pair);
    expect(post).toHaveBeenCalledWith("/auth/me/password", {
      current_password: "old",
      new_password: "new-secret-123",
    });
  });

  it("omits the current password for OAuth-created accounts", async () => {
    post.mockResolvedValue({ data: pair } as never);
    await changePassword({ new_password: "new-secret-123" });
    expect(post).toHaveBeenCalledWith("/auth/me/password", {
      new_password: "new-secret-123",
    });
  });
});
