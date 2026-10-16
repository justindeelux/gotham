// JUS-187: the Add-server wizard offers "Install Docker" over the SSH session
// and streams the control plane's install output into the log modal.
import { flushPromises, mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick } from "vue";

import { i18n, registerDiscoveredCatalogs, setLocale } from "@/shared/i18n";
import { useAddServerWizard } from "@/features/servers/composables/useAddServerWizard";
import type { AddServerWizardContext } from "@/features/servers/composables/useAddServerWizard";
import { useServersStore } from "@/features/servers/stores/servers";

beforeEach(() => {
  setActivePinia(createPinia());
  registerDiscoveredCatalogs();
  setLocale("en", null);
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function harness() {
  let wizard: AddServerWizardContext | null = null;
  const Child = defineComponent({
    setup() {
      wizard = useAddServerWizard(() => undefined);
      return () => h("div");
    },
  });
  const wrapper = mount(
    defineComponent({
      render: () => h(NMessageProvider, null, { default: () => h(Child) }),
    }),
    { global: { plugins: [i18n] } },
  );
  return { wrapper, wizard: wizard as unknown as AddServerWizardContext };
}

/** streamResponse builds a 200 chunked response body from string chunks. */
function streamResponse(chunks: string[]): Response {
  const encoder = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) {
        controller.enqueue(encoder.encode(chunk));
      }
      controller.close();
    },
  });
  return { ok: true, status: 200, body } as unknown as Response;
}

async function registerAndFailDocker(wizard: AddServerWizardContext): Promise<void> {
  const store = useServersStore();
  vi.spyOn(store, "addServer").mockResolvedValue({ id: "srv-1" } as never);
  vi.spyOn(store, "validate").mockResolvedValue({
    ok: false,
    message: "",
    checks: [
      { name: "docker", ok: false, detail: "Docker Engine is not installed on this node" },
      { name: "cpu", ok: true, detail: "Linux x86_64" },
    ],
  } as never);
  wizard.form.keyMode = "existing";
  wizard.form.keyId = "key-1";
  await wizard.handleCreate();
  await nextTick();
  await wizard.handleValidate();
  await nextTick();
}

describe("wizard Docker install (JUS-187)", () => {
  it("offers the install only once the docker probe fails", async () => {
    const { wizard, wrapper } = harness();
    expect(wizard.dockerInstallOffer.value).toBe(false);

    await registerAndFailDocker(wizard);
    expect(wizard.dockerInstallOffer.value).toBe(true);
    wrapper.unmount();
  });

  it("streams the install output and re-runs the probe afterwards", async () => {
    const { wizard, wrapper } = harness();
    await registerAndFailDocker(wizard);

    const fetchMock = vi
      .fn()
      .mockResolvedValue(streamResponse(["Reading package lists...\n", "Setting up docker.io\n"]));
    vi.stubGlobal("fetch", fetchMock);

    const store = useServersStore();
    wizard.openDockerInstall();
    expect(wizard.dockerInstallOpen.value).toBe(true);
    await flushPromises();

    expect(wizard.dockerInstall.status.value).toBe("done");
    expect(wizard.dockerInstall.lines.value.map((line) => line.text)).toEqual([
      "Reading package lists...",
      "Setting up docker.io",
    ]);
    // The install never carries a stored password: only per-run options.
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(init.body).toBe("{}");

    const validationsBefore = vi.mocked(store.validate).mock.calls.length;
    await wizard.recheckAfterDockerInstall();
    expect(wizard.dockerInstallOpen.value).toBe(false);
    expect(store.validate).toHaveBeenCalledTimes(validationsBefore + 1);
    wrapper.unmount();
  });

  it("surfaces an honest preflight failure from the control plane", async () => {
    const { wizard, wrapper } = harness();
    await registerAndFailDocker(wizard);

    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: false,
        status: 400,
        text: async () => '{"message":"Docker install is not supported on this node"}',
      } as unknown as Response),
    );

    wizard.openDockerInstall();
    await flushPromises();

    expect(wizard.dockerInstall.status.value).toBe("error");
    expect(wizard.dockerInstall.error.value).toContain("not supported");
    wrapper.unmount();
  });
});
