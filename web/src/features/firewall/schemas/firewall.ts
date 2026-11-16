import { z } from "zod";

export const firewallBackendSchema = z.enum([
  "none",
  "ufw",
  "firewalld",
  "nftables",
  "iptables",
]);

export const firewallActionSchema = z.enum(["allow", "deny"]);
export const firewallDirectionSchema = z.enum(["in", "out"]);
export const firewallProtocolSchema = z.enum(["tcp", "udp", "any"]);

export const firewallStatusSchema = z.object({
  backend: firewallBackendSchema,
  available: z.boolean(),
  enabled: z.boolean(),
  manageable: z.boolean(),
  detail: z.string().optional(),
  ssh_port: z.number().int(),
  agent_port: z.number().int(),
  control_plane_port: z.number().int(),
});

export const firewallRuleSchema = z.object({
  id: z.string(),
  action: firewallActionSchema,
  direction: firewallDirectionSchema,
  protocol: firewallProtocolSchema,
  port: z.string(),
  source: z.string(),
  comment: z.string(),
  enabled: z.boolean(),
  protected: z.boolean(),
  read_only: z.boolean(),
});

export const firewallStatusEnvelopeSchema = z.object({
  status: firewallStatusSchema,
});

export const firewallStateEnvelopeSchema = z.object({
  status: firewallStatusSchema,
  rules: z.array(firewallRuleSchema).optional(),
});

export const firewallRulesEnvelopeSchema = z.object({
  rules: z.array(firewallRuleSchema).optional(),
});

const v4 = /^((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\.){3}(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)$/;

function isIPv4(value: string): boolean {
  return v4.test(value);
}

function isIPv6(value: string): boolean {
  if (!/^[0-9a-fA-F:]{2,39}$/.test(value) || !value.includes(":")) {
    return false;
  }
  try {
    return new URL(`http://[${value}]/`).hostname !== "";
  } catch {
    return false;
  }
}

export function isValidFirewallSource(value: string): boolean {
  if (value === "") {
    return true;
  }
  const slash = value.lastIndexOf("/");
  if (slash < 0) {
    return isIPv4(value) || isIPv6(value);
  }
  const address = value.slice(0, slash);
  const bitsText = value.slice(slash + 1);
  if (bitsText === "" || !/^\d{1,3}$/.test(bitsText)) {
    return false;
  }
  const bits = Number(bitsText);
  if (isIPv4(address)) {
    return bits <= 32;
  }
  if (isIPv6(address)) {
    return bits <= 128;
  }
  return false;
}

export function isValidFirewallPort(value: string): boolean {
  if (value === "") {
    return true;
  }
  const match = /^(\d{1,5})(?:-(\d{1,5}))?$/.exec(value);
  if (!match) {
    return false;
  }
  const first = Number(match[1]);
  if (first < 1 || first > 65535) {
    return false;
  }
  if (match[2] === undefined) {
    return true;
  }
  const last = Number(match[2]);
  return last >= first && last <= 65535;
}

export const firewallRuleFormSchema = z.object({
  action: firewallActionSchema,
  direction: firewallDirectionSchema,
  protocol: firewallProtocolSchema,
  port: z
    .string()
    .trim()
    .refine(isValidFirewallPort, "firewall.validation.port"),
  source: z
    .string()
    .trim()
    .refine(isValidFirewallSource, "firewall.validation.source"),
  comment: z
    .string()
    .trim()
    .max(200, "firewall.validation.comment")
    .refine(
      (value) => /^[A-Za-z0-9 ._(),:/+@-]*$/.test(value),
      "firewall.validation.comment",
    ),
  enabled: z.boolean(),
});

export type FirewallRuleForm = z.infer<typeof firewallRuleFormSchema>;
