import type {
  CreateProviderInput,
  UpdateProviderInput,
} from "@/features/backups/api/backups";

export function toProviderBody(
  input: CreateProviderInput | UpdateProviderInput,
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
