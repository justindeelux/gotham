// Profile form mounts (JUS-27): errors, submit payloads, busy state,
// and per-mount state (typed passwords must not survive navigation).
import { NButton, NDescriptions, NFormItem, NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { createMemoryHistory, createRouter, RouterView } from "vue-router";
import { defineComponent, h, nextTick } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import type { VueWrapper } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/profile/api/profile", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/profile/api/profile")>();
  return { ...actual, patchDisplayName: vi.fn(), changePassword: vi.fn() };
});

import { changePassword, patchDisplayName } from "@/features/profile/api/profile";
import ChangePasswordForm from "@/features/profile/components/ChangePasswordForm.vue";
import DisplayNameForm from "@/features/profile/components/DisplayNameForm.vue";
import ProfileIdentityCard from "@/features/profile/components/ProfileIdentityCard.vue";
import { useAuthStore } from "@/features/auth";
import type { User } from "@/shared/api/token";

const mockPatch = vi.mocked(patchDisplayName);
const mockChange = vi.mocked(changePassword);

function shell(child: object) {
  return defineComponent({
    render() {
      return h(NMessageProvider, null, { default: () => h(child as never) });
    },
  });
}

function seedAuth(user: User): void {
  setActivePinia(createPinia());
  const auth = useAuthStore();
  auth.user = user;
  auth.accessToken = "access";
  auth.refreshToken = "refresh";
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

async function mountChild(child: object): Promise<VueWrapper> {
  // Attached: only a connected form dispatches the native submit, which is
  // what the exactly-once submit tests need.
  const wrapper = mount(shell(child), {
    attachTo: globalThis.document.body,
    global: { stubs: { transition: false } },
  });
  await nextTick();
  await flushPromises();
  await nextTick();
  return wrapper;
}

async function clickButton(wrapper: VueWrapper, label: string): Promise<void> {
  const target = wrapper
    .findAllComponents(NButton)
    .find((button) => button.text() === label);
  expect(target, `expected a button labelled ${label}`).toBeDefined();
  await target!.trigger("click");
  await flushPromises();
  await nextTick();
  await flushPromises();
}

async function setInput(wrapper: VueWrapper, id: string, value: string): Promise<void> {
  const input = wrapper.find(id);
  expect(input.exists(), `expected input ${id}`).toBe(true);
  await input.setValue(value);
  await nextTick();
}

function inputValue(wrapper: VueWrapper, id: string): string {
  const element = wrapper.find(id).element as unknown as { value: string };
  return element.value;
}

function deferred<T>() {
  let resolve!: (_value: T) => void;
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

beforeEach(() => {
  vi.restoreAllMocks();
  globalThis.document.body.innerHTML = "";
});

describe("DisplayNameForm", () => {
  it("submits the trimmed name and updates the store user", async () => {
    seedAuth(baseUser());
    const updated = { ...baseUser(), display_name: "Ada L" };
    mockPatch.mockResolvedValue(updated);
    const wrapper = await mountChild(DisplayNameForm);
    await setInput(wrapper, "#profile-display-name", "  Ada L  ");
    await clickButton(wrapper, "Save display name");
    expect(mockPatch).toHaveBeenCalledWith("Ada L");
    expect(useAuthStore().user?.display_name).toBe("Ada L");
    wrapper.unmount();
  });

  it("sends null when cleared", async () => {
    seedAuth(baseUser());
    mockPatch.mockResolvedValue({ ...baseUser(), display_name: undefined });
    const wrapper = await mountChild(DisplayNameForm);
    await setInput(wrapper, "#profile-display-name", "   ");
    await clickButton(wrapper, "Save display name");
    expect(mockPatch).toHaveBeenCalledWith(null);
    wrapper.unmount();
  });

  it("blocks a 65-character name inline without calling the API", async () => {
    seedAuth(baseUser());
    const wrapper = await mountChild(DisplayNameForm);
    await setInput(wrapper, "#profile-display-name", "x".repeat(65));
    await clickButton(wrapper, "Save display name");
    expect(mockPatch).not.toHaveBeenCalled();
    expect(wrapper.find(".n-form-item-blank--error").exists()).toBe(true);
    expect(wrapper.find(".n-form-item-feedback").text()).toContain(
      "Display name must be 1-64 characters",
    );
    wrapper.unmount();
  });

  it("submits exactly once per button click", async () => {
    seedAuth(baseUser());
    mockPatch.mockResolvedValue({ ...baseUser(), display_name: "Ada" });
    const wrapper = await mountChild(DisplayNameForm);
    await setInput(wrapper, "#profile-display-name", "Ada");
    await clickButton(wrapper, "Save display name");
    expect(mockPatch).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("submits exactly once per Enter", async () => {
    seedAuth(baseUser());
    mockPatch.mockResolvedValue({ ...baseUser(), display_name: "Ada" });
    const wrapper = await mountChild(DisplayNameForm);
    await setInput(wrapper, "#profile-display-name", "Ada");
    // Enter fires the input keyup handler and the native form submit in the
    // same tick; the submitting guard dedupes them into one request.
    await wrapper.find("#profile-display-name").trigger("keyup.enter");
    await wrapper.find("form").trigger("submit");
    await flushPromises();
    await nextTick();
    expect(mockPatch).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("shows a busy submit and recovers", async () => {
    seedAuth(baseUser());
    const gate = deferred<User>();
    mockPatch.mockReturnValue(gate.promise);
    const wrapper = await mountChild(DisplayNameForm);
    await setInput(wrapper, "#profile-display-name", "Ada");
    const target = wrapper
      .findAllComponents(NButton)
      .find((button) => button.text() === "Save display name")!;
    await target.trigger("click");
    await flushPromises();
    await nextTick();
    expect(target.props("loading")).toBe(true);
    gate.resolve({ ...baseUser(), display_name: "Ada" });
    await flushPromises();
    await nextTick();
    expect(target.props("loading")).toBe(false);
    wrapper.unmount();
  });

  it("stacks the input above its hint in a full-width vertical stack", async () => {
    seedAuth(baseUser());
    const wrapper = await mountChild(DisplayNameForm);
    // Same vertical field stack as the password fields: input and hint share
    // one column container, so the hint can never sit beside the input.
    const stack = wrapper.find(".field-stack");
    expect(stack.exists()).toBe(true);
    expect(stack.find("#profile-display-name").exists()).toBe(true);
    expect(stack.find(".field-hint").exists()).toBe(true);
    wrapper.unmount();
  });

  it("keeps one visible heading: the input keeps its name via aria-label", async () => {
    seedAuth(baseUser());
    const wrapper = await mountChild(DisplayNameForm);
    expect(wrapper.find(".n-card-header__main").text()).toContain("Display name");
    const item = wrapper.findComponent(NFormItem);
    expect(item.props("showLabel")).toBe(false);
    // No label element renders, so no reserved label row; the input carries
    // the accessible name itself.
    expect(wrapper.find(".n-form-item-label").exists()).toBe(false);
    expect(wrapper.find("#profile-display-name").attributes("aria-label")).toBe(
      "Display name",
    );
    wrapper.unmount();
  });
});

describe("ChangePasswordForm", () => {
  it("submits current + new and installs the fresh pair", async () => {
    seedAuth(baseUser());
    const pair = {
      user: baseUser(),
      access_token: "new-access",
      token_type: "Bearer",
      expires_in: 900,
      refresh_token: "new-refresh",
    };
    mockChange.mockResolvedValue(pair);
    const setSession = vi.spyOn(useAuthStore(), "setSession");
    const wrapper = await mountChild(ChangePasswordForm);
    await setInput(wrapper, "#profile-current-password", "old-secret-123");
    await setInput(wrapper, "#profile-new-password", "new-secret-1234");
    await setInput(wrapper, "#profile-confirm-password", "new-secret-1234");
    await clickButton(wrapper, "Change password");
    expect(mockChange).toHaveBeenCalledWith({
      current_password: "old-secret-123",
      new_password: "new-secret-1234",
    });
    expect(setSession).toHaveBeenCalledWith(pair);
    expect(mockChange).toHaveBeenCalledTimes(1);
    // Success clears the typed passwords.
    for (const id of [
      "#profile-current-password",
      "#profile-new-password",
      "#profile-confirm-password",
    ]) {
      expect(inputValue(wrapper, id)).toBe("");
    }
    wrapper.unmount();
  });

  it("submits exactly once per Enter", async () => {
    seedAuth(baseUser());
    mockChange.mockResolvedValue({
      user: baseUser(),
      access_token: "new-access",
      token_type: "Bearer",
      expires_in: 900,
      refresh_token: "new-refresh",
    });
    const wrapper = await mountChild(ChangePasswordForm);
    await setInput(wrapper, "#profile-current-password", "old-secret-123");
    await setInput(wrapper, "#profile-new-password", "new-secret-1234");
    await setInput(wrapper, "#profile-confirm-password", "new-secret-1234");
    // Enter fires the input keyup handler and the native form submit in the
    // same tick; the submitting guard dedupes them into one request.
    await wrapper.find("#profile-confirm-password").trigger("keyup.enter");
    await wrapper.find("form").trigger("submit");
    await flushPromises();
    await nextTick();
    expect(mockChange).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("follows has_password flips while mounted", async () => {
    seedAuth({ ...baseUser(), has_password: false });
    const pair = {
      user: baseUser(),
      access_token: "new-access",
      token_type: "Bearer",
      expires_in: 900,
      refresh_token: "new-refresh",
    };
    mockChange.mockResolvedValue(pair);
    const wrapper = await mountChild(ChangePasswordForm);
    expect(wrapper.find("#profile-current-password").exists()).toBe(false);

    // A late fetchMe (or a first password set) flips the field live.
    useAuthStore().user!.has_password = true;
    await nextTick();
    await flushPromises();
    expect(wrapper.find("#profile-current-password").exists()).toBe(true);
    await setInput(wrapper, "#profile-current-password", "old-secret-123");
    await setInput(wrapper, "#profile-new-password", "new-secret-1234");
    await setInput(wrapper, "#profile-confirm-password", "new-secret-1234");
    await clickButton(wrapper, "Change password");
    expect(mockChange).toHaveBeenCalledWith({
      current_password: "old-secret-123",
      new_password: "new-secret-1234",
    });

    // And back: the field leaves and the payload omits the current password.
    mockChange.mockClear();
    useAuthStore().user!.has_password = false;
    await nextTick();
    await flushPromises();
    expect(wrapper.find("#profile-current-password").exists()).toBe(false);
    await setInput(wrapper, "#profile-new-password", "new-secret-1234");
    await setInput(wrapper, "#profile-confirm-password", "new-secret-1234");
    await clickButton(wrapper, "Change password");
    expect(mockChange).toHaveBeenCalledWith({
      new_password: "new-secret-1234",
    });
    wrapper.unmount();
  });

  it("hides the current field and omits it without a password", async () => {
    seedAuth({ ...baseUser(), has_password: false });
    const pair = {
      user: baseUser(),
      access_token: "new-access",
      token_type: "Bearer",
      expires_in: 900,
      refresh_token: "new-refresh",
    };
    mockChange.mockResolvedValue(pair);
    const wrapper = await mountChild(ChangePasswordForm);
    expect(wrapper.find("#profile-current-password").exists()).toBe(false);
    await setInput(wrapper, "#profile-new-password", "new-secret-1234");
    await setInput(wrapper, "#profile-confirm-password", "new-secret-1234");
    await clickButton(wrapper, "Change password");
    expect(mockChange).toHaveBeenCalledWith({
      new_password: "new-secret-1234",
    });
    wrapper.unmount();
  });

  it("blocks a weak new password inline without calling the API", async () => {
    seedAuth(baseUser());
    const wrapper = await mountChild(ChangePasswordForm);
    await setInput(wrapper, "#profile-current-password", "old-secret-123");
    await setInput(wrapper, "#profile-new-password", "short");
    await setInput(wrapper, "#profile-confirm-password", "short");
    await clickButton(wrapper, "Change password");
    expect(mockChange).not.toHaveBeenCalled();
    expect(wrapper.find(".n-form-item-blank--error").exists()).toBe(true);
    wrapper.unmount();
  });

  it("blocks a mismatched confirmation inline", async () => {
    seedAuth(baseUser());
    const wrapper = await mountChild(ChangePasswordForm);
    await setInput(wrapper, "#profile-current-password", "old-secret-123");
    await setInput(wrapper, "#profile-new-password", "new-secret-1234");
    await setInput(wrapper, "#profile-confirm-password", "other-secret-99");
    await clickButton(wrapper, "Change password");
    expect(mockChange).not.toHaveBeenCalled();
    expect(wrapper.find(".n-form-item-feedback").text()).toContain(
      "Passwords do not match",
    );
    wrapper.unmount();
  });

  it("surfaces a wrong current password as a form error", async () => {
    seedAuth(baseUser());
    mockChange.mockRejectedValue({
      status: 400,
      message: "current password is incorrect",
    });
    const wrapper = await mountChild(ChangePasswordForm);
    await setInput(wrapper, "#profile-current-password", "wrong-password");
    await setInput(wrapper, "#profile-new-password", "new-secret-1234");
    await setInput(wrapper, "#profile-confirm-password", "new-secret-1234");
    await clickButton(wrapper, "Change password");
    expect(wrapper.find(".n-alert").text()).toContain(
      "current password is incorrect",
    );
    wrapper.unmount();
  });

  it("renders a hidden username input for password managers", async () => {
    seedAuth(baseUser());
    const wrapper = await mountChild(ChangePasswordForm);
    const username = wrapper.find('input[autocomplete="username"]');
    expect(username.exists()).toBe(true);
    const element = username.element as unknown as {
      value: string;
      readOnly: boolean;
      tabIndex: number;
    };
    expect(element.value).toBe("ada@gotham.dev");
    expect(element.readOnly).toBe(true);
    expect(element.tabIndex).toBe(-1);
    expect(username.attributes("aria-hidden")).toBe("true");
    // No name attribute: never submitted. Class clips it out of view and
    // the absolute position keeps it out of the layout.
    expect(username.attributes("name")).toBeUndefined();
    expect(username.classes()).toContain("username-fix");
    wrapper.unmount();
  });

  it("drops typed passwords on remount", async () => {
    seedAuth(baseUser());
    const first = await mountChild(ChangePasswordForm);
    await setInput(first, "#profile-current-password", "old-secret-123");
    await setInput(first, "#profile-new-password", "new-secret-1234");
    await setInput(first, "#profile-confirm-password", "new-secret-1234");
    first.unmount();
    const second = await mountChild(ChangePasswordForm);
    for (const id of [
      "#profile-current-password",
      "#profile-new-password",
      "#profile-confirm-password",
    ]) {
      expect(inputValue(second, id), `expected ${id} to clear on remount`).toBe(
        "",
      );
    }
    second.unmount();
  });

  it("drops typed passwords across route navigation away and back", async () => {
    seedAuth(baseUser());
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: "/a", component: ChangePasswordForm },
        { path: "/b", component: { template: "<div />" } },
      ],
    });
    await router.push("/a");
    await router.isReady();
    const wrapper = mount(
      defineComponent({
        render: () =>
          h(NMessageProvider, null, { default: () => h(RouterView) }),
      }),
      {
        attachTo: globalThis.document.body,
        global: { plugins: [router], stubs: { transition: false } },
      },
    );
    await flushPromises();
    await nextTick();
    await setInput(wrapper, "#profile-current-password", "old-secret-123");
    await setInput(wrapper, "#profile-new-password", "new-secret-1234");
    await setInput(wrapper, "#profile-confirm-password", "new-secret-1234");
    await router.push("/b");
    await flushPromises();
    await nextTick();
    await router.push("/a");
    await flushPromises();
    await nextTick();
    for (const id of [
      "#profile-current-password",
      "#profile-new-password",
      "#profile-confirm-password",
    ]) {
      expect(inputValue(wrapper, id), `expected ${id} to clear after navigation`).toBe(
        "",
      );
    }
    wrapper.unmount();
  });
});

describe("ProfileIdentityCard", () => {
  it("renders facts side by side with labels left of values", async () => {
    seedAuth(baseUser());
    const wrapper = await mountChild(ProfileIdentityCard);
    const descriptions = wrapper.findComponent(NDescriptions);
    expect(descriptions.props("labelPlacement")).toBe("left");
    expect(descriptions.props("column")).toBe(1);
    expect(descriptions.props("bordered")).toBe(true);
    // Narrow containers stack label-over-value via a container query.
    expect(wrapper.find(".identity-facts-wrap").exists()).toBe(true);
    wrapper.unmount();
  });

  it("renders facts, role, member-since and the avatar note", async () => {
    seedAuth(baseUser());
    const wrapper = await mountChild(ProfileIdentityCard);
    const text = wrapper.text();
    expect(text).toContain("ada@gotham.dev");
    expect(text).toContain("Member");
    expect(text).toContain("2026");
    expect(text).toContain("GitHub");
    expect(text).toContain("A");
    wrapper.unmount();
  });

  it("labels a platform admin", async () => {
    seedAuth({ ...baseUser(), role: "admin", is_platform_admin: true });
    const wrapper = await mountChild(ProfileIdentityCard);
    expect(wrapper.text()).toContain("Platform admin");
    wrapper.unmount();
  });

  it("falls back to the email without a display name", async () => {
    seedAuth({ ...baseUser(), display_name: undefined });
    const wrapper = await mountChild(ProfileIdentityCard);
    expect(wrapper.text()).toContain("ada@gotham.dev");
    wrapper.unmount();
  });
});
