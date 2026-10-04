import { http } from "@/shared/api/http";
import type { AuthResult, User } from "@/shared/api/token";
import { parseWith } from "@/shared/validation/parse";

import {
  meEnvelopeSchema,
  passwordChangeEnvelopeSchema,
} from "@/features/profile/schemas/profile";

/**
 * Typed client for the profile routes served by the control plane
 * (see the PF-1 contract):
 *
 *   PATCH /auth/me          {display_name: string | null} -> {"user"}
 *   POST  /auth/me/password {current_password?, new_password} -> AuthResult
 *
 * A blank display name is sent as null, which clears it. The password
 * change returns a fresh token pair for the caller; every other session
 * of the account is ended server-side.
 */

/** patchDisplayName updates (or, with null, clears) the display name. */
export async function patchDisplayName(displayName: string | null): Promise<User> {
  const response = await http.patch<{ user: User }>("/auth/me", {
    display_name: displayName,
  });
  parseWith(meEnvelopeSchema, response.data, { context: "MeEnvelope" });
  return response.data.user;
}

/** Password change input: current is required only with a password set. */
export interface ChangePasswordInput {
  current_password?: string;
  new_password: string;
}

/** changePassword sets a new password and returns the fresh token pair. */
export async function changePassword(input: ChangePasswordInput): Promise<AuthResult> {
  const response = await http.post<AuthResult>("/auth/me/password", input);
  parseWith(passwordChangeEnvelopeSchema, response.data, {
    context: "PasswordChangeEnvelope",
  });
  return response.data;
}
