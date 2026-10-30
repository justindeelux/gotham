import { http } from "@/shared/api/http";
import { parseWith } from "@/shared/validation/parse";

import { stateEnvelopeSchema } from "@/features/instance-settings/schemas/instance";

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
}

export interface GeneralInput {
  control_plane_url: string;
  instance_name: string;
}

export interface PendingControlPlaneURL {
  previous: string | null;
  proposed: string;
  deadline: string;
  synced: boolean;
}

export interface InstanceState {
  general: GeneralSettings;
  pending_control_plane_url?: PendingControlPlaneURL | null;
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

export const confirmControlPlaneURL = (): Promise<InstanceState> =>
  unwrap(http.post("/instance/settings/control-plane-url/confirm"));

export const revertControlPlaneURL = (): Promise<InstanceState> =>
  unwrap(http.post("/instance/settings/control-plane-url/revert"));
