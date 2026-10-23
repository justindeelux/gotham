/* global window: readonly */
import { createPinia, setActivePinia } from "pinia";
import { defineComponent, h, nextTick } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import type { VueWrapper } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/shared/api/http", () => ({
  http: { get: vi.fn(), post: vi.fn(), patch: vi.fn() },
}));

import { http } from "@/shared/api/http";
import { useAuthStore } from "@/features/auth";
import { useMfaChallenge } from "@/features/auth/composables/useMfaChallenge";
import MfaChallengeForm from "@/features/auth/components/MfaChallengeForm.vue";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const post = vi.mocked(http.post);

const user = {
  id: "u-1",
  email: "ada@gotham.dev",
  created_at: "2026-01-01T00:00:00Z",
  has_password: true,
};

const pair = {
  user,
  access_token: "access",
  token_type: "Bearer",
  expires_in: 900,
  refresh_token: "refresh",
};

const HALF = 5 * 60 * 1000;

beforeEach(() => {
  setActivePinia(createPinia());
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  window.localStorage.clear();
  globalThis.document.body.innerHTML = "";
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe("auth store MFA challenge", () => {
  it("expires the challenge after the token lifetime", () => {
    vi.useFakeTimers();
    const authStore = useAuthStore();
    authStore.setMfaChallenge("half");

    expect(authStore.mfaPending).toBe(true);
    expect(authStore.isMfaChallengeLive()).toBe(true);

    vi.advanceTimersByTime(HALF + 1);
    expect(authStore.mfaPending).toBe(true);
    expect(authStore.isMfaChallengeLive()).toBe(false);
  });

  it("clears the challenge explicitly and on sign-out", () => {
    const authStore = useAuthStore();

    authStore.setMfaChallenge("half");
    authStore.clearMfaChallenge();
    expect(authStore.mfaPending).toBe(false);

    authStore.setMfaChallenge("half");
    authStore.clearSession();
    expect(authStore.mfaPending).toBe(false);
  });

  it("sets the challenge from a login that requires a second factor", async () => {
    const authStore = useAuthStore();
    post.mockResolvedValue({
      data: { ...pair, access_token: "", refresh_token: "", mfa_required: true, mfa_token: "half" },
    } as never);

    await authStore.login("ada@gotham.dev", "pw");
    expect(authStore.mfaPending).toBe(true);
    expect(authStore.isMfaChallengeLive()).toBe(true);
  });
});

function probeComponent(holder: { mfa?: ReturnType<typeof useMfaChallenge> }) {
  return defineComponent({
    setup() {
      holder.mfa = useMfaChallenge();
      return () => h("div");
    },
  });
}

async function mountProbe(holder: { mfa?: ReturnType<typeof useMfaChallenge> }) {
  const wrapper = mount(probeComponent(holder), {
    global: { plugins: [i18n] },
  });
  await nextTick();
  return wrapper;
}

describe("useMfaChallenge", () => {
  it("starts the session on a correct code", async () => {
    const authStore = useAuthStore();
    authStore.setMfaChallenge("half");
    post.mockResolvedValue({ data: pair } as never);

    const holder: { mfa?: ReturnType<typeof useMfaChallenge> } = {};
    const wrapper = await mountProbe(holder);
    holder.mfa!.form.code = "123456";

    await expect(holder.mfa!.submit()).resolves.toBe("success");
    expect(authStore.isAuthenticated).toBe(true);
    expect(authStore.mfaPending).toBe(false);
    expect(holder.mfa!.form.code).toBe("");
    wrapper.unmount();
  });

  it("clears and refocuses the code field on a wrong code", async () => {
    const authStore = useAuthStore();
    authStore.setMfaChallenge("half");
    post.mockRejectedValue({ status: 401, message: "invalid code" });

    const holder: { mfa?: ReturnType<typeof useMfaChallenge> } = {};
    const wrapper = await mountProbe(holder);
    const focus = vi.fn();
    holder.mfa!.codeRef.value = { focus };
    holder.mfa!.form.code = "000000";

    await expect(holder.mfa!.submit()).resolves.toBe("failed");
    expect(holder.mfa!.errorMessage.value).toContain("did not match");
    expect(holder.mfa!.form.code).toBe("");
    expect(focus).toHaveBeenCalled();
    expect(authStore.mfaPending).toBe(true);
    wrapper.unmount();
  });

  it("returns expired when the token lifetime has elapsed", async () => {
    const authStore = useAuthStore();
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-01-01T00:00:00Z"));
    authStore.setMfaChallenge("half");

    const holder: { mfa?: ReturnType<typeof useMfaChallenge> } = {};
    const wrapper = await mountProbe(holder);
    vi.advanceTimersByTime(HALF + 1);
    holder.mfa!.form.code = "123456";

    await expect(holder.mfa!.submit()).resolves.toBe("expired");
    expect(authStore.mfaPending).toBe(false);
    expect(post).not.toHaveBeenCalled();
    vi.useRealTimers();
    wrapper.unmount();
  });

  it("returns expired and clears the challenge on a lockout (429)", async () => {
    const authStore = useAuthStore();
    authStore.setMfaChallenge("half");
    post.mockRejectedValue({ status: 429, message: "too many incorrect codes" });

    const holder: { mfa?: ReturnType<typeof useMfaChallenge> } = {};
    const wrapper = await mountProbe(holder);
    holder.mfa!.form.code = "000000";

    await expect(holder.mfa!.submit()).resolves.toBe("expired");
    expect(authStore.mfaPending).toBe(false);
    wrapper.unmount();
  });

  it("switches the code field between authenticator and recovery input", async () => {
    const holder: { mfa?: ReturnType<typeof useMfaChallenge> } = {};
    const wrapper = await mountProbe(holder);
    expect(holder.mfa!.mode.value).toBe("totp");
    holder.mfa!.form.code = "AAA";

    holder.mfa!.toggleMode();
    expect(holder.mfa!.mode.value).toBe("recovery");
    expect(holder.mfa!.form.code).toBe("");
    wrapper.unmount();
  });
});

describe("MfaChallengeForm", () => {
  it("offers a one-time-code field with a numeric keypad by default", async () => {
    const authStore = useAuthStore();
    authStore.setMfaChallenge("half");
    const wrapper: VueWrapper = mount(MfaChallengeForm, {
      global: { plugins: [i18n] },
    });
    await nextTick();

    const input = wrapper.find("input");
    expect(input.attributes("autocomplete")).toBe("one-time-code");
    expect(input.attributes("inputmode")).toBe("numeric");
    expect(input.attributes("autofocus")).toBeDefined();

    await wrapper
      .findAll("button")
      .find((button) => button.text().includes("Use a recovery code"))!
      .trigger("click");
    await nextTick();
    expect(wrapper.find("input").attributes("inputmode")).toBe("text");
    wrapper.unmount();
  });

  it("emits expired instead of success when the challenge is dead", async () => {
    const wrapper: VueWrapper = mount(MfaChallengeForm, {
      global: { plugins: [i18n] },
    });
    await nextTick();

    await wrapper
      .findAll("button")
      .find((button) => button.text() === "Verify")!
      .trigger("click");
    await flushPromises();

    expect(wrapper.emitted("expired")).toBeTruthy();
    expect(wrapper.emitted("success")).toBeFalsy();
    wrapper.unmount();
  });
});
