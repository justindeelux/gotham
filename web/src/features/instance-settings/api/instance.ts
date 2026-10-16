import { http } from "@/shared/api/http";
import { parseWith } from "@/shared/validation/parse";

import {
  hostHelperEnvelopeSchema,
  stateEnvelopeSchema,
} from "@/features/instance-settings/schemas/instance";

/**
 * Typed client for the instance-settings routes served by `internal/instance`
 * (platform operators only; everyone else receives 403):
 *
 *   GET  /instance/settings
 *   PUT  /instance/settings/general | system | network
 *   POST /instance/settings/network/confirm | revert
 *
 * Invalid input answers 400 `{message, errors: {field: message}}`; the field
 * messages are English server text and are shown as received.
 */

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
  /** Explicit confirmation for a change that would disturb the active
   * interface (static-to-DHCP or a different address/gateway). The server
   * refuses such a change without it; omitted (false) otherwise. */
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

/** Install/probe state of the fixed host helper (JUS-185). */
export type HostHelperState = "ready" | "missing" | "denied";

/** Host readiness: one accurate message per cause instead of one merged blur. */
export interface HostState {
  helper: HostHelperState;
  /** True when the in-product Install can run: the root-owned payload and the
   * hostctl drop-in (with the pinned repair verb) are both present. False means
   * only a control-plane update can help. */
  repair: boolean;
  /** The host runs systemd-networkd + systemd-resolved + networkctl. */
  network_stack: boolean;
  /** The host has systemctl and hostnamectl. */
  system_stack: boolean;
  /** The network section can be applied right now (helper ready). */
  network: boolean;
  /** The system section can be applied right now (helper ready). */
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

/** Result of the in-product host-helper install. */
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

/**
 * installHostHelper installs the fixed host helper and its verb-pinned sudoers
 * rule through the root-owned tool installer, returning the fresh state and the
 * installer output. It is the in-product action offered when the helper is
 * missing or not permitted; the caller re-renders from the returned settings.
 */
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

/** serverFieldErrors extracts the 400 per-field messages ({} when none). */
export function serverFieldErrors(error: unknown): Record<string, string> {
  return (error as ErrorShape)?.cause?.response?.data?.errors ?? {};
}

/** hostHelperErrorLog returns the installer output of a failed install (""). */
export function hostHelperErrorLog(error: unknown): string {
  return (error as ErrorShape)?.cause?.response?.data?.log ?? "";
}

/** isForbidden reports the 403 a non-operator receives. */
export function isForbidden(error: unknown): boolean {
  return (error as ErrorShape)?.status === 403;
}

/** errorText is the server message of a failed call. */
export function errorText(error: unknown): string {
  return (error as ErrorShape)?.message ?? "";
}
