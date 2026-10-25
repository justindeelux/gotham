import { createPinia, setActivePinia } from "pinia";
import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { defineComponent } from "vue";

import { router } from "@/app/router";
import NoPermissionPage from "@/app/pages/NoPermissionPage.vue";
import { useAuthStore } from "@/features/auth";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const RouterLinkStub = defineComponent({
  props: ["to"],
  template: `<a href="#" @click.prevent="() => undefined"><slot :navigate="() => undefined" /></a>`,
});

function signInAsOwner(isAdmin: boolean): void {
  const auth = useAuthStore();
  auth.accessToken = "test-access";
  auth.refreshToken = "test-refresh";
  auth.user = {
    id: "u-1",
    email: "ada@gotham.dev",
    display_name: "Ada",
    created_at: "2026-01-01T00:00:00Z",
    ...(isAdmin ? { role: "owner" } : {}),
  };
}

beforeEach(() => {
  setActivePinia(createPinia());
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
});

afterEach(() => {
  globalThis.document.body.innerHTML = "";
});

describe("owner route guard", () => {
  it("flags the admin-only routes and the permission page wiring", () => {
    const byName = new Map(router.getRoutes().map((route) => [route.name, route]));
    for (const name of ["files", "server-files", "servers", "domains", "users", "instance", "updates"]) {
      expect(byName.get(name)?.meta.requiresOwner).toBe(true);
    }
    expect(byName.get("dashboard")?.meta.requiresOwner).toBeFalsy();
    const forbidden = byName.get("forbidden");
    expect(forbidden?.path).toBe("/forbidden");
    expect(forbidden?.meta.titleKey).toBe("titles.forbidden");
    const catchAll = router.getRoutes().find((route) => route.path === "/:pathMatch(.*)*");
    expect(catchAll).toBeDefined();
  });

  it("sends a non-admin to the permission page on direct navigation", async () => {
    signInAsOwner(false);
    await router.push("/domains");
    expect(router.currentRoute.value.name).toBe("forbidden");
    await router.push("/settings/updates");
    expect(router.currentRoute.value.name).toBe("forbidden");
    await router.push("/settings/instance");
    expect(router.currentRoute.value.name).toBe("forbidden");
  });

  it("lets the owner through to the owner routes", async () => {
    signInAsOwner(true);
    await router.push("/domains");
    expect(router.currentRoute.value.name).toBe("domains");
    await router.push("/settings/updates");
    expect(router.currentRoute.value.name).toBe("updates");
    await router.push("/settings/instance");
    expect(router.currentRoute.value.name).toBe("instance");
  });
});

describe("permission page", () => {
  it("renders the denial copy and a dashboard exit in both locales", async () => {
    const wrapper = mount(NoPermissionPage, {
      attachTo: globalThis.document.body,
      global: {
        plugins: [i18n],
        stubs: { RouterLink: RouterLinkStub },
      },
    });
    expect(wrapper.text()).toContain("You don't have permission to view this");
    expect(wrapper.text()).toContain("owner");
    expect(wrapper.text()).toContain("Back to dashboard");
    syncComposerLocale("vi");
    await flushPromises();
    expect(wrapper.text()).toContain("Bạn không có quyền xem trang này");
    expect(wrapper.text()).toContain("Quay lại bảng điều khiển");
    wrapper.unmount();
  });
});
