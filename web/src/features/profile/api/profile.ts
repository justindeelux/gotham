import { http } from "@/shared/api/http";
import type { AuthResult, User } from "@/shared/api/token";
import { parseWith } from "@/shared/validation/parse";

import {
  meEnvelopeSchema,
  passwordChangeEnvelopeSchema,
} from "@/features/profile/schemas/profile";

export async function patchDisplayName(displayName: string | null): Promise<User> {
  const response = await http.patch<{ user: User }>("/auth/me", {
    display_name: displayName,
  });
  parseWith(meEnvelopeSchema, response.data, { context: "MeEnvelope" });
  return response.data.user;
}

export interface ChangePasswordInput {
  current_password?: string;
  new_password: string;
}

export async function changePassword(input: ChangePasswordInput): Promise<AuthResult> {
  const response = await http.post<AuthResult>("/auth/me/password", input);
  parseWith(passwordChangeEnvelopeSchema, response.data, {
    context: "PasswordChangeEnvelope",
  });
  return response.data;
}
