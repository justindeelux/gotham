/**
 * English catalog for the teams feature: page shell, team list, members
 * and invites panels, and the create/rename/invite/token dialogs.
 * Role display words come from the shared `common.roles` catalog (I18N-1);
 * team names, emails, tokens and URLs stay raw parameters.
 */
const en = {
  page: {
    eyebrow: "Team · Access control",
    title: "Teams",
    descriptionPre:
      "Every resource belongs to exactly one team, and a member holds one " +
      "of three roles —",
    descriptionPost:
      "Owners manage ownership, admins manage resources and members, " +
      "read-only members may only look.",
    newTeam: "New team",
    unavailableTitle: "Teams unavailable",
    unavailableDesc:
      "Team management is not enabled on this control plane (FEATURE_TEAMS=false).",
    yourRole: "your role: {role}",
    personal: "personal",
    membersTab: "Members ({count})",
    invitesTab: "Invites ({count})",
    refresh: "Refresh",
  },
  list: {
    title: "Your teams",
    /**
     * Team count with library pluralization (one | other): the numeric
     * argument selects the segment, `{count}` renders it.
     */
    count: "{count} team | {count} teams",
    rename: "Rename",
    delete: "Delete",
    deleteConfirm:
      'Delete team "{name}"? Resources must be moved first — a team that ' +
      "still owns any is refused.",
    empty: "No teams yet.",
  },
  members: {
    member: "Member",
    role: "Role",
    roleOfAria: "Role of {email}",
    joined: "Joined",
    actions: "Actions",
    personalOwner: "personal team owner",
    remove: "Remove",
    removeConfirm: "Remove {email} from {team}?",
    footnote:
      "A team always keeps at least one owner: demoting or removing the " +
      "last one is refused with the backend's message. The personal team's " +
      "owner membership is immutable.",
  },
  invites: {
    email: "Email",
    role: "Role",
    sent: "Sent",
    expires: "Expires",
    actions: "Actions",
    inviteMember: "Invite member",
    singleUse: "Invites are single-use and bound to the email address.",
    readOnlyNote: "Your role does not list or manage invites.",
    revoke: "Revoke",
    revokeConfirm: "Revoke the invite to {email}?",
  },
  dialogs: {
    newTitle: "New team",
    newDesc:
      "You become the new team's owner. Resources can be moved in from the " +
      "applications and servers you already manage.",
    teamNamePlaceholder: "Team name",
    teamNameAria: "Team name",
    createTeam: "Create team",
    renameTitle: "Rename team",
    save: "Save",
    cancel: "Cancel",
    inviteTitle: "Invite member",
    inviteEmailPlaceholder: "name{'@'}example.com",
    inviteEmailAria: "Invite email",
    inviteRoleAria: "Invite role",
    inviteHint:
      "Invites can grant admin or read-only — ownership is transferred " +
      "from the members table, never invited.",
    createInvite: "Create invite",
    tokenTitle: "Invite created — copy the link now",
    tokenWarning:
      "This link is shown once and never stored. Email delivery is not " +
      "wired yet (BE-8.3) — send it to {email} yourself.",
    newMemberLinkAria: "New member link",
    acceptLinkAria: "Accept link",
    tokenText: "token: {token}",
    tokenBody:
      "Send the first link to {email} to create a new account: registration " +
      "is closed on an instance that already has an account, so the invite " +
      "is the only way in. The second link is for someone who already has " +
      "an account and is signed in. Either way the member joins as {role}. " +
      "It expires {expiry}.",
    copyLink: "Copy link",
    copied: "Copied",
    done: "Done",
  },
  toast: {
    createdTeam: "Created team {name}",
    renamed: "Team renamed",
    deletedTeam: "Deleted team {name}",
    roleChanged: "{email} is now {role}",
    removedMember: "Removed {email}",
    revokedInvite: "Revoked the invite to {email}",
    linkCopied: "Invite link copied",
    clipboardDenied:
      "Clipboard is unavailable — select the link and copy it manually.",
  },
  errors: {
    sessionExpired: "Your session expired. Please sign in again.",
    forbiddenRole: "Your team role does not allow this action.",
    notFound: "Not found. It may have been removed already.",
    conflictChanged:
      "The team changed while you were editing it. Reload and retry.",
    inviteExpired: "This invite expired. Issue a new one.",
    invalidRequest: "Invalid request.",
    requestFailed: "Request failed",
    unexpected: "Something went wrong. Please try again.",
  },
};

export default en;
