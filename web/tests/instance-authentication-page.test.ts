import { NMessageProvider } from "naive-ui";
import { flushPromises, mount } from "@vue/test-utils";
import type { Component } from "vue";
import { defineComponent, h } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/instance-settings/api/instance", () => ({
  getInstanceSettings: vi.fn(),
  saveGeneral: vi.fn(),
  saveCleanupRetention: vi.fn(),
}));

vi.mock("@/features/instance-settings/api/oauthProviders", () => ({
  getOAuthProviders: vi.fn(),
  saveOAuthProvider: vi.fn(),
  oauthProviderNames: ["github", "google"],
}));

vi.mock("@/features/domains/api/proxy", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/domains/api/proxy")>();
  return {
    ...actual,
    listRouters: vi.fn().mockResolvedValue({ nodes: [], routers: [] }),
  };
});

import { getInstanceSettings } from "@/features/instance-settings/api/instance";
import type { InstanceState } from "@/features/instance-settings/api/instance";
import { getOAuthProviders } from "@/features/instance-settings/api/oauthProviders";
import AuthenticationPage from "@/features/instance-settings/pages/AuthenticationPage.vue";
import InstanceSettingsPage from "@/features/instance-settings/pages/InstanceSettingsPage.vue";
import en from "@/features/instance-settings/locales/en";
import vietnamese from "@/features/instance-settings/locales/vi";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

const state: InstanceState = {
  general: {
    control_plane_url: {
      value: "https://gotham.example.com",
      source: "db",
      locked: false,
    },
    instance_name: { value: "Gotham", source: "db", locked: false },
  },
  cleanup: { history_days: 30, backup_file_min_age_days: 7 },
};

const github = {
  name: "github",
  enabled: true,
  client_id: "gh-client-id",
  has_secret: true,
};

function mountPage(component: Component) {
  const host = defineComponent({
    render() {
      return h(NMessageProvider, null, { default: () => h(component) });
    },
  });
  return mount(host, {
    attachTo: globalThis.document.body,
    global: { plugins: [i18n], stubs: { transition: false } },
  });
}

beforeEach(() => {
  resetLocaleState();
  i18n.global.mergeLocaleMessage("en", { "instance-settings": en });
  i18n.global.mergeLocaleMessage("vi", { "instance-settings": vietnamese });
  syncComposerLocale("en");
  vi.mocked(getInstanceSettings).mockReset().mockResolvedValue(state);
  vi.mocked(getOAuthProviders).mockReset().mockResolvedValue([github]);
});

describe("Authentication page", () => {
  it("renders both provider cards on the owner-only settings page", async () => {
    const wrapper = mountPage(AuthenticationPage);
    await flushPromises();

    expect(wrapper.find("h1").text()).toBe(en.oauth.title);
    expect(wrapper.findAll(".n-card")).toHaveLength(2);
    expect(wrapper.text()).toContain("GitHub");
    expect(wrapper.text()).toContain("Google");
    expect(wrapper.find("#oauth-github-callback").exists()).toBe(true);
    wrapper.unmount();
  });

  it("keeps the General page free of the OAuth cards", async () => {
    const wrapper = mountPage(InstanceSettingsPage);
    await flushPromises();

    expect(wrapper.find("h1").text()).toBe(en.page.title);
    expect(wrapper.find("#oauth-github-client-id").exists()).toBe(false);
    expect(wrapper.text()).not.toContain("GitHub");
    expect(wrapper.text()).not.toContain("Google");
    wrapper.unmount();
  });

  it("renders the page title and cards in Vietnamese without raw keys", async () => {
    syncComposerLocale("vi");
    const wrapper = mountPage(AuthenticationPage);
    await flushPromises();

    expect(wrapper.find("h1").text()).toBe(vietnamese.oauth.title);
    expect(wrapper.text()).toContain(vietnamese.oauth.eyebrow);
    expect(wrapper.text()).toContain(vietnamese.oauth.providers.google);
    expect(wrapper.text()).not.toContain("instance-settings.oauth");
    wrapper.unmount();
  });
});
