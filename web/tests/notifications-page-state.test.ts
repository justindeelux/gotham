// Page-instance state regression tests for the notifications split (JUS-24 fix 1).
//
// The channel draft, open flag, secrets and resource lists must be created per
// page mount and dropped on unmount. Teleport is stubbed so dialog content
// renders inline.
import { NButton, NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { defineComponent, h, nextTick } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import type { VueWrapper } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/notifications/api/notifications", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/notifications/api/notifications")>();
  return { ...actual, listChannels: vi.fn() };
});
vi.mock("@/features/applications", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/features/applications")>();
  return { ...actual, listApplications: vi.fn() };
});
vi.mock("@/features/databases", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/features/databases")>();
  return { ...actual, listDatabases: vi.fn() };
});

import { listChannels } from "@/features/notifications/api/notifications";
import notificationsEn from "@/features/notifications/locales/en";
import notificationsVi from "@/features/notifications/locales/vi";
import NotificationsPage from "@/features/notifications/pages/NotificationsPage.vue";
import { provideChannelDialog } from "@/features/notifications/composables/useChannelDialog";
import type { ChannelDialogState } from "@/features/notifications/composables/useChannelDialog";
import { listApplications } from "@/features/applications";
import { listDatabases } from "@/features/databases";
import { useTeamsStore } from "@/features/teams";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

/** Catalogs render through the real composer: register before mounting. */
beforeEach(() => {
  resetLocaleState();
  i18n.global.mergeLocaleMessage("en", { notifications: notificationsEn });
  i18n.global.mergeLocaleMessage("vi", { notifications: notificationsVi });
  syncComposerLocale("en");
});

function shell(child: object) {
  return defineComponent({
    render() {
      return h(NMessageProvider, null, { default: () => h(child as never) });
    },
  });
}

/** seedTeams presets the teams store before the page mounts. */
function seedTeams(): void {
  const teams = useTeamsStore();
  (teams as unknown as Record<string, unknown>).ensureTeams = vi
    .fn()
    .mockResolvedValue(undefined);
  teams.teams = [
    { id: "team-1", name: "Acme", is_personal: false, role: "owner" },
  ] as never;
  teams.activeTeamId = "team-1";
  (teams as unknown as Record<string, unknown>).featureDisabled = false;
}

async function mountPage() {
  const wrapper = mount(shell(NotificationsPage), {
    global: { plugins: [i18n], stubs: { teleport: true } },
  });
  await nextTick();
  await flushPromises();
  await nextTick();
  return wrapper;
}

/** dialogHarness mounts a bare provider for direct composable tests. */
async function dialogHarness(): Promise<{ wrapper: VueWrapper; state: ChannelDialogState }> {
  let state!: ChannelDialogState;
  const Inner = defineComponent({
    setup() {
      state = provideChannelDialog();
      return () => h("div");
    },
  });
  const wrapper = mount(
    defineComponent({ render: () => h(NMessageProvider, null, { default: () => h(Inner) }) }),
  );
  await flushPromises();
  return { wrapper, state };
}

/** clickButton clicks the first naive button with the exact label. */
async function clickButton(wrapper: VueWrapper, label: string): Promise<void> {
  const buttons = wrapper.findAllComponents(NButton);
  const target = buttons.find((button) => button.text() === label);
  expect(target, `expected a button labelled ${label}`).toBeDefined();
  await target!.trigger("click");
  await flushPromises();
  await nextTick();
}

function deferred<T>() {
  let resolve!: (_value: T) => void;
  let reject!: (_error: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

describe("notifications page instance state", () => {
  it("drops typed secrets and closes the dialog on remount", async () => {
    setActivePinia(createPinia());
    seedTeams();
    vi.mocked(listChannels).mockResolvedValue([]);
    vi.mocked(listApplications).mockResolvedValue([]);
    vi.mocked(listDatabases).mockResolvedValue([]);

    let wrapper = await mountPage();
    await clickButton(wrapper, "New channel");
    expect(wrapper.html()).toContain("New notification channel");
    const webhook = wrapper.find('[aria-label="Webhook URL"] input');
    expect(webhook.exists()).toBe(true);
    await webhook.setValue("SECRET-HOOK");
    expect((webhook.element as unknown as { value: string }).value).toBe("SECRET-HOOK");
    wrapper.unmount();

    // A fresh mount reopens with a blank draft: the secret is gone.
    wrapper = await mountPage();
    expect(wrapper.html()).not.toContain("New notification channel");
    await clickButton(wrapper, "New channel");
    const reopened = wrapper.find('[aria-label="Webhook URL"] input');
    expect(reopened.exists()).toBe(true);
    expect((reopened.element as unknown as { value: string }).value).toBe("");
    wrapper.unmount();
  });
});

describe("useChannelDialog.loadResources", () => {
  it("ignores a stale response that loses the token race", async () => {
    setActivePinia(createPinia());
    const { wrapper, state } = await dialogHarness();
    const first = deferred<Array<{ id: string; name: string }>>();
    const second = deferred<Array<{ id: string; name: string }>>();
    vi.mocked(listApplications)
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    vi.mocked(listDatabases).mockResolvedValue([]);

    const firstRead = state.loadResources("team-1");
    const secondRead = state.loadResources("team-1");
    second.resolve([{ id: "app-new", name: "New" }]);
    await secondRead;
    first.resolve([{ id: "app-stale", name: "Stale" }]);
    await firstRead;
    expect(state.resourceApplications.value).toEqual([{ id: "app-new", name: "New" }]);
    wrapper.unmount();
  });

  it("drops the previous team list on a team switch", async () => {
    setActivePinia(createPinia());
    const { wrapper, state } = await dialogHarness();
    vi.mocked(listApplications).mockResolvedValue([{ id: "app-a", name: "A" }]);
    vi.mocked(listDatabases).mockResolvedValue([]);

    await state.loadResources("team-a");
    expect(state.resourceApplications.value).toEqual([{ id: "app-a", name: "A" }]);

    const pending = deferred<Array<{ id: string; name: string }>>();
    vi.mocked(listApplications).mockReturnValue(pending.promise);
    const read = state.loadResources("team-b");
    // The switch drops the old team's list before the read settles.
    expect(state.resourceApplications.value).toEqual([]);
    pending.resolve([{ id: "app-b", name: "B" }]);
    await read;
    expect(state.resourceApplications.value).toEqual([{ id: "app-b", name: "B" }]);
    wrapper.unmount();
  });

  it("keeps the database picker when the applications list fails", async () => {
    setActivePinia(createPinia());
    const { wrapper, state } = await dialogHarness();
    vi.mocked(listApplications).mockRejectedValue(new Error("FEATURE_APPLICATIONS=false"));
    vi.mocked(listDatabases).mockResolvedValue([{ id: "db-1", name: "Main" }]);

    await state.loadResources("team-1");
    expect(state.resourceApplications.value).toEqual([]);
    expect(state.resourceDatabases.value).toEqual([{ id: "db-1", name: "Main" }]);
    const labels = state.scopeOptions.value.map((option) => option.label);
    expect(labels).toContain("Application (unavailable)");
    expect(labels).toContain("Database");
    wrapper.unmount();
  });
});
