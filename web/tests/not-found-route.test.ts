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
    expect(router.resolve("/applications").name).not.toBe("not-found");
  });
});
