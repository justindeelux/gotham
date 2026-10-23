import { z } from "zod";

export interface AuthSession {
  id: string;
  user_agent: string;
  ip: string;
  created_at: string;
  last_used_at: string;
  current: boolean;
}

export const sessionSchema = z.object({
  id: z.string(),
  user_agent: z.string(),
  ip: z.string(),
  created_at: z.string(),
  last_used_at: z.string(),
  current: z.boolean(),
});

export const sessionsEnvelopeSchema = z.object({
  sessions: z.array(sessionSchema),
});
