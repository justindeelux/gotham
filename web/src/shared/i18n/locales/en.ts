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
    },
  },
  validation: {
    invalid: "Invalid value",
    required: "This field is required",
  },
  language: {
    label: "Language",
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
