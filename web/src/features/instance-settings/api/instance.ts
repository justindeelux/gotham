import { http } from "@/shared/api/http";
import { parseWith } from "@/shared/validation/parse";

import {
  hostHelperEnvelopeSchema,
  stateEnvelopeSchema,
} from "@/features/instance-settings/schemas/instance";

export type ValueSource = "env" | "db" | "default";

export interface SettingField {
  value: string;
  source: ValueSource;
  locked: boolean;
  env_var?: string;
}

export interface GeneralSettings {
  control_plane_url: SettingField;
  instance_name: SettingField;
  timezone: SettingField;
}

export interface GeneralInput {
  control_plane_url: string;
  instance_name: string;
  timezone: string;
}

export interface IPv4Settings {
  mode: "dhcp" | "static";
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
}

export interface PendingNetwork {
  previous: NetworkSettings;
  proposed: NetworkSettings;
  deadline: string;
}

export type HostHelperState = "ready" | "missing" | "denied";

export interface HostState {
  helper: HostHelperState;
  repair: boolean;
  network_stack: boolean;
  system_stack: boolean;
  network: boolean;
  system: boolean;
}

export interface InstanceState {
  general: GeneralSettings;
  network: NetworkSettings;
  system: SystemSettings;
  capabilities: { network: boolean; system: boolean };
  host: HostState;
  pending: PendingNetwork | null;
}

export interface HostHelperResult {
  settings: InstanceState;
  log: string;
}

async function unwrap(request: Promise<{ data: unknown }>): Promise<InstanceState> {
  const response = await request;
  parseWith(stateEnvelopeSchema, response.data, { context: "InstanceStateEnvelope" });
  return (response.data as { settings: InstanceState }).settings;
}

export const getInstanceSettings = (): Promise<InstanceState> =>
  unwrap(http.get("/instance/settings"));

export const saveGeneral = (input: GeneralInput): Promise<InstanceState> =>
  unwrap(http.put("/instance/settings/general", input));

export const saveSystem = (input: SystemSettings): Promise<InstanceState> =>
  unwrap(http.put("/instance/settings/system", input));

export const saveNetwork = (input: NetworkSettings): Promise<InstanceState> =>
  unwrap(http.put("/instance/settings/network", input));

export const confirmNetwork = (): Promise<InstanceState> =>
  unwrap(http.post("/instance/settings/network/confirm"));

export const revertNetwork = (): Promise<InstanceState> =>
  unwrap(http.post("/instance/settings/network/revert"));

export async function installHostHelper(): Promise<HostHelperResult> {
  const response = await http.post("/instance/settings/host-helper");
  const parsed = parseWith(hostHelperEnvelopeSchema, response.data, {
    context: "HostHelperEnvelope",
  });
  return { settings: parsed.settings as InstanceState, log: parsed.log };
}

interface ErrorShape {
  status?: number | null;
  message?: string;
  cause?: {
    response?: {
      data?: { errors?: Record<string, string>; log?: string };
    };
  };
}

export function serverFieldErrors(error: unknown): Record<string, string> {
  return (error as ErrorShape)?.cause?.response?.data?.errors ?? {};
}

export function hostHelperErrorLog(error: unknown): string {
  return (error as ErrorShape)?.cause?.response?.data?.log ?? "";
}

export function isForbidden(error: unknown): boolean {
  return (error as ErrorShape)?.status === 403;
}

export function errorText(error: unknown): string {
  return (error as ErrorShape)?.message ?? "";
}
