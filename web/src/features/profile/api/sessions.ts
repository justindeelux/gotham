import { http } from "@/shared/api/http";
import { parseWith } from "@/shared/validation/parse";

import type { AuthSession } from "@/features/profile/schemas/sessions";
import { sessionsEnvelopeSchema } from "@/features/profile/schemas/sessions";

export async function listSessions(): Promise<AuthSession[]> {
  const response = await http.get<{ sessions: AuthSession[] }>(
    "/auth/me/sessions",
  );
  parseWith(sessionsEnvelopeSchema, response.data, {
    context: "SessionsEnvelope",
  });
  return response.data.sessions;
}

export async function revokeSession(id: string): Promise<void> {
  await http.delete(`/auth/me/sessions/${encodeURIComponent(id)}`);
}

export async function revokeOtherSessions(): Promise<void> {
  await http.post("/auth/me/sessions/revoke-others");
}
