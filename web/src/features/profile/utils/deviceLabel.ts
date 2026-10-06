/**
 * deviceLabel parses a session user agent into a short readable label
 * ("Chrome on macOS"). Deliberately dependency-free and heuristic: it only
 * sniffs the common browser/OS tokens and falls back to the catalog's
 * unknown-device string, with the raw string kept in the row's title
 * attribute. The locale selects the fallback/joiner language (callers pass
 * the active locale); browser/OS names stay untranslated proper nouns.
 */
import type { ProfileMessages } from "@/features/profile/locales/en";
import enCatalog from "@/features/profile/locales/en";
import viCatalog from "@/features/profile/locales/vi";

/** DeviceLocale selects the fallback/joiner language for device labels. */
export type DeviceLocale = "en" | "vi";

const deviceTables: Record<DeviceLocale, ProfileMessages["sessions"]> = {
  en: enCatalog.sessions,
  vi: viCatalog.sessions,
};

/** deviceLabel renders a readable "browser on OS" label for a user agent. */
export function deviceLabel(
  userAgent: string | null | undefined,
  locale: DeviceLocale = "en",
): string {
  const table = deviceTables[locale] ?? deviceTables.en;
  if (!userAgent || userAgent.trim() === "") {
    return table.unknownDevice;
  }
  const browser = browserName(userAgent);
  const os = osName(userAgent);
  if (browser && os) {
    return `${browser} ${table.deviceOn} ${os}`;
  }
  return browser ?? os ?? table.unknownDevice;
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
