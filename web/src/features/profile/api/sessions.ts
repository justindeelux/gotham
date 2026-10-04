import { http } from "@/shared/api/http";
import { parseWith } from "@/shared/validation/parse";

import type { AuthSession } from "@/features/profile/schemas/sessions";
import { sessionsEnvelopeSchema } from "@/features/profile/schemas/sessions";

/**
 * Typed client for the session routes served by the control plane
 * (see the PF-4 contract, PF-2 backend):
 *
 *   GET    /auth/me/sessions               -> {"sessions": [...]}
 *   DELETE /auth/me/sessions/{id}          -> 204
 *   POST   /auth/me/sessions/revoke-others -> 204
 *
 * All routes take the JWT bearer only; a scoped API token answers 403.
 */

/** listSessions returns the account sessions, current first when marked. */
export async function listSessions(): Promise<AuthSession[]> {
  const response = await http.get<{ sessions: AuthSession[] }>(
    "/auth/me/sessions",
  );
  parseWith(sessionsEnvelopeSchema, response.data, {
    context: "SessionsEnvelope",
  });
  return response.data.sessions;
}

/** revokeSession ends one session by id. Unknown ids answer 404. */
export async function revokeSession(id: string): Promise<void> {
  await http.delete(`/auth/me/sessions/${encodeURIComponent(id)}`);
}

/** revokeOtherSessions ends every session except the caller's. */
export async function revokeOtherSessions(): Promise<void> {
  await http.post("/auth/me/sessions/revoke-others");
}
