/* global document:readonly */
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { createMemoryHistory, createRouter } from "vue-router";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { useEditorStore } from "../src/features/files/stores/editor";
import type { EditorTab } from "../src/features/files/stores/editor";
import { useEditorWindowStore } from "../src/features/files/stores/editorWindow";
import { useServersStore } from "../src/features/servers/stores/servers";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "../src/shared/i18n";

const { default: ServerRail } = await import(
  "../src/features/servers/components/ServerRail.vue"
);

function makeTab(id: string): EditorTab {
  return {
    id,
    kind: "file",
    root: "volumes",
    path: `${id}.md`,
    name: `${id}.md`,
    doc: "hello",
    original: "hello",
    etag: '"1"',
    eol: "\n",
    bom: false,
    encoding: "utf-8",
    size: 5,
    mode: 0o100644,
    truncated: false,
    language: "markdown",
    languageManual: false,
    status: "ready",
    error: "",
    saving: false,
  };
}

function railRouter(): ReturnType<typeof createRouter> {
  const stub = { template: "<div />" };
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "dashboard", component: stub },
      { path: "/servers", name: "servers", component: stub },
      { path: "/servers/:id", name: "server-detail", component: stub },
    ],
  });
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  globalThis.document.body.innerHTML = "";
  vi.restoreAllMocks();
});

async function mountRail(count: number) {
  setActivePinia(createPinia());
  const router = railRouter();
  router.push("/");
  await router.isReady();
  const servers = useServersStore();
  servers.servers = [];
  const editor = useEditorStore();
  editor.tabs = Array.from({ length: count }, (_, index) => makeTab(`t${index}`));
  editor.activeId = editor.tabs[0]?.id ?? "";
  const editorWindow = useEditorWindowStore();
  editorWindow.setMinimized(true);
  const wrapper = mount(ServerRail, {
    attachTo: document.body,
    global: { plugins: [router, i18n] },
  });
  await flushPromises();
  return { wrapper, editorWindow };
}

describe("ServerRail editor icon (JUS-232)", () => {
  it("shows the badge count with an accessible name", async () => {
    const { wrapper } = await mountRail(2);
    const button = wrapper.find(".rail-btn--editor");
    expect(button.exists()).toBe(true);
    expect(button.attributes("aria-label")).toBe("Code editor — 2 open");
    expect(button.find(".rail-badge").text()).toBe("2");
    wrapper.unmount();
  });

  it("caps a large count at 9+", async () => {
    const { wrapper } = await mountRail(12);
    expect(wrapper.find(".rail-btn--editor .rail-badge").text()).toBe("9+");
    wrapper.unmount();
  });

  it("sits above Overview and Alert, split by dividers", async () => {
    const { wrapper } = await mountRail(1);
    const items = wrapper.findAll(".rail-foot .rail-item");
    expect(items[0]!.find(".rail-btn--editor").exists()).toBe(true);
    expect(wrapper.findAll(".rail-foot > .rail-sep")).toHaveLength(2);
    wrapper.unmount();
  });

  it("restores the window on click and hides with no tabs", async () => {
    const { wrapper, editorWindow } = await mountRail(1);
    expect(editorWindow.minimized).toBe(true);
    await wrapper.find(".rail-btn--editor").trigger("click");
    expect(editorWindow.minimized).toBe(false);
    useEditorStore().closeAll();
    await flushPromises();
    expect(wrapper.find(".rail-btn--editor").exists()).toBe(false);
    wrapper.unmount();
  });
});
