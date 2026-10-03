import { http } from "./http";

/** Wire body of GET /version: the running control-plane binary version. */
interface VersionResponse {
  version: string;
}

/** getVersion returns the running control-plane binary version. */
export async function getVersion(): Promise<string> {
  const response = await http.get<VersionResponse>("/version");
  return response.data.version;
}
