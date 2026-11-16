import { http } from "@/shared/api/http";

export interface InstanceInfo {
  version: string;
  port?: number;
}

let instanceInfo: Promise<InstanceInfo> | null = null;

// The sidebar and the topbar both read this endpoint; one request is enough.
export function getInstanceInfo(): Promise<InstanceInfo> {
  instanceInfo ??= http.get<InstanceInfo>("/version").then(
    (response) => response.data,
    (error: unknown) => {
      instanceInfo = null;
      throw error;
    },
  );
  return instanceInfo;
}

export async function getVersion(): Promise<string> {
  return (await getInstanceInfo()).version;
}

export function formatVersionTag(version: string | null | undefined): string | null {
  if (!version) {
    return null;
  }
  const raw = version.startsWith("v") ? version.slice(1) : version;
  if (raw === "") {
    return null;
  }
  return raw === "dev" ? "dev" : `v${raw}`;
}
