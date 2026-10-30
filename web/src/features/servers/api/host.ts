import { http } from "@/shared/api/http";
import { parseWith } from "@/shared/validation/parse";

import {
  nodeHelperEnvelopeSchema,
  nodeHostEnvelopeSchema,
} from "@/features/servers/schemas/host";

export type NodeCapability =
  | "ready"
  | "unsupported_stack"
  | "helper_outdated"
  | "helper_missing"
  | "helper_denied"
  | "agent_outdated"
  | "offline";

export interface IPv4Settings {
  mode: string;
  address: string;
  gateway: string;
}

export interface IPv6Settings extends IPv4Settings {
  enabled: boolean;
}

export interface NetworkSettings {
  dns_servers: string[];
  ipv4: IPv4Settings;
  ipv6: IPv6Settings;
  confirm_interface_change?: boolean;
}

export interface SystemSettings {
  hostname: string;
  ntp_enabled: boolean;
  ntp_servers: string[];
  timezone: string;
}

export interface PendingNetwork {
  previous: NetworkSettings;
  proposed: NetworkSettings;
  deadline: string;
}

export interface NodeHostState {
  capability: NodeCapability;
  reason?: string;
  repair: boolean;
  network: NetworkSettings;
  system: SystemSettings;
  pending: PendingNetwork | null;
}

export interface NodeHelperResult {
  state: NodeHostState;
  log: string;
}

function unwrap(request: Promise<{ data: unknown }>): Promise<NodeHostState> {
  return request.then((response) => {
    const parsed = parseWith(nodeHostEnvelopeSchema, response.data, {
      context: "NodeHostEnvelope",
    });
    return parsed.host;
  });
}

export const getNodeHost = (serverId: string): Promise<NodeHostState> =>
  unwrap(http.get(`/servers/${serverId}/host`));

export const saveNodeSystem = (
  serverId: string,
  input: SystemSettings,
): Promise<NodeHostState> => unwrap(http.put(`/servers/${serverId}/host/system`, input));

export const saveNodeNetwork = (
  serverId: string,
  input: NetworkSettings,
): Promise<NodeHostState> => unwrap(http.put(`/servers/${serverId}/host/network`, input));

export const confirmNodeNetwork = (serverId: string): Promise<NodeHostState> =>
  unwrap(http.post(`/servers/${serverId}/host/network/confirm`));

export const revertNodeNetwork = (serverId: string): Promise<NodeHostState> =>
  unwrap(http.post(`/servers/${serverId}/host/network/revert`));

export async function installNodeHelper(serverId: string): Promise<NodeHelperResult> {
  const response = await http.post(`/servers/${serverId}/host/helper`);
  const parsed = parseWith(nodeHelperEnvelopeSchema, response.data, {
    context: "NodeHelperEnvelope",
  });
  return { state: parsed.host, log: parsed.log };
}
