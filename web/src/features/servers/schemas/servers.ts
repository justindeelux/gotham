import { z } from "zod";

import {
  NAME_PATTERN,
  USER_PATTERN,
  isValidHost,
} from "@/features/servers/utils/serverValidation";

/**
 * Shared server connection schemas (JUS-23 V3/V4). Both the add-server
 * wizard and the edit-server modal build their Naive rules from this module,
 * so the two forms cannot drift apart. Message strings are preserved
 * verbatim from the pre-zod rules; the wizard and the modal keep their own
 * key-ID strings (they differ), selected via requiredField.
 */
export const serverMessages = {
  nameRequired: "Enter a node name.",
  namePattern: "Letters, digits, dots, dashes, and underscores only.",
  hostRequired: "Enter an IP address or hostname.",
  hostInvalid: "Enter a valid IPv4 address or hostname.",
  portRequired: "Enter an SSH port (1-65535).",
  portRange: "Port must be a number from 1 to 65535.",
  sshUserRequired: "Enter the SSH user.",
  sshUserPattern: "Enter a valid Unix username (lowercase, digits, _, -).",
  keyNameRequired: "Enter a key name.",
  privateKeyRequired: "Paste the PEM-encoded private key.",
  wizardKeyIdRequired: "Enter an existing key ID.",
  editKeyIdRequired: "Enter a key ID.",
  nodePasswordRequired: "Enter the node password.",
} as const;

/**
 * serverNameSchema mirrors the old two-rule field: required (untrimmed, so
 * whitespace-only passes as before) then the trimmed pattern check, which
 * also passes on blank so the required message wins for empty input.
 */
export const serverNameSchema = z
  .string({
    required_error: serverMessages.nameRequired,
    invalid_type_error: serverMessages.nameRequired,
  })
  .min(1, serverMessages.nameRequired)
  .refine((value) => value.trim() === "" || NAME_PATTERN.test(value.trim()), {
    message: serverMessages.namePattern,
  });

/** serverHostSchema accepts an IPv4 literal or a DNS-style hostname. */
export const serverHostSchema = z
  .string({
    required_error: serverMessages.hostRequired,
    invalid_type_error: serverMessages.hostRequired,
  })
  .min(1, serverMessages.hostRequired)
  .refine((value) => isValidHost(value), {
    message: serverMessages.hostInvalid,
  });

/**
 * serverPortSchema keeps the old two-message split: missing/non-numeric
 * input (null from a cleared NInputNumber, NaN, strings) reports the
 * required message, while a present number that is fractional or out of
 * range reports the range message.
 */
export const serverPortSchema = z
  .number({
    required_error: serverMessages.portRequired,
    invalid_type_error: serverMessages.portRequired,
  })
  .superRefine((value, ctx) => {
    if (Number.isNaN(value)) {
      ctx.addIssue({ code: z.ZodIssueCode.custom, message: serverMessages.portRequired });
      return;
    }
    if (!Number.isInteger(value) || value < 1 || value > 65535) {
      ctx.addIssue({ code: z.ZodIssueCode.custom, message: serverMessages.portRange });
    }
  });

export const serverUserSchema = z
  .string({
    required_error: serverMessages.sshUserRequired,
    invalid_type_error: serverMessages.sshUserRequired,
  })
  .min(1, serverMessages.sshUserRequired)
  .refine((value) => value.trim() === "" || USER_PATTERN.test(value.trim()), {
    message: serverMessages.sshUserPattern,
  });

/**
 * requiredField is a conditional-required text field (key name, private key,
 * key ID, node password): present-but-blank is the only failure, using the
 * caller's message. Pair with the adapter's `when` predicate reading the
 * same authMode/keyMode state the old computed rules read.
 */
export function requiredField(message: string): z.ZodString {
  return z
    .string({ required_error: message, invalid_type_error: message })
    .min(1, message);
}
