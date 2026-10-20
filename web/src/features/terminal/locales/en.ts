/**
 * Terminal English catalog (JUS-245): the shared page surface, the floating
 * tabbed window, its confirmation dialog and lifecycle states. Session
 * values (target, cwd, shell, exit codes) are interpolated, never translated.
 */
const en = {
  window: {
    label: "Terminal window",
    heading: "Terminal",
    minimize: "Minimize terminal window",
    close: "Close terminal window",
    resize: "Resize terminal window",
    tabs: "Terminal sessions",
    closeTab: "Close {title}",
    activeTarget: "Target",
    activeCwd: "Working directory",
  },
  rail: {
    openWithCount: "Terminal window, {count} open",
  },
  pane: {
    openInWindow: "Open in window",
    movedTitle: "This terminal is open in the window",
    movedBody:
      "Its session, buffer and working directory moved to the floating window. This session cannot return to the page.",
    showWindow: "Show window",
    close: "Close terminal",
    host: "Host shell",
    target: "Target",
    cwd: "Working directory",
    shell: "Shell",
    size: "{cols} × {rows}",
    serviceUser: "Runs as {user}",
    privilege: "Full host administration (effectively root).",
    detailLabel: "Terminal for {title}",
  },
  status: {
    connecting: "Connecting…",
    connected: "Connected",
    exited: "Exited",
    disconnected: "Disconnected",
    error: "Error",
  },
  exited: {
    title: "Session ended",
    withCode: "The shell exited with code {code}.",
    withSignal: "The shell was terminated by {signal}.",
    ended: "The shell session ended.",
  },
  disconnected: {
    title: "Connection lost",
    body: "The stream closed. Starting a new session does not replay previous input.",
  },
  errors: {
    title: "Terminal unavailable",
    auth: "Your session has ended. Sign in again to open a terminal.",
    forbidden: "Only a platform operator can open a host terminal.",
    node: "The node was not found or is no longer managed.",
    quota: "The terminal session quota is reached. Close a session and try again.",
    unsupported: "This node agent does not support terminal sessions.",
    unavailable: "The node agent is unavailable. Try again once it reconnects.",
    network: "The control plane could not be reached.",
    generic: "The terminal could not be started.",
  },
  actions: {
    retry: "Start a new session",
    show: "Show terminal",
    minimize: "Minimize",
    close: "Close",
  },
  close: {
    titleTab: "Close this terminal?",
    titleWindow: "Close all terminals?",
    messageTab: "The shell for {title} ends and its running process is stopped.",
    messageWindow: "These sessions end and their running processes stop:",
    listLabel: "Sessions to close",
    cancel: "Keep terminals",
    confirm: "Close terminal",
    confirmAll: "Close all",
  },
  shortcuts: {
    title: "Terminal keyboard shortcuts",
    show: "Terminal keyboard shortcuts",
    escape:
      "Ctrl+Shift+Escape leaves the terminal input and returns focus to the window tab.",
    closeTab: "Delete on a focused tab closes that terminal after confirmation.",
    closeWindow: "Alt+Shift+W closes all terminals after confirmation.",
    minimize: "Alt+M minimizes the window.",
  },
};

export default en;
