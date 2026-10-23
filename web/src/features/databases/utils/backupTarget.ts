import type {
  CreateBackupTargetInput,
  UpdateBackupTargetInput,
} from "@/features/databases/api/backups";

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
