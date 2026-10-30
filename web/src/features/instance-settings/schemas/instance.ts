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
  }),
});
