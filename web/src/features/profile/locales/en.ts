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
    confirmOthers:
      "Sign out {count} other session? Those devices will need to sign in again. | Sign out {count} other sessions? Those devices will need to sign in again.",
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
  totp: {
    title: "Two-factor authentication",
    intro:
      "Require a time-based code from an authenticator app in addition to your password when you sign in.",
    statusOn: "On",
    statusOff: "Off",
    recoveryRemaining: "Recovery codes left: {count}",
    retry: "Retry",
    enable: "Set up two-factor authentication",
    disable: "Turn off",
    regenerate: "Regenerate recovery codes",
    setupTitle: "Set up two-factor authentication",
    scanHint:
      "Scan this code with your authenticator app (Google Authenticator, 1Password, Authy).",
    qrLabel: "Authenticator setup QR code",
    manualHint: "Or enter this key by hand:",
    secretLabel: "Setup key",
    copySecret: "Copy key",
    passwordLabel: "Current password",
    passwordPlaceholder: "Your current password",
    codeLabel: "Code from the app",
    codePlaceholder: "6-digit code",
    codeHint: "From your authenticator app, or a recovery code.",
    confirmEnable: "Turn on",
    disableTitle: "Turn off two-factor authentication",
    disableHint:
      "Enter your password and a current code from the app or a recovery code.",
    disableSubmit: "Turn off",
    regenerateTitle: "Regenerate recovery codes",
    regenerateHint:
      "Your current recovery codes stop working. Enter your password and a current code.",
    regenerateSubmit: "Regenerate",
    codesHint:
      "Save these recovery codes somewhere safe. Each one works once if you lose your device, and they are shown only now.",
    codesLabel: "Recovery codes",
    copyCodes: "Copy codes",
    codesSaved: "I saved them",
    cancel: "Cancel",
    enabledToast: "Two-factor authentication is on.",
    disabledToast: "Two-factor authentication is off.",
    regeneratedToast: "Recovery codes regenerated.",
  },
  validation: {
    displayNameLength: "Display name must be 1-64 characters",
    currentPasswordRequired: "Current password is required",
  },
};

export default en;

/** ProfileMessages is the shape every profile locale must satisfy. */
export type ProfileMessages = typeof en;
