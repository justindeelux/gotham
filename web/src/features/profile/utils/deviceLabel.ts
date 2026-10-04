/**
 * deviceLabel parses a session user agent into a short readable label
 * ("Chrome on macOS"). Deliberately dependency-free and heuristic: it only
 * sniffs the common browser/OS tokens and falls back to "Unknown device",
 * with the raw string kept in the row's title attribute.
 */

/** deviceLabel renders a readable "browser on OS" label for a user agent. */
export function deviceLabel(userAgent: string | null | undefined): string {
  if (!userAgent || userAgent.trim() === "") {
    return "Unknown device";
  }
  const browser = browserName(userAgent);
  const os = osName(userAgent);
  if (browser && os) {
    return `${browser} on ${os}`;
  }
  return browser ?? os ?? "Unknown device";
}

/** browserName sniffs the common browser tokens, Edge before Chrome. */
function browserName(userAgent: string): string | null {
  if (/\bEdg(e|A|iOS)?\//.test(userAgent)) {
    return "Edge";
  }
  if (/\bCriOS\//.test(userAgent)) {
    return "Chrome";
  }
  if (/\bFxiOS\//.test(userAgent)) {
    return "Firefox";
  }
  if (/\bOPR\/|Opera/.test(userAgent)) {
    return "Opera";
  }
  if (/\bChrome\//.test(userAgent)) {
    return "Chrome";
  }
  if (/\bFirefox\//.test(userAgent)) {
    return "Firefox";
  }
  if (/\bSafari\//.test(userAgent)) {
    return "Safari";
  }
  return null;
}

/** osName sniffs the common OS tokens, mobile before desktop. */
function osName(userAgent: string): string | null {
  if (/\bAndroid\b/.test(userAgent)) {
    return "Android";
  }
  if (/\biPhone|\biPad\b/.test(userAgent)) {
    return "iOS";
  }
  if (/\bWindows\b/.test(userAgent)) {
    return "Windows";
  }
  if (/\bMac OS X\b|\bMacintosh\b/.test(userAgent)) {
    return "macOS";
  }
  if (/\bLinux\b/.test(userAgent)) {
    return "Linux";
  }
  return null;
}
