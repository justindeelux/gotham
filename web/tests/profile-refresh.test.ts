// Profile account refresh (PF-6, JUS-27 follow-up): ProfilePage refreshes the
// account from GET /auth/me on every mount so a session restored from
// localStorage cannot serve stale facts, and a slow refresh never overwrites
// a newer display name saved while it was in flight.
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { createMemoryHistory, createRouter } from "vue-router";
import { defineComponent, h, nextTick } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import type { VueWrapper } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/shared/api/http", () => ({
  http: { get: vi.fn(), patch: vi.fn(), post: vi.fn(), delete: vi.fn() },
}));

vi.mock("@/features/profile/api/sessions", () => ({
  listSessions: vi.fn().mockResolvedValue([]),
  revokeSession: vi.fn(),
  revokeOtherSessions: vi.fn(),
}));

import { http } from "@/shared/api/http";
import {
  clearSession as clearTokenSession,
  setSession as persistTokenSession,
} from "@/shared/api/token";
import ProfilePage from "@/features/profile/pages/ProfilePage.vue";
import { useAuthStore } from "@/features/auth";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";
import type { User } from "@/shared/api/token";

const mockGet = vi.mocked(http.get);

function staleUser(): User {
  return {
    id: "u-1",
    email: "ada@gotham.dev",
    created_at: "2026-03-04T12:00:00Z",
    display_name: "Ada",
    has_password: true,
    is_platform_admin: false,
  };
}

function freshUser(): User {
  return { ...staleUser(), is_platform_admin: true };
}

function seedAuth(): void {
  setActivePinia(createPinia());
  const auth = useAuthStore();
  auth.user = staleUser();
  auth.accessToken = "access";
  auth.refreshToken = "refresh";
}

function shell() {
  return defineComponent({
    render() {
      return h(NMessageProvider, null, {
        default: () => h(ProfilePage as never),
      });
    },
  });
}

async function mountPage(): Promise<VueWrapper> {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/", component: { template: "<div />" } }],
  });
  await router.push("/");
  await router.isReady();
  const wrapper = mount(shell(), {
    attachTo: globalThis.document.body,
    global: { plugins: [router, i18n], stubs: { transition: false } },
  });
  await flushPromises();
  await nextTick();
  return wrapper;
}

function meCalls(): string[] {
  return mockGet.mock.calls
    .map((call) => call[0])
    .filter((url): url is string => url === "/auth/me");
}

function deferred<T>() {
  let resolve!: (_value: T) => void;
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  vi.restoreAllMocks();
  globalThis.document.body.innerHTML = "";
});

describe("ProfilePage account refresh", () => {
  it("refreshes the account once on mount", async () => {
    seedAuth();
    mockGet.mockResolvedValue({ data: { user: freshUser() } } as never);
    const fetchMe = vi.spyOn(useAuthStore(), "fetchMe");
    const wrapper = await mountPage();
    expect(fetchMe).toHaveBeenCalledTimes(1);
    expect(meCalls()).toHaveLength(1);
    wrapper.unmount();
  });

  it("corrects a stale stored account and flips the role label", async () => {
    seedAuth();
    mockGet.mockResolvedValue({ data: { user: freshUser() } } as never);
    const wrapper = await mountPage();
    expect(useAuthStore().user?.is_platform_admin).toBe(true);
    expect(wrapper.text()).toContain("Platform rolePlatform admin");
    wrapper.unmount();
  });

  it("refetches on remount", async () => {
    seedAuth();
    mockGet.mockResolvedValue({ data: { user: freshUser() } } as never);
    const first = await mountPage();
    first.unmount();
    const second = await mountPage();
    second.unmount();
    expect(meCalls()).toHaveLength(2);
  });

  it("keeps the stored user when the refresh fails", async () => {
    seedAuth();
    mockGet.mockRejectedValue({ status: 500, message: "boom" });
    const wrapper = await mountPage();
    await flushPromises();
    expect(useAuthStore().user).toEqual(staleUser());
    expect(wrapper.text()).toContain("Member");
    wrapper.unmount();
  });

  it("a slow refresh does not overwrite a newer display name", async () => {
    seedAuth();
    const gate = deferred<{ data: { user: User } }>();
    mockGet.mockReturnValue(gate.promise as never);
    const wrapper = await mountPage();
    await flushPromises();
    // A display-name save lands while the refresh is in flight (the same
    // setUser path as useDisplayNameForm).
    useAuthStore().setUser({ ...staleUser(), display_name: "Ada L" });
    // The stale server read resolves afterwards and must be discarded.
    gate.resolve({ data: { user: staleUser() } });
    await flushPromises();
    await nextTick();
    expect(useAuthStore().user?.display_name).toBe("Ada L");
    wrapper.unmount();
  });

  it("applies the retried read after a 401 refresh rotation", async () => {
    seedAuth();
    const gate = deferred<{ data: { user: User } }>();
    mockGet.mockReturnValue(gate.promise as never);
    const wrapper = await mountPage();
    await flushPromises();
    // The first /me 401s; the interceptor rotates the token pair, which
    // installs the same account through the token module (no local write).
    persistTokenSession({
      user: { ...staleUser() },
      accessToken: "rotated-access",
      refreshToken: "rotated-refresh",
    });
    // The retried /me resolves with the corrected facts and still applies.
    gate.resolve({ data: { user: freshUser() } });
    await flushPromises();
    await nextTick();
    expect(useAuthStore().user?.is_platform_admin).toBe(true);
    expect(wrapper.text()).toContain("Platform rolePlatform admin");
    wrapper.unmount();
  });

  it("discards the read when a different account signs in meanwhile", async () => {
    seedAuth();
    const gate = deferred<{ data: { user: User } }>();
    mockGet.mockReturnValue(gate.promise as never);
    const wrapper = await mountPage();
    await flushPromises();
    const other: User = {
      ...staleUser(),
      id: "u-2",
      email: "bob@gotham.dev",
      display_name: "Bob",
    };
    useAuthStore().setSession({
      user: other,
      access_token: "other-access",
      refresh_token: "other-refresh",
    });
    gate.resolve({ data: { user: freshUser() } });
    await flushPromises();
    await nextTick();
    expect(useAuthStore().user).toEqual(other);
    wrapper.unmount();
  });

  it("never resurrects a session cleared meanwhile", async () => {
    seedAuth();
    const gate = deferred<{ data: { user: User } }>();
    mockGet.mockReturnValue(gate.promise as never);
    const wrapper = await mountPage();
    await flushPromises();
    useAuthStore().clearSession();
    gate.resolve({ data: { user: freshUser() } });
    await flushPromises();
    await nextTick();
    expect(useAuthStore().user).toBeNull();
    expect(useAuthStore().isAuthenticated).toBe(false);
    wrapper.unmount();
  });

  it("applies the response when the stored user is null at start", async () => {
    setActivePinia(createPinia());
    const auth = useAuthStore();
    auth.user = null;
    auth.accessToken = "access";
    auth.refreshToken = "refresh";
    expect(auth.user).toBeNull();
    mockGet.mockResolvedValue({ data: { user: freshUser() } } as never);
    await auth.fetchMe();
    expect(auth.user).toEqual(freshUser());
  });

  it("applies the mount refresh for a token-only session", async () => {
    setActivePinia(createPinia());
    const auth = useAuthStore();
    auth.user = null;
    auth.accessToken = "access";
    auth.refreshToken = "refresh";
    // The App.vue boot condition: tokens restored, account not yet loaded.
    expect(auth.isAuthenticated && auth.user === null).toBe(true);
    mockGet.mockResolvedValue({ data: { user: freshUser() } } as never);
    const wrapper = await mountPage();
    expect(useAuthStore().user).toEqual(freshUser());
    expect(wrapper.text()).toContain("Platform rolePlatform admin");
    wrapper.unmount();
  });

  it("a null-user boot read is discarded when another account signs in", async () => {
    setActivePinia(createPinia());
    const auth = useAuthStore();
    auth.user = null;
    auth.accessToken = "access";
    auth.refreshToken = "refresh";
    const gate = deferred<{ data: { user: User } }>();
    mockGet.mockReturnValue(gate.promise as never);
    const pending = auth.fetchMe();
    await flushPromises();
    const other: User = {
      ...staleUser(),
      id: "u-2",
      email: "bob@gotham.dev",
      display_name: "Bob",
    };
    auth.setSession({
      user: other,
      access_token: "other-access",
      refresh_token: "other-refresh",
    });
    gate.resolve({ data: { user: staleUser() } });
    await pending;
    expect(useAuthStore().user).toEqual(other);
  });

  it("discards the read on a cross-tab account switch (id only, no local write)", async () => {
    seedAuth();
    const gate = deferred<{ data: { user: User } }>();
    mockGet.mockReturnValue(gate.promise as never);
    const wrapper = await mountPage();
    await flushPromises();
    // Another tab signs in as someone else: the token module notifies, the
    // store applies, and no sequence is bumped — only the id differs.
    const other: User = {
      ...staleUser(),
      id: "u-2",
      email: "bob@gotham.dev",
      display_name: "Bob",
    };
    persistTokenSession({
      user: other,
      accessToken: "other-access",
      refreshToken: "other-refresh",
    });
    gate.resolve({ data: { user: freshUser() } });
    await flushPromises();
    await nextTick();
    expect(useAuthStore().user).toEqual(other);
    wrapper.unmount();
  });

  it("does not resurrect on a cross-tab sign-out (id only, no local write)", async () => {
    seedAuth();
    const gate = deferred<{ data: { user: User } }>();
    mockGet.mockReturnValue(gate.promise as never);
    const wrapper = await mountPage();
    await flushPromises();
    // Another tab (or a forced 401 logout) clears the token module: the
    // store applies the empty session without a sequence bump.
    clearTokenSession();
    gate.resolve({ data: { user: freshUser() } });
    await flushPromises();
    await nextTick();
    expect(useAuthStore().user).toBeNull();
    expect(useAuthStore().isAuthenticated).toBe(false);
    wrapper.unmount();
  });
});
