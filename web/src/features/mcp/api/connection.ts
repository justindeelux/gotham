import { http } from "@/shared/api/http";
import { parseWith } from "@/shared/validation/parse";

import { mcpConnectionSchema } from "@/features/mcp/schemas/connection";
import type { McpConnection } from "@/features/mcp/schemas/connection";

export async function fetchMcpConnection(): Promise<McpConnection> {
  const response = await http.get<unknown>("/mcp/connection");
  return parseWith(mcpConnectionSchema, response.data, {
    context: "McpConnection",
  });
}
