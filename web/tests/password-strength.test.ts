// Password strength contract for RegisterPage (follows docs/design/login.html).
import { describe, expect, it } from "vitest";

import {
  countCharClasses,
  meetsPasswordPolicy,
  scorePassword,
  strengthKindOf,
  strengthLabelOf,
  strengthOf,
} from "../src/features/auth/utils/passwordStrength";

describe("countCharClasses", () => {
  it("counts each class once", () => {
    expect(countCharClasses("")).toBe(0);
    expect(countCharClasses("abc")).toBe(1);
    expect(countCharClasses("aB1!")).toBe(4);
    expect(countCharClasses("ABC123")).toBe(2);
  });
});

describe("scorePassword", () => {
  it("rates length, case mix, digits, and symbols up to 4", () => {
    expect(scorePassword("")).toBe(0);
    expect(scorePassword("abcdefghij")).toBe(1);
    expect(scorePassword("Abcdefghij")).toBe(2);
    expect(scorePassword("Abcdefghij12")).toBe(3);
    expect(scorePassword("Abcdefghij12!")).toBe(4);
    expect(scorePassword("Abcdefghijklmnop12!")).toBe(4);
  });
});

describe("strengthOf", () => {
  it("stays 0 when empty and at least 1 otherwise", () => {
    expect(strengthOf("")).toBe(0);
    expect(strengthOf("abcdefghij")).toBe(1);
    expect(strengthOf("Abcdefghij12!")).toBe(4);
  });
});

describe("strengthLabelOf/strengthKindOf", () => {
  it("names and classes every score", () => {
    expect(
      [0, 1, 2, 3, 4].map((score) => strengthLabelOf(score)),
    ).toEqual(["Not entered", "Very weak", "Weak", "Fair", "Strong"]);
    expect(strengthKindOf(4)).toBe("on");
    expect(strengthKindOf(3)).toBe("mid");
    expect(strengthKindOf(2)).toBe("weak");
    expect(strengthKindOf(0)).toBe("weak");
  });
});

describe("meetsPasswordPolicy", () => {
  it("requires 10+ characters with 2+ classes", () => {
    expect(meetsPasswordPolicy("Abcdefghij")).toBe(true);
    expect(meetsPasswordPolicy("abcdefghij")).toBe(false);
    expect(meetsPasswordPolicy("Abcdefg1")).toBe(false);
  });
});
