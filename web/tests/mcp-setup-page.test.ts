import { NMessageProvider } from "naive-ui";
import { defineComponent, h } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import type { VueWrapper } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/mcp/api/connection", () => ({
  fetchMcpConnection: vi.fn(),
}));

vi.mock("@/features/api-tokens", () => ({
  createToken: vi.fn(),
  describeTokenError: vi.fn((_error: unknown, fallback: string) => fallback),
}));

import { createToken } from "@/features/api-tokens";
import type { CreatedToken } from "@/features/api-tokens";
import { fetchMcpConnection } from "@/features/mcp/api/connection";
import {
  mcpTokenName,
  useMcpSetupPage,
} from "@/features/mcp/composables/useMcpSetupPage";
import { mcpConnectionSchema } from "@/features/mcp/schemas/connection";
import {
  buildMcpSnippets,
  mcpTokenPlaceholder,
} from "@/features/mcp/utils/snippets";
import { checkCatalogParity } from "@/shared/i18n/catalog";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const mockConnection = vi.mocked(fetchMcpConnection);
const mockCreate = vi.mocked(createToken);

const connection = mcpConnectionSchema.parse({
  base_url: "https://gotham.example.com",
  mcp_url: "https://gotham.example.com/mcp",
  https: true,
  role: "owner",
  capabilities: ["context", "strict_reads"],
});

function createdToken(token: string): CreatedToken {
  return {
    id: "t-1",
    name: "MCP setup 2026-10-26",
    scopes: ["read"],
    token,
    created_at: "2026-10-26T10:00:00Z",
  };
}

async function mountPage(): Promise<{
  page: ReturnType<typeof useMcpSetupPage>;
  wrapper: VueWrapper;
}> {
  const holder: { page?: ReturnType<typeof useMcpSetupPage> } = {};
  const Probe = defineComponent({
    setup() {
      holder.page = useMcpSetupPage();
      return () => h("div");
    },
  });
  const wrapper = mount(
    defineComponent({
      render() {
        return h(NMessageProvider, null, { default: () => h(Probe) });
      },
    }),
    { global: { plugins: [i18n] } },
  );
  await flushPromises();
  return { page: holder.page!, wrapper };
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  vi.clearAllMocks();
  mockConnection.mockResolvedValue(connection);
  globalThis.document.body.innerHTML = "";
});

describe("mcp snippets", () => {
  it("fills every client with the URL and keeps the token out unless revealed", () => {
    const preview = buildMcpSnippets("https://gotham.example.com/mcp", null);
    expect(preview.map((snippet) => snippet.id)).toEqual([
      "claude-code",
      "claude-desktop",
      "url-token",
    ]);
    for (const snippet of preview) {
      expect(snippet.text).toContain("https://gotham.example.com/mcp");
      expect(snippet.text).toContain(mcpTokenPlaceholder);
    }

    const revealed = buildMcpSnippets("https://gotham.example.com/mcp", "gt1_secret");
    expect(revealed[0]!.text).toBe(
      'claude mcp add --transport http gotham https://gotham.example.com/mcp --header "Authorization: Bearer gt1_secret"',
    );
    const config = JSON.parse(revealed[1]!.text) as {
      mcpServers: Record<string, { url: string; headers: Record<string, string> }>;
    };
    expect(config.mcpServers.gotham?.url).toBe("https://gotham.example.com/mcp");
    expect(config.mcpServers.gotham?.headers.Authorization).toBe(
      "Bearer gt1_secret",
    );
    expect(revealed[2]!.text).toBe(
      "URL: https://gotham.example.com/mcp\nToken: gt1_secret",
    );
  });
});

describe("mcp setup page", () => {
  it("never stores the plaintext token and clears it when the reveal closes", async () => {
    const { page, wrapper } = await mountPage();
    expect(mockConnection).toHaveBeenCalled();
    expect(page.snippets.value[0]!.text).not.toContain("gt1_secret");

    page.accessLevel.value = "deploy";
    mockCreate.mockResolvedValue(createdToken("gt1_secret"));
    await page.create();

    expect(mockCreate).toHaveBeenCalledWith(mcpTokenName(), "deploy");
    expect(page.revealed.value?.token).toBe("gt1_secret");
    expect(page.revealedSnippets.value[0]!.text).toContain("gt1_secret");
    expect(page.snippets.value[0]!.text).not.toContain("gt1_secret");

    page.closeReveal();
    expect(page.revealed.value).toBeNull();
    expect(page.revealedSnippets.value[0]!.text).toContain(mcpTokenPlaceholder);
    wrapper.unmount();
  });

  it("drops the token on its own when the page unmounts", async () => {
    const { page, wrapper } = await mountPage();
    mockCreate.mockResolvedValue(createdToken("gt1_secret"));
    await page.create();
    expect(page.revealed.value?.token).toBe("gt1_secret");

    wrapper.unmount();
    expect(page.revealed.value).toBeNull();
  });

  it("names the token by date and offers no admin access level", async () => {
    expect(mcpTokenName(new Date("2026-10-26T10:00:00Z"))).toBe(
      "MCP setup 2026-10-26",
    );
    const en = (await import("@/features/mcp/locales/en")).default;
    expect(Object.keys(en.access).sort()).toEqual([
      "create",
      "deploy",
      "hint",
      "memberHint",
      "read",
      "title",
    ]);
  });

  it("keeps the EN and VI catalogs in parity", async () => {
    const en = (await import("@/features/mcp/locales/en")).default;
    const vi = (await import("@/features/mcp/locales/vi")).default;
    expect(checkCatalogParity(en, vi)).toEqual([]);
  });
});
