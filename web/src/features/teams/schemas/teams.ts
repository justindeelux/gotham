import { z } from "zod";

export const teamNameSchema: z.ZodString = z.string().trim().min(1);

export const inviteEmailSchema: z.ZodString = z.string().trim().min(1);

export function isTeamNameValid(value: unknown): boolean {
  return teamNameSchema.safeParse(value).success;
}

export function isInviteEmailValid(value: unknown): boolean {
  return inviteEmailSchema.safeParse(value).success;
}
