/* global Response:readonly, TextEncoder:readonly, ReadableStream:readonly, RequestInit:readonly, AbortSignal:readonly, DOMException:readonly, Headers:readonly */
// JUS-187: the Add-server wizard offers "Install Docker" over the SSH session
// when the control plane reports the node installable, and streams the install
// output into the log modal. The terminal outcome travels on the last line of
// the stream, because the HTTP status is a committed 200 after the first byte;
// it is honored only there and only with the response's nonce.
import { flushPromises, mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick } from "vue";

import { i18n, registerDiscoveredCatalogs, setLocale } from "@/shared/i18n";
import { useAddServerWizard } from "@/features/servers/composables/useAddServerWizard";
import type { AddServerWizardContext } from "@/features/servers/composables/useAddServerWizard";
import { useServersStore } from "@/features/servers/stores/servers";

const MARKER = "\u001e[gotham-install] ";
const NONCE = "nonce-123";

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

/** markerLine renders one terminal marker line. */
function markerLine(payload: Record<string, unknown>): string {
  return `${MARKER}${JSON.stringify(payload)}\n`;
}

/**
 * streamResponse builds a 200 chunked response. nonce "" omits the nonce
 * header, so no marker can be accepted.
 */
function streamResponse(chunks: string[], nonce: string = NONCE): Response {
  const encoder = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) {
        controller.enqueue(encoder.encode(chunk));
      }
      controller.close();
    },
  });
  const headers = new Headers();
  if (nonce !== "") {
    headers.set("X-Install-Nonce", nonce);
  }
  return { ok: true, status: 200, body, headers } as unknown as Response;
}

/** settle flushes the async stream reader to completion. */
async function settle(): Promise<void> {
  for (let i = 0; i < 5; i += 1) {
    await Promise.resolve();
  }
  await flushPromises();
}

async function registerAndFailDocker(
  wizard: AddServerWizardContext,
  installable: boolean,
): Promise<void> {
  const store = useServersStore();
  vi.spyOn(store, "addServer").mockResolvedValue({ id: "srv-1" } as never);
  vi.spyOn(store, "validate").mockResolvedValue({
    ok: false,
    message: "",
    checks: [
      {
        name: "docker",
        ok: false,
        detail: "Docker Engine is not installed on this node",
        installable,
      },
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
  it("offers the install only when the probe reports the node installable", async () => {
    const notInstallable = harness();
    await registerAndFailDocker(notInstallable.wizard, false);
    expect(notInstallable.wizard.dockerInstallOffer.value).toBe(false);
    notInstallable.wrapper.unmount();

    const installable = harness();
    await registerAndFailDocker(installable.wizard, true);
    expect(installable.wizard.dockerInstallOffer.value).toBe(true);
    installable.wrapper.unmount();
  });

  it("streams output, reads the ok marker, and re-runs the probe", async () => {
    const { wizard, wrapper } = harness();
    await registerAndFailDocker(wizard, true);

    const fetchMock = vi.fn().mockResolvedValue(
      streamResponse([
        "Reading package lists...\n",
        "Setting up docker.io\n",
        markerLine({ status: "ok", nonce: NONCE }),
      ]),
    );
    vi.stubGlobal("fetch", fetchMock);

    const store = useServersStore();
    wizard.openDockerInstall();
    expect(wizard.dockerInstallOpen.value).toBe(true);
    await settle();

    expect(wizard.dockerInstall.status.value).toBe("done");
    expect(wizard.dockerInstall.error.value).toBe("");
    // The marker is consumed, never rendered.
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

  it("turns a mid-stream failure marker into an error", async () => {
    const { wizard, wrapper } = harness();
    await registerAndFailDocker(wizard, true);

    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        streamResponse([
          "Setting up docker.io\n",
          markerLine({
            status: "fail",
            nonce: NONCE,
            exit_code: 100,
            message: "docker install failed: exit status 100",
          }),
        ]),
      ),
    );

    wizard.openDockerInstall();
    await settle();

    expect(wizard.dockerInstall.status.value).toBe("error");
    expect(wizard.dockerInstall.error.value).toContain("exit status 100");
    expect(wizard.dockerInstall.lines.value.map((line) => line.text)).toEqual([
      "Setting up docker.io",
    ]);
    wrapper.unmount();
  });

  it("treats a stream without a marker as a lost connection", async () => {
    const { wizard, wrapper } = harness();
    await registerAndFailDocker(wizard, true);

    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(streamResponse(["partial output\n"])));

    wizard.openDockerInstall();
    await settle();

    expect(wizard.dockerInstall.status.value).toBe("error");
    expect(wizard.dockerInstall.error.value).toContain("Connection lost");
    wrapper.unmount();
  });

  it("does not let remote output forge the marker prefix", async () => {
    const { wizard, wrapper } = harness();
    await registerAndFailDocker(wizard, true);

    // The node's output copies the marker bytes but is not the JSON the server
    // writes, then the stream ends without a real terminal line: the install
    // must read as lost, not as success.
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(streamResponse(["banner\n", `${MARKER}ok\n`])),
    );

    wizard.openDockerInstall();
    await settle();

    expect(wizard.dockerInstall.status.value).toBe("error");
    expect(wizard.dockerInstall.error.value).toContain("Connection lost");
    // The impersonated line is rendered as ordinary output, not swallowed.
    expect(wizard.dockerInstall.lines.value.map((line) => line.text)).toEqual([
      "banner",
      `${MARKER}ok`,
    ]);
    wrapper.unmount();
  });

  it("rejects a final marker that does not carry the response nonce", async () => {
    const { wizard, wrapper } = harness();
    await registerAndFailDocker(wizard, true);

    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        streamResponse(["output\n", markerLine({ status: "ok", nonce: "forged" })]),
      ),
    );

    wizard.openDockerInstall();
    await settle();

    expect(wizard.dockerInstall.status.value).toBe("error");
    expect(wizard.dockerInstall.error.value).toContain("Connection lost");
    wrapper.unmount();
  });

  it("rejects every marker when the response carries no nonce", async () => {
    const { wizard, wrapper } = harness();
    await registerAndFailDocker(wizard, true);

    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        streamResponse(["output\n", markerLine({ status: "ok", nonce: NONCE })], ""),
      ),
    );

    wizard.openDockerInstall();
    await settle();

    expect(wizard.dockerInstall.status.value).toBe("error");
    expect(wizard.dockerInstall.error.value).toContain("Connection lost");
    wrapper.unmount();
  });

  it("accepts a marker only as the final line of the stream", async () => {
    const { wizard, wrapper } = harness();
    await registerAndFailDocker(wizard, true);

    // A well-formed, correctly-nonced marker that is NOT last is ordinary text.
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        streamResponse([
          markerLine({ status: "ok", nonce: NONCE }),
          "still installing\n",
        ]),
      ),
    );

    wizard.openDockerInstall();
    await settle();

    expect(wizard.dockerInstall.status.value).toBe("error");
    expect(wizard.dockerInstall.error.value).toContain("Connection lost");
    wrapper.unmount();
  });

  it("surfaces an honest preflight failure from the control plane", async () => {
    const { wizard, wrapper } = harness();
    await registerAndFailDocker(wizard, true);

    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: false,
        status: 400,
        text: async () => '{"message":"automatic Docker install is not supported on rhel"}',
      } as unknown as Response),
    );

    wizard.openDockerInstall();
    await settle();

    expect(wizard.dockerInstall.status.value).toBe("error");
    expect(wizard.dockerInstall.error.value).toContain("not supported");
    wrapper.unmount();
  });

  it("caps the rendered line count for a very long stream", async () => {
    const { wizard, wrapper } = harness();
    await registerAndFailDocker(wizard, true);

    const bulk = Array.from({ length: 2500 }, (_v, i) => `line ${i}\n`).join("");
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(streamResponse([bulk, markerLine({ status: "ok", nonce: NONCE })])),
    );

    wizard.openDockerInstall();
    await settle();

    expect(wizard.dockerInstall.status.value).toBe("done");
    // The cap plus its trim slack bounds the buffer, so a long install cannot
    // grow the rendered list without bound.
    const rendered = wizard.dockerInstall.lines.value.length;
    expect(rendered).toBeLessThanOrEqual(2000 + 200);
    expect(rendered).toBeGreaterThan(2000);
    wrapper.unmount();
  });

  it("aborts the stream when the modal closes mid-install", async () => {
    const { wizard, wrapper } = harness();
    await registerAndFailDocker(wizard, true);

    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation(
        (_url: string, init: RequestInit) =>
          new Promise<Response>((_resolve, reject) => {
            const signal = init.signal as AbortSignal;
            signal.addEventListener("abort", () => {
              reject(new DOMException("aborted", "AbortError"));
            });
          }),
      ),
    );

    wizard.openDockerInstall();
    await settle();
    expect(wizard.dockerInstall.status.value).toBe("running");

    wizard.closeDockerInstall();
    expect(wizard.dockerInstallOpen.value).toBe(false);
    await settle();
    // An aborted install is not reported as a failure, and it is no longer
    // running, so reopening the modal can start a fresh attempt.
    expect(wizard.dockerInstall.status.value).toBe("idle");
    wrapper.unmount();
  });
});
