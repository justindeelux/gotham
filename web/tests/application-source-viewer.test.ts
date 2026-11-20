import { flushPromises, mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick } from "vue";

const { mockGetApplicationSource } = vi.hoisted(() => ({
  mockGetApplicationSource: vi.fn(),
}));

vi.mock("@/features/applications/api/applications", async (importOriginal) => {
  const mod = await importOriginal<Record<string, unknown>>();
  return { ...mod, getApplicationSource: mockGetApplicationSource };
});

import type {
  Application,
  ApplicationSource,
} from "@/features/applications/api/applications";
import ApplicationSourceViewer from "@/features/applications/components/ApplicationSourceViewer.vue";
import CodeEditor from "@/features/files/components/CodeEditor.vue";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";

beforeEach(() => {
  setActivePinia(createPinia());
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  mockGetApplicationSource.mockReset();
});

function composeApp(): Application {
  return {
    id: "app-compose",
    name: "shop",
    environment_id: "env-1",
    environment_name: "production",
    project_id: "proj-1",
    project_name: "shop",
    provider: "",
    repo: "",
    clone_url: "",
    source_type: "compose",
    branch: "",
    build_pack: "",
    compose_content: "services:\n  web:\n    image: nginx\n",
    compose_file: "",
    compose_service: "web",
    image_ref: "",
    has_registry_credential: false,
    primary_domain: "",
    server_id: "server-1",
    server_name: "node-1",
    created_at: "2026-09-01T10:00:00Z",
    updated_at: "2026-09-01T10:00:00Z",
  } as Application;
}

function mountViewer(source: ApplicationSource, application = composeApp()) {
  mockGetApplicationSource.mockResolvedValue(source);
  const Harness = defineComponent({
    render: () =>
      h(NMessageProvider, null, {
        default: () => h(ApplicationSourceViewer, { application }),
      }),
  });
  return mount(Harness, { global: { plugins: [i18n] } });
}

async function expand(wrapper: ReturnType<typeof mountViewer>): Promise<void> {
  await wrapper.find("button[aria-expanded]").trigger("click");
  await flushPromises();
}

describe("ApplicationSourceViewer", () => {
  it("is collapsed by default and toggles open and closed", async () => {
    const wrapper = mountViewer({ kind: "dockerfile", raw: "FROM a\n", deployable: "FROM a\n" });
    await flushPromises();

    const toggle = wrapper.find("button[aria-expanded]");
    expect(toggle.attributes("aria-expanded")).toBe("false");
    expect(wrapper.findComponent(CodeEditor).exists()).toBe(false);

    await expand(wrapper);
    expect(wrapper.find("button[aria-expanded]").attributes("aria-expanded")).toBe("true");
    expect(wrapper.findComponent(CodeEditor).exists()).toBe(true);

    await expand(wrapper);
    expect(wrapper.findComponent(CodeEditor).exists()).toBe(false);
  });

  it("switches between the raw and deployable documents", async () => {
    const wrapper = mountViewer({
      kind: "compose",
      raw: "services:\n  web:\n    image: nginx\n",
      deployable: "services:\n  web:\n    image: nginx\n    environment:\n      DB_PASSWORD: <redacted>\n",
    });
    await flushPromises();
    await expand(wrapper);

    const viewer = wrapper.findComponent(CodeEditor);
    expect(viewer.props("preset")).toBe("yaml");
    expect(viewer.props("readOnly")).toBe(true);
    expect(viewer.props("wrap")).toBe(true);
    expect(viewer.props("modelValue")).toBe("services:\n  web:\n    image: nginx\n");

    const radios = wrapper.findAll('input[type="radio"]');
    expect(radios).toHaveLength(2);
    await radios[1].setValue();
    await nextTick();

    expect(wrapper.findComponent(CodeEditor).props("modelValue")).toContain(
      "DB_PASSWORD: <redacted>",
    );
  });

  it("is read-only", async () => {
    const wrapper = mountViewer({
      kind: "dockerfile",
      raw: "FROM alpine:3.20\n",
      deployable: "FROM alpine:3.20\n",
    });
    await flushPromises();
    await expand(wrapper);

    const viewer = wrapper.findComponent(CodeEditor);
    expect(viewer.props("preset")).toBe("dockerfile");
    expect(viewer.props("readOnly")).toBe(true);
    expect(wrapper.find("textarea").exists()).toBe(false);
    expect(wrapper.find('[contenteditable="true"]').exists()).toBe(false);
  });

  it("renders the Vietnamese labels from the catalog", async () => {
    setLocale("vi", null);
    const wrapper = mountViewer({
      kind: "dockerfile",
      raw: "FROM alpine:3.20\n",
      deployable: "FROM alpine:3.20\n",
    });
    await flushPromises();
    await expand(wrapper);

    expect(wrapper.text()).toContain("Mã nguồn triển khai");
    expect(wrapper.text()).toContain("Nguyên bản");
    expect(wrapper.text()).toContain("Bản triển khai");
    expect(wrapper.text()).not.toContain("applications.source.");
  });

  it("shows an empty state when the application has no Dockerfile or compose file", async () => {
    const application = { ...composeApp(), source_type: "image" } as Application;
    const wrapper = mountViewer(
      { kind: "", raw: "", deployable: "" },
      application,
    );
    await flushPromises();
    await expand(wrapper);

    expect(wrapper.findComponent(CodeEditor).exists()).toBe(false);
    expect(wrapper.text()).toContain("no Dockerfile or compose source");
    expect(wrapper.text()).not.toContain("could not be read");
  });
});
