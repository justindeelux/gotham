/* global HTMLInputElement: readonly */
import { NMessageProvider } from "naive-ui";
import { flushPromises, mount } from "@vue/test-utils";
import { defineComponent, h } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/instance-settings/api/oauthProviders", () => ({
  getOAuthProviders: vi.fn(),
  saveOAuthProvider: vi.fn(),
  oauthProviderNames: ["github", "google"],
}));

import {
  getOAuthProviders,
  saveOAuthProvider,
} from "@/features/instance-settings/api/oauthProviders";
import AuthenticationSection from "@/features/instance-settings/components/AuthenticationSection.vue";
import en from "@/features/instance-settings/locales/en";
import vietnamese from "@/features/instance-settings/locales/vi";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

const github = {
  name: "github",
  enabled: true,
  client_id: "gh-client-id",
  has_secret: true,
};

const mockGet = vi.mocked(getOAuthProviders);
const mockSave = vi.mocked(saveOAuthProvider);

function mountSection() {
  const host = defineComponent({
    render() {
      return h(NMessageProvider, null, {
        default: () =>
          h(AuthenticationSection, {
            controlPlaneUrl: "https://gotham.example.com",
          }),
      });
    },
  });
  return mount(host, {
    attachTo: globalThis.document.body,
    global: { plugins: [i18n], stubs: { transition: false } },
  });
}

function card(wrapper: ReturnType<typeof mountSection>, index: number) {
  return wrapper.findAll(".n-card")[index];
}

function saveButton(wrapper: ReturnType<typeof mountSection>, index: number) {
  return card(wrapper, index)
    .findAll("button")
    .find((button) => button.text() === "Save");
}

beforeEach(() => {
  resetLocaleState();
  i18n.global.mergeLocaleMessage("en", { "instance-settings": en });
  i18n.global.mergeLocaleMessage("vi", { "instance-settings": vietnamese });
  syncComposerLocale("en");
  mockGet.mockReset();
  mockSave.mockReset();
});

describe("OAuth provider cards", () => {
  it("loads both providers and their callback URLs", async () => {
    mockGet.mockResolvedValue([github]);
    const wrapper = mountSection();
    await flushPromises();

    expect(mockGet).toHaveBeenCalledTimes(1);
    expect(wrapper.findAll(".n-card")).toHaveLength(2);
    expect(wrapper.text()).toContain("GitHub");
    expect(wrapper.text()).toContain("Google");

    expect(
      (wrapper.find("#oauth-github-client-id").element as HTMLInputElement).value,
    ).toBe("gh-client-id");
    expect(
      (wrapper.find("#oauth-github-callback").element as HTMLInputElement).value,
    ).toBe("https://gotham.example.com/api/v1/auth/oauth/github/callback");
    expect(
      (wrapper.find("#oauth-google-callback").element as HTMLInputElement).value,
    ).toBe("https://gotham.example.com/api/v1/auth/oauth/google/callback");
    wrapper.unmount();
  });

  it("puts the provider name and the Enabled switch in one header row", async () => {
    mockGet.mockResolvedValue([github]);
    const wrapper = mountSection();
    await flushPromises();

    const header = card(wrapper, 0).find(".oauth-card-header");
    expect(header.text()).toContain("GitHub");
    expect(header.text()).toContain(en.oauth.enabled);
    expect(header.find(".n-switch").exists()).toBe(true);
    expect(header.find(".n-switch").attributes("aria-label")).toBe(
      en.oauth.enabled,
    );
    wrapper.unmount();
  });

  it("saves without a new secret and never renders the stored one", async () => {
    mockGet.mockResolvedValue([
      { ...github, client_secret: "gh-client-secret" } as typeof github,
    ]);
    mockSave.mockResolvedValue(github);
    const wrapper = mountSection();
    await flushPromises();

    const secret = wrapper.find("#oauth-github-client-secret");
    expect((secret.element as HTMLInputElement).value).toBe("");
    expect(secret.attributes("placeholder")).toBe(
      en.oauth.clientSecretSaved,
    );
    expect(wrapper.html()).not.toContain("gh-client-secret");

    await saveButton(wrapper, 0)!.trigger("click");
    await flushPromises();

    expect(mockSave).toHaveBeenCalledWith("github", {
      client_id: "gh-client-id",
      enabled: true,
    });
    expect(mockSave.mock.calls[0][1]).not.toHaveProperty("client_secret");
    wrapper.unmount();
  });

  it("sends a typed secret trimmed and clears it after the save", async () => {
    mockGet.mockResolvedValue([{ ...github, has_secret: false }]);
    mockSave.mockResolvedValue({ ...github, has_secret: true });
    const wrapper = mountSection();
    await flushPromises();

    await wrapper.find("#oauth-github-client-secret").setValue("  new-secret  ");
    await saveButton(wrapper, 0)!.trigger("click");
    await flushPromises();

    expect(mockSave).toHaveBeenCalledWith("github", {
      client_id: "gh-client-id",
      client_secret: "new-secret",
      enabled: true,
    });
    expect(
      (wrapper.find("#oauth-github-client-secret").element as HTMLInputElement)
        .value,
    ).toBe("");
    expect(wrapper.find("#oauth-github-client-secret").attributes("placeholder"))
      .toBe(en.oauth.clientSecretSaved);
    wrapper.unmount();
  });

  it("renders the section in Vietnamese without raw keys", async () => {
    syncComposerLocale("vi");
    mockGet.mockResolvedValue([github]);
    const wrapper = mountSection();
    await flushPromises();

    expect(wrapper.text()).toContain(vietnamese.oauth.providers.google);
    expect(wrapper.text()).toContain(vietnamese.oauth.callback);
    expect(wrapper.text()).not.toContain("instance-settings.oauth");
    wrapper.unmount();
  });
});
