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

vi.mock("@/features/teams", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/teams")>();
  return { ...actual, acceptInvite: vi.fn() };
});

import { changePassword, patchDisplayName } from "@/features/profile/api/profile";
import {
  listSessions,
  revokeOtherSessions,
} from "@/features/profile/api/sessions";
import ChangePasswordForm from "@/features/profile/components/ChangePasswordForm.vue";
import DisplayNameForm from "@/features/profile/components/DisplayNameForm.vue";
import SessionsPanel from "@/features/profile/components/SessionsPanel.vue";
import SessionRow from "@/features/profile/components/SessionRow.vue";
import LoginPage from "@/features/auth/pages/LoginPage.vue";
import InviteAcceptPage from "@/features/auth/pages/InviteAcceptPage.vue";
import AuthLayout from "@/app/layouts/AuthLayout.vue";
import AppTopbar from "@/app/layouts/AppTopbar.vue";
import { provideMobileNav } from "@/app/layouts/useMobileNav";
import MeCard from "@/app/layouts/MeCard.vue";
import LanguageSelect from "@/shared/ui/LanguageSelect.vue";
import { applyRouteTitle, router } from "@/app/router/index";
import { acceptInvite } from "@/features/teams";
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
const mockPatch = vi.mocked(patchDisplayName);
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

  it("refreshes pre-submit blur errors without showing pristine errors", async () => {
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

    // Blur with a bad value: English feedback appears before any submit.
    setLocale("en", null);
    await nextTick();
    await setInput(wrapper, "#login-email", "not-an-email");
    await wrapper.find("#login-email").trigger("blur");
    await flushPromises();
    await nextTick();
    expect(wrapper.find(".n-form-item-feedback").text()).toContain(
      "Enter a valid email address",
    );
    // Only the blurred field shows feedback; the untouched password does not.
    expect(wrapper.findAll(".n-form-item-blank--error")).toHaveLength(1);

    // Switching refreshes exactly the visible error; draft and route kept.
    setLocale("vi", null);
    await nextTick();
    await flushPromises();
    expect(wrapper.find(".n-form-item-feedback").text()).toContain(
      "Nhập địa chỉ email hợp lệ",
    );
    expect(wrapper.findAll(".n-form-item-blank--error")).toHaveLength(1);
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
  it("refreshes a pre-submit display-name error and stays pristine after success", async () => {
    seedAuth();
    const wrapper = mount(shell(DisplayNameForm), {
      attachTo: globalThis.document.body,
      global: { plugins: [i18n], stubs: { transition: false } },
    });
    await nextTick();
    await flushPromises();

    // Blur with a 65-character name: feedback before any submit.
    await setInput(wrapper, "#profile-display-name", "x".repeat(65));
    await wrapper.find("#profile-display-name").trigger("blur");
    await flushPromises();
    await nextTick();
    expect(wrapper.find(".n-form-item-feedback").text()).toContain(
      "Display name must be 1-64 characters",
    );
    setLocale("vi", null);
    await nextTick();
    await flushPromises();
    expect(wrapper.find(".n-form-item-feedback").text()).toContain(
      "Tên hiển thị phải từ 1-64 ký tự",
    );

    // Fix the value and submit successfully: the banner clears and a later
    // switch shows no pristine errors on the untouched form.
    setLocale("en", null);
    await nextTick();
    await setInput(wrapper, "#profile-display-name", "Ada L");
    mockPatch.mockResolvedValue({
      id: "u-1",
      email: "ada@gotham.dev",
      created_at: "2026-03-04T12:00:00Z",
      display_name: "Ada L",
    });
    const submit = wrapper
      .findAll("button")
      .find((button) => button.text() === "Save display name");
    await submit!.trigger("click");
    await flushPromises();
    await nextTick();
    expect(mockPatch).toHaveBeenCalledWith("Ada L");
    setLocale("vi", null);
    await nextTick();
    await flushPromises();
    expect(wrapper.find(".n-form-item-blank--error").exists()).toBe(false);
    wrapper.unmount();
  });

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

  it("keeps a cleared change-password form pristine on locale switch", async () => {
    seedAuth();
    mockChange.mockResolvedValue({
      user: {
        id: "u-1",
        email: "ada@gotham.dev",
        created_at: "2026-03-04T12:00:00Z",
        display_name: "Ada",
        has_password: true,
        is_platform_admin: false,
      },
      access_token: "new-access",
      token_type: "Bearer",
      expires_in: 900,
      refresh_token: "new-refresh",
    });
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
    expect(mockChange).toHaveBeenCalledTimes(1);
    // Success clears the fields with no errors showing.
    for (const id of [
      "#profile-current-password",
      "#profile-new-password",
      "#profile-confirm-password",
    ]) {
      expect(
        (wrapper.find(id).element as HTMLInputElement).value,
        `expected ${id} to clear on success`,
      ).toBe("");
    }
    expect(wrapper.find(".n-form-item-blank--error").exists()).toBe(false);
    // A later locale switch must not surface required errors on the
    // cleared pristine fields, and issues no second API call.
    setLocale("vi", null);
    await nextTick();
    await flushPromises();
    expect(wrapper.find(".n-form-item-blank--error").exists()).toBe(false);
    expect(mockChange).toHaveBeenCalledTimes(1);
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

describe("unknown diagnostics carry a localized summary", () => {
  it("shows a reactive summary plus raw detail on login failure", async () => {
    seedAuth();
    const auth = useAuthStore();
    auth.accessToken = null;
    auth.user = null;
    const login = vi.spyOn(auth, "login").mockRejectedValue({
      status: 401,
      message: "invalid email or password",
    });
    const { wrapper } = await mountWith(LoginPage);
    await setInput(wrapper, "#login-email", "ada@gotham.dev");
    await setInput(wrapper, "#login-password", "wrong");
    const submit = wrapper
      .findAll("button")
      .find((button) => button.text() === "Sign in");
    await submit!.trigger("click");
    await flushPromises();
    await nextTick();
    expect(login).toHaveBeenCalledTimes(1);
    expect(wrapper.find(".n-alert").text()).toContain(
      "Request failed: invalid email or password",
    );
    setLocale("vi", null);
    await nextTick();
    await flushPromises();
    // Localized summary in Vietnamese, raw diagnostic retained verbatim,
    // no second API call from the switch itself.
    expect(wrapper.find(".n-alert").text()).toContain(
      "Yêu cầu thất bại: invalid email or password",
    );
    expect(login).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("pairs the common summary with the raw team diagnostic on invite failure", async () => {
    seedAuth();
    vi.mocked(acceptInvite).mockRejectedValue({
      status: 410,
      message: "teams: invite consumed",
    });
    const InviteAcceptPageView = InviteAcceptPage;
    const test = testRouter();
    test.addRoute({
      path: "/invite/accept",
      name: "invite-accept",
      component: { template: "<div />" },
    });
    await test.push({ path: "/invite/accept", query: { token: "abc" } });
    await test.isReady();
    const wrapper = mount(shell(InviteAcceptPageView), {
      attachTo: globalThis.document.body,
      global: { plugins: [test, i18n], stubs: { transition: false } },
    });
    await flushPromises();
    await nextTick();
    await nextTick();
    expect(wrapper.find(".n-alert").text()).toContain(
      "Request failed: invite consumed",
    );
    setLocale("vi", null);
    await nextTick();
    await flushPromises();
    expect(wrapper.find(".n-alert").text()).toContain(
      "Yêu cầu thất bại: invite consumed",
    );
    wrapper.unmount();
  });
});

describe("session row invalid timestamps", () => {
  it("renders the unknown-time key instead of the unknown-IP key", async () => {
    seedAuth();
    const wrapper = mount(SessionRow, {
      props: {
        session: {
          id: "s-bad",
          user_agent: "Mozilla/5.0 Chrome/126.0",
          ip: "",
          created_at: "not-a-date",
          last_used_at: "also-bad",
          current: false,
        },
        busy: false,
      },
      attachTo: globalThis.document.body,
      global: { plugins: [i18n], stubs: { transition: false } },
    });
    await nextTick();
    const time = wrapper.find("time");
    expect(time.attributes("title")).toBe("unknown");
    setLocale("vi", null);
    await nextTick();
    expect(wrapper.find("time").attributes("title")).toBe("không rõ");
    // The IP fallback still uses its own key.
    expect(wrapper.text()).toContain("không rõ");
    wrapper.unmount();
  });
});

describe("phone topbar keeps essential controls", () => {
  it("yields search and stubs under 640px without hiding language or account", async () => {
    const { readFileSync } = await import("node:fs");
    const { dirname, resolve } = await import("node:path");
    const { fileURLToPath } = await import("node:url");
    const root = resolve(
      dirname(fileURLToPath(import.meta.url)),
      "..",
    );
    const topbar = readFileSync(
      resolve(root, "src/app/layouts/AppTopbar.vue"),
      "utf8",
    );
    const phone = topbar.match(/@media\s*\(max-width:\s*640px\)\s*\{([\s\S]*?)\n\}/);
    expect(phone, "expected a 640px topbar rule").not.toBeNull();
    const rule = phone![1];
    // Non-essential stubs yield room...
    expect(rule).toMatch(/\.topbar\s+\.search[\s\S]*display:\s*none/);
    expect(rule).toMatch(/\.is-stub[\s\S]*display:\s*none/);
    // ...while the language selector and account controls are never hidden.
    expect(topbar).not.toMatch(
      /language-select[\s\S]{0,120}?display:\s*none/,
    );
    expect(rule).not.toContain("language-select");
  });
});
