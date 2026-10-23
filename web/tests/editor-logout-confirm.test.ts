/* global document:readonly, window:readonly */
import { NDropdown, NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { flushPromises, mount } from "@vue/test-utils";
import { createMemoryHistory, createRouter } from "vue-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h } from "vue";

const codemirror = vi.hoisted(() => ({
  sessions: [] as unknown[],
}));

vi.mock("@/features/files/composables/useCodeMirror", () => ({
  createEditorSession: vi.fn(async () => {
    const session = {
      getDoc: () => "",
      setDoc: vi.fn(),
      setLanguage: vi.fn(),
      setSettings: vi.fn(),
      setReadOnly: vi.fn(),
      focus: vi.fn(),
      requestMeasure: vi.fn(),
      search: {
        setOptions: vi.fn(),
        status: vi.fn(),
        next: vi.fn(),
        previous: vi.fn(),
        replace: vi.fn(),
        replaceAll: vi.fn(),
        clear: vi.fn(),
      },
      lineInfo: vi.fn(() => ({ line: 1, total: 10 })),
      gotoLine: vi.fn(() => null),
      destroy: vi.fn(),
    };
    codemirror.sessions.push(session);
    return session;
  }),
}));

vi.mock("@/features/files/composables/useMergeView", () => ({
  createMergeSession: vi.fn(async () => ({
    setSettings: vi.fn(),
    destroy: vi.fn(),
    requestMeasure: vi.fn(),
  })),
}));

vi.mock("@/features/files/composables/useFileDownload", () => ({
  useFileDownload: () => ({ pending: { value: false }, download: vi.fn() }),
}));

vi.mock("@/shared/api/http", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/shared/api/http")>();
  return {
    ...actual,
    http: {
      get: vi.fn().mockRejectedValue(new Error("no server in unit tests")),
      post: vi.fn().mockResolvedValue({}),
      put: vi.fn().mockRejectedValue(new Error("no server in unit tests")),
      delete: vi.fn().mockRejectedValue(new Error("no server in unit tests")),
    },
  };
});

import AccountMenu from "@/app/layouts/AccountMenu.vue";
import FilesEditor from "@/features/files/components/FilesEditor.vue";
import { useAuthStore } from "@/features/auth/stores/auth";
import { useEditorStore } from "@/features/files/stores/editor";
import type { EditorTab } from "@/features/files/stores/editor";
import { useFilesStore } from "@/features/files/stores/files";
import { useTeamsStore } from "@/features/teams";
import type { Team } from "@/features/teams/api/teams";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

function makeTab(id: string, dirty: boolean): EditorTab {
  return {
    id,
    kind: "file",
    root: "volumes",
    path: `${id}.yaml`,
    name: `${id}.yaml`,
    doc: dirty ? "changed" : "start",
    original: "start",
    etag: '"v1"',
    eol: "\n",
    bom: false,
    encoding: "utf-8",
    size: 6,
    mode: 0o100644,
    truncated: false,
    language: "yaml",
    languageManual: false,
    status: "ready",
    error: "",
    saving: false,
  };
}

function team(id: string, role: Team["role"]): Team {
  return {
    id,
    name: id,
    is_personal: false,
    role,
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
  };
}

async function mountShell() {
  setActivePinia(createPinia());
  const teams = useTeamsStore();
  teams.teams = [team("team-a", "owner")];
  teams.activeTeamId = "team-a";
  teams.loaded = true;

  const files = useFilesStore();
  files.roots = [{ name: "volumes", path: "/v", writable: true, free_bytes: 1 }];
  files.rootName = "volumes";

  const auth = useAuthStore();
  auth.user = {
    id: "u-1",
    email: "ada@gotham.dev",
    created_at: "2026-01-01T00:00:00Z",
  };
  auth.accessToken = "a";
  auth.refreshToken = "r";

  const editor = useEditorStore();
  const stub = { template: "<div />" };
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "dashboard", component: stub },
      { path: "/profile", name: "profile", component: stub },
      { path: "/login", name: "login", component: stub },
    ],
  });
  await router.push("/");
  await router.isReady();
  const shell = defineComponent({
    render: () =>
      h(NMessageProvider, null, {
        default: () =>
          h("div", null, [
            h("div", { id: "account-menu" }, [h(AccountMenu)]),
            h("div", { id: "editor" }, [h(FilesEditor, { serverId: "srv" })]),
          ]),
      }),
  });
  const wrapper = mount(shell, {
    attachTo: document.body,
    global: { plugins: [router, i18n], stubs: { teleport: true } },
  });
  await flushPromises();
  return { wrapper, auth, editor, router };
}

async function signOut(wrapper: ReturnType<typeof mountShell>["wrapper"]): Promise<void> {
  const menu = wrapper.find("#account-menu").findComponent(NDropdown);
  expect(menu.exists()).toBe(true);
  await (menu.vm.$emit("select", "sign-out") as unknown as Promise<void>);
  await flushPromises();
}

function leaveDialog(wrapper: ReturnType<typeof mountShell>["wrapper"]) {
  return wrapper
    .findAll(".app-modal")
    .find((node) => node.text().includes("Discard unsaved changes?"));
}

async function clickButton(
  wrapper: ReturnType<typeof mountShell>["wrapper"],
  text: string,
): Promise<void> {
  const button = wrapper
    .findAll("button")
    .find((node) => node.text() === text);
  expect(button).toBeTruthy();
  await button!.trigger("click");
  await flushPromises();
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  window.localStorage.clear();
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

afterEach(() => {
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

describe("dirty logout confirmation (JUS-232 fix round 2)", () => {
  it("prompts on logout with a dirty tab; staying aborts the logout", async () => {
    const { wrapper, auth, editor, router } = await mountShell();
    const tab = makeTab("t1", true);
    editor.tabs = [tab];
    editor.activeId = tab.id;
    editor.serverId = "srv";
    await flushPromises();

    await signOut(wrapper);
    expect(leaveDialog(wrapper)).toBeTruthy();
    expect(auth.isAuthenticated).toBe(true);
    expect(editor.tabs).toHaveLength(1);

    await clickButton(wrapper, "Stay");
    expect(leaveDialog(wrapper)).toBeUndefined();
    expect(auth.isAuthenticated).toBe(true);
    expect(editor.tabs).toHaveLength(1);
    expect(router.currentRoute.value.name).not.toBe("login");
    wrapper.unmount();
  });

  it("discarding on logout signs out and resets the editor", async () => {
    const { wrapper, auth, editor, router } = await mountShell();
    const tab = makeTab("t1", true);
    editor.tabs = [tab];
    editor.activeId = tab.id;
    editor.serverId = "srv";
    await flushPromises();

    await signOut(wrapper);
    expect(leaveDialog(wrapper)).toBeTruthy();
    await clickButton(wrapper, "Discard and leave");

    expect(editor.tabs).toHaveLength(0);
    expect(auth.isAuthenticated).toBe(false);
    expect(router.currentRoute.value.name).toBe("login");
    wrapper.unmount();
  });

  it("logs out without a prompt when nothing is dirty", async () => {
    const { wrapper, auth, editor, router } = await mountShell();
    const tab = makeTab("t1", false);
    editor.tabs = [tab];
    editor.activeId = tab.id;
    await flushPromises();

    await signOut(wrapper);
    expect(leaveDialog(wrapper)).toBeUndefined();
    expect(auth.isAuthenticated).toBe(false);
    expect(router.currentRoute.value.name).toBe("login");
    wrapper.unmount();
  });
});
