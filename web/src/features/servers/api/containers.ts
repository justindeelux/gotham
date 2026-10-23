import { http } from "@/shared/api/http";
import { activeLocale } from "@/shared/i18n/locale";
import { isApiError, stripErrorPrefix } from "./servers";

import enCatalog from "../locales/en";
import viCatalog from "../locales/vi";

function catalogFor(locale?: string | null): typeof enCatalog {
  return (locale ?? activeLocale.value) === "vi" ? viCatalog : enCatalog;
}

export interface Container {
  id: string;
  name: string;
  image: string;
  state: string;
  status: string;
  ports: string[];
  created?: string;
}

export interface PullImageInput {
  image: string;
}

export interface RunContainerInput {
  image: string;
  name?: string;
  env?: string[];
  command?: string[];
  entrypoint?: string[];
  labels?: Record<string, string>;
  ports?: string[];
  volumes?: string[];
  networks?: string[];
}

interface ContainerListEnvelope {
  containers: Container[];
}

interface ContainerActionEnvelope {
  container_id: string;
}

interface PullImageEnvelope {
  image: string;
}

export type ContainerAction = "start" | "stop" | "restart";

export async function listContainers(serverId: string): Promise<Container[]> {
  const response = await http.get<ContainerListEnvelope>(
    `/servers/${serverId}/containers`,
  );
  return response.data.containers ?? [];
}

async function containerAction(
  serverId: string,
  containerId: string,
  action: ContainerAction,
): Promise<string> {
  const response = await http.post<ContainerActionEnvelope>(
    `/servers/${serverId}/containers/${encodeURIComponent(containerId)}/${action}`,
    {},
  );
  return response.data.container_id;
}

export function startContainer(
  serverId: string,
  containerId: string,
): Promise<string> {
  return containerAction(serverId, containerId, "start");
}

export function stopContainer(
  serverId: string,
  containerId: string,
): Promise<string> {
  return containerAction(serverId, containerId, "stop");
}

export function restartContainer(
  serverId: string,
  containerId: string,
): Promise<string> {
  return containerAction(serverId, containerId, "restart");
}

export async function pullImage(
  serverId: string,
  image: string,
): Promise<string> {
  const response = await http.post<PullImageEnvelope>(
    `/servers/${serverId}/images/pull`,
    { image } satisfies PullImageInput,
  );
  return response.data.image;
}

export async function runContainer(
  serverId: string,
  input: RunContainerInput,
): Promise<string> {
  const response = await http.post<ContainerActionEnvelope>(
    `/servers/${serverId}/containers/run`,
    input,
  );
  return response.data.container_id;
}

export async function startContainerLogStream(
  serverId: string,
  containerId: string,
): Promise<number> {
  const response = await http.post<{ channel: string; replay?: number }>(
    `/servers/${serverId}/containers/${encodeURIComponent(containerId)}/logs/stream`,
    {},
  );
  return response.data.replay ?? 0;
}

export function describeContainerError(error: unknown, locale?: string | null): string {
  const errors = catalogFor(locale).errors;
  if (isApiError(error)) {
    return stripErrorPrefix(error.message) || errors.requestFailed;
  }
  if (error instanceof Error) {
    return stripErrorPrefix(error.message) || errors.unexpected;
  }
  return errors.unexpected;
}
