import { z } from "zod";

import { requiredString } from "@/shared/validation/primitives";

// A new folder or a rename takes one name, never a path.
export const containerFileNameSchema = requiredString(
  "applications.files.prompt.required",
).refine(
  (value) => !value.includes("/") && value !== "." && value !== "..",
  "applications.files.prompt.invalidName",
);

// The destination of a copy or a move is an absolute path inside the container.
export const containerFileDestinationSchema = requiredString(
  "applications.files.prompt.required",
).refine(
  (value) => value.startsWith("/") && !value.split("/").includes(".."),
  "applications.files.prompt.invalidPath",
);

export const containerFileModeSchema = z
  .string()
  .trim()
  .regex(/^[0-7]{3,4}$/, "applications.files.prompt.modeHint");
