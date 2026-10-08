// PE-5 fix round 1 (H2): create wizards stay reactive to the route scope.
// Flipping the scope refs while a wizard is mounted re-seeds the form, so
// the summary and the payload always name the current environment.
import { flushPromises, mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick, ref } from "vue";

import { useCreateAppWizard } from "../src/features/applications/composables/useCreateAppWizard";
import * as applicationsApi from "../src/features/applications/api/applications";
import { useCreateDatabaseWizard } from "../src/features/databases/composables/useCreateDatabaseWizard";
import { useImportService } from "../src/features/services/composables/useImportService";
import { useTemplateWizard } from "../src/features/templates/composables/useTemplateWizard";

beforeEach(() => {
  setActivePinia(createPinia());
  vi.restoreAllMocks();
});

function shell(child: object) {
  return mount({
    render: () => h(NMessageProvider, null, { default: () => h(child as never) }),
  });
}

describe("create wizards follow the live route scope", () => {
  it("re-seeds the app wizard when the scope changes while mounted", async () => {
    const show = ref(false);
    const projectId = ref("proj-a");
    const environmentId = ref("env-a");
    let form: { projectId: string; environmentId: string } | null = null;
    const Harness = defineComponent({
      setup() {
        const wizard = useCreateAppWizard(show, (() => undefined) as never, {
          projectId,
          environmentId,
        });
        form = wizard.form;
        return () => h("div");
      },
    });
    const wrapper = shell(Harness);
    expect(form!.environmentId).toBe("env-a");

    environmentId.value = "env-b";
    projectId.value = "proj-b";
    await nextTick();
    await flushPromises();
    expect(form!.projectId).toBe("proj-b");
    expect(form!.environmentId).toBe("env-b");
    wrapper.unmount();
  });

  it("re-seeds the database wizard when the scope changes while mounted", async () => {
    const show = ref(false);
    const environmentId = ref("env-a");
    let form: { environmentId: string } | null = null;
    const Harness = defineComponent({
      setup() {
        const wizard = useCreateDatabaseWizard({
          show,
          projectId: "proj-a",
          environmentId,
          onCreated: () => undefined,
          onUpdateShow: () => undefined,
        });
        form = wizard.form;
        return () => h("div");
      },
    });
    const wrapper = shell(Harness);
    environmentId.value = "env-b";
    await nextTick();
    await flushPromises();
    expect(form!.environmentId).toBe("env-b");
    wrapper.unmount();
  });

  it("re-seeds the service import when the scope changes while mounted", async () => {
    const show = ref(false);
    const environmentId = ref("env-a");
    let scope: { value: string } | null = null;
    const Harness = defineComponent({
      setup() {
        const dialog = useImportService(show, { projectId: "proj-a", environmentId });
        scope = dialog.scopeEnvironmentId;
        return () => h("div");
      },
    });
    const wrapper = shell(Harness);
    environmentId.value = "env-b";
    await nextTick();
    await flushPromises();
    expect(scope!.value).toBe("env-b");
    wrapper.unmount();
  });

  it("re-seeds the template wizard when the scope changes while mounted", async () => {
    const show = ref(false);
    const slug = ref("redis");
    const environmentId = ref("env-a");
    let scope: { value: string } | null = null;
    const Harness = defineComponent({
      setup() {
        const wizard = useTemplateWizard(show, slug, {
          projectId: "proj-a",
          environmentId,
        });
        scope = wizard.scopeEnvironmentId;
        return () => h("div");
      },
    });
    const wrapper = shell(Harness);
    environmentId.value = "env-b";
    await nextTick();
    await flushPromises();
    expect(scope!.value).toBe("env-b");
    wrapper.unmount();
  });

  it("submits the app payload to the current environment, not the stale one", async () => {
    const show = ref(true);
    const environmentId = ref("env-a");
    const create = vi
      .spyOn(applicationsApi, "createApplication")
      .mockResolvedValue({ application: { id: "app-1", name: "web" } } as never);
    let submit!: () => Promise<void>;
    let form!: { environmentId: string; name: string; publicCloneUrl: string };
    const Harness = defineComponent({
      setup() {
        const wizard = useCreateAppWizard(show, (() => undefined) as never, {
          projectId: "proj-a",
          environmentId,
        });
        form = wizard.form as never;
        submit = () => wizard.handleSubmit();
        return () => h("div");
      },
    });
    const wrapper = shell(Harness);
    // The route moves before submit: the payload must follow it.
    environmentId.value = "env-b";
    await nextTick();
    form.name = "web";
    // git_public carries the pasted URL, not a provider selection.
    form.publicCloneUrl = "https://github.com/o/r.git";
    await submit();
    expect(create).toHaveBeenCalledTimes(1);
    expect(create.mock.calls[0]![0]).toMatchObject({ environment_id: "env-b" });
    wrapper.unmount();
  });
});
