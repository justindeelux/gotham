import { http } from "@/shared/api/http";

/** Wire body of GET /version: the running control-plane binary version. */
interface VersionResponse {
  version: string;
}

/** getVersion returns the running control-plane binary version. */
export async function getVersion(): Promise<string> {
  const response = await http.get<VersionResponse>("/version");
  return response.data.version;
}

/**
 * formatVersionTag is the sidebar-head tag text: the v-prefixed release, bare
 * "dev" for dev builds, null while loading or on error (the tag hides). A
 * leading v from the backend is stripped so it never renders vv0.2.0.
 */
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
