import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/auth/api/oauthProviders", () => ({
  getEnabledOAuthProviders: vi.fn(),
}));

import { getEnabledOAuthProviders } from "@/features/auth/api/oauthProviders";
import OAuthProviderButtons from "@/features/auth/components/OAuthProviderButtons.vue";
import en from "@/features/auth/locales/en";
import vietnamese from "@/features/auth/locales/vi";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

const mockGet = vi.mocked(getEnabledOAuthProviders);

function mountButtons(mode: "signin" | "signup" = "signin") {
  return mount(OAuthProviderButtons, {
    props: { mode },
    attachTo: globalThis.document.body,
    global: { plugins: [i18n], stubs: { transition: false } },
  });
}

beforeEach(() => {
  resetLocaleState();
  i18n.global.mergeLocaleMessage("en", { auth: en });
  i18n.global.mergeLocaleMessage("vi", { auth: vietnamese });
  syncComposerLocale("en");
  mockGet.mockReset();
});

describe("OAuth provider buttons", () => {
  it("renders nothing and no divider when no provider is enabled", async () => {
    mockGet.mockResolvedValue([]);
    const wrapper = mountButtons();
    await flushPromises();

    expect(wrapper.findAll("a")).toHaveLength(0);
    expect(wrapper.find(".auth-divider").exists()).toBe(false);
    wrapper.unmount();
  });

  it("renders one button for the enabled provider", async () => {
    mockGet.mockResolvedValue(["github"]);
    const wrapper = mountButtons();
    await flushPromises();

    const links = wrapper.findAll("a");
    expect(links).toHaveLength(1);
    expect(links[0].attributes("href")).toBe("/api/v1/auth/oauth/github/login");
    expect(links[0].text()).toBe("Sign in with GitHub");
    expect(wrapper.find(".auth-divider").exists()).toBe(true);
    wrapper.unmount();
  });

  it("renders one button per enabled provider", async () => {
    mockGet.mockResolvedValue(["github", "google"]);
    const wrapper = mountButtons();
    await flushPromises();

    const links = wrapper.findAll("a");
    expect(links).toHaveLength(2);
    expect(links[0].attributes("href")).toBe("/api/v1/auth/oauth/github/login");
    expect(links[1].attributes("href")).toBe("/api/v1/auth/oauth/google/login");
    expect(links[0].text()).toBe("Sign in with GitHub");
    expect(links[1].text()).toBe("Sign in with Google");
    wrapper.unmount();
  });

  it("uses the sign-up copy for the register page", async () => {
    mockGet.mockResolvedValue(["google"]);
    const wrapper = mountButtons("signup");
    await flushPromises();

    expect(wrapper.find("a").text()).toBe("Sign up with Google");
    wrapper.unmount();
  });

  it("renders the Vietnamese labels", async () => {
    syncComposerLocale("vi");
    mockGet.mockResolvedValue(["github", "google"]);
    const wrapper = mountButtons();
    await flushPromises();

    expect(wrapper.text()).toContain("Đăng nhập bằng GitHub");
    expect(wrapper.text()).toContain("Đăng nhập bằng Google");
    expect(wrapper.text()).not.toContain("auth.oauth");
    wrapper.unmount();
  });

  it("renders nothing when the provider request fails", async () => {
    mockGet.mockRejectedValue(new Error("offline"));
    const wrapper = mountButtons();
    await flushPromises();

    expect(wrapper.findAll("a")).toHaveLength(0);
    expect(wrapper.find(".auth-divider").exists()).toBe(false);
    wrapper.unmount();
  });
});
