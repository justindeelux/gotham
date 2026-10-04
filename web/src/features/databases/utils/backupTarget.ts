import type {
  CreateBackupTargetInput,
  UpdateBackupTargetInput,
} from "@/features/databases/api/backups";

/**
 * toTargetBody builds the create/update body for a storage target. Blank
 * optional fields are dropped so the server keeps the stored value, except
 * region and prefix: those are clearable, so an explicit empty string is sent
 * as a clear marker instead of being silently preserved.
 */
export function toTargetBody(
  input: CreateBackupTargetInput | UpdateBackupTargetInput,
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
