/**
 * English profile catalog (namespace `profile`): account facts, display-name
 * and change-password forms, session management copy and the validation
 * message keys the profile schemas store. Only values are translated in
 * `vi.ts`; keys, params and interpolation stay identical.
 */
const en = {
  page: {
    eyebrow: "Settings · Account",
    title: "Profile",
    description:
      "Your account facts, the name shown in the sidebar, and your password. Your email address cannot be changed here.",
  },
  identity: {
    title: "Account",
    avatarNote:
      "Your avatar comes from GitHub when you sign in with GitHub, otherwise your initials are shown.",
    email: "Email",
    platformRole: "Platform role",
    admin: "Platform admin",
    member: "Member",
    memberSince: "Member since",
    signedIn: "Signed in",
  },
  displayName: {
    title: "Display name",
    ariaLabel: "Display name",
    placeholder: "Ada Lovelace",
    hint: "Shown in the sidebar instead of your email. Clear it to go back to your email. 1-64 characters.",
    submit: "Save display name",
    updated: "Display name updated.",
  },
  password: {
    title: "Change password",
    currentLabel: "Current password",
    currentPlaceholder: "Your current password",
    newLabel: "New password",
    newPlaceholder: "At least 10 characters",
    confirmLabel: "Confirm new password",
    confirmPlaceholder: "Repeat the new password",
    hint: "At least 10 characters with 2 character classes: lowercase, uppercase, digits, symbols.",
    submit: "Change password",
    changed: "Password changed. Other devices were signed out.",
  },
  sessions: {
    title: "Active sessions",
    intro:
      "Every device signed in to your account. Ending a session signs that device out; ending this device signs you out here.",
    loading: "Loading sessions",
    empty: "No active sessions.",
    retry: "Retry",
    thisDevice: "This device",
    created: "Created",
    lastActive: "Last active",
    unknownIp: "unknown",
    unknownTime: "unknown",
    unknownDevice: "Unknown device",
    deviceOn: "on",
    signOut: "Sign out",
    keep: "Keep",
    signOutOthers: "Sign out all other devices",
    confirmOthersPositive: "Sign out others",
    confirmCurrent:
      "Sign out this device? You will be signed out here and returned to the sign-in page.",
    confirmOther: "Sign out {label}? That device will need to sign in again.",
    confirmOthersOne:
      "Sign out {count} other session? Those devices will need to sign in again.",
    confirmOthersMany:
      "Sign out {count} other sessions? Those devices will need to sign in again.",
    needsReauth:
      "Your sign-in predates session management. Sign in again to manage other sessions.",
    signInAgain: "Sign in again",
    loadFailed: "Could not load sessions. Try again.",
    endFailed: "Could not sign out that session. Try again.",
    revokeOthersFailed: "Could not sign out the other sessions. Try again.",
    listStale: "Signed out, but the session list may be out of date.",
    signedOut: "Other devices were signed out.",
    sessionSignedOut: "Session signed out.",
    signedOutHere: "Signed out on this device.",
  },
  validation: {
    displayNameLength: "Display name must be 1-64 characters",
    currentPasswordRequired: "Current password is required",
  },
};

export default en;

/** ProfileMessages is the shape every profile locale must satisfy. */
export type ProfileMessages = typeof en;
