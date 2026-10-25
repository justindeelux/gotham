const en = {
  page: {
    eyebrow: "Settings · Connect",
    title: "MCP setup",
    description:
      "Connect Claude Code, Claude Desktop, Cursor or another MCP client to this control plane. Pick an access level, create a token and paste a ready-made configuration into the client.",
  },
  access: {
    title: "1. Choose what the client may do",
    hint: "The token keeps this access level. Create a new token to change it later.",
    memberHint:
      "The token keeps this access level, and the client only reaches the projects you have access to. Create a new token to change it later.",
    read: {
      name: "Read only",
      description:
        "Observe projects, servers, applications and resources. Nothing can be created, changed or deployed.",
    },
    deploy: {
      name: "Read and deploy",
      description:
        "Everything read only allows, plus create, change and deploy tools. Choose this when the assistant should make changes.",
    },
    create: "Create token",
  },
  clients: {
    title: "2. Copy a client configuration",
    hint: "Create a token to fill these in with the real value. Nothing is stored in the browser; copy it before you leave the page.",
    copy: "Copy",
    claudeCode: "Claude Code (terminal)",
    claudeDesktop: "Claude Desktop / Cursor",
    urlToken: "Other clients (URL and token)",
  },
  https: {
    title: "This connection is not encrypted",
    owner:
      "The control-plane URL is plain HTTP, so tokens and tool output travel unencrypted. Set the control-plane URL to an HTTPS address in the instance settings.",
    member:
      "The control-plane URL is plain HTTP, so tokens and tool output travel unencrypted. Ask the instance owner to set the control-plane URL to an HTTPS address.",
    open: "Open instance settings",
  },
  reveal: {
    title: "Your MCP configuration",
    warning:
      "Copy a snippet or the token now. The token is shown once and cannot be shown again; the snippets keep it only while this dialog is open.",
    aria: "New MCP token",
    copy: "Copy token",
    done: "Done",
  },
  toast: {
    created: "Token created.",
  },
  errors: {
    loadFailed: "Could not load the MCP connection. Try again.",
    createFailed: "Could not create the token. Try again.",
    retry: "Retry",
  },
};

export default en;

export type McpMessages = typeof en;
