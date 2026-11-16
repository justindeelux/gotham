import { errorCode, errorParams, errorText } from "@/shared/api/errors";

import type { FirewallStatus } from "../api/firewall";
import en from "../locales/en";

// Every code the API returns maps to `firewall.errors.<code>`; both catalogs
// carry the same keys.
const knownCodes: Record<string, string> = en.errors;
const accessLabels: Record<string, string> = en.access;

export interface Translate {
  (_key: string, _params?: Record<string, string>): string;
}

function localizedParams(
  params: Record<string, string>,
  t: Translate,
): Record<string, string> {
  const access = params.access;
  if (access === undefined || !(access in accessLabels)) {
    return params;
  }
  return { ...params, access: t(`firewall.access.${access}`) };
}

// firewallErrorText translates a known code and keeps the server message for
// an unknown one, so an older agent never shows a raw key or a blank alert.
export function firewallErrorText(
  error: unknown,
  t: Translate,
  fallback: string,
): string {
  const code = errorCode(error);
  if (code !== "" && code in knownCodes) {
    return t(`firewall.errors.${code}`, localizedParams(errorParams(error), t));
  }
  return errorText(error) || fallback;
}

// Codes whose translation is generic: the server message carries the real
// reason and is shown next to the translated text.
const detailCodes = ["helper_failed"];

// firewallErrorDetail returns that reason, or "" when the translated text
// already says everything (including an empty or identical message).
export function firewallErrorDetail(error: unknown, t: Translate): string {
  if (!detailCodes.includes(errorCode(error))) {
    return "";
  }
  const detail = errorText(error).trim();
  return detail === "" || detail === firewallErrorText(error, t, "") ? "" : detail;
}

// firewallDetailText translates why a node's firewall cannot be managed.
export function firewallDetailText(
  status: FirewallStatus,
  t: Translate,
): string {
  const code = status.detail_code ?? "";
  if (code !== "" && code in knownCodes) {
    return t(`firewall.errors.${code}`, localizedParams(status.detail_params ?? {}, t));
  }
  return status.detail || t("firewall.status.unsupportedBody");
}
