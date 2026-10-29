import { mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, ref } from "vue";

import { useCreateDatabaseWizard } from "@/features/databases/composables/useCreateDatabaseWizard";
import DatabaseProvisioningBanner from "@/features/databases/components/DatabaseProvisioningBanner.vue";
import {
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

registerDiscoveredCatalogs();

const { createDatabase } = vi.hoisted(() => ({ createDatabase: vi.fn() }));

vi.mock("@/features/databases/api/databases", async (importOriginal) => {
  const mod =
    await importOriginal<typeof import("@/features/databases/api/databases")>();
  return { ...mod, createDatabase: (...args: unknown[]) => createDatabase(...args) };
});

function creatingDatabase() {
  return {
    id: "db-1",
    name: "orders",
    environment_id: "env-1",
    environment_name: "production",
    project_id: "proj-1",
    project_name: "shop",
    engine: "postgres",
    version: "16-alpine",
    status: "creating",
    server_id: "srv-1",
    server_name: "node",
    container_id: "",
    public_port: 0,
    volume: "vol-1",
    created_at: "2026-09-01T12:00:00.000Z",
    updated_at: "2026-09-01T12:00:00.000Z",
  };
}

function harness(setup: () => unknown) {
  return mount({
    render: () =>
      h(NMessageProvider, null, {
        default: () => h(defineComponent({ setup: setup as never })),
      }),
  });
}

beforeEach(() => {
  resetLocaleState();
  syncComposerLocale("en");
  setActivePinia(createPinia());
  createDatabase.mockReset();
});

describe("async create database wizard", () => {
  it("hands the creating database over immediately after the 202 response", async () => {
    const created = {
      database: creatingDatabase(),
      credentials: { username: "orders", password: "pw", database: "orders" },
    };
    createDatabase.mockResolvedValue(created);

    let wizard: ReturnType<typeof useCreateDatabaseWizard> | null = null;
    const onCreated = vi.fn();
    const wrapper = harness(() => {
      wizard = useCreateDatabaseWizard({
        show: ref(false),
        onCreated,
        onUpdateShow: () => undefined,
        projectId: "proj-1",
        environmentId: "env-1",
      });
      return () => h("div");
    });

    wizard!.form.name = "orders";
    wizard!.form.serverId = "srv-1";
    await wizard!.handleSubmit();

    expect(createDatabase).toHaveBeenCalledTimes(1);
    expect(onCreated).toHaveBeenCalledTimes(1);
    expect(onCreated.mock.calls[0][0].database.status).toBe("creating");
    wrapper.unmount();
  });
});

describe("database provisioning banner", () => {
  it("explains that setup can take a few minutes while creating", () => {
    const wrapper = mount(DatabaseProvisioningBanner, {
      props: { status: "creating" },
    });
    expect(wrapper.text()).toContain("Setting up the database");
    expect(wrapper.text()).toContain("image downloads");
    wrapper.unmount();
  });

  it("shows the stored reason when provisioning failed", () => {
    const wrapper = mount(DatabaseProvisioningBanner, {
      props: { status: "error", statusMessage: "agent unavailable" },
    });
    expect(wrapper.text()).toContain("Provisioning failed");
    expect(wrapper.text()).toContain("agent unavailable");
    wrapper.unmount();
  });

  it("stays out of the way for a healthy database", () => {
    const wrapper = mount(DatabaseProvisioningBanner, {
      props: { status: "running" },
    });
    expect(wrapper.text()).toBe("");
    wrapper.unmount();
  });
});
