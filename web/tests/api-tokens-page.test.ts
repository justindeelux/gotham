import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { defineComponent, h } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import type { VueWrapper } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/api-tokens/api/tokens", () => ({
  listTokens: vi.fn(),
  createToken: vi.fn(),
  rotateToken: vi.fn(),
  revokeToken: vi.fn(),
  describeTokenError: vi.fn((_error: unknown, fallback: string) => fallback),
}));

import {
  createToken,
  listTokens,
} from "@/features/api-tokens/api/tokens";
import { checkCatalogParity } from "@/shared/i18n/catalog";
import { useApiTokensPage } from "@/features/api-tokens/composables/useApiTokensPage";
import { useAuthStore } from "@/features/auth";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";
import type { CreatedToken } from "@/features/api-tokens/schemas/tokens";

const mockList = vi.mocked(listTokens);
const mockCreate = vi.mocked(createToken);

function seedAuth(role: "owner" | "member"): void {
  setActivePinia(createPinia());
  const auth = useAuthStore();
  auth.user = {
    id: "u-1",
    email: "ada@gotham.dev",
    created_at: "2026-03-04T12:00:00Z",
    role,
  };
  auth.accessToken = "access";
  auth.refreshToken = "refresh";
}

function createdToken(token: string): CreatedToken {
  return {
    id: "t-1",
    name: "ci-deploy",
    scopes: ["deploy"],
    token,
    created_at: "2026-10-26T10:00:00Z",
  };
}

async function mountProbe(): Promise<{
  page: ReturnType<typeof useApiTokensPage>;
  wrapper: VueWrapper;
}> {
  const holder: { page?: ReturnType<typeof useApiTokensPage> } = {};
  const Probe = defineComponent({
    setup() {
      holder.page = useApiTokensPage();
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
  mockList.mockResolvedValue([]);
  globalThis.document.body.innerHTML = "";
});

describe("api tokens page", () => {
  it("keeps the admin scope away from members and offers it to the owner", async () => {
    seedAuth("member");
    const member = await mountProbe();
    expect(member.page.scopeOptions.value).toEqual(["read", "deploy"]);
    member.wrapper.unmount();

    seedAuth("owner");
    const owner = await mountProbe();
    expect(owner.page.scopeOptions.value).toEqual(["read", "deploy", "admin"]);
    owner.wrapper.unmount();
  });

  it("reveals the plaintext once and clears it when the reveal closes", async () => {
    seedAuth("owner");
    const { page, wrapper } = await mountProbe();
    mockCreate.mockResolvedValue(createdToken("gt1_secret"));

    page.openCreate();
    page.createName.value = " ci-deploy ";
    page.createScope.value = "deploy";
    await page.submitCreate();

    expect(mockCreate).toHaveBeenCalledWith("ci-deploy", "deploy");
    expect(page.createOpen.value).toBe(false);
    expect(page.revealed.value?.token).toBe("gt1_secret");

    page.closeReveal();
    expect(page.revealed.value).toBeNull();
    wrapper.unmount();
  });

  it("validates the token name after the field is touched", async () => {
    seedAuth("member");
    const { page, wrapper } = await mountProbe();

    page.openCreate();
    expect(page.createNameError.value).toBeNull();
    page.touchCreate();
    expect(page.createNameError.value).toBe("Name is required");

    page.createName.value = "ci";
    expect(page.createNameError.value).toBeNull();
    wrapper.unmount();
  });

  it("keeps the EN and VI catalogs in parity", async () => {
    const en = (await import("@/features/api-tokens/locales/en")).default;
    const vi = (await import("@/features/api-tokens/locales/vi")).default;
    expect(checkCatalogParity(en, vi)).toEqual([]);
  });
});
