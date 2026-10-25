import { z } from "zod";

export const QUICK_COMMAND_VERSION = 1 as const;

export const QUICK_COMMANDS_MAX = 40 as const;

export const QUICK_COMMAND_LABEL_MAX = 64 as const;

export const QUICK_COMMAND_TEXT_MAX = 2048 as const;

export const QUICK_COMMAND_ABBREVIATION_MAX = 24 as const;

export const QUICK_COMMANDS_MAX_BYTES = 512 * 1024;

export const QUICK_COMMAND_ABBREVIATION_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;

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

export const quickCommandLabelSchema = z
  .string({ error: messages.labelRequired })
  .trim()
  .min(1, messages.labelRequired)
  .max(QUICK_COMMAND_LABEL_MAX, messages.labelTooLong);

export const quickCommandTextSchema = z
  .string({
    error: messages.commandRequired,
  })
  .trim()
  .min(1, messages.commandRequired)
  .max(QUICK_COMMAND_TEXT_MAX, messages.commandTooLong)
  .refine((value) => !value.includes("\u0000"), messages.commandNul)
  // eslint-disable-next-line no-control-regex -- deliberate C0/C1 range.
  .refine((value) => !/[\u0000-\u0008\u000b-\u001f\u007f-\u009f]/.test(value), messages.commandControl)
  .refine((value) => !/\p{Bidi_Control}/u.test(value), messages.commandBidi);

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

export const quickCommandIdSchema = z
  .string()
  .min(1, messages.idInvalid)
  .max(64, messages.idInvalid);

export const quickCommandSchema = z.object({
  id: quickCommandIdSchema,
  label: quickCommandLabelSchema,
  command: quickCommandTextSchema,
  abbreviation: quickCommandAbbreviationSchema,
});
export type QuickCommand = z.infer<typeof quickCommandSchema>;

export function quickCommandDraftSchema(
  commands: QuickCommand[],
  editingId = "",
): z.ZodType<QuickCommandDraft> {
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

export interface QuickCommandDraft {
  label: string;
  command: string;
  abbreviation?: string;
}

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

export const quickCommandsSnapshotSchema = z.object({
  version: z.literal(QUICK_COMMAND_VERSION),
  commands: quickCommandListSchema,
});

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

export function newQuickCommandId(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `cmd-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}
