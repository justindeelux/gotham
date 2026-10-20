import { z } from "zod";

/**
 * Saved terminal quick commands (JUS-249).
 *
 * The stored payload is untrusted: it can be edited by hand, written by an
 * older build, or truncated by a full disk. `parseQuickCommands` validates
 * every entry and drops anything unusable instead of failing the whole list.
 * Abbreviations are unique per user (case-insensitive) so a typed shortcut can
 * never be ambiguous.
 */

/** QUICK_COMMAND_VERSION is the persisted payload version. */
export const QUICK_COMMAND_VERSION = 1 as const;

/** QUICK_COMMANDS_MAX bounds one user's saved list. */
export const QUICK_COMMANDS_MAX = 40 as const;

/** QUICK_COMMAND_LABEL_MAX bounds the human label. */
export const QUICK_COMMAND_LABEL_MAX = 64 as const;

/** QUICK_COMMAND_TEXT_MAX bounds the inserted command text (characters). */
export const QUICK_COMMAND_TEXT_MAX = 2048 as const;

/** QUICK_COMMAND_ABBREVIATION_MAX bounds the typed shortcut. */
export const QUICK_COMMAND_ABBREVIATION_MAX = 24 as const;

/** QUICK_COMMANDS_MAX_BYTES rejects a runaway stored payload. */
export const QUICK_COMMANDS_MAX_BYTES = 512 * 1024;

/**
 * QUICK_COMMAND_ABBREVIATION_PATTERN keeps shortcuts unambiguous in a shell
 * line: no spaces, quotes, globs or control characters, so replacing one is
 * always a plain delete of the typed characters.
 */
export const QUICK_COMMAND_ABBREVIATION_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;

/** quickCommandMessages are catalog keys, resolved by the shared adapter. */
const messages = {
  idInvalid: "terminal.quickCommands.validation.idInvalid",
  labelRequired: "terminal.quickCommands.validation.labelRequired",
  labelTooLong: "terminal.quickCommands.validation.labelTooLong",
  commandRequired: "terminal.quickCommands.validation.commandRequired",
  commandTooLong: "terminal.quickCommands.validation.commandTooLong",
  commandNul: "terminal.quickCommands.validation.commandNul",
  commandControl: "terminal.quickCommands.validation.commandControl",
  commandBidi: "terminal.quickCommands.validation.commandBidi",
  abbreviationTooLong: "terminal.quickCommands.validation.abbreviationTooLong",
  abbreviationFormat: "terminal.quickCommands.validation.abbreviationFormat",
  abbreviationDuplicate: "terminal.quickCommands.validation.abbreviationDuplicate",
  tooMany: "terminal.quickCommands.validation.tooMany",
} as const;

/** quickCommandLabelSchema requires a trimmed, bounded label. */
export const quickCommandLabelSchema = z
  .string({ required_error: messages.labelRequired, invalid_type_error: messages.labelRequired })
  .trim()
  .min(1, messages.labelRequired)
  .max(QUICK_COMMAND_LABEL_MAX, messages.labelTooLong);

/** quickCommandTextSchema requires trimmed, bounded command text. */
export const quickCommandTextSchema = z
  .string({
    required_error: messages.commandRequired,
    invalid_type_error: messages.commandRequired,
  })
  .trim()
  .min(1, messages.commandRequired)
  .max(QUICK_COMMAND_TEXT_MAX, messages.commandTooLong)
  .refine((value) => !value.includes("\u0000"), messages.commandNul)
  // Newline and tab are the only controls insertion can handle (inside
  // bracketed paste). Reject the rest at save time for earlier feedback;
  // prepareInsertion still refuses them for legacy stored data.
  // eslint-disable-next-line no-control-regex -- deliberate C0/C1 range.
  .refine((value) => !/[\u0000-\u0008\u000b-\u001f\u007f-\u009f]/.test(value), messages.commandControl)
  .refine((value) => !/\p{Bidi_Control}/u.test(value), messages.commandBidi);

/**
 * quickCommandAbbreviationSchema normalises an empty optional field to
 * undefined and validates the typed shortcut alphabet.
 */
export const quickCommandAbbreviationSchema = z.preprocess(
  (value) => {
    if (typeof value !== "string") {
      return value;
    }
    const trimmed = value.trim();
    return trimmed === "" ? undefined : trimmed;
  },
  z
    .string()
    .max(QUICK_COMMAND_ABBREVIATION_MAX, messages.abbreviationTooLong)
    .regex(QUICK_COMMAND_ABBREVIATION_PATTERN, messages.abbreviationFormat)
    .optional(),
);

/** quickCommandIdSchema guards a persisted id. */
export const quickCommandIdSchema = z
  .string()
  .min(1, messages.idInvalid)
  .max(64, messages.idInvalid);

/** quickCommandSchema is one saved command. */
export const quickCommandSchema = z.object({
  id: quickCommandIdSchema,
  label: quickCommandLabelSchema,
  command: quickCommandTextSchema,
  abbreviation: quickCommandAbbreviationSchema,
});
export type QuickCommand = z.infer<typeof quickCommandSchema>;

/**
 * quickCommandDraftSchema validates a form draft against the current list.
 * The edit target keeps its own abbreviation: re-saving an unchanged command
 * is not a duplicate of itself.
 */
export function quickCommandDraftSchema(
  commands: QuickCommand[],
  editingId = "",
): z.ZodType<QuickCommandDraft, z.ZodTypeDef, unknown> {
  return z
    .object({
      label: quickCommandLabelSchema,
      command: quickCommandTextSchema,
      abbreviation: quickCommandAbbreviationSchema,
    })
    .superRefine((draft, context) => {
      const abbreviation = draft.abbreviation;
      if (!abbreviation) {
        return;
      }
      const normalized = abbreviation.toLowerCase();
      const duplicate = commands.some(
        (command) =>
          command.id !== editingId &&
          command.abbreviation?.toLowerCase() === normalized,
      );
      if (duplicate) {
        context.addIssue({
          code: z.ZodIssueCode.custom,
          path: ["abbreviation"],
          message: messages.abbreviationDuplicate,
        });
      }
    });
}

/** quickCommandDraftSchemaForField exposes the abbreviation rule to Naive UI. */
export function quickCommandAbbreviationFieldSchema(
  commands: QuickCommand[],
  editingId = "",
): z.ZodType<string | undefined> {
  return quickCommandAbbreviationSchema.superRefine((value, context) => {
    if (!value) {
      return;
    }
    const normalized = value.toLowerCase();
    const duplicate = commands.some(
      (command) =>
        command.id !== editingId &&
        command.abbreviation?.toLowerCase() === normalized,
    );
    if (duplicate) {
      context.addIssue({
        code: z.ZodIssueCode.custom,
        message: messages.abbreviationDuplicate,
      });
    }
  }) as z.ZodType<string | undefined>;
}

/** QuickCommandDraft is the add/edit form shape. */
export interface QuickCommandDraft {
  label: string;
  command: string;
  abbreviation?: string;
}

/** quickCommandListSchema validates a complete list (used before writing). */
export const quickCommandListSchema = z
  .array(quickCommandSchema)
  .max(QUICK_COMMANDS_MAX, messages.tooMany)
  .superRefine((commands, context) => {
    const seen = new Set<string>();
    for (const [index, command] of commands.entries()) {
      const abbreviation = command.abbreviation?.toLowerCase();
      if (!abbreviation) {
        continue;
      }
      if (seen.has(abbreviation)) {
        context.addIssue({
          code: z.ZodIssueCode.custom,
          path: [index, "abbreviation"],
          message: messages.abbreviationDuplicate,
        });
      }
      seen.add(abbreviation);
    }
  });

/** quickCommandsSnapshotSchema is the versioned shape written to storage. */
export const quickCommandsSnapshotSchema = z.object({
  version: z.literal(QUICK_COMMAND_VERSION),
  commands: quickCommandListSchema,
});

/**
 * parseQuickCommands resolves an untrusted stored payload into a safe list.
 * A wrong version, a non-array or a malformed entry is dropped rather than
 * poisoning the whole list; duplicate abbreviations keep the first entry so a
 * suggestion is never ambiguous. The list is capped at QUICK_COMMANDS_MAX.
 */
export function parseQuickCommands(data: unknown): QuickCommand[] {
  if (typeof data !== "object" || data === null) {
    return [];
  }
  const record = data as { version?: unknown; commands?: unknown };
  if (record.version !== QUICK_COMMAND_VERSION || !Array.isArray(record.commands)) {
    return [];
  }
  const parsed: QuickCommand[] = [];
  const seen = new Set<string>();
  for (const raw of record.commands.slice(0, QUICK_COMMANDS_MAX)) {
    const result = quickCommandSchema.safeParse(raw);
    if (!result.success) {
      continue;
    }
    const command = result.data;
    const abbreviation = command.abbreviation?.toLowerCase();
    if (abbreviation) {
      if (seen.has(abbreviation)) {
        continue;
      }
      seen.add(abbreviation);
    }
    parsed.push(command);
  }
  return parsed;
}

/** newQuickCommandId returns a collision-resistant local id. */
export function newQuickCommandId(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `cmd-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}
