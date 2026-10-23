import { http } from "@/shared/api/http";

interface VersionResponse {
  version: string;
}

export async function getVersion(): Promise<string> {
  const response = await http.get<VersionResponse>("/version");
  return response.data.version;
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
