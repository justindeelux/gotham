import { z } from "zod";

export const toolchainActionSchema = z.enum(["install", "uninstall"]);

export const toolchainToolSchema = z.object({
  id: z.string(),
  name: z.string(),
  description: z.string().optional(),
  category: z.string().optional(),
  installed: z.boolean(),
  version: z.string().optional(),
  path: z.string().optional(),
  installable: z.boolean(),
  uninstallable: z.boolean(),
  needs_root: z.boolean(),
  deps: z.array(z.string()).optional(),
  min_version: z.string().optional(),
  version_ok: z.boolean(),
});

export const toolchainJobSchema = z.object({
  id: z.string(),
  server_id: z.string(),
  tool_id: z.string(),
  action: toolchainActionSchema,
  status: z.enum(["queued", "running", "succeeded", "failed", "canceled"]),
  created_at: z.string(),
  started_at: z.string().optional().nullable(),
  finished_at: z.string().optional().nullable(),
});

export const toolchainToolsEnvelopeSchema = z.object({
  tools: z.array(toolchainToolSchema),
});

export const toolchainJobsEnvelopeSchema = z.object({
  jobs: z.array(toolchainJobSchema),
});

export const toolchainEnqueueEnvelopeSchema = z.object({
  jobs: z.array(toolchainJobSchema),
  skipped: z.array(z.string()),
});

export const toolchainCancelEnvelopeSchema = z.object({
  job: toolchainJobSchema,
});

export const toolchainJobDetailEnvelopeSchema = z.object({
  job: toolchainJobSchema,
  log: z.string(),
  next_seq: z.number(),
});
