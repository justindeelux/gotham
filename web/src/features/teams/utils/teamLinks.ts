export function acceptLink(token: string): string {
  return `${window.location.origin}/invite/accept?token=${encodeURIComponent(token)}`;
}

export function registerLink(token: string): string {
  return `${window.location.origin}/register?invite=${encodeURIComponent(token)}`;
}
