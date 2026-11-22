import { z } from "zod";

export const enabledOAuthProvidersSchema = z.object({
  providers: z.array(z.string()),
});
