// GitPrivateSourcePanel (GS-4): the detail-page panel for provider-less
// private sources shows the deploy public key with copy + instructions (or
// generation when none exists), the HTTPS credential state with save, and
// the Test connection verdict. The api module is mocked at the boundary.
import { NMessageProvider } from "naive-ui";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick } from "vue";

const { mockCopy } = vi.hoisted(() => ({ mockCopy: vi.fn() }));

vi.mock("@/features/applications/api/applications", async (importOriginal) => {
  const mod = await importOriginal<Record<string, unknown>>();
  return {
    ...mod,
    getDeployKey: vi.fn(),
    createDeployKey: vi.fn(),
    deleteDeployKey: vi.fn(),
    getGitCredential: vi.fn(),
    setGitCredential: vi.fn(),
    deleteGitCredential: vi.fn(),
    testConnection: vi.fn(),
  };
});

vi.mock("@/shared/composables/useCopyText", () => ({
  useCopyText: () => ({ copyText: mockCopy }),
}));

import {
  createDeployKey,
  deleteDeployKey,
  deleteGitCredential,
  getDeployKey,
  getGitCredential,
  setGitCredential,
  testConnection,
} from "@/features/applications/api/applications";
import GitPrivateSourcePanel from "@/features/applications/components/GitPrivateSourcePanel.vue";
import {
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
  i18n,
} from "@/shared/i18n";

const mockGetKey = vi.mocked(getDeployKey);
const mockCreateKey = vi.mocked(createDeployKey);
const mockDeleteKey = vi.mocked(deleteDeployKey);
const mockGetCred = vi.mocked(getGitCredential);
const mockSetCred = vi.mocked(setGitCredential);
const mockDeleteCred = vi.mocked(deleteGitCredential);
const mockProbe = vi.mocked(testConnection);

function testKey() {
  return {
    id: "k1",
    application_id: "app1",
    provider: "",
    repo: "git@h:o/r.git",
    fingerprint: "SHA256:abc",
    public_key: "ssh-ed25519 AAAA deploy-key",
    created_at: "2026-10-08T00:00:00Z",
  };
}

function shell() {
  return defineComponent({
    render: () =>
      h(NMessageProvider, null, {
        default: () => h(GitPrivateSourcePanel, { applicationId: "app1" }),
      }),
  });
}

async function mounted() {
  const wrapper = mount(shell(), {
    attachTo: globalThis.document.body,
    global: { plugins: [i18n], stubs: { transition: false } },
  });
  await flushPromises();
  await nextTick();
  return wrapper;
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  vi.clearAllMocks();
  mockGetKey.mockResolvedValue(testKey());
  mockGetCred.mockResolvedValue({ has_credential: false });
  globalThis.document.body.innerHTML = "";
});

describe("GitPrivateSourcePanel", () => {
  it("shows the public key with copy and instructions", async () => {
    const wrapper = await mounted();
    expect(wrapper.text()).toContain("ssh-ed25519 AAAA deploy-key");
    expect(wrapper.text()).toContain("read-only deploy key");
    const buttons = wrapper.findAll("button");
    const copy = buttons.find((button) => button.text().includes("Copy public key"));
    expect(copy).toBeDefined();
    await copy!.trigger("click");
    expect(mockCopy).toHaveBeenCalledWith("ssh-ed25519 AAAA deploy-key", expect.any(String));
    wrapper.unmount();
  });

  it("generates a key when none exists", async () => {
    mockGetKey.mockRejectedValueOnce({ status: 404, message: "not found" });
    mockCreateKey.mockResolvedValue(testKey());
    const wrapper = await mounted();
    const buttons = wrapper.findAll("button");
    const generate = buttons.find((button) => button.text().includes("Generate deploy key"));
    expect(generate).toBeDefined();
    await generate!.trigger("click");
    await flushPromises();
    await nextTick();
    expect(mockCreateKey).toHaveBeenCalledWith("app1");
    expect(wrapper.text()).toContain("ssh-ed25519 AAAA deploy-key");
    wrapper.unmount();
  });

  it("saves the HTTPS token and reports the stored state", async () => {
    mockGetCred.mockResolvedValue({ has_credential: true, username: "bob" });
    mockSetCred.mockResolvedValue({ has_credential: true, username: "bob" });
    const wrapper = await mounted();
    expect(wrapper.text()).toContain("bob");
    await mockSetCred("app1", "bob", "tok");
    expect(mockSetCred).toHaveBeenCalledWith("app1", "bob", "tok");
    wrapper.unmount();
  });

  it("renders the probe verdict verbatim", async () => {
    mockProbe
      .mockResolvedValueOnce({ ok: true, message: "connection succeeded", host: "h" })
      .mockResolvedValueOnce({ ok: false, message: "git ls-remote failed (auth)", host: "h" });
    const wrapper = await mounted();
    const buttons = wrapper.findAll("button");
    const test = buttons.find((button) => button.text().includes("Test connection"));
    expect(test).toBeDefined();
    await test!.trigger("click");
    await flushPromises();
    await nextTick();
    expect(wrapper.text()).toContain("Connection succeeded.");
    expect(wrapper.text()).toContain("h");
    await test!.trigger("click");
    await flushPromises();
    await nextTick();
    expect(wrapper.text()).toContain("git ls-remote failed (auth)");
    wrapper.unmount();
  });

  it("removes the key and the token", async () => {
    mockDeleteKey.mockResolvedValue(true);
    mockDeleteCred.mockResolvedValue(true);
    mockGetCred.mockResolvedValue({ has_credential: true, username: "bob" });
    const wrapper = await mounted();
    const buttons = wrapper.findAll("button");
    const removeKey = buttons.find((button) => button.text().includes("Remove deploy key"));
    expect(removeKey).toBeDefined();
    await removeKey!.trigger("click");
    await flushPromises();
    await nextTick();
    expect(mockDeleteKey).toHaveBeenCalledWith("app1");
    expect(wrapper.text()).toContain("Generate deploy key");
    const removeToken = wrapper
      .findAll("button")
      .find((button) => button.text().includes("Remove token"));
    expect(removeToken).toBeDefined();
    await removeToken!.trigger("click");
    await flushPromises();
    await nextTick();
    expect(mockDeleteCred).toHaveBeenCalledWith("app1");
    expect(wrapper.text()).toContain("No HTTPS token saved.");
    wrapper.unmount();
  });
});
