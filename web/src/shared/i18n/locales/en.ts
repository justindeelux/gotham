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
      platformAdmin: "Platform admin",
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
      servers: "Servers",
      domains: "Domains & SSL",
      toolchain: "App Store",
      teams: "Members & roles",
      notifications: "Notification channels",
      gitSources: "Git sources",
      instance: "Instance",
      updates: "Updates & settings",
    },
  },
  shell: {
    navToggle: "Toggle navigation",
    serversToggle: "Toggle servers",
    envTitle: "Serving environment: {env}",
    cpPortTitle: "Control-plane HTTP port",
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
    providerCallback: "Provider connected",
    githubAppCallback: "GitHub App",
    dashboard: "Dashboard",
    servers: "Servers",
    serverDetail: "Server detail",
    serverContainers: "Containers",
    serverFiles: "Files",
    files: "File manager",
    projects: "Projects",
    projectDetail: "Project detail",
    environmentDetail: "Environment detail",
    applicationDetail: "Application detail",
    databaseDetail: "Database detail",
    domains: "Domains & SSL",
    serviceDetail: "Service detail",
    toolchain: "App Store",
    teams: "Teams",
    notifications: "Notification channels",
    instance: "Instance settings",
    gitSources: "Git sources",
    profile: "Profile",
    updates: "Updates",
    inviteAccept: "Team invite",
    notFound: "Page not found",
    forbidden: "No permission",
  },
  denied: {
    eyebrow: "Access control",
    title: "You don't have permission to view this",
    body: "Only a Platform admin can open this section. Ask yours for access, or go back to the dashboard.",
    back: "Back to dashboard",
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
