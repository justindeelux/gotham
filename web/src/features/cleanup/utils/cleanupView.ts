import type { CleanupTier } from "@/features/cleanup/api/cleanup";

const reasonKeys: Record<string, string> = {
  dangling: "cleanup.reasons.dangling",
  "unused build cache": "cleanup.reasons.buildCache",
  "not used by any container": "cleanup.reasons.unusedImage",
  "not owned by any recorded resource": "cleanup.reasons.orphanedVolume",
  "no backups row references this file": "cleanup.reasons.backupFile",
  journal_archived: "cleanup.reasons.journalArchived",
  package_cache: "cleanup.reasons.packageCache",
  offline: "cleanup.reasons.offline",
  busy: "cleanup.reasons.busy",
  unsupported: "cleanup.reasons.unsupported",
  not_found: "cleanup.reasons.not_found",
};

const noticeReasons = new Set(["unsupported", "helper_outdated", "agent_outdated"]);

export function reasonKey(reason: string): string | null {
  if (reasonKeys[reason]) {
    return reasonKeys[reason];
  }
  return reason.startsWith("stopped") ? "cleanup.reasons.stopped" : null;
}

export function noticeKey(reason: string): string | null {
  return noticeReasons.has(reason) ? `cleanup.notices.${reason}` : null;
}

export function tierTagType(tier: CleanupTier): "success" | "warning" | "error" {
  switch (tier) {
    case "safe":
      return "success";
    case "dangerous":
      return "error";
    default:
      return "warning";
  }
}
