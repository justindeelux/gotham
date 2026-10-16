import { http } from "@/shared/api/http";
import { parseWith } from "@/shared/validation/parse";

import {
  recoveryCodesEnvelopeSchema,
  totpEnrollmentSchema,
  totpStatusSchema,
} from "@/features/profile/schemas/totp";

/**
 * Typed client for the two-factor routes (JUS-196, interactive session only):
 *
 *   GET  /auth/me/totp               -> {enabled, recovery_codes_remaining}
 *   POST /auth/me/totp/enroll        -> {secret, otpauth_url}
 *   POST /auth/me/totp/enable        {password?, code} -> {recovery_codes}
 *   POST /auth/me/totp/disable       {password?, code} -> 204
 *   POST /auth/me/totp/recovery-codes{password?, code} -> {recovery_codes}
 *
 * `password` is required only when the account has one. The returned recovery
 * codes are shown once; the server stores only their hashes.
 */

/** TotpStatus is the account's two-factor state. */
export interface TotpStatus {
  enabled: boolean;
  recovery_codes_remaining: number;
}

/** TotpEnrollment is the setup material for a pending enrollment. */
export interface TotpEnrollment {
  secret: string;
  otpauth_url: string;
}

/** TotpCodeInput is the body of the confirm/disable/regenerate calls. */
export interface TotpCodeInput {
  password?: string;
  code: string;
}

/** fetchTotpStatus reads the account's two-factor state. */
export async function fetchTotpStatus(): Promise<TotpStatus> {
  const response = await http.get<TotpStatus>("/auth/me/totp");
  parseWith(totpStatusSchema, response.data, { context: "TotpStatus" });
  return response.data;
}

/** beginTotpEnrollment starts a pending enrollment and returns its setup data. */
export async function beginTotpEnrollment(): Promise<TotpEnrollment> {
  const response = await http.post<TotpEnrollment>("/auth/me/totp/enroll", {});
  parseWith(totpEnrollmentSchema, response.data, { context: "TotpEnrollment" });
  return response.data;
}

/** enableTotp confirms the pending enrollment and returns the recovery codes. */
export async function enableTotp(input: TotpCodeInput): Promise<string[]> {
  const response = await http.post<{ recovery_codes: string[] }>(
    "/auth/me/totp/enable",
    input,
  );
  parseWith(recoveryCodesEnvelopeSchema, response.data, {
    context: "TotpRecoveryCodes",
  });
  return response.data.recovery_codes;
}

/** disableTotp turns two-factor off. */
export async function disableTotp(input: TotpCodeInput): Promise<void> {
  await http.post("/auth/me/totp/disable", input);
}

/** regenerateTotpRecoveryCodes replaces the recovery codes. */
export async function regenerateTotpRecoveryCodes(
  input: TotpCodeInput,
): Promise<string[]> {
  const response = await http.post<{ recovery_codes: string[] }>(
    "/auth/me/totp/recovery-codes",
    input,
  );
  parseWith(recoveryCodesEnvelopeSchema, response.data, {
    context: "TotpRecoveryCodes",
  });
  return response.data.recovery_codes;
}
