import { z } from "zod";

const m = (key: string): string => `instance-settings.validation.${key}`;

export const controlPlaneUrlSchema = z
  .string()
  .trim()
  .refine((value) => {
    if (value === "") {
      return true;
    }
    try {
      const url = new URL(value);
      return (
        (url.protocol === "http:" || url.protocol === "https:") &&
        url.hostname !== "" &&
        url.username === "" &&
        url.password === "" &&
        url.search === "" &&
        url.hash === ""
      );
    } catch {
      return false;
    }
  }, m("url"));

export const instanceNameSchema = z
  .string()
  .trim()
  .min(1, m("name"))
  .max(64, m("name"));

export const cleanupHistoryDaysSchema = z
  .number()
  .int(m("cleanupHistory"))
  .min(7, m("cleanupHistory"))
  .max(3650, m("cleanupHistory"));

export const cleanupBackupFileMinAgeDaysSchema = z
  .number()
  .int(m("cleanupBackupAge"))
  .min(1, m("cleanupBackupAge"))
  .max(365, m("cleanupBackupAge"));

export const cleanupRetentionSchema = z.object({
  history_days: cleanupHistoryDaysSchema,
  backup_file_min_age_days: cleanupBackupFileMinAgeDaysSchema,
});

const settingFieldSchema = z.object({
  value: z.string(),
  source: z.enum(["env", "db", "default"]),
  locked: z.boolean(),
});

export const stateEnvelopeSchema = z.object({
  settings: z.object({
    general: z.object({
      control_plane_url: settingFieldSchema,
      instance_name: settingFieldSchema,
    }),
    pending_control_plane_url: z
      .object({
        previous: z.string().nullable(),
        proposed: z.string(),
        deadline: z.string(),
        synced: z.boolean(),
      })
      .nullable()
      .optional(),
    cleanup: cleanupRetentionSchema,
  }),
});
