import { http } from "@/shared/api/http";
import { parseWith } from "@/shared/validation/parse";

import {
  firewallRulesEnvelopeSchema,
  firewallStateEnvelopeSchema,
  firewallStatusEnvelopeSchema,
} from "../schemas/firewall";
import type { z } from "zod";

export type FirewallStatus = z.infer<typeof firewallStatusEnvelopeSchema>["status"];
export type FirewallRule = NonNullable<
  z.infer<typeof firewallRulesEnvelopeSchema>["rules"]
>[number];

export interface FirewallState {
  status: FirewallStatus;
  rules: FirewallRule[];
}

export interface FirewallRuleInput {
  action: string;
  direction: string;
  protocol: string;
  port: string;
  source: string;
  comment: string;
  enabled: boolean;
}

function rulesFrom(data: unknown): FirewallRule[] {
  const parsed = parseWith(firewallRulesEnvelopeSchema, data, {
    context: "FirewallRulesEnvelope",
  });
  return parsed.rules ?? [];
}

export async function getFirewallStatus(serverId: string): Promise<FirewallStatus> {
  const response = await http.get(`/servers/${serverId}/firewall`);
  const parsed = parseWith(firewallStatusEnvelopeSchema, response.data, {
    context: "FirewallStatusEnvelope",
  });
  return parsed.status;
}

export async function setFirewallEnabled(
  serverId: string,
  enabled: boolean,
): Promise<FirewallState> {
  const response = await http.put(`/servers/${serverId}/firewall`, { enabled });
  const parsed = parseWith(firewallStateEnvelopeSchema, response.data, {
    context: "FirewallStateEnvelope",
  });
  return { status: parsed.status, rules: parsed.rules ?? [] };
}

export async function listFirewallRules(serverId: string): Promise<FirewallRule[]> {
  const response = await http.get(`/servers/${serverId}/firewall/rules`);
  return rulesFrom(response.data);
}

export async function addFirewallRule(
  serverId: string,
  rule: FirewallRuleInput,
): Promise<FirewallRule[]> {
  const response = await http.post(`/servers/${serverId}/firewall/rules`, { rule });
  return rulesFrom(response.data);
}

export async function updateFirewallRule(
  serverId: string,
  ruleId: string,
  rule: FirewallRuleInput,
): Promise<FirewallRule[]> {
  const response = await http.put(
    `/servers/${serverId}/firewall/rules/${encodeURIComponent(ruleId)}`,
    { rule },
  );
  return rulesFrom(response.data);
}

export async function deleteFirewallRule(
  serverId: string,
  ruleId: string,
): Promise<FirewallRule[]> {
  const response = await http.delete(
    `/servers/${serverId}/firewall/rules/${encodeURIComponent(ruleId)}`,
  );
  return rulesFrom(response.data);
}
