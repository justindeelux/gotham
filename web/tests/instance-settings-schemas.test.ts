import { describe, expect, it } from "vitest";

import {
  controlPlaneUrlSchema,
  dnsListSchema,
  ipv4AddressSchema,
  ipv6AddressSchema,
  hostnameSchema,
  timezoneSchema,
} from "@/features/instance-settings/schemas/instance";

describe("instance-settings schemas", () => {
  it("validates the control-plane URL", () => {
    expect(controlPlaneUrlSchema.safeParse("https://g.example.com").success).toBe(true);
    expect(controlPlaneUrlSchema.safeParse("").success).toBe(true);
    for (const bad of ["ftp://x.io", "g.example.com", "https://u:p@x.io", "https://x.io/?a=1"]) {
      expect(controlPlaneUrlSchema.safeParse(bad).success, bad).toBe(false);
    }
  });

  it("validates timezone, DNS, addresses and hostname", () => {
    expect(timezoneSchema.safeParse("Europe/Berlin").success).toBe(true);
    expect(timezoneSchema.safeParse("Mars/Base").success).toBe(false);
    expect(dnsListSchema.safeParse("1.1.1.1, 2606:4700:4700::1111").success).toBe(true);
    expect(dnsListSchema.safeParse("1.1.1.1, 1.1.1.1").success).toBe(false);
    expect(dnsListSchema.safeParse("1.1.1.1;x").success).toBe(false);
    expect(ipv4AddressSchema.safeParse("10.0.0.5/24").success).toBe(true);
    expect(ipv4AddressSchema.safeParse("10.0.0.5").success).toBe(false);
    expect(ipv6AddressSchema.safeParse("2001:db8::10/64").success).toBe(true);
    expect(ipv6AddressSchema.safeParse("2001:db8::10/200").success).toBe(false);
    expect(hostnameSchema.safeParse("gotham-1.lan").success).toBe(true);
    expect(hostnameSchema.safeParse("-bad").success).toBe(false);
  });
});
