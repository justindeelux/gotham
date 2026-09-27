import { http } from "./http";
import { isApiError } from "./servers";

/**
 * Typed client for the container routes served by `internal/containers`:
 *
 *   GET  /servers/{id}/containers
 *   POST /servers/{id}/containers/{containerID}/start
 *   POST /servers/{id}/containers/{containerID}/stop
 *   POST /servers/{id}/containers/{containerID}/restart
 *   POST /servers/{id}/images/pull
 *   POST /servers/{id}/containers/run
 *
 * Paths are relative to the shared axios instance (`baseURL: /api/v1`), so the
 * auth header and refresh-on-401 behaviour come from `./http` unchanged.
 */

/**
 * A container running on a managed node, as mapped by the control plane.
 *
 * `state` is the raw Docker lifecycle state; `status` is its human string
 * (e.g. "Up 2 hours", "Exited (1) 3 hours ago"); `ports` carries operator
 * declared mappings from the `gotham.ports` label until the agent contract
 * transports Docker port bindings (see `internal/containers/container.go`).
 */
export interface Container {
  id: string;
  name: string;
  image: string;
  state: string;
  status: string;
  ports: string[];
  created?: string;
}

/** Body accepted by POST /servers/{id}/images/pull. */
export interface PullImageInput {
  image: string;
}

/**
 * Body accepted by POST /servers/{id}/containers/run. Mirrors the control-plane
 * `RunOptions` DTO; `image` is the only required field.
 */
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

/** Wire envelope for a container list. */
interface ContainerListEnvelope {
  containers: Container[];
}

/** Wire envelope for a container action, reporting the targeted container. */
interface ContainerActionEnvelope {
  container_id: string;
}

/** Wire envelope echoing the pulled image. */
interface PullImageEnvelope {
  image: string;
}

/** The supported per-row container actions. */
export type ContainerAction = "start" | "stop" | "restart";

/** listContainers returns every container on one server. */
export async function listContainers(serverId: string): Promise<Container[]> {
  const response = await http.get<ContainerListEnvelope>(
    `/servers/${serverId}/containers`,
  );
  return response.data.containers ?? [];
}

/**
 * containerAction runs start/stop/restart for one container. All three share
 * the same route shape and response, so they funnel through this helper.
 */
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

/** startContainer starts one container. */
export function startContainer(
  serverId: string,
  containerId: string,
): Promise<string> {
  return containerAction(serverId, containerId, "start");
}

/** stopContainer stops one container. */
export function stopContainer(
  serverId: string,
  containerId: string,
): Promise<string> {
  return containerAction(serverId, containerId, "stop");
}

/** restartContainer restarts one container. */
export function restartContainer(
  serverId: string,
  containerId: string,
): Promise<string> {
  return containerAction(serverId, containerId, "restart");
}

/** pullImage pulls an image onto one server; the request is accepted async. */
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

/** runContainer creates and starts a raw container, returning its id. */
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

/** describeContainerError maps a thrown error to a user-facing message. */
export function describeContainerError(error: unknown): string {
  if (isApiError(error)) {
    return error.message || "Request failed";
  }
  if (error instanceof Error) {
    return error.message;
  }
  return "Something went wrong. Please try again.";
}
