/* global HTMLElement: readonly, KeyboardEvent: readonly */
import { NDropdown } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { flushPromises, mount } from "@vue/test-utils";
import { nextTick } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import AccountMenu from "@/app/layouts/AccountMenu.vue";
import { useAuthStore } from "@/features/auth";
import { useTeamsStore } from "@/features/teams";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

beforeEach(() => {
  setActivePinia(createPinia());
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
});

afterEach(() => {
  globalThis.document.body.innerHTML = "";
  vi.restoreAllMocks();
});

async function mountMenu(compact: boolean, displayName = "Ada Lovelace") {
  const auth = useAuthStore();
  auth.user = {
    id: "u-1",
    email: "ada@gotham.dev",
    display_name: displayName,
    created_at: "2026-01-01T00:00:00Z",
  };
  const teams = useTeamsStore();
  teams.loaded = true;
  teams.activeTeamId = "t-1";
  teams.teams = [{
    id: "t-1",
    name: "Core",
    role: "admin",
    member_count: 1,
    created_at: "2026-01-01T00:00:00Z",
  }];
  vi.spyOn(teams, "ensureTeams").mockResolvedValue(undefined);
  const page = { template: "<div />" };
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", component: page },
      { path: "/login", name: "login", component: page },
      { path: "/settings/profile", name: "profile", component: page },
    ],
  });
  await router.push("/");
  const wrapper = mount(AccountMenu, {
    props: { compact },
    attachTo: globalThis.document.body,
    global: { plugins: [router, i18n], stubs: { transition: false } },
  });
  await wrapper.find("button").trigger("click");
  await flushPromises();
  return { wrapper, router, auth };
}

describe("shared account menu", () => {
  it.each([false, true])("shows user info and separated actions (compact: %s)", async (compact) => {
    const { wrapper } = await mountMenu(compact);
    const header = globalThis.document.querySelector(".account-menu-header")!;
    expect(header.textContent).toContain("Ada Lovelace");
    expect(header.textContent).toContain("ada@gotham.dev");
    expect(header.textContent).toContain("admin");
    expect(header.querySelector("button, a, [tabindex]")).toBeNull();
    const dropdown = wrapper.findComponent(NDropdown);
    expect(dropdown.props("options").map((option) => option.key)).toEqual([
      "user-info", "header-divider", "profile", "footer-divider", "sign-out",
    ]);
    expect(dropdown.props("keyboard")).toBe(true);
    expect(globalThis.document.querySelector<HTMLElement>(".n-dropdown")!.style.minWidth)
      .toBe("240px");
    wrapper.unmount();
  });

  it("falls back to email and refreshes open-menu labels and role with the locale", async () => {
    const { wrapper } = await mountMenu(true, "   ");
    const header = globalThis.document.querySelector(".account-menu-header")!;
    expect(header.querySelector(".account-menu-name")!.textContent).toBe("ada@gotham.dev");
    syncComposerLocale("vi");
    await nextTick();
    await flushPromises();
    expect(header.querySelector(".account-menu-role")!.textContent)
      .toBe(i18n.global.t("common.roles.admin"));
    const labels = [...globalThis.document.querySelectorAll(".n-dropdown-option")]
      .map((node) => node.textContent?.trim());
    expect(labels).toEqual([i18n.global.t("shell.profile"), i18n.global.t("shell.signOut")]);
    wrapper.unmount();
  });

  it("supports keyboard selection", async () => {
    const { wrapper, router } = await mountMenu(true);
    globalThis.document.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true }));
    await nextTick();
    globalThis.document.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
    await flushPromises();
    expect(router.currentRoute.value.name).toBe("profile");
    wrapper.unmount();
  });

  it("redirects to login even when revoking the session fails", async () => {
    const { wrapper, auth, router } = await mountMenu(true);
    const logout = vi.spyOn(auth, "logout").mockRejectedValue(new Error("Revoke failed"));
    wrapper.findComponent(NDropdown).vm.$emit("select", "sign-out");
    await flushPromises();
    expect(logout).toHaveBeenCalledOnce();
    expect(router.currentRoute.value.name).toBe("login");
    wrapper.unmount();
  });
});
