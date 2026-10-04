/** Invite link builders for the one-time token dialog. */
export function acceptLink(token: string): string {
  return `${window.location.origin}/invite/accept?token=${encodeURIComponent(token)}`;
}

/**
 * registerLink builds the onboarding URL for a NEW member: registration is
 * closed once the instance has an account, so a fresh account must come
 * through the invite (P-A2). The accept link above still serves someone who
 * already has an account, which is why both are shown.
 */
export function registerLink(token: string): string {
  return `${window.location.origin}/register?invite=${encodeURIComponent(token)}`;
}
