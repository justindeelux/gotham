import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useAuthStore } from "../src/features/auth/stores/auth";
import { useRegisterInvite } from "../src/features/auth/composables/useRegisterInvite";

beforeEach(() => {
  setActivePinia(createPinia());
  vi.restoreAllMocks();
});

describe("useRegisterInvite", () => {
  it("holds the token and blocks submission before validation", () => {
    const invite = useRegisterInvite();
    invite.holdToken("tok123");
    expect(invite.inviteToken.value).toBe("tok123");
    expect(invite.inviteChecking.value).toBe(true);
  });

  it("fills the invited email on a usable token", async () => {
    const auth = useAuthStore();
    vi.spyOn(auth, "validateInvite").mockResolvedValue({ email: "invited@x.y" });
    const invite = useRegisterInvite();
    invite.holdToken("tok123");
    const valid = await invite.acceptInvite();
    expect(valid).toBe(true);
    expect(invite.inviteEmail.value).toBe("invited@x.y");
    expect(invite.inviteChecking.value).toBe(false);
  });

  it("reports an unusable token without an address", async () => {
    const auth = useAuthStore();
    vi.spyOn(auth, "validateInvite").mockRejectedValue(new Error("expired"));
    const invite = useRegisterInvite();
    invite.holdToken("stale");
    const valid = await invite.acceptInvite();
    expect(valid).toBe(false);
    expect(invite.inviteEmail.value).toBe("");
    expect(invite.inviteChecking.value).toBe(false);
  });
});
