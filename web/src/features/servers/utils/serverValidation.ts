
export const HOST_PATTERN = /^[0-9a-zA-Z.-]+$/;
export const NAME_PATTERN = /^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$/;
export const USER_PATTERN = /^[a-z_][a-z0-9_-]*[$]?$/;

export function isValidHost(value: string): boolean {
  const trimmed = value.trim();
  if (trimmed === "" || !HOST_PATTERN.test(trimmed)) {
    return false;
  }
  const ipv4 =
    /^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/;
  const hostname =
    /^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$/;
  return ipv4.test(trimmed) || hostname.test(trimmed);
}
