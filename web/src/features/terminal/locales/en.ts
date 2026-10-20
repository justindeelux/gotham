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
    pageWithCount: "Page terminals, {count} open",
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
    cwdInvalid:
      "The working directory is not allowed. Open a folder inside the server's allowed root.",
    cwdMissing: "That folder no longer exists on the server.",
    cwdUnreadable: "The node agent cannot read that folder.",
    generic: "The terminal could not be started.",
  },
  actions: {
    retry: "Start new session",
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
      "Ctrl+Shift+E leaves the terminal input and returns focus to the window tab or the pane action. Shift+Tab also leaves the shell.",
    closeTab: "Delete on a focused tab closes that terminal after confirmation.",
    closeWindow:
      "Alt+Shift+W closes all window terminals after confirmation when focus is on the window header or a tab.",
    minimize:
      "Alt+M minimizes the window when focus is on its header or a tab.",
    hint:
      "Ctrl+Shift+E or Shift+Tab leaves the terminal input. Inside the shell, Alt and Option keys go to the terminal (readline, vim, tmux); Alt+M and Alt+Shift+W work from the window chrome.",
  },
  page: {
    navLabel: "Terminal",
    title: "Terminal",
    eyebrow: "Operations",
    description:
      "Open a host shell on a managed server. Terminal access is full host administration and is limited to platform operators.",
    tabLabel: "Terminal",
    switchServer: "Server",
    chooseServer: "Choose a server",
    chooseHint: "Select a ready server to start a page terminal.",
    empty: "No managed servers yet",
    emptyHint: "Add and validate a server before opening a terminal.",
    addServer: "Add a server",
    starting: "Starting terminal…",
    sessionClosedTitle: "Session closed",
    sessionClosedBody:
      "This page no longer has a live terminal. Nothing starts automatically; use the button below when you are ready.",
    notAvailableTitle: "Terminal not available",
    notAvailableBody:
      "The terminal is reserved for platform operators. Ask an operator for access, then reload this page.",
    notReadyTitle: "Terminal unavailable",
    notReadyBody:
      "This server is {status}. A terminal can only start on a ready node.",
    unknownServer: "This server is not managed by this control plane.",
  },
  dock: {
    label: "Paused page terminals",
    heading: "Page terminals",
    paused: "Paused",
    resume: "Resume",
    resumeTab: "Resume {title} terminal",
    openInWindow: "Open in window",
    openInWindowTab: "Open {title} in the window",
    expand: "Show paused page terminals",
    collapse: "Hide paused page terminals",
    closeTab: "Close {title}",
  },
  appearance: {
    label: "Terminal theme",
    title: "Terminal theme",
    themes: {
      gothamDark: "Gotham Dark",
      gothamLight: "Gotham Light",
    },
  },
  banner: {
    server: "server",
  },
  quickCommands: {
    open: "Quick commands",
    title: "Quick commands",
    count: "{count} saved",
    add: "Add command",
    addTitle: "New command",
    editTitle: "Edit command",
    insert: "Insert",
    insertLabel: "Insert {label} into the terminal",
    editLabel: "Edit {label}",
    deleteLabel: "Delete {label}",
    save: "Save command",
    cancel: "Cancel",
    label: "Label",
    labelPlaceholder: "Deploy API",
    command: "Command",
    commandPlaceholder: "docker compose up -d",
    abbreviation: "Abbreviation (optional)",
    abbreviationPlaceholder: "dcu",
    abbreviationHint:
      "Type this at a supported prompt to see the command. Letters, digits, dot, dash and underscore only.",
    empty: "No saved commands yet.",
    storageNote:
      "Saved in this browser for this account. Not encrypted — do not store secrets.",
    saveFailed:
      "This browser could not save the change. Your commands still work until reload.",
    saveRejected:
      "The command could not be saved. Check the fields and the abbreviation.",
    deleteBody: "Delete {label} from this browser?",
    inserted: "Inserted {label}. Press Enter in the terminal to run it.",
    insertMultiline:
      "Multiline commands need bracketed paste support; nothing was inserted.",
    insertControl:
      "The command contains control characters; nothing was inserted.",
    insertEmpty: "The command is empty; nothing was inserted.",
    insertUnavailable: "The terminal is not ready; nothing was inserted.",
    dismissNotice: "Dismiss",
    hintTitle: "Abbreviation suggestions are off",
    hintBody:
      "This shell does not emit prompt marks. Add the optional snippet to your shell rc file; Gotham never injects it into the shell.",
    hintAlternate:
      "A fullscreen application is active; suggestions stay off until the shell prompt returns.",
    hintUnsupported:
      "Automatic suggestions support bash and zsh. Manual insertion works in every shell.",
    copySnippet: "Copy setup snippet",
    copied: "Copied",
    validation: {
      labelRequired: "Give this command a label.",
      labelTooLong: "Labels are limited to 64 characters.",
      commandRequired: "Enter the command text to insert.",
      commandTooLong: "Commands are limited to 2,048 characters.",
      commandNul: "Commands cannot contain NUL characters.",
      abbreviationTooLong: "Abbreviations are limited to 24 characters.",
      abbreviationFormat:
        "Use letters, digits, dot, dash or underscore, starting with a letter or digit.",
      abbreviationDuplicate: "Another command already uses this abbreviation.",
      tooMany: "You can save up to 40 commands.",
      idInvalid: "This command id is invalid.",
    },
  },
  suggestions: {
    label: "Quick command suggestions",
    accept: "Insert quick command {label}",
    announce:
      "Suggestion: {label} for {abbreviation}. Press Ctrl+Space to choose or Escape to dismiss.",
    hint: "Ctrl+Space to choose · Esc to dismiss",
    dismiss: "Dismiss suggestion",
  },
};

export default en;
