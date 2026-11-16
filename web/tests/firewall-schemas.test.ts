import { describe, expect, it } from "vitest";

import {
  firewallRuleFormSchema,
  firewallRulesEnvelopeSchema,
  firewallStatusEnvelopeSchema,
  isValidFirewallPort,
  isValidFirewallSource,
} from "@/features/firewall/schemas/firewall";

describe("firewall port validation", () => {
  it("accepts single ports and ranges", () => {
    for (const value of ["", "1", "22", "65535", "8000-8100"]) {
      expect(isValidFirewallPort(value)).toBe(true);
    }
  });

  it("rejects malformed values", () => {
    for (const value of ["0", "65536", "70000", "8000-", "-22", "9000-8000", "22/tcp", "a"]) {
      expect(isValidFirewallPort(value)).toBe(false);
    }
  });
});

describe("firewall source validation", () => {
  it("accepts addresses and CIDRs", () => {
    for (const value of ["", "10.0.0.1", "10.0.0.0/8", "2001:db8::1", "2001:db8::/32"]) {
      expect(isValidFirewallSource(value)).toBe(true);
    }
  });

  it("rejects hostnames and bad prefixes", () => {
    for (const value of ["example.com", "10.0.0.0/33", "2001:db8::/129", "10.0.0.0/"]) {
      expect(isValidFirewallSource(value)).toBe(false);
    }
  });
});

describe("firewall rule form", () => {
  const valid = {
    action: "allow",
    direction: "in",
    protocol: "tcp",
    port: "22",
    source: "10.0.0.0/8",
    comment: "SSH",
    enabled: true,
  };

  it("accepts a complete rule", () => {
    expect(firewallRuleFormSchema.safeParse(valid).success).toBe(true);
  });

  it("accepts an any-port and any-source rule", () => {
    expect(
      firewallRuleFormSchema.safeParse({ ...valid, port: "", source: "" }).success,
    ).toBe(true);
  });

  it("rejects a bad port, source and comment", () => {
    expect(firewallRuleFormSchema.safeParse({ ...valid, port: "70000" }).success).toBe(false);
    expect(
      firewallRuleFormSchema.safeParse({ ...valid, source: "example.com" }).success,
    ).toBe(false);
    expect(
      firewallRuleFormSchema.safeParse({ ...valid, comment: "bad;comment" }).success,
    ).toBe(false);
    expect(
      firewallRuleFormSchema.safeParse({ ...valid, comment: "a".repeat(201) }).success,
    ).toBe(false);
  });
});

describe("firewall API envelopes", () => {
  it("parses a status and a rule list", () => {
    const status = firewallStatusEnvelopeSchema.safeParse({
      status: {
        backend: "ufw",
        available: true,
        enabled: true,
        manageable: true,
        detail: "",
        ssh_port: 22,
        agent_port: 9443,
        control_plane_port: 8000,
      },
    });
    expect(status.success).toBe(true);

    const rules = firewallRulesEnvelopeSchema.safeParse({
      rules: [
        {
          id: "r-1",
          action: "allow",
          direction: "in",
          protocol: "tcp",
          port: "22",
          source: "",
          comment: "SSH",
          enabled: true,
          protected: true,
          read_only: false,
        },
      ],
    });
    expect(rules.success).toBe(true);
  });
});
