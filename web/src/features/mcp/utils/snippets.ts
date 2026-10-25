export const mcpTokenPlaceholder = "<YOUR_TOKEN>";

export type McpSnippetId = "claude-code" | "url-headers" | "url-token";

export interface McpSnippet {
  id: McpSnippetId;
  text: string;
}

// The token is only passed in while the reveal dialog is open; otherwise the
// snippets show a placeholder so no secret is kept on the page.
export function buildMcpSnippets(
  mcpUrl: string,
  token: string | null,
): McpSnippet[] {
  const secret = token ?? mcpTokenPlaceholder;
  const urlHeadersConfig = JSON.stringify(
    {
      mcpServers: {
        gotham: {
          url: mcpUrl,
          headers: { Authorization: `Bearer ${secret}` },
        },
      },
    },
    null,
    2,
  );
  return [
    {
      id: "claude-code",
      text: `claude mcp add --transport http gotham ${mcpUrl} --header "Authorization: Bearer ${secret}"`,
    },
    { id: "url-headers", text: urlHeadersConfig },
    { id: "url-token", text: `URL: ${mcpUrl}\nToken: ${secret}` },
  ];
}
