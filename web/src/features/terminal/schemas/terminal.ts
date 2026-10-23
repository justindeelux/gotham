import { z } from "zod";

export const terminalTargetSchema = z.string().min(1).max(64);

export const terminalSessionSchema = z.object({
  id: z.string().min(1),
  server_id: z.string().min(1),
  target: terminalTargetSchema,
  root: z.string().default(""),
  cwd: z.string().default(""),
  cols: z.number().int().min(1).max(1000),
  rows: z.number().int().min(1).max(1000),
  expires_at: z.string().min(1),
  websocket_path: z.string().startsWith("/"),
  subprotocol: z.literal("gotham.terminal.v1"),
  application_id: z.string().min(1).optional(),
  service: z.string().optional(),
  container_id: z.string().min(1).optional(),
  container_name: z.string().min(1).optional(),
  target_label: z.string().min(1).optional(),
});
export type TerminalSessionInfo = z.infer<typeof terminalSessionSchema>;

export const applicationTerminalTargetSchema = z.object({
  service: z.string(),
  container_id: z.string().min(1),
  name: z.string(),
  image: z.string(),
  state: z.string(),
  replica: z.number().int().min(0),
  label: z.string(),
});
export type ApplicationTerminalTarget = z.infer<
  typeof applicationTerminalTargetSchema
>;

export const applicationTerminalTargetsSchema = z.object({
  application_id: z.string().min(1),
  application_name: z.string(),
  server_id: z.string().min(1),
  source_type: z.string(),
  containers: z.array(applicationTerminalTargetSchema),
});
export type ApplicationTerminalTargets = z.infer<
  typeof applicationTerminalTargetsSchema
>;

export const terminalHostTargetSchema = z.object({
  id: z.string().min(1),
  name: z.string(),
  status: z.string(),
});
export type TerminalHostTarget = z.infer<typeof terminalHostTargetSchema>;

export const terminalTargetGroupSchema = z.object({
  application_id: z.string().min(1),
  application_name: z.string(),
  server_id: z.string().min(1),
  source_type: z.string(),
  containers: z.array(applicationTerminalTargetSchema),
});
export type TerminalTargetGroup = z.infer<typeof terminalTargetGroupSchema>;

export const terminalTargetErrorCodeSchema = z.enum([
  "unavailable",
  "disabled",
  "internal",
  "timeout",
]);
export type TerminalTargetErrorCode = z.infer<
  typeof terminalTargetErrorCodeSchema
>;

export const terminalTargetErrorSchema = z.object({
  source: z.string(),
  id: z.string().optional(),
  name: z.string().optional(),
  code: terminalTargetErrorCodeSchema,
  count: z.number().int().min(1).optional(),
});
export type TerminalTargetError = z.infer<typeof terminalTargetErrorSchema>;

export const terminalTargetsSchema = z.object({
  servers: z.array(terminalHostTargetSchema),
  applications: z.array(terminalTargetGroupSchema),
  errors: z.array(terminalTargetErrorSchema),
  truncated: z.boolean(),
});
export type TerminalTargets = z.infer<typeof terminalTargetsSchema>;

export interface CreateTerminalInput {
  target?: string;
  root?: string;
  cwd?: string;
  cols: number;
  rows: number;
}

export interface CreateApplicationTerminalInput {
  service: string;
  containerId: string;
  cols: number;
  rows: number;
}

export const TERMINAL_WINDOW_VERSION = 1 as const;

export const TERMINAL_WINDOW_DEFAULT_WIDTH = 880 as const;
export const TERMINAL_WINDOW_DEFAULT_HEIGHT = 600 as const;

export const TERMINAL_WINDOW_MIN_WIDTH = 460 as const;
export const TERMINAL_WINDOW_MIN_HEIGHT = 300 as const;

export const TERMINAL_WINDOW_MAX_DIMENSION = 4096 as const;

export const TERMINAL_WINDOW_CHROME_MARGIN = 104 as const;

export const TERMINAL_WINDOW_SIDE_MARGIN = 48 as const;

export const TERMINAL_WINDOW_NARROW_VIEWPORT = 640 as const;

export const terminalWindowSchema = z.object({
  width: z
    .number()
    .int()
    .min(TERMINAL_WINDOW_MIN_WIDTH)
    .max(TERMINAL_WINDOW_MAX_DIMENSION)
    .catch(TERMINAL_WINDOW_DEFAULT_WIDTH),
  height: z
    .number()
    .int()
    .min(TERMINAL_WINDOW_MIN_HEIGHT)
    .max(TERMINAL_WINDOW_MAX_DIMENSION)
    .catch(TERMINAL_WINDOW_DEFAULT_HEIGHT),
  x: z.number().int().min(0).max(8192).nullable().catch(null).default(null),
  y: z.number().int().min(0).max(8192).nullable().catch(null).default(null),
  minimized: z.boolean().catch(false),
});
export type TerminalWindowState = z.infer<typeof terminalWindowSchema>;

export const DEFAULT_TERMINAL_WINDOW: TerminalWindowState = {
  width: TERMINAL_WINDOW_DEFAULT_WIDTH,
  height: TERMINAL_WINDOW_DEFAULT_HEIGHT,
  x: null,
  y: null,
  minimized: false,
};

export const terminalWindowSnapshotSchema = terminalWindowSchema.extend({
  version: z.literal(TERMINAL_WINDOW_VERSION),
});

export function parseTerminalWindow(data: unknown): TerminalWindowState {
  if (typeof data !== "object" || data === null) {
    return DEFAULT_TERMINAL_WINDOW;
  }
  if ((data as { version?: unknown }).version !== TERMINAL_WINDOW_VERSION) {
    return DEFAULT_TERMINAL_WINDOW;
  }
  const parsed = terminalWindowSchema.safeParse(data);
  return parsed.success ? parsed.data : DEFAULT_TERMINAL_WINDOW;
}
