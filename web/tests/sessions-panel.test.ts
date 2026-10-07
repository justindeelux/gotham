// Sessions panel (JUS-28): composable state, panel mounts, and the
// confirm flows. The PF-2 backend ships in parallel, so the session routes
// are mocked at the api-module boundary.
import { NButton, NMessageProvider, NPopconfirm } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { createMemoryHistory, createRouter } from "vue-router";
import type { Router } from "vue-router";
import { defineComponent, h, nextTick } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import type { VueWrapper } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/profile/api/sessions", () => ({
  listSessions: vi.fn(),
  revokeSession: vi.fn(),
  revokeOtherSessions: vi.fn(),
}));

import {
  listSessions,
  revokeOtherSessions,
  revokeSession,
} from "@/features/profile/api/sessions";
import SessionRow from "@/features/profile/components/SessionRow.vue";
import SessionsPanel from "@/features/profile/components/SessionsPanel.vue";
import { useSessionsPanel } from "@/features/profile/composables/useSessionsPanel";
import { useAuthStore } from "@/features/auth";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";
import type { User } from "@/shared/api/token";
import type { AuthSession } from "@/features/profile/schemas/sessions";

const mockList = vi.mocked(listSessions);
const mockRevoke = vi.mocked(revokeSession);
const mockRevokeOthers = vi.mocked(revokeOtherSessions);

const CHROME_UA =
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 " +
  "(KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36";
const FIREFOX_UA =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:127.0) Gecko/20100101 Firefox/127.0";

function currentSession(): AuthSession {
  return {
    id: "s-current",
    user_agent: CHROME_UA,
    ip: "203.0.113.7",
    created_at: "2026-10-01T10:00:00Z",
    last_used_at: "2026-10-03T10:00:00Z",
    current: true,
  };
}

function otherSession(): AuthSession {
  return {
    id: "s-other",
    user_agent: FIREFOX_UA,
    ip: "",
    created_at: "2026-09-20T10:00:00Z",
    last_used_at: "2026-10-02T10:00:00Z",
    current: false,
  };
}

function baseUser(): User {
  return {
    id: "u-1",
    email: "ada@gotham.dev",
    created_at: "2026-03-04T12:00:00Z",
    display_name: "Ada",
    has_password: true,
    is_platform_admin: false,
  };
}

function seedAuth(): void {
  setActivePinia(createPinia());
  const auth = useAuthStore();
  auth.user = baseUser();
  auth.accessToken = "access";
  auth.refreshToken = "refresh";
}

function testRouter(): Router {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", component: { template: "<div />" } },
      { path: "/login", name: "login", component: { template: "<div />" } },
    ],
  });
  return router;
}

function shell(child: object) {
  return defineComponent({
    render() {
      return h(NMessageProvider, null, { default: () => h(child as never) });
    },
  });
}

function mountOptions(router: Router) {
  return {
    attachTo: globalThis.document.body,
    global: { plugins: [router, i18n], stubs: { transition: false } },
  };
}

/** mountShell mounts a component under the message provider and a router. */
async function mountShell(
  child: object,
  router: Router,
): Promise<VueWrapper> {
  const wrapper = mount(shell(child), mountOptions(router));
  await router.push("/");
  await router.isReady();
  await nextTick();
  await flushPromises();
  await nextTick();
  return wrapper;
}

/** panelOf pulls the live composable out of a probe mount. */
function probeComponent(
  target: { panel?: ReturnType<typeof useSessionsPanel> },
): object {
  return defineComponent({
    setup() {
      target.panel = useSessionsPanel();
      return () => h("div");
    },
  });
}

/** clickPopoverPositive clicks the trigger, then the popover confirm. */
async function confirmThroughPopover(row: VueWrapper): Promise<void> {
  await row.findComponent(NButton).trigger("click");
  await flushPromises();
  await nextTick();
  await flushPromises();
  const popover = globalThis.document.body.querySelector(".n-popconfirm");
  expect(popover, "expected the confirm popover to open").not.toBeNull();
  const positive = [...popover!.querySelectorAll("button")].find((button) =>
    button.textContent?.includes("Sign out"),
  );
  expect(positive, "expected a Sign out confirm button").toBeDefined();
  positive!.dispatchEvent(new globalThis.MouseEvent("click", { bubbles: true }));
  await flushPromises();
  await nextTick();
  await flushPromises();
  await nextTick();
}

function rowWrappers(wrapper: VueWrapper): VueWrapper[] {
  return wrapper.findAllComponents(SessionRow) as unknown as VueWrapper[];
}

function buttonByLabel(wrapper: VueWrapper, label: string) {
  const target = wrapper
    .findAllComponents(NButton)
    .find((button) => button.text() === label);
  expect(target, `expected a button labelled ${label}`).toBeDefined();
  return target!;
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  vi.restoreAllMocks();
  globalThis.document.body.innerHTML = "";
});

describe("useSessionsPanel", () => {
  async function mountProbe(): Promise<{
    holder: { panel?: ReturnType<typeof useSessionsPanel> };
    wrapper: VueWrapper;
    router: Router;
    logout: ReturnType<typeof vi.fn>;
  }> {
    seedAuth();
    const holder: { panel?: ReturnType<typeof useSessionsPanel> } = {};
    const router = testRouter();
    const wrapper = await mountShell(probeComponent(holder), router);
    const logout = vi
      .spyOn(useAuthStore(), "logout")
      .mockResolvedValue(undefined);
    return { holder, wrapper, router, logout };
  }

  it("loads the list once per mount", async () => {
    mockList.mockResolvedValue([currentSession(), otherSession()]);
    const { holder, wrapper } = await mountProbe();
    await holder.panel!.load();
    expect(mockList).toHaveBeenCalledTimes(1);
    expect(holder.panel!.sessions.value).toHaveLength(2);
    expect(holder.panel!.loaded.value).toBe(true);
    expect(holder.panel!.loading.value).toBe(false);
    expect(holder.panel!.others.value).toHaveLength(1);
    wrapper.unmount();
  });

  it("reports a load failure with a retry", async () => {
    mockList.mockRejectedValueOnce({ status: 500, message: "boom" });
    const { holder, wrapper } = await mountProbe();
    await holder.panel!.load();
    expect(holder.panel!.errorMessage.value).toBe(
      "Could not load sessions. Try again.",
    );
    expect(holder.panel!.loaded.value).toBe(false);
    mockList.mockResolvedValue([currentSession()]);
    await holder.panel!.load();
    expect(holder.panel!.errorMessage.value).toBe("");
    expect(holder.panel!.sessions.value).toHaveLength(1);
    wrapper.unmount();
  });

  it("drops the row locally when the refresh fails after a revoke", async () => {
    mockList.mockResolvedValue([currentSession(), otherSession()]);
    const { holder, wrapper } = await mountProbe();
    await holder.panel!.load();
    mockRevoke.mockResolvedValue(undefined);
    mockList.mockRejectedValueOnce({ status: 500, message: "boom" });
    await holder.panel!.endSession(otherSession());
    expect(mockRevoke).toHaveBeenCalledTimes(1);
    expect(holder.panel!.sessions.value.map((row) => row.id)).toEqual([
      "s-current",
    ]);
    expect(holder.panel!.errorMessage.value).toBe(
      "Signed out, but the session list may be out of date.",
    );
    wrapper.unmount();
  });

  it("keeps only the current row when the revoke-others refresh fails", async () => {
    mockList.mockResolvedValue([currentSession(), otherSession()]);
    const { holder, wrapper } = await mountProbe();
    await holder.panel!.load();
    mockRevokeOthers.mockResolvedValue(undefined);
    mockList.mockRejectedValueOnce({ status: 500, message: "boom" });
    await holder.panel!.endOtherSessions();
    expect(holder.panel!.sessions.value.map((row) => row.id)).toEqual([
      "s-current",
    ]);
    expect(holder.panel!.errorMessage.value).toBe(
      "Signed out, but the session list may be out of date.",
    );
    wrapper.unmount();
  });

  it("applies only the latest load response", async () => {
    const { holder, wrapper } = await mountProbe();
    let resolveFirst!: (_list: AuthSession[]) => void;
    let resolveSecond!: (_list: AuthSession[]) => void;
    const first = new Promise<AuthSession[]>((resolve) => {
      resolveFirst = resolve;
    });
    const second = new Promise<AuthSession[]>((resolve) => {
      resolveSecond = resolve;
    });
    mockList.mockReturnValueOnce(first).mockReturnValueOnce(second);
    const older = holder.panel!.load();
    const newer = holder.panel!.load();
    resolveSecond([currentSession()]);
    await newer;
    resolveFirst([currentSession(), otherSession()]);
    await older;
    expect(holder.panel!.sessions.value).toHaveLength(1);
    expect(holder.panel!.loaded.value).toBe(true);
    expect(holder.panel!.loading.value).toBe(false);
    expect(holder.panel!.errorMessage.value).toBe("");
    wrapper.unmount();
  });

  it("lets a newer load win over a stale revoke refresh", async () => {
    mockList.mockResolvedValue([currentSession(), otherSession()]);
    const { holder, wrapper } = await mountProbe();
    await holder.panel!.load();
    mockRevoke.mockResolvedValue(undefined);
    let resolveRefresh!: (_list: AuthSession[]) => void;
    const refresh = new Promise<AuthSession[]>((resolve) => {
      resolveRefresh = resolve;
    });
    mockList.mockReturnValueOnce(refresh);
    const ending = holder.panel!.endSession(otherSession());
    await flushPromises();
    await flushPromises();
    mockList.mockResolvedValue([currentSession()]);
    await holder.panel!.load();
    resolveRefresh([currentSession(), otherSession()]);
    await ending;
    expect(holder.panel!.sessions.value.map((row) => row.id)).toEqual([
      "s-current",
    ]);
    wrapper.unmount();
  });

  it("ends another session and refreshes the list", async () => {
    mockList.mockResolvedValue([currentSession(), otherSession()]);
    const { holder, wrapper } = await mountProbe();
    await holder.panel!.load();
    mockList.mockResolvedValue([currentSession()]);
    await holder.panel!.endSession(otherSession());
    expect(mockRevoke).toHaveBeenCalledTimes(1);
    expect(mockRevoke).toHaveBeenCalledWith("s-other");
    expect(mockList).toHaveBeenCalledTimes(2);
    expect(holder.panel!.sessions.value).toHaveLength(1);
    expect(holder.panel!.errorMessage.value).toBe("");
    wrapper.unmount();
  });

  it("treats a 404 revoke as already gone and refreshes", async () => {
    mockList.mockResolvedValue([currentSession(), otherSession()]);
    const { holder, wrapper } = await mountProbe();
    await holder.panel!.load();
    mockRevoke.mockRejectedValueOnce({ status: 404, message: "gone" });
    mockList.mockResolvedValue([currentSession()]);
    await holder.panel!.endSession(otherSession());
    expect(holder.panel!.errorMessage.value).toBe("");
    expect(holder.panel!.sessions.value).toHaveLength(1);
    wrapper.unmount();
  });

  it("surfaces a failed revoke without losing the list", async () => {
    mockList.mockResolvedValue([currentSession(), otherSession()]);
    const { holder, wrapper } = await mountProbe();
    await holder.panel!.load();
    mockRevoke.mockRejectedValueOnce({ status: 500, message: "boom" });
    await holder.panel!.endSession(otherSession());
    expect(holder.panel!.errorMessage.value).toBe(
      "Could not sign out that session. Try again.",
    );
    expect(holder.panel!.sessions.value).toHaveLength(2);
    wrapper.unmount();
  });

  it("dedupes a concurrent double end into one request", async () => {
    mockList.mockResolvedValue([currentSession(), otherSession()]);
    const { holder, wrapper } = await mountProbe();
    await holder.panel!.load();
    mockRevoke.mockResolvedValue(undefined);
    mockList.mockResolvedValue([currentSession()]);
    const first = holder.panel!.endSession(otherSession());
    const second = holder.panel!.endSession(otherSession());
    await Promise.all([first, second]);
    expect(mockRevoke).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("ends the current session through the sign-out path", async () => {
    mockList.mockResolvedValue([currentSession(), otherSession()]);
    const { holder, wrapper, router, logout } = await mountProbe();
    await holder.panel!.load();
    mockRevoke.mockResolvedValue(undefined);
    await holder.panel!.endSession(currentSession());
    expect(mockRevoke).toHaveBeenCalledWith("s-current");
    expect(logout).toHaveBeenCalledTimes(1);
    expect(router.currentRoute.value.name).toBe("login");
    wrapper.unmount();
  });

  it("ends all other sessions and refreshes", async () => {
    mockList.mockResolvedValue([currentSession(), otherSession()]);
    const { holder, wrapper } = await mountProbe();
    await holder.panel!.load();
    mockRevokeOthers.mockResolvedValue(undefined);
    mockList.mockResolvedValue([currentSession()]);
    await holder.panel!.endOtherSessions();
    expect(mockRevokeOthers).toHaveBeenCalledTimes(1);
    expect(holder.panel!.sessions.value).toHaveLength(1);
    wrapper.unmount();
  });

  it("flags the 409 path instead of erroring", async () => {
    mockList.mockResolvedValue([currentSession(), otherSession()]);
    const { holder, wrapper } = await mountProbe();
    await holder.panel!.load();
    mockRevokeOthers.mockRejectedValueOnce({
      status: 409,
      message: "sign in again to manage other sessions",
    });
    await holder.panel!.endOtherSessions();
    expect(holder.panel!.needsReauth.value).toBe(true);
    expect(holder.panel!.errorMessage.value).toBe("");
    wrapper.unmount();
  });

  it("signs out again from the 409 path", async () => {
    const { holder, wrapper, router, logout } = await mountProbe();
    await holder.panel!.signOutHere();
    expect(logout).toHaveBeenCalledTimes(1);
    expect(router.currentRoute.value.name).toBe("login");
    wrapper.unmount();
  });

  it("still navigates when logout rejects", async () => {
    const { holder, wrapper, router, logout } = await mountProbe();
    logout.mockRejectedValueOnce(new Error("network down"));
    await expect(holder.panel!.signOutHere()).resolves.toBeUndefined();
    expect(logout).toHaveBeenCalledTimes(1);
    expect(router.currentRoute.value.name).toBe("login");
    wrapper.unmount();
  });
});

describe("SessionsPanel mount", () => {
  async function mountPanel(sessions: AuthSession[]): Promise<{
    wrapper: VueWrapper;
    router: Router;
    logout: ReturnType<typeof vi.fn>;
  }> {
    seedAuth();
    mockList.mockResolvedValue(sessions);
    const router = testRouter();
    const wrapper = await mountShell(SessionsPanel, router);
    const logout = vi
      .spyOn(useAuthStore(), "logout")
      .mockResolvedValue(undefined);
    return { wrapper, router, logout };
  }

  it("renders rows with the badge, ips, and times", async () => {
    const { wrapper } = await mountPanel([currentSession(), otherSession()]);
    const rows = rowWrappers(wrapper);
    expect(rows).toHaveLength(2);
    expect(rows[0]!.text()).toContain("This device");
    expect(rows[1]!.text()).not.toContain("This device");
    expect(wrapper.text()).toContain("Chrome on macOS");
    expect(wrapper.text()).toContain("Firefox on Windows");
    expect(wrapper.text()).toContain("203.0.113.7");
    expect(wrapper.text()).toContain("unknown");
    wrapper.unmount();
  });

  it("keeps the raw user agent in the row title", async () => {
    const { wrapper } = await mountPanel([currentSession()]);
    const row = wrapper.find(".session-row");
    expect(row.attributes("title")).toBe(CHROME_UA);
    wrapper.unmount();
  });

  it("shows the empty state without rows or actions", async () => {
    const { wrapper } = await mountPanel([]);
    expect(wrapper.text()).toContain("No active sessions.");
    expect(wrapper.findAllComponents(SessionRow)).toHaveLength(0);
    const revokeOthers = buttonByLabel(wrapper, "Sign out all other devices");
    expect(revokeOthers.props("disabled")).toBe(true);
    wrapper.unmount();
  });

  it("shows the error with a working retry", async () => {
    seedAuth();
    mockList.mockRejectedValueOnce({ status: 500, message: "boom" });
    const router = testRouter();
    const wrapper = await mountShell(SessionsPanel, router);
    expect(wrapper.find(".n-alert").text()).toContain(
      "Could not load sessions. Try again.",
    );
    mockList.mockResolvedValue([currentSession()]);
    await buttonByLabel(wrapper, "Retry").trigger("click");
    await flushPromises();
    await nextTick();
    expect(wrapper.findAllComponents(SessionRow)).toHaveLength(1);
    wrapper.unmount();
  });

  it("signs out one session through the confirm exactly once", async () => {
    const { wrapper } = await mountPanel([currentSession(), otherSession()]);
    mockList.mockResolvedValue([currentSession()]);
    const rows = rowWrappers(wrapper);
    await confirmThroughPopover(rows[1]!);
    expect(mockRevoke).toHaveBeenCalledTimes(1);
    expect(mockRevoke).toHaveBeenCalledWith("s-other");
    expect(rowWrappers(wrapper)).toHaveLength(1);
    wrapper.unmount();
  });

  it("keeps the list visible when a revoke fails", async () => {
    const { wrapper } = await mountPanel([currentSession(), otherSession()]);
    mockRevoke.mockRejectedValueOnce({ status: 500, message: "boom" });
    const rows = rowWrappers(wrapper);
    await confirmThroughPopover(rows[1]!);
    expect(mockRevoke).toHaveBeenCalledTimes(1);
    expect(wrapper.find(".n-alert").text()).toContain(
      "Could not sign out that session. Try again.",
    );
    expect(rowWrappers(wrapper)).toHaveLength(2);
    wrapper.unmount();
  });

  it("shows the stale notice with a working retry after a failed refresh", async () => {
    const { wrapper } = await mountPanel([currentSession(), otherSession()]);
    mockRevoke.mockResolvedValue(undefined);
    mockList.mockRejectedValueOnce({ status: 500, message: "boom" });
    const rows = rowWrappers(wrapper);
    await confirmThroughPopover(rows[1]!);
    expect(wrapper.find(".n-alert").text()).toContain(
      "Signed out, but the session list may be out of date.",
    );
    expect(rowWrappers(wrapper)).toHaveLength(1);
    mockList.mockResolvedValue([currentSession()]);
    await buttonByLabel(wrapper, "Retry").trigger("click");
    await flushPromises();
    await nextTick();
    expect(wrapper.find(".n-alert").exists()).toBe(false);
    wrapper.unmount();
  });

  it("signs out all other devices through the confirm exactly once", async () => {
    const { wrapper } = await mountPanel([currentSession(), otherSession()]);
    mockList.mockResolvedValue([currentSession()]);
    const trigger = buttonByLabel(wrapper, "Sign out all other devices");
    expect(trigger.props("disabled")).toBe(false);
    await trigger.trigger("click");
    await flushPromises();
    await nextTick();
    await flushPromises();
    const popover = globalThis.document.body.querySelector(".n-popconfirm");
    expect(popover).not.toBeNull();
    const positive = [...popover!.querySelectorAll("button")].find((button) =>
      button.textContent?.includes("Sign out others"),
    );
    expect(positive).toBeDefined();
    positive!.dispatchEvent(new globalThis.MouseEvent("click", { bubbles: true }));
    await flushPromises();
    await nextTick();
    await flushPromises();
    expect(mockRevokeOthers).toHaveBeenCalledTimes(1);
    expect(rowWrappers(wrapper)).toHaveLength(1);
    wrapper.unmount();
  });

  it("disables revoke-others with no other sessions", async () => {
    const { wrapper } = await mountPanel([currentSession()]);
    expect(
      buttonByLabel(wrapper, "Sign out all other devices").props("disabled"),
    ).toBe(true);
    wrapper.unmount();
  });

  it("signs out here when the current session ends", async () => {
    const { wrapper, router, logout } = await mountPanel([
      currentSession(),
      otherSession(),
    ]);
    const rows = rowWrappers(wrapper);
    await confirmThroughPopover(rows[0]!);
    expect(mockRevoke).toHaveBeenCalledTimes(1);
    expect(mockRevoke).toHaveBeenCalledWith("s-current");
    expect(logout).toHaveBeenCalledTimes(1);
    expect(router.currentRoute.value.name).toBe("login");
    wrapper.unmount();
  });

  it("offers sign-in-again on the 409 path", async () => {
    const { wrapper, router, logout } = await mountPanel([
      currentSession(),
      otherSession(),
    ]);
    mockRevokeOthers.mockRejectedValueOnce({
      status: 409,
      message: "sign in again to manage other sessions",
    });
    const trigger = buttonByLabel(wrapper, "Sign out all other devices");
    await trigger.trigger("click");
    await flushPromises();
    await nextTick();
    await flushPromises();
    const popover = globalThis.document.body.querySelector(".n-popconfirm");
    const positive = [...popover!.querySelectorAll("button")].find((button) =>
      button.textContent?.includes("Sign out others"),
    );
    positive!.dispatchEvent(new globalThis.MouseEvent("click", { bubbles: true }));
    await flushPromises();
    await nextTick();
    await flushPromises();
    expect(wrapper.text()).toContain(
      "Your sign-in predates session management.",
    );
    await buttonByLabel(wrapper, "Sign in again").trigger("click");
    await flushPromises();
    await nextTick();
    expect(logout).toHaveBeenCalledTimes(1);
    expect(router.currentRoute.value.name).toBe("login");
    wrapper.unmount();
  });

  it("leaves no stale list on remount", async () => {
    const first = await mountPanel([currentSession(), otherSession()]);
    expect(rowWrappers(first.wrapper)).toHaveLength(2);
    first.wrapper.unmount();
    const second = await mountPanel([currentSession()]);
    expect(rowWrappers(second.wrapper)).toHaveLength(1);
    expect(second.wrapper.text()).not.toContain("Firefox on Windows");
    second.wrapper.unmount();
  });

  it("renders the plural confirm body in the open popover", async () => {
    const { wrapper } = await mountPanel([
      currentSession(),
      otherSession(),
      { ...otherSession(), id: "s-third" },
    ]);
    await buttonByLabel(wrapper, "Sign out all other devices").trigger("click");
    await flushPromises();
    await nextTick();
    await flushPromises();
    const popover = globalThis.document.body.querySelector(".n-popconfirm");
    expect(popover, "expected the confirm popover to open").not.toBeNull();
    expect(popover!.textContent).toContain(
      "Sign out 2 other sessions? Those devices will need to sign in again.",
    );
    wrapper.unmount();
  });
});

describe("others confirm plural", () => {
  it("selects exact 0/1/many wording through the real compiler", async () => {
    const { i18n: composer } = await import("@/shared/i18n");
    const choose = (count: number): string =>
      String(
        composer.global.t(
          "profile.sessions.confirmOthers",
          { count },
          { plural: count },
        ),
      );
    expect(choose(0)).toBe(
      "Sign out 0 other sessions? Those devices will need to sign in again.",
    );
    expect(choose(1)).toBe(
      "Sign out 1 other session? Those devices will need to sign in again.",
    );
    expect(choose(5)).toBe(
      "Sign out 5 other sessions? Those devices will need to sign in again.",
    );
    const { setLocale } = await import("@/shared/i18n");
    setLocale("vi", null);
    try {
      expect(choose(1)).toBe(
        "Đăng xuất 1 phiên khác? Các thiết bị đó sẽ phải đăng nhập lại.",
      );
      expect(choose(5)).toBe(
        "Đăng xuất 5 phiên khác? Các thiết bị đó sẽ phải đăng nhập lại.",
      );
    } finally {
      resetLocaleState();
      syncComposerLocale("en");
    }
  });
});

describe("SessionRow", () => {
  it("marks the current row error and keeps others neutral", async () => {
    seedAuth();
    const router = testRouter();
    const current = await mountShell(
      defineComponent({
        render: () =>
          h(SessionRow, { session: currentSession(), busy: false }),
      }),
      router,
    );
    expect(current.findComponent(NButton).props("type")).toBe("error");
    current.unmount();
    const other = await mountShell(
      defineComponent({
        render: () => h(SessionRow, { session: otherSession(), busy: false }),
      }),
      router,
    );
    expect(other.findComponent(NButton).props("type")).toBe("default");
    other.unmount();
  });

  it("renders a busy sign-out without firing", async () => {
    seedAuth();
    const router = testRouter();
    const wrapper = await mountShell(
      defineComponent({
        render: () => h(SessionRow, { session: otherSession(), busy: true }),
      }),
      router,
    );
    expect(wrapper.findComponent(NButton).props("loading")).toBe(true);
    expect(wrapper.findComponent(NPopconfirm).exists()).toBe(true);
    wrapper.unmount();
  });

  it("titles times with date and time", async () => {
    seedAuth();
    const router = testRouter();
    const wrapper = await mountShell(
      defineComponent({
        render: () => h(SessionRow, { session: currentSession(), busy: false }),
      }),
      router,
    );
    const titles = wrapper.findAll("time").map((node) => node.attributes("title"));
    expect(titles).toHaveLength(2);
    for (const title of titles) {
      expect(title).toMatch(/2026/);
      expect(title).toContain(":");
    }
    wrapper.unmount();
  });
});
