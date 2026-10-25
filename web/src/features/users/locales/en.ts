const en = {
  page: {
    eyebrow: "Access · Users",
    title: "Users",
    description:
      "Invite people to a set of projects and manage who can reach this instance.",
    invite: "Invite user",
  },
  members: {
    title: "Accounts",
  },
  invites: {
    title: "Pending invites",
    empty: "No pending invites.",
  },
  columns: {
    user: "User",
    email: "Email",
    role: "Role",
    projects: "Projects",
    joined: "Joined",
    expires: "Expires",
    actions: "Actions",
  },
  allProjects: "All projects",
  noProjects: "No projects",
  ownerHint: "Owner account",
  actions: {
    projects: "Project access",
    reset: "Reset",
    remove: "Remove",
    revoke: "Revoke",
  },
  removeConfirm: "Remove {email}? Their resources move to the owner.",
  resetConfirm: "Reset {email}'s sessions and two-factor setup?",
  revokeConfirm: "Revoke the invite to {email}?",
  invite: {
    title: "Invite a user",
    email: "Email",
    projects: "Projects",
    projectsPlaceholder: "Choose the projects this person can reach",
    submit: "Send invite",
    copyLink: "Copy invite link",
    linkHint:
      "Share this single-use link. It expires in seven days and only works once.",
    linkAria: "Invite link",
  },
  access: {
    title: "Project access",
    projects: "Projects this member can reach",
  },
  toast: {
    invited: "Invite created.",
    linkCopied: "Invite link copied.",
    clipboardDenied: "Clipboard is unavailable — select the link and copy it.",
    accessSaved: "Project access saved.",
    removed: "Removed {email}.",
    reset: "Reset sessions for {email}.",
    inviteRevoked: "Revoked the invite to {email}.",
  },
  validation: {
    emailRequired: "Enter an email address.",
    emailInvalid: "Enter a valid email address.",
    emailMaxLength: "The email address is too long.",
  },
  errors: {
    sessionExpired: "Your session expired. Please sign in again.",
    forbidden: "Only the owner can manage users.",
    notFound: "That account no longer exists.",
    conflict: "That email is already registered.",
  },
};

export default en;
