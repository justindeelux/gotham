import { describe, expect, it } from "vitest";

import { generateSecret } from "@/shared/utils/generateSecret";

describe("generateSecret", () => {
  it("produces a 24 character alphanumeric value", () => {
    const value = generateSecret();
    expect(value).toHaveLength(24);
    expect(value).toMatch(/^[A-Za-z0-9]+$/);
  });

  it("honours a custom length", () => {
    expect(generateSecret(16)).toHaveLength(16);
  });

  it("never repeats the same value across many runs", () => {
    const values = new Set(Array.from({ length: 100 }, () => generateSecret()));
    expect(values.size).toBe(100);
  });
});
