import { http } from "@/shared/api/http";
import { parseWith } from "@/shared/validation/parse";

import {
  cleanupCategoriesResponseSchema,
  cleanupItemSchema,
  cleanupRunItemSchema,
  cleanupRunResponseSchema,
  cleanupScanResponseSchema,
} from "../schemas/cleanup";
import type { z } from "zod";

export type CleanupTier = "safe" | "careful" | "dangerous";

export interface CleanupCategory {
  key: string;
  tier: CleanupTier;
  manual_only: boolean;
}

export interface CleanupItem {
  id: string;
  category: string;
  tier: CleanupTier;
  name: string;
  size_bytes: number;
  created_at: string;
  reason: string;
}

export interface CleanupNotice {
  category: string;
  reason: string;
}

export interface CleanupNodeScan {
  server_id: string;
  status: string;
  reason: string;
  items: CleanupItem[];
  notices: CleanupNotice[];
}

export interface CleanupRunItem {
  id: string;
  category: string;
  name: string;
  status: string;
  message: string;
  reclaimed_bytes: number;
}

export interface CleanupRunResult {
  server_id: string;
  status: string;
  reason: string;
  run_id?: string;
  reclaimed_bytes: number;
  items: CleanupRunItem[];
}

type ScanNode = z.infer<typeof cleanupScanResponseSchema>["nodes"][number];
type ScanItem = z.infer<typeof cleanupItemSchema>;
type RunItem = z.infer<typeof cleanupRunItemSchema>;

function normalizeItem(item: ScanItem): CleanupItem {
  return {
    id: item.id,
    category: item.category,
    tier: item.tier,
    name: item.name,
    size_bytes: item.size_bytes,
    created_at: item.created_at ?? "",
    reason: item.reason ?? "",
  };
}

function normalizeNode(node: ScanNode): CleanupNodeScan {
  return {
    server_id: node.server_id,
    status: node.status,
    reason: node.reason ?? "",
    items: (node.items ?? []).map(normalizeItem),
    notices: (node.notices ?? []).map((notice) => ({
      category: notice.category,
      reason: notice.reason,
    })),
  };
}

function normalizeRunItem(item: RunItem): CleanupRunItem {
  return {
    id: item.id,
    category: item.category,
    name: item.name,
    status: item.status,
    message: item.message ?? "",
    reclaimed_bytes: item.reclaimed_bytes,
  };
}

export async function listCleanupCategories(): Promise<CleanupCategory[]> {
  const response = await http.get<unknown>("/cleanup/categories");
  const parsed = parseWith(
    cleanupCategoriesResponseSchema,
    response.data,
    { context: "cleanup categories" },
  );
  return parsed.categories ?? [];
}

export async function scanNode(
  serverId: string,
  minAgeHours: number,
): Promise<CleanupNodeScan> {
  const response = await http.post<unknown>("/cleanup/scan", {
    server_ids: [serverId],
    min_age_hours: minAgeHours,
  });
  const parsed = parseWith(cleanupScanResponseSchema, response.data, {
    context: "cleanup scan",
  });
  const node = parsed.nodes?.[0];
  if (!node) {
    return {
      server_id: serverId,
      status: "failed",
      reason: "no response",
      items: [],
      notices: [],
    };
  }
  return normalizeNode(node);
}

export async function cleanNode(
  serverId: string,
  ids: string[],
  minAgeHours: number,
): Promise<CleanupRunResult> {
  const response = await http.post<unknown>("/cleanup/clean", {
    server_id: serverId,
    ids,
    min_age_hours: minAgeHours,
  });
  const parsed = parseWith(cleanupRunResponseSchema, response.data, {
    context: "cleanup run",
  });
  return {
    server_id: parsed.server_id,
    status: parsed.status,
    reason: parsed.reason ?? "",
    run_id: parsed.run_id,
    reclaimed_bytes: parsed.reclaimed_bytes,
    items: (parsed.items ?? []).map(normalizeRunItem),
  };
}
