import { z } from "zod";

export const cleanupTierSchema = z.enum(["safe", "careful", "dangerous"]);

export const cleanupCategorySchema = z.object({
  key: z.string(),
  tier: cleanupTierSchema,
  manual_only: z.boolean(),
});

export const cleanupCategoriesResponseSchema = z.object({
  categories: z.array(cleanupCategorySchema),
});

export const cleanupNoticeSchema = z.object({
  category: z.string(),
  reason: z.string(),
});

export const cleanupItemSchema = z.object({
  id: z.string(),
  category: z.string(),
  tier: cleanupTierSchema,
  name: z.string(),
  size_bytes: z.number(),
  created_at: z.string().optional(),
  reason: z.string().optional(),
});

export const cleanupNodeSchema = z.object({
  server_id: z.string(),
  status: z.string(),
  reason: z.string().optional(),
  items: z.array(cleanupItemSchema).optional(),
  notices: z.array(cleanupNoticeSchema).optional(),
});

export const cleanupScanResponseSchema = z.object({
  nodes: z.array(cleanupNodeSchema),
});

export const cleanupRunItemSchema = z.object({
  id: z.string(),
  category: z.string(),
  name: z.string(),
  status: z.string(),
  message: z.string().optional(),
  reclaimed_bytes: z.number(),
});

export const cleanupRunResponseSchema = z.object({
  server_id: z.string(),
  status: z.string(),
  reason: z.string().optional(),
  run_id: z.string().optional(),
  reclaimed_bytes: z.number(),
  items: z.array(cleanupRunItemSchema).optional(),
});
