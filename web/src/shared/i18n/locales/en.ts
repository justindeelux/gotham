/**
 * Common English catalog: shared actions, role labels, status fallbacks,
 * error summaries and validation fallbacks. Feature catalogs live in
 * `features/<module>/locales/en.ts` under their own namespace; this file
 * holds only strings two or more features share.
 */
const en = {
  common: {
    actions: {
      save: "Save",
      cancel: "Cancel",
      close: "Close",
      back: "Back",
      retry: "Retry",
      reload: "Reload",
      confirm: "Confirm",
      delete: "Delete",
      create: "Create",
      edit: "Edit",
    },
    roles: {
      owner: "owner",
      admin: "admin",
      readOnly: "read-only",
      member: "Team member",
    },
    status: {
      unknown: "unknown",
      never: "never",
    },
    errors: {
      requestFailed: "Request failed",
      unexpected: "Something went wrong. Please try again.",
      rateLimited: "Too many attempts, please wait",
    },
    clipboard: {
      copied: "{label} copied to clipboard",
      copyFailed: "Could not copy {label}",
    },
    chart: {
      seriesOf: "Time series chart of {names}",
    },
  },
  validation: {
    invalid: "Invalid value",
    required: "This field is required",
  },
  language: {
    label: "Language",
    names: {
      en: "English",
      vi: "Tiếng Việt",
    },
  },
  nav: {
    label: "Product navigation",
    sections: {
      operations: "Operations",
      team: "Team",
      system: "System",
    },
    items: {
      dashboard: "Dashboard",
      projects: "Projects",
      files: "File manager",
      templates: "Template library",
      servers: "Servers",
      domains: "Domains & SSL",
      teams: "Members & roles",
      notifications: "Notification channels",
      tokens: "API tokens",
      updates: "Updates & settings",
    },
    stubSuffix: "— no UI yet",
  },
  shell: {
    navToggle: "Toggle navigation",
    searchLabel: "Search (coming soon)",
    searchPlaceholder: "Search apps, servers, databases…",
    searchSoon: "Search is coming soon",
    notifications: "Notifications (coming soon)",
    notificationsSoon: "Notifications — no UI yet",
    docs: "Docs (coming soon)",
    docsSoon: "Docs — coming soon",
    envTitle: "Serving environment: {env}",
    cpPortTitle: "Control-plane HTTP port",
    grpcPortTitle: "Agent gRPC port",
    account: "Account",
    signIn: "Sign in",
    profile: "Profile",
    signOut: "Sign out",
    signedIn: "Signed in",
  },
  titles: {
    login: "Sign in",
    register: "Create account",
    oauthCallback: "Signing in",
    dashboard: "Dashboard",
    servers: "Servers",
    serverDetail: "Server detail",
    serverContainers: "Containers",
    projects: "Projects",
    projectDetail: "Project detail",
    environmentDetail: "Environment detail",
    applicationDetail: "Application detail",
    databaseDetail: "Database detail",
    domains: "Domains & SSL",
    serviceDetail: "Service detail",
    templates: "Template library",
    teams: "Teams",
    notifications: "Notification channels",
    profile: "Profile",
    inviteAccept: "Team invite",
  },
  time: {
    justNow: "just now",
    inMoment: "in a moment",
    never: "never",
    unknown: "unknown",
    expiresToday: "expires today",
    expiredToday: "expired today",
  },
};

export default en;

/** CommonMessages is the shape every locale must satisfy. */
export type CommonMessages = typeof en;
