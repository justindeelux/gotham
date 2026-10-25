import { z } from "zod";

export const projectRefSchema = z.object({
  id: z.string().uuid(),
  name: z.string(),
});

export const accountSchema = z.object({
  id: z.string().uuid(),
  email: z.string(),
  role: z.enum(["owner", "member"]),
  avatar: z.string().nullable().optional(),
  display_name: z.string().nullable().optional(),
  has_password: z.boolean().optional(),
  created_at: z.string(),
  projects: z.array(projectRefSchema),
});

export const inviteSchema = z.object({
  id: z.string().uuid(),
  email: z.string(),
  projects: z.array(projectRefSchema),
  expires_at: z.string(),
  accepted_at: z.string().nullable().optional(),
  created_at: z.string(),
});

export const userListEnvelopeSchema = z.object({
  users: z.array(accountSchema),
});

export const inviteListEnvelopeSchema = z.object({
  invites: z.array(inviteSchema),
});

export const createdInviteEnvelopeSchema = z.object({
  invite: inviteSchema,
  token: z.string(),
});

export const accountEnvelopeSchema = z.object({
  user: accountSchema,
});

export type Account = z.infer<typeof accountSchema>;
export type Invite = z.infer<typeof inviteSchema>;
export type ProjectRef = z.infer<typeof projectRefSchema>;
export type CreatedInvite = z.infer<typeof createdInviteEnvelopeSchema>;

export const inviteEmailSchema = z
  .string()
  .trim()
  .min(1, "users.validation.emailRequired")
  .email("users.validation.emailInvalid")
  .max(254, "users.validation.emailMaxLength");

export const inviteProjectsSchema = z.array(z.string().uuid());

export function isInviteEmailValid(value: unknown): boolean {
  return inviteEmailSchema.safeParse(value).success;
}
