import { isApiError } from "@/features/servers";
import { http } from "@/shared/api/http";
import { i18n } from "@/shared/i18n";

export type ContainerFileKind = "file" | "directory" | "symlink" | "special";

export interface ContainerFileEntry {
  path: string;
  name: string;
  kind: ContainerFileKind;
  size: number;
  mode: number;
  uid: number;
  gid: number;
  modified_at?: string;
  symlink_target?: string;
}

export interface ContainerFileListing {
  directory: ContainerFileEntry;
  entries: ContainerFileEntry[];
  truncated: boolean;
  cursor?: string;
}

export interface ContainerFileCapabilities {
  readable: boolean;
  exec_available: boolean;
  exec_blocked_by?: "not_running" | "no_shell" | "unsupported" | string;
  state: string;
}

export type ContainerFileAction =
  | "mkdir"
  | "rename"
  | "move"
  | "copy"
  | "delete"
  | "chmod";

export interface ContainerFileChange {
  path: string;
  destination?: string;
  overwrite?: boolean;
  mode?: number;
}

function containerFilesBase(appId: string, containerId: string): string {
  return `/applications/${appId}/containers/${encodeURIComponent(containerId)}/files`;
}

export async function listContainerFiles(
  appId: string,
  containerId: string,
  path: string,
  cursor = "",
): Promise<ContainerFileListing> {
  const response = await http.get<ContainerFileListing>(containerFilesBase(appId, containerId), {
    params: { path, cursor: cursor || undefined },
  });
  return {
    directory: response.data.directory,
    entries: response.data.entries ?? [],
    truncated: response.data.truncated === true,
    cursor: response.data.cursor,
  };
}

export async function containerFileCapabilities(
  appId: string,
  containerId: string,
): Promise<ContainerFileCapabilities> {
  const response = await http.get<ContainerFileCapabilities>(
    `${containerFilesBase(appId, containerId)}/capabilities`,
  );
  return response.data;
}

export async function statContainerFile(
  appId: string,
  containerId: string,
  path: string,
): Promise<ContainerFileEntry> {
  const response = await http.get<ContainerFileEntry>(
    `${containerFilesBase(appId, containerId)}/stat`,
    { params: { path } },
  );
  return response.data;
}

export interface ContainerFileText {
  text: string;
  isText: boolean;
}

// readContainerFile decodes the bytes as strict UTF-8: the editor must never
// round-trip a binary file through a lossy string and save the damage back.
export async function readContainerFile(
  appId: string,
  containerId: string,
  path: string,
): Promise<ContainerFileText> {
  const response = await http.get<ArrayBuffer>(`${containerFilesBase(appId, containerId)}/content`, {
    params: { path },
    responseType: "arraybuffer",
    timeout: 0,
  });
  const bytes = new Uint8Array(response.data);
  let text: string;
  try {
    text = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch {
    return { text: "", isText: false };
  }
  if (text.includes("\u0000")) {
    return { text: "", isText: false };
  }
  return { text, isText: true };
}

// destinationExists tells the copy/move overwrite question apart from every
// other conflict (for example copying a folder into itself).
export function destinationExists(error: unknown): boolean {
  return isApiError(error) && error.status === 409 && /already exists/i.test(error.message);
}

// isContainerNotRunning reads the stable code the control plane adds when the
// listing needs a running container.
export function isContainerNotRunning(error: unknown): boolean {
  return isApiError(error) && error.code === "not_running";
}

export async function writeContainerFile(
  appId: string,
  containerId: string,
  path: string,
  content: string,
): Promise<void> {
  await http.put(`${containerFilesBase(appId, containerId)}/content`, content, {
    params: { path },
    headers: { "Content-Type": "text/plain; charset=utf-8" },
    timeout: 0,
  });
}

export async function uploadContainerFile(
  appId: string,
  containerId: string,
  directory: string,
  file: File,
): Promise<void> {
  await http.put(`${containerFilesBase(appId, containerId)}/upload`, file, {
    params: { path: directory, name: file.name },
    headers: { "Content-Type": "application/octet-stream" },
    timeout: 0,
  });
}

export async function changeContainerFiles(
  appId: string,
  containerId: string,
  action: ContainerFileAction,
  change: ContainerFileChange,
): Promise<void> {
  if (action === "delete") {
    await http.delete(containerFilesBase(appId, containerId), { params: { path: change.path } });
    return;
  }
  await http.request({
    method: action === "chmod" ? "put" : "post",
    url: `${containerFilesBase(appId, containerId)}/${action}`,
    data: {
      path: change.path,
      destination: change.destination ?? "",
      overwrite: change.overwrite === true,
      mode: change.mode ?? 0,
    },
  });
}

export async function downloadContainerFile(
  appId: string,
  containerId: string,
  path: string,
  name: string,
): Promise<void> {
  const response = await http.get<Blob>(`${containerFilesBase(appId, containerId)}/download`, {
    params: { path },
    responseType: "blob",
    timeout: 0,
  });
  const url = URL.createObjectURL(response.data);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = name;
  anchor.rel = "noopener";
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  setTimeout(() => URL.revokeObjectURL(url), 0);
}

// describeContainerFileError keeps the localized summary and appends the
// server's own explanation, the same way the application errors do.
export function describeContainerFileError(error: unknown): string {
  const t = i18n.global.t.bind(i18n.global);
  if (isApiError(error)) {
    if (error.status === 401) {
      return String(t("applications.errors.sessionExpired"));
    }
    if (error.status === 403) {
      return String(t("applications.files.errors.forbidden"));
    }
    if (error.status === 404) {
      return String(t("applications.files.errors.notFound"));
    }
    if (error.status === 409) {
      return String(t("applications.files.errors.conflict"));
    }
    if (error.status === 413) {
      return String(t("applications.files.errors.tooLarge"));
    }
    if (error.status === 503) {
      return String(t("applications.files.errors.unavailable"));
    }
    return withDetail(String(t("applications.files.errors.failed")), error.message);
  }
  if (error instanceof Error) {
    return withDetail(String(t("common.errors.unexpected")), error.message);
  }
  return String(t("common.errors.unexpected"));
}

function withDetail(summary: string, detail: string): string {
  const cleaned = detail.trim();
  if (cleaned === "" || cleaned === summary) {
    return summary;
  }
  return `${summary} ${cleaned}`;
}
