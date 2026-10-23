import { mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick, ref } from "vue";

import { useCreateDatabaseWizard } from "../src/features/databases/composables/useCreateDatabaseWizard";
import { engineByValue } from "../src/features/databases/utils/databaseEngines";

beforeEach(() => {
  setActivePinia(createPinia());
  vi.restoreAllMocks();
});

function harness(setup: () => unknown) {
  return mount({
    render: () =>
      h(NMessageProvider, null, {
        default: () => h(defineComponent({ setup: setup as never })),
      }),
  });
}

describe("create database wizard preselects the engine default version", () => {
  it("opens with the postgres default shown", () => {
    let form: { version: string } | null = null;
    const wrapper = harness(() => {
      const wizard = useCreateDatabaseWizard({
        show: ref(false),
        onCreated: () => undefined,
        onUpdateShow: () => undefined,
      });
      form = wizard.form;
      return () => h("div");
    });
    expect(form!.version).toBe(engineByValue("postgres").defaultVersion);
    wrapper.unmount();
  });

  it("reselects the new engine default when the engine changes", async () => {
    let form: { engine: string; version: string } | null = null;
    const wrapper = harness(() => {
      const wizard = useCreateDatabaseWizard({
        show: ref(false),
        onCreated: () => undefined,
        onUpdateShow: () => undefined,
      });
      form = wizard.form;
      return () => h("div");
    });
    form!.engine = "mysql";
    await nextTick();
    expect(form!.version).toBe(engineByValue("mysql").defaultVersion);
    wrapper.unmount();
  });
});
