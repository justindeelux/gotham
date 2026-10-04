import { z } from "zod";

/**
 * Session schemas (JUS-28). The list/revoke routes are served by the PF-2
 * backend; the web build codes against the fixed contract and the tests mock
 * the transport, so this file pins the envelope shape only.
 */

/** AuthSession mirrors one entry of GET /auth/me/sessions {"sessions"}. */
export interface AuthSession {
  id: string;
  user_agent: string;
  ip: string;
  created_at: string;
  last_used_at: string;
  current: boolean;
}

/** sessionSchema validates one session entry (user_agent/ip may be ""). */
export const sessionSchema = z.object({
  id: z.string(),
  user_agent: z.string(),
  ip: z.string(),
  created_at: z.string(),
  last_used_at: z.string(),
  current: z.boolean(),
});

/** sessionsEnvelopeSchema validates the GET /auth/me/sessions envelope. */
export const sessionsEnvelopeSchema = z.object({
  sessions: z.array(sessionSchema),
});
