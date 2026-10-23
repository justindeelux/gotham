export { default as TerminalPane } from "./components/TerminalPane.vue";
export { default as TerminalPageDock } from "./components/TerminalPageDock.vue";
export { default as TerminalSessionStage } from "./components/TerminalSessionStage.vue";
export { default as TerminalSurface } from "./components/TerminalSurface.vue";
export { default as TerminalWindow } from "./components/TerminalWindow.vue";
export { useTerminalStore } from "./stores/terminal";
export { useTerminalAppearanceStore } from "./stores/terminalAppearance";
export type {
  TerminalCloseRequest,
  TerminalMode,
  TerminalOpenOptions,
  TerminalTab,
} from "./stores/terminal";
export { useTerminalWindowStore } from "./stores/terminalWindow";
export {
  DEFAULT_TERMINAL_THEME,
  TERMINAL_THEMES,
} from "./schemas/terminalAppearance";
export type {
  TerminalAppearance,
  TerminalThemeName,
} from "./schemas/terminalAppearance";
export { useQuickCommandsStore } from "./stores/quickCommands";
export type { QuickCommand, QuickCommandDraft } from "./schemas/quickCommands";
export type { QuickCommandSuggestion } from "./utils/quickCommandEngine";
export type { TerminalStatus } from "./transport/terminalRuntime";
