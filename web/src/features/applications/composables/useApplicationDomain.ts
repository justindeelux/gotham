import { hostDomainSchema } from "@/features/applications/schemas/applications";

export function isValidDomain(value: string): boolean {
  return hostDomainSchema.safeParse(value).success;
}
