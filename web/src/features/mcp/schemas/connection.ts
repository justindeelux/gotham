import { z } from "zod";

export const mcpConnectionSchema = z.object({
  base_url: z.string(),
  mcp_url: z.string(),
  https: z.boolean(),
  role: z.enum(["owner", "member"]),
  capabilities: z.array(z.string()),
});

export type McpConnection = z.infer<typeof mcpConnectionSchema>;
