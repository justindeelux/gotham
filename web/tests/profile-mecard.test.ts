// MeCard profile entry (JUS-27): dropdown items, display-name fallback,
// and navigation to the profile page.
import { NDropdown } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { createMemoryHistory, createRouter } from "vue-router";
import { nextTick } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

import MeCard from "../src/app/layouts/MeCard.vue";
import { useAuthStore } from "../src/features/auth";
import { useTeamsStore } from "../src/features/teams";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "../src/shared/i18n";

const stub = { template: "<div />" };

function testRouter() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", component: stub },
      { path: "/login", name: "login", component: stub },
      { path: "/settings/profile", name: "profile", component: stub },
    ],
  });
  return router;
}

async function mountCard(user: Record<string, unknown>) {
  setActivePinia(createPinia());
  const auth = useAuthStore();
  auth.user = {
    id: "u-1",
    email: "ada@gotham.dev",
    created_at: "2026-01-01T00:00:00Z",
    ...user,
  } as never;
  auth.accessToken = "access";
  const teams = useTeamsStore();
  vi.spyOn(teams, "ensureTeams").mockResolvedValue(undefined);
  teams.loaded = true;
  const router = testRouter();
  await router.push("/");
  await router.isReady();
  const wrapper = mount(MeCard, {
    attachTo: globalThis.document.body,
    global: { plugins: [router, i18n], stubs: { transition: false } },
  });
  await flushPromises();
  await nextTick();
  return { wrapper, router, auth };
}

/** openMenu clicks the account button and reads the dropdown labels. */
async function openMenu(
  wrapper: ReturnType<typeof mount>,
): Promise<string[]> {
  await wrapper.find("button.me-card").trigger("click");
  await flushPromises();
  await nextTick();
  await flushPromises();
  return [...globalThis.document.body.querySelectorAll(".n-dropdown-option")]
    .map((node) => node.textContent?.trim() ?? "")
    .filter((label) => label !== "");
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  globalThis.document.body.innerHTML = "";
  vi.restoreAllMocks();
});

describe("MeCard account menu", () => {
  it("lists Profile before Sign out", async () => {
    const { wrapper } = await mountCard({});
    expect(await openMenu(wrapper)).toEqual(["Profile", "Sign out"]);
    wrapper.unmount();
  });

  it("navigates to the profile page on Profile", async () => {
    const { wrapper, router } = await mountCard({});
    expect(await openMenu(wrapper)).toContain("Profile");
    wrapper.findComponent(NDropdown).vm.$emit("select", "profile");
    await flushPromises();
    await nextTick();
    expect(router.currentRoute.value.name).toBe("profile");
    wrapper.unmount();
  });

  it("shows the display name, falling back to the email", async () => {
    const named = await mountCard({ display_name: "Ada" });
    expect(named.wrapper.find(".me-email").text()).toBe("Ada");
    named.wrapper.unmount();
    globalThis.document.body.innerHTML = "";

    const unnamed = await mountCard({ display_name: undefined });
    expect(unnamed.wrapper.find(".me-email").text()).toBe("ada@gotham.dev");
    unnamed.wrapper.unmount();
  });

  it("signs out on Sign out", async () => {
    const { wrapper, router, auth } = await mountCard({});
    const logout = vi.spyOn(auth, "logout").mockResolvedValue(undefined);
    expect(await openMenu(wrapper)).toContain("Sign out");
    wrapper.findComponent(NDropdown).vm.$emit("select", "sign-out");
    await flushPromises();
    await nextTick();
    expect(logout).toHaveBeenCalled();
    expect(router.currentRoute.value.name).toBe("login");
    wrapper.unmount();
  });
});
