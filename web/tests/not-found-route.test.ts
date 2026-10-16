// JUS-193: an unknown URL renders the standalone 404 page inside the
// authenticated shell instead of silently redirecting to the dashboard, and
// the auth redirect stays intact (the catch-all inherits requiresAuth).
import { describe, expect, it } from "vitest";

import { router } from "@/app/router";

describe("unknown route resolves to the not-found page", () => {
  it("matches the catch-all with the shell's auth requirement", () => {
    const resolved = router.resolve("/no/such/page");
    expect(resolved.name).toBe("not-found");
    expect(resolved.meta.requiresAuth).toBe(true);
  });

  it("never captures a known route", () => {
    expect(router.resolve("/dashboard").name).toBe("dashboard");
    // Retired flat routes keep their explicit redirect (not the 404 page).
    expect(router.resolve("/applications").name).not.toBe("not-found");
  });
});
