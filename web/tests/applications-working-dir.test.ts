import { describe, expect, it } from "vitest";

import {
  MAX_WORKING_DIR_LENGTH,
  workingDirSchema,
} from "@/features/applications/schemas/applications";

describe("workingDirSchema", () => {
  it("accepts the repository root and relative folders", () => {
    for (const value of ["", "  ", "docs", "apps/web", " apps/web "]) {
      expect(workingDirSchema.safeParse(value).success).toBe(true);
    }
  });

  it("rejects escapes the control plane refuses", () => {
    for (const value of [
      "..",
      "../x",
      "apps/../../x",
      "/etc",
      "apps\\web",
      ".git",
      "apps/.git/objects",
      "apps//web",
      "./apps",
      "a".repeat(MAX_WORKING_DIR_LENGTH + 1),
    ]) {
      expect(workingDirSchema.safeParse(value).success, value).toBe(false);
    }
  });
});
