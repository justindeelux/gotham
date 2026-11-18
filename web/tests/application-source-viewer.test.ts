import { flushPromises, mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
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
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";

beforeEach(() => {
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

describe("ApplicationSourceViewer", () => {
  it("switches between the raw and deployable documents", async () => {
    const wrapper = mountViewer({
      kind: "compose",
      raw: "services:\n  web:\n    image: nginx\n",
      deployable: "services:\n  web:\n    image: nginx\n    environment:\n      DB_PASSWORD: <redacted>\n",
    });
    await flushPromises();

    expect(wrapper.find(".source-view").element.textContent).toBe(
      "services:\n  web:\n    image: nginx\n",
    );

    const radios = wrapper.findAll('input[type="radio"]');
    expect(radios).toHaveLength(2);
    await radios[1].setValue();
    await nextTick();

    expect(wrapper.find(".source-view").text()).toContain(
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

    expect(wrapper.find("textarea").exists()).toBe(false);
    expect(wrapper.find("[contenteditable]").exists()).toBe(false);
  });

  it("renders the Vietnamese labels from the catalog", async () => {
    setLocale("vi", null);
    const wrapper = mountViewer({
      kind: "dockerfile",
      raw: "FROM alpine:3.20\n",
      deployable: "FROM alpine:3.20\n",
    });
    await flushPromises();

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

    expect(wrapper.find(".source-view").exists()).toBe(false);
    expect(wrapper.text()).toContain("no Dockerfile or compose source");
    expect(wrapper.text()).not.toContain("could not be read");
  });
});
