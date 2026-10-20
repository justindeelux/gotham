import { z } from "zod";

/**
 * Terminal session shapes (JUS-245). The HTTP response is untrusted input:
 * the API layer parses it instead of casting, so a control plane that drifts
 * fails loudly at the boundary rather than producing a half-valid tab.
 */

/** TerminalTarget is the execution target; JUS-244 adds containers later. */
export const terminalTargetSchema = z.string().min(1).max(64);

/**
 * terminalSessionSchema validates one created session (201 body). A container
 * session (JUS-244) omits `root`/`cwd` and adds the resolved target identity;
 * the browser never supplies a container id on its own, so these fields are
 * mandatory display metadata only when the control plane returns them.
 */
export const terminalSessionSchema = z.object({
  id: z.string().min(1),
  server_id: z.string().min(1),
  target: terminalTargetSchema,
  // root and cwd are omitted by the control plane when the session was created
  // without a File manager context (the Terminal page, the server detail tab
  // and application container sessions); default them to "" so it stays valid.
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

/** CreateTerminalInput is the body accepted by the host create endpoint. */
export interface CreateTerminalInput {
  target?: string;
  root?: string;
  cwd?: string;
  cols: number;
  rows: number;
}

/**
 * CreateApplicationTerminalInput is the exact container selection the discovery
 * response returned. The control plane re-validates the pair and answers 404
 * "the selected container is no longer available" when it moved.
 */
export interface CreateApplicationTerminalInput {
  service: string;
  containerId: string;
  cols: number;
  rows: number;
}

/** TERMINAL_WINDOW_VERSION is the persisted geometry version. */
export const TERMINAL_WINDOW_VERSION = 1 as const;

/** Terminal window factory geometry; comfortably fits a 1280x800 viewport. */
export const TERMINAL_WINDOW_DEFAULT_WIDTH = 880 as const;
export const TERMINAL_WINDOW_DEFAULT_HEIGHT = 600 as const;

/** Smallest usable terminal window (drag floor). */
export const TERMINAL_WINDOW_MIN_WIDTH = 460 as const;
export const TERMINAL_WINDOW_MIN_HEIGHT = 300 as const;

/** One persisted side never exceeds this, even after a corrupt write. */
export const TERMINAL_WINDOW_MAX_DIMENSION = 4096 as const;

/** Shell chrome the floating window never covers (top bar + bottom gap). */
export const TERMINAL_WINDOW_CHROME_MARGIN = 104 as const;

/** Side gutters mirrored by the pinned window's CSS. */
export const TERMINAL_WINDOW_SIDE_MARGIN = 48 as const;

/** Below this width a dragged offset is meaningless and resets to pinned. */
export const TERMINAL_WINDOW_NARROW_VIEWPORT = 640 as const;

/** terminalWindowSchema validates the persisted geometry fields. */
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

/** DEFAULT_TERMINAL_WINDOW is the factory geometry (pinned default position). */
export const DEFAULT_TERMINAL_WINDOW: TerminalWindowState = {
  width: TERMINAL_WINDOW_DEFAULT_WIDTH,
  height: TERMINAL_WINDOW_DEFAULT_HEIGHT,
  x: null,
  y: null,
  minimized: false,
};

/** terminalWindowSnapshotSchema is the versioned shape written to storage. */
export const terminalWindowSnapshotSchema = terminalWindowSchema.extend({
  version: z.literal(TERMINAL_WINDOW_VERSION),
});

/**
 * parseTerminalWindow resolves untrusted stored data into complete geometry.
 * A wrong version is treated as unreadable (defaults) rather than misread; a
 * missing or mistyped field keeps its default.
 */
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
