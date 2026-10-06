// I18N-2 acceptance: live switching on the login and profile forms keeps
// drafts, refreshes labels/errors/banners/titles reactively, and never
// changes password policy, triggers, required marks, or navigation.
/* global document: readonly, window: readonly, HTMLInputElement: readonly */
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { createMemoryHistory, createRouter } from "vue-router";
import { defineComponent, h, nextTick } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/profile/api/profile", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/profile/api/profile")>();
  return { ...actual, patchDisplayName: vi.fn(), changePassword: vi.fn() };
});

vi.mock("@/features/profile/api/sessions", () => ({
  listSessions: vi.fn(),
  revokeSession: vi.fn(),
  revokeOtherSessions: vi.fn(),
}));

import { changePassword } from "@/features/profile/api/profile";
import {
  listSessions,
  revokeOtherSessions,
} from "@/features/profile/api/sessions";
import ChangePasswordForm from "@/features/profile/components/ChangePasswordForm.vue";
import SessionsPanel from "@/features/profile/components/SessionsPanel.vue";
import LoginPage from "@/features/auth/pages/LoginPage.vue";
import AuthLayout from "@/app/layouts/AuthLayout.vue";
import AppTopbar from "@/app/layouts/AppTopbar.vue";
import { provideMobileNav } from "@/app/layouts/useMobileNav";
import MeCard from "@/app/layouts/MeCard.vue";
import LanguageSelect from "@/shared/ui/LanguageSelect.vue";
import { applyRouteTitle, router } from "@/app/router/index";
import { useAuthStore } from "@/features/auth";
import { useTeamsStore } from "@/features/teams";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";
import type { User } from "@/shared/api/token";

const mockChange = vi.mocked(changePassword);
const mockList = vi.mocked(listSessions);
const mockRevokeOthers = vi.mocked(revokeOtherSessions);

function seedAuth(): void {
  setActivePinia(createPinia());
  const auth = useAuthStore();
  auth.user = {
    id: "u-1",
    email: "ada@gotham.dev",
    created_at: "2026-03-04T12:00:00Z",
    display_name: "Ada",
    has_password: true,
    is_platform_admin: false,
  } as User;
  auth.accessToken = "access";
  auth.refreshToken = "refresh";
}

function shell(child: object) {
  return defineComponent({
    render() {
      return h(NMessageProvider, null, { default: () => h(child as never) });
    },
  });
}

function testRouter() {
  const stub = { template: "<div />" };
  const test = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/login", name: "login", component: stub },
      { path: "/dashboard", name: "dashboard", component: stub },
    ],
  });
  return test;
}

async function mountWith(child: object, route = "/login") {
  const test = testRouter();
  await test.push(route);
  await test.isReady();
  const wrapper = mount(shell(child), {
    attachTo: globalThis.document.body,
    global: { plugins: [test, i18n], stubs: { transition: false } },
  });
  await nextTick();
  await flushPromises();
  return { wrapper, router: test };
}

async function setInput(
  wrapper: ReturnType<typeof mount>,
  id: string,
  value: string,
): Promise<void> {
  const input = wrapper.find(id);
  expect(input.exists(), `expected input ${id}`).toBe(true);
  await input.setValue(value);
  await nextTick();
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  window.localStorage.removeItem("gotham-locale");
  if (!globalThis.window.matchMedia) {
    globalThis.window.matchMedia = (() => ({
      matches: false,
      media: "",
      addEventListener: () => {},
      removeEventListener: () => {},
    })) as unknown as typeof globalThis.window.matchMedia;
  }
  vi.restoreAllMocks();
  globalThis.document.body.innerHTML = "";
  document.title = "Gotham";
});

describe("login live switch", () => {
  it("refreshes labels without losing the draft, navigating, or calling the API", async () => {
    seedAuth();
    const auth = useAuthStore();
    auth.accessToken = null;
    auth.user = null;
    const login = vi.spyOn(auth, "login").mockResolvedValue(undefined);
    const { wrapper, router: test } = await mountWith(LoginPage);
    expect(wrapper.text()).toContain("Sign in");

    await setInput(wrapper, "#login-email", "ada@gotham.dev");
    await setInput(wrapper, "#login-password", "secret");
    setLocale("vi", null);
    await nextTick();
    await flushPromises();

    expect(wrapper.text()).toContain("Đăng nhập");
    expect(
      (wrapper.find("#login-email").element as HTMLInputElement).value,
    ).toBe("ada@gotham.dev");
    expect(
      (wrapper.find("#login-password").element as HTMLInputElement).value,
    ).toBe("secret");
    expect(test.currentRoute.value.name).toBe("login");
    expect(login).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("refreshes visible field errors without showing pristine errors", async () => {
    seedAuth();
    const auth = useAuthStore();
    auth.accessToken = null;
    auth.user = null;
    const { wrapper } = await mountWith(LoginPage);

    // Pristine form: switching shows no errors anywhere.
    setLocale("vi", null);
    await nextTick();
    await flushPromises();
    expect(wrapper.find(".n-form-item-blank--error").exists()).toBe(false);

    // Submit empty: English feedback appears, then refreshes in Vietnamese
    // while the typed draft survives.
    setLocale("en", null);
    await nextTick();
    await setInput(wrapper, "#login-email", "not-an-email");
    const submit = wrapper
      .findAll("button")
      .find((button) => button.text() === "Sign in");
    expect(submit, "expected a submit button").toBeDefined();
    await submit!.trigger("click");
    await flushPromises();
    await nextTick();
    expect(wrapper.find(".n-form-item-feedback").text()).toContain(
      "Enter a valid email address",
    );
    setLocale("vi", null);
    await nextTick();
    await flushPromises();
    expect(wrapper.find(".n-form-item-feedback").text()).toContain(
      "Nhập địa chỉ email hợp lệ",
    );
    expect(
      (wrapper.find("#login-email").element as HTMLInputElement).value,
    ).toBe("not-an-email");
    wrapper.unmount();
  });
});

describe("route titles", () => {
  it("stores stable keys for the owned auth/profile routes", () => {
    const routes = router.getRoutes();
    const titleOf = (name: string): unknown =>
      routes.find((route) => route.name === name)?.meta.titleKey;
    expect(titleOf("login")).toBe("titles.login");
    expect(titleOf("register")).toBe("titles.register");
    expect(titleOf("profile")).toBe("titles.profile");
    expect(titleOf("invite-accept")).toBe("titles.inviteAccept");
    expect(titleOf("oauth-callback")).toBe("titles.oauthCallback");
  });

  it("renders document.title in the active locale without navigating", () => {
    applyRouteTitle("titles.login");
    expect(document.title).toBe("Sign in — Gotham");
    setLocale("vi", null);
    applyRouteTitle("titles.login");
    expect(document.title).toBe("Đăng nhập — Gotham");
    applyRouteTitle(undefined);
    expect(document.title).toBe("Gotham");
  });
});

describe("shell selector and account menu", () => {
  it("places the same selector in the auth shell and the topbar", async () => {
    seedAuth();
    const auth = await mountWith(AuthLayout);
    expect(auth.wrapper.findComponent(LanguageSelect).exists()).toBe(true);
    auth.wrapper.unmount();
    globalThis.document.body.innerHTML = "";

    // AppTopbar consumes the mobile-nav injection, so it mounts under a
    // provider like the real AppLayout supplies.
    seedAuth();
    const provider = defineComponent({
      setup() {
        provideMobileNav();
        return () => h(AppTopbar);
      },
    });
    const test = testRouter();
    await test.push("/dashboard");
    await test.isReady();
    const top = mount(shell(provider), {
      attachTo: globalThis.document.body,
      global: { plugins: [test, i18n], stubs: { transition: false } },
    });
    await nextTick();
    await flushPromises();
    expect(top.findComponent(LanguageSelect).exists()).toBe(true);
    top.unmount();
  });

  it("renders the team role and menu through the common catalog", async () => {
    seedAuth();
    const teams = useTeamsStore();
    vi.spyOn(teams, "ensureTeams").mockResolvedValue(undefined);
    teams.loaded = true;
    teams.activeTeamId = "t-1";
    teams.teams = [
      {
        id: "t-1",
        name: "core",
        role: "admin",
        member_count: 1,
        created_at: "2026-01-01T00:00:00Z",
      } as never,
    ];
    const { wrapper } = await mountWith(MeCard);
    await nextTick();
    await flushPromises();
    expect(wrapper.find(".me-role").text()).toBe("admin");
    setLocale("vi", null);
    await nextTick();
    await flushPromises();
    expect(wrapper.find(".me-role").text()).toBe("quản trị viên");
    wrapper.unmount();
  });
});

describe("profile banners react to the locale", () => {
  it("refreshes a 429 change-password banner without altering policy", async () => {
    seedAuth();
    mockChange.mockRejectedValue({ status: 429, message: "" });
    const wrapper = mount(shell(ChangePasswordForm), {
      attachTo: globalThis.document.body,
      global: { plugins: [i18n], stubs: { transition: false } },
    });
    await nextTick();
    await flushPromises();
    await setInput(wrapper, "#profile-current-password", "old-secret-123");
    await setInput(wrapper, "#profile-new-password", "new-secret-1234");
    await setInput(wrapper, "#profile-confirm-password", "new-secret-1234");
    const submit = wrapper
      .findAll("button")
      .find((button) => button.text() === "Change password");
    await submit!.trigger("click");
    await flushPromises();
    await nextTick();
    expect(wrapper.find(".n-alert").text()).toContain("Too many attempts");
    setLocale("vi", null);
    await nextTick();
    await flushPromises();
    expect(wrapper.find(".n-alert").text()).toContain("Quá nhiều lần thử");
    // The draft survives the switch; the policy gate is unchanged.
    expect(
      (wrapper.find("#profile-new-password").element as HTMLInputElement)
        .value,
    ).toBe("new-secret-1234");
    wrapper.unmount();
  });

  it("refreshes the session re-auth notice after a 409", async () => {
    seedAuth();
    mockList.mockResolvedValue([
      {
        id: "s-current",
        user_agent: "Mozilla/5.0 Chrome/126.0",
        ip: "203.0.113.7",
        created_at: "2026-10-01T10:00:00Z",
        last_used_at: "2026-10-03T10:00:00Z",
        current: true,
      },
      {
        id: "s-other",
        user_agent: "Mozilla/5.0 Firefox/127.0",
        ip: "",
        created_at: "2026-09-20T10:00:00Z",
        last_used_at: "2026-10-02T10:00:00Z",
        current: false,
      },
    ]);
    mockRevokeOthers.mockRejectedValue({ status: 409, message: "conflict" });
    const test = testRouter();
    await test.push("/");
    await test.isReady();
    const wrapper = mount(shell(SessionsPanel), {
      attachTo: globalThis.document.body,
      global: { plugins: [test, i18n], stubs: { transition: false } },
    });
    await flushPromises();
    await nextTick();
    await nextTick();
    await wrapper
      .findAll("button")
      .find((button) => button.text() === "Sign out all other devices")!
      .trigger("click");
    await flushPromises();
    await nextTick();
    const popover = globalThis.document.body.querySelector(".n-popconfirm");
    const positive = [...popover!.querySelectorAll("button")].find((button) =>
      button.textContent?.includes("Sign out others"),
    )!;
    positive.dispatchEvent(new globalThis.MouseEvent("click", { bubbles: true }));
    await flushPromises();
    await nextTick();
    expect(wrapper.text()).toContain("predates session management");
    setLocale("vi", null);
    await nextTick();
    await flushPromises();
    expect(wrapper.text()).toContain("Đăng nhập lại");
    wrapper.unmount();
  });
});
