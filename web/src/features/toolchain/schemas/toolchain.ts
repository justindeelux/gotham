import { z } from "zod";

/** Tool action accepted by POST .../toolchain/jobs. */
export const toolchainActionSchema = z.enum(["install", "uninstall"]);

/** One catalog entry with its detected state on a node. */
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

/** One queued install/uninstall job (never carries the log). */
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

/** GET .../tools body. */
export const toolchainToolsEnvelopeSchema = z.object({
  tools: z.array(toolchainToolSchema),
});

/** GET .../toolchain/jobs body. */
export const toolchainJobsEnvelopeSchema = z.object({
  jobs: z.array(toolchainJobSchema),
});

/** POST .../toolchain/jobs body. */
export const toolchainEnqueueEnvelopeSchema = z.object({
  jobs: z.array(toolchainJobSchema),
  skipped: z.array(z.string()),
});

/** POST .../jobs/{jobID}/cancel body. */
export const toolchainCancelEnvelopeSchema = z.object({
  job: toolchainJobSchema,
});

/** GET .../jobs/{jobID} body: job plus a log page. */
export const toolchainJobDetailEnvelopeSchema = z.object({
  job: toolchainJobSchema,
  log: z.string(),
  next_seq: z.number(),
});
