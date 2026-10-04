// Unit tests for the shared server connection validation (JUS-24).

import { describe, expect, it } from "vitest";

import {
  NAME_PATTERN,
  USER_PATTERN,
  isValidHost,
} from "../src/features/servers/utils/serverValidation";

describe("isValidHost", () => {
  it("accepts IPv4 literals", () => {
    expect(isValidHost("203.0.113.90")).toBe(true);
    expect(isValidHost(" 10.0.0.1 ")).toBe(true);
  });

  it("accepts DNS-style hostnames", () => {
    expect(isValidHost("node3.internal")).toBe(true);
    expect(isValidHost("build-node-03")).toBe(true);
  });

  it("rejects blanks and illegal characters", () => {
    expect(isValidHost("")).toBe(false);
    expect(isValidHost("   ")).toBe(false);
    expect(isValidHost("bad host!")).toBe(false);
  });

  it("accepts out-of-range IPv4-shaped input as a hostname (matches the forms)", () => {
    // "999.1.1.1" fails the IPv4 check but passes the hostname check, so the
    // forms accept it. Preserved as-is; flagged in the F2-servers report.
    expect(isValidHost("999.1.1.1")).toBe(true);
  });
});

describe("name and user patterns", () => {
  it("accepts node names the forms allow", () => {
    expect(NAME_PATTERN.test("build-node-03")).toBe(true);
    expect(NAME_PATTERN.test("node_1.prod")).toBe(true);
    expect(NAME_PATTERN.test("-leading-dash")).toBe(false);
  });

  it("accepts Unix usernames the forms allow", () => {
    expect(USER_PATTERN.test("root")).toBe(true);
    expect(USER_PATTERN.test("deploy_2")).toBe(true);
    expect(USER_PATTERN.test("Root")).toBe(false);
  });
});
