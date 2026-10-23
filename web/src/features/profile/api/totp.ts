import { http } from "@/shared/api/http";
import { parseWith } from "@/shared/validation/parse";

import {
  recoveryCodesEnvelopeSchema,
  totpEnrollmentSchema,
  totpStatusSchema,
} from "@/features/profile/schemas/totp";

export interface TotpStatus {
  enabled: boolean;
  recovery_codes_remaining: number;
}

export interface TotpEnrollment {
  secret: string;
  otpauth_url: string;
}

export interface TotpCodeInput {
  password?: string;
  code: string;
}

export async function fetchTotpStatus(): Promise<TotpStatus> {
  const response = await http.get<TotpStatus>("/auth/me/totp");
  parseWith(totpStatusSchema, response.data, { context: "TotpStatus" });
  return response.data;
}

export async function beginTotpEnrollment(): Promise<TotpEnrollment> {
  const response = await http.post<TotpEnrollment>("/auth/me/totp/enroll", {});
  parseWith(totpEnrollmentSchema, response.data, { context: "TotpEnrollment" });
  return response.data;
}

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

export async function disableTotp(input: TotpCodeInput): Promise<void> {
  await http.post("/auth/me/totp/disable", input);
}

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
