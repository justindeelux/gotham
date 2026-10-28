import { z } from "zod";

const pathSchema = z
  .string()
  .trim()
  .max(512)
  .refine(
    (value) =>
      value === "" ||
      (value.startsWith("/") &&
        !value.includes("?") &&
        !value.includes("#") &&
        !value.includes("//") &&
        !/\s/.test(value)),
    { message: "applications.websiteCheck.pathInvalid" },
  );

const statusSchema = z
  .number()
  .int()
  .min(100, { message: "applications.websiteCheck.statusInvalid" })
  .max(599, { message: "applications.websiteCheck.statusInvalid" });

export const websiteCheckSchema = z
  .object({
    enabled: z.boolean(),
    path: pathSchema,
    expected_min: statusSchema,
    expected_max: statusSchema,
    timeout_seconds: z
      .number()
      .int()
      .min(1, { message: "applications.websiteCheck.timeoutInvalid" })
      .max(30, { message: "applications.websiteCheck.timeoutInvalid" }),
  })
  .refine((value) => value.expected_min <= value.expected_max, {
    message: "applications.websiteCheck.statusInvalid",
    path: ["expected_max"],
  });
