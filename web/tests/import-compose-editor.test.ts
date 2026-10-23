import { flushPromises, mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick } from "vue";

import CodeEditor from "@/features/files/components/CodeEditor.vue";
import ImportComposeDialog from "@/features/services/components/ImportComposeDialog.vue";
import { useServicesStore } from "@/features/services/stores/services";
import { useServersStore } from "@/features/servers/stores/servers";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const typedYaml = "services:\n  web:\n    image: nginx:1.28-alpine\n";

const ServerPickerStub = defineComponent({
  name: "ServerPicker",
  props: { modelValue: { type: String, default: "" } },
  emits: ["update:modelValue"],
  render: () => h("div", { class: "server-picker-stub" }),
});

beforeEach(() => {
  setActivePinia(createPinia());
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  globalThis.document.body.innerHTML = "";
});

describe("import compose code editor", () => {
  it("routes the editor document into the import payload", async () => {
    const services = useServicesStore();
    const create = vi
      .spyOn(services, "create")
      .mockResolvedValue({ id: "service-1", name: "blog-staging" } as never);
    const servers = useServersStore();
    vi.spyOn(servers, "fetchServers").mockResolvedValue(undefined);

    const wrapper = mount(
      defineComponent({
        render: () =>
          h(NMessageProvider, null, {
            default: () =>
              h(ImportComposeDialog, {
                show: true,
                projectId: "proj-1",
                environmentId: "env-1",
              }),
          }),
      }),
      {
        attachTo: globalThis.document.body,
        global: {
          plugins: [i18n],
          stubs: { teleport: true, ResourceScopeSummary: true, ServerPicker: ServerPickerStub },
        },
      },
    );
    await flushPromises();

    await wrapper.find(".field-import-name input").setValue("blog-staging");
    wrapper.findComponent(ServerPickerStub).vm.$emit("update:modelValue", "srv-1");

    const code = wrapper.findComponent(CodeEditor);
    expect(code.props("preset")).toBe("yaml");
    expect(code.props("placeholder")).toBe(
      "services:\n  web:\n    image: nginx:1.27-alpine",
    );
    expect(code.props("ariaLabel")).toBe("compose.yaml");
    code.vm.$emit("update:modelValue", typedYaml);
    await nextTick();

    const submit = wrapper.findAll("button").find((button) => button.text() === "Import service");
    expect(submit).toBeDefined();
    await submit!.trigger("click");
    await flushPromises();

    expect(create).toHaveBeenCalledWith({
      name: "blog-staging",
      environment_id: "env-1",
      server_id: "srv-1",
      compose_yaml: typedYaml,
    });
    wrapper.unmount();
  });

  it("follows the locale switch without losing the imported YAML draft", async () => {
    const services = useServicesStore();
    const create = vi
      .spyOn(services, "create")
      .mockResolvedValue({ id: "service-1", name: "blog-staging" } as never);
    const servers = useServersStore();
    vi.spyOn(servers, "fetchServers").mockResolvedValue(undefined);

    const wrapper = mount(
      defineComponent({
        render: () =>
          h(NMessageProvider, null, {
            default: () =>
              h(ImportComposeDialog, {
                show: true,
                projectId: "proj-1",
                environmentId: "env-1",
              }),
          }),
      }),
      {
        attachTo: globalThis.document.body,
        global: {
          plugins: [i18n],
          stubs: { teleport: true, ResourceScopeSummary: true, ServerPicker: ServerPickerStub },
        },
      },
    );
    await flushPromises();

    const code = wrapper.findComponent(CodeEditor);
    const typed = `${typedYaml}# typed before locale switch\n`;
    code.vm.$emit("update:modelValue", typed);
    await nextTick();

    syncComposerLocale("vi");
    await nextTick();
    await flushPromises();

    const switched = wrapper.findComponent(CodeEditor);
    expect(switched.props("ariaLabel")).toBe("Tệp compose.yaml");
    expect(switched.props("placeholder")).toBe(
      "# Dán tài liệu compose tại đây\nservices:\n  web:\n    image: nginx:1.27-alpine",
    );
    expect(switched.props("modelValue")).toBe(typed);
    expect(create).not.toHaveBeenCalled();
    syncComposerLocale("en");
    wrapper.unmount();
  });
});
