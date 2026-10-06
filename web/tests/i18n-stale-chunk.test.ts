// Stale-chunk fallback copy resolves without loading any feature chunk.
import { describe, expect, it } from "vitest";

import { staleChunkCopy } from "@/shared/i18n/staleFallback";

describe("staleChunkCopy", () => {
  it("returns English copy by default", () => {
    const copy = staleChunkCopy("en");
    expect(copy.message).toContain("reload");
    expect(copy.reload).toBe("Reload");
  });

  it("returns Vietnamese copy for the vi locale", () => {
    const copy = staleChunkCopy("vi");
    expect(copy.message).toContain("tải lại");
    expect(copy.reload).toBe("Tải lại");
  });
});
