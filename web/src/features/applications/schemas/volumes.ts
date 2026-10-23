import { z } from "zod";

import { sourceTypeSchema } from "@/features/applications/schemas/applications";

export const volumeMountSchema = z.object({
  destination: z.string(),
  read_only: z.boolean(),
  service: z.string().optional(),
  container: z.string().optional(),
  state: z.enum(["running", "stopped", "unverified"]),
});
export type VolumeMount = z.infer<typeof volumeMountSchema>;

export const volumeFileManagerSchema = z.object({
  server_id: z.string(),
  root: z.string(),
  path: z.string(),
});
export type VolumeFileManager = z.infer<typeof volumeFileManagerSchema>;

export const volumeSchema = z.object({
  name: z.string(),
  kind: z.enum(["managed", "named", "bind", "tmpfs", "unknown"]),
  host_path: z.string().optional(),
  state: z.enum(["running", "stopped", "unverified"]),
  mounts: z.array(volumeMountSchema),
  file_manager: volumeFileManagerSchema.optional(),
  open_reason: z.string().optional(),
});
export type ApplicationVolume = z.infer<typeof volumeSchema>;

export const volumeListingSchema = z.object({
  source_type: sourceTypeSchema,
  runtime: z.enum(["available", "unavailable", "limited"]),
  reason: z.string().optional(),
  volumes: z.array(volumeSchema),
});
export type VolumeListing = z.infer<typeof volumeListingSchema>;
