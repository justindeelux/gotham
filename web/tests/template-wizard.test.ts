// useTemplateWizard core flow: open, validation gating, create/deploy payloads.
import { flushPromises, mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, ref } from "vue";
import type { Ref } from "vue";

import type { Service } from "../src/features/services";
import * as servicesApi from "../src/features/services/api/services";
import { useServersStore } from "../src/features/servers/stores/servers";
import { useTemplatesStore } from "../src/features/templates/stores/templates";
import { useTemplateWizard } from "../src/features/templates/composables/useTemplateWizard";

type Wizard = ReturnType<typeof useTemplateWizard>;

let wizard: Wizard | null = null;
const showRef: Ref<boolean> = ref(false);
const slugRef: Ref<string> = ref("redis");

const Harness = defineComponent({
  setup() {
    wizard = useTemplateWizard(showRef, slugRef);
    return () => h("div");
  },
});

let wrapper: ReturnType<typeof mount> | null = null;

function harness(): void {
  wrapper = mount({
    render: () =>
      h(NMessageProvider, null, {
        default: () => h(Harness),
      }),
  });
}

const detail = {
  slug: "redis",
  name: "Redis",
  icon: "db",
  description: "Cache",
  fields: [
    { key: "password", label: "Password", type: "secret", required: true },
    { key: "port", label: "Port", type: "text", required: false, default: "6379" },
  ],
};

const renderResult = {
  slug: "redis",
  compose_yaml: "services:\n  redis: {}",
  env: { REDIS_PASSWORD: "secret" },
  spec: { services: ["redis"], named_volumes: [], domains: [] },
};

const createdService = {
  id: "svc-1",
  name: "redis",
  status: "created",
  server_id: "srv-1",
  server_name: "node",
  environment_id: "env-1",
  environment_name: "production",
  project_id: "proj-1",
  project_name: "shop",
  compose_project: "redis-abc",
  env: {},
  domains: [],
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
} as Service;

beforeEach(() => {
  setActivePinia(createPinia());
  showRef.value = false;
  slugRef.value = "redis";
  wizard = null;
  vi.restoreAllMocks();
});

async function openWizard(): Promise<void> {
  wrapper?.unmount();
  wrapper = null;
  const templates = useTemplatesStore();
  vi.spyOn(templates, "fetchDetail").mockResolvedValue(globalThis.structuredClone(detail) as never);
  vi.spyOn(templates, "render").mockResolvedValue(globalThis.structuredClone(renderResult) as never);
  const servers = useServersStore();
  vi.spyOn(servers, "fetchServers").mockResolvedValue(undefined);
  harness();
  showRef.value = true;
  await flushPromises();
}

describe("open", () => {
  it("loads the schema and seeds defaults on show", async () => {
    await openWizard();
    expect(wizard?.detail.value?.slug).toBe("redis");
    expect(wizard?.values.value).toEqual({ password: "", port: "6379" });
    expect(wizard?.name.value).toBe("redis");
    expect(wizard?.step.value).toBe(1);
  });

  it("surfaces detail errors", async () => {
    const templates = useTemplatesStore();
    vi.spyOn(templates, "fetchDetail").mockRejectedValue(new Error("gone"));
    const servers = useServersStore();
    vi.spyOn(servers, "fetchServers").mockResolvedValue(undefined);
    harness();
    showRef.value = true;
    await flushPromises();
    expect(wizard?.detailError.value).not.toBeNull();
  });
});

describe("step gating", () => {
  it("blocks Next while required fields are empty", async () => {
    await openWizard();
    wizard?.next();
    expect(wizard?.step.value).toBe(1);
    expect(wizard?.showErrors.value).toBe(true);
  });

  it("advances to preview and create once valid", async () => {
    await openWizard();
    if (wizard) {
      wizard.values.value = { password: "hunter2", port: "6379" };
    }
    wizard?.next();
    expect(wizard?.step.value).toBe(2);
    await flushPromises();
    expect(wizard?.render.value?.compose_yaml).toContain("redis");
    wizard?.next();
    expect(wizard?.step.value).toBe(3);
  });
});

describe("create and deploy", () => {
  it("creates with the rendered document plus secret env, then deploys", async () => {
    await openWizard();
    if (wizard) {
      wizard.values.value = { password: "hunter2", port: "6379" };
    }
    wizard?.next();
    await flushPromises();
    wizard?.next();
    expect(wizard?.step.value).toBe(3);
    const createService = vi
      .spyOn(servicesApi, "createService")
      .mockResolvedValue(globalThis.structuredClone(createdService));
    const deployService = vi.spyOn(servicesApi, "deployService").mockResolvedValue({
      service: globalThis.structuredClone(createdService),
      deploy: { id: "dep-1" },
    } as never);
    if (wizard) {
      wizard.name.value = "redis";
      wizard.serverId.value = "srv-1";
      wizard.scopeEnvironmentId.value = "env-1";
    }
    await wizard?.handleCreate();
    expect(createService).toHaveBeenCalledWith({
      name: "redis",
      environment_id: "env-1",
      server_id: "srv-1",
      compose_yaml: "services:\n  redis: {}",
      env: { REDIS_PASSWORD: "secret" },
    });
    expect(wizard?.created.value?.id).toBe("svc-1");
    await wizard?.handleDeploy();
    expect(deployService).toHaveBeenCalledWith("svc-1");
    expect(wizard?.deployed.value).toBe(true);
  });

  it("blocks create without a name, node or scope", async () => {
    await openWizard();
    if (wizard) {
      wizard.values.value = { password: "hunter2", port: "6379" };
      wizard.name.value = "";
      wizard.serverId.value = "";
    }
    wizard?.next();
    await flushPromises();
    wizard?.next();
    const createService = vi.spyOn(servicesApi, "createService");
    await wizard?.handleCreate();
    expect(createService).not.toHaveBeenCalled();
    expect(wizard?.nameError.value).not.toBe("");
    expect(wizard?.nodeError.value).not.toBe("");
    expect(wizard?.scopeError.value).not.toBe("");
  });

  it("resets every value including secrets", async () => {
    await openWizard();
    if (wizard) {
      wizard.values.value = { password: "hunter2", port: "6379" };
      wizard.step.value = 3;
    }
    wizard?.reset();
    expect(wizard?.step.value).toBe(1);
    expect(wizard?.values.value).toEqual({});
    expect(wizard?.render.value).toBeNull();
  });
});
