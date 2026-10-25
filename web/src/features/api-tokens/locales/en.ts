const en = {
  page: {
    eyebrow: "Settings · Account",
    title: "API tokens",
    description:
      "Bearer tokens for CI jobs, scripts and integrations that call the Gotham API. Each token belongs to your account and can only do what its scope allows.",
    create: "Create token",
  },
  list: {
    title: "Your tokens",
    empty: "No API tokens yet. Create one for your automation.",
    retry: "Retry",
    columns: {
      name: "Name",
      scopes: "Scopes",
      created: "Created",
      lastUsed: "Last used",
      status: "Status",
      actions: "Actions",
    },
    neverUsed: "Never",
    active: "Active",
    revoked: "Revoked",
    revokedOn: "Revoked {date}",
    rotate: "Rotate",
    revoke: "Revoke",
    rotateConfirm:
      "Rotate {name}? The current secret stops working immediately, so update the system that uses it.",
    rotatePositive: "Rotate",
    revokeConfirm:
      "Revoke {name}? This cannot be undone and requests using it will fail.",
    revokePositive: "Revoke",
  },
  create: {
    title: "Create API token",
    name: "Name",
    namePlaceholder: "ci-deploy",
    nameHint:
      "Shown in this list so you can tell tokens apart. Up to 64 characters.",
    scope: "Scope",
    submit: "Create token",
  },
  reveal: {
    title: "Your new API token",
    warning:
      "Copy this token now. It is shown once and cannot be shown again; store it like a password.",
    aria: "New API token",
    copy: "Copy token",
    copyLabel: "API token",
    done: "Done",
  },
  scopes: {
    read: {
      name: "Read",
      description:
        "View projects, servers and applications. Cannot change anything.",
    },
    deploy: {
      name: "Deploy",
      description:
        "Read, plus deploy, change project resources and read secrets.",
    },
    admin: {
      name: "Admin",
      description:
        "Everything, including owner-only actions: user access, the file manager, domains and updates.",
    },
  },
  toast: {
    created: "Token created.",
    rotated: "Token rotated.",
    revoked: "Token revoked.",
  },
  errors: {
    loadFailed: "Could not load tokens. Try again.",
    createFailed: "Could not create the token. Try again.",
    rotateFailed: "Could not rotate the token. Try again.",
    revokeFailed: "Could not revoke the token. Try again.",
    sessionExpired: "Your session expired. Sign in again.",
    forbidden: "You don't have permission to do that.",
    notFound: "That token no longer exists.",
  },
  validation: {
    nameRequired: "Name is required",
    nameMaxLength: "Name must be 64 characters or fewer",
  },
};

export default en;

export type ApiTokensMessages = typeof en;
