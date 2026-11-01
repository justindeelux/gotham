import type {
  CreateStorageInput,
  UpdateStorageInput,
} from "@/features/storages/api/storages";

// Drops blank optional fields so an edit keeps the stored value, while region
// and prefix stay explicit so an empty value clears them. Credentials left
// blank are write-only and keep the sealed value on the server.
export function toProviderBody(
  input: CreateStorageInput | UpdateStorageInput,
): Record<string, unknown> {
  const body: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(input)) {
    if (typeof value === "string") {
      if (value.trim() !== "" || key === "region" || key === "prefix") {
        body[key] = value;
      }
    } else if (value !== undefined) {
      body[key] = value;
    }
  }
  return body;
}
