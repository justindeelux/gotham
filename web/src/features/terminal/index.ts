/**
 * Terminal feature public surface (JUS-245).
 *
 * Entry points added by later packages (standalone page, server detail tab,
 * application tab, file manager) import from here only:
 *
 * ```ts
 * const terminal = useTerminalStore();
 * const tab = await terminal.openPageSession({ serverId, serverName, root, cwd });
 * // mount <TerminalPane :tab-id="tab.id" /> inside the page/tab
 * const win = await terminal.openWindowSession({ serverId, serverName, root, cwd });
 * ```
 *
 * `openPageSession` is idempotent per server+target+root+cwd; a detached tab
 * is returned unchanged (its pane shows the one-way notice) and no duplicate
 * shell is created. `openWindowSession` always starts a new independent shell.
 * Closing always goes through `requestClose` so the shared confirmation runs.
 */
export { default as TerminalPane } from "./components/TerminalPane.vue";
export { default as TerminalSurface } from "./components/TerminalSurface.vue";
export { default as TerminalWindow } from "./components/TerminalWindow.vue";
export { useTerminalStore } from "./stores/terminal";
export type {
  TerminalCloseRequest,
  TerminalMode,
  TerminalOpenOptions,
  TerminalTab,
} from "./stores/terminal";
export { useTerminalWindowStore } from "./stores/terminalWindow";
export type { TerminalStatus } from "./transport/terminalRuntime";
