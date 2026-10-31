import { describe, expect, it } from "vitest";

import { generateKeyName } from "@/features/servers/utils/keyName";

describe("generateKeyName", () => {
  it("uses a lowercase first name and 12 random [a-z0-9] characters", () => {
    for (let i = 0; i < 20; i += 1) {
      expect(generateKeyName()).toMatch(/^[a-z]+-[a-z0-9]{12}$/);
    }
  });
});
