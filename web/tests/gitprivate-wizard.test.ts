// Wizard SSH post-create flow (GS-4 fix round 1): an SSH private source
// creates the application and its deploy key WITHOUT queueing the first
// deploy; Deploy stays gated until a connection test passes or the operator
// confirms the registration. The HTTPS flow still seals the token and
// auto-deploys. The api and store modules are mocked at the boundary.
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick, ref } from "vue";

const { mockDeploy } = vi.hoisted(() => ({ mockDeploy: vi.fn() }));

vi.mock("@/features/applications/api/applications", async (importOriginal) => {
  const mod = await importOriginal<Record<string, unknown>>();
  return {
    ...mod,
    createApplication: vi.fn(),
    createDeployKey: vi.fn(),
    deleteApplication: vi.fn(),
    setGitCredential: vi.fn(),
    testConnection: vi.fn(),
  };
});

vi.mock("@/features/applications/stores/applications", () => ({
  useApplicationsStore: () => ({ deploy: mockDeploy }),
}));

import {
  createApplication,
  createDeployKey,
  deleteApplication,
  setGitCredential,
  testConnection,
} from "@/features/applications/api/applications";
import {
  useCreateAppWizard,
  type WizardEvents,
} from "@/features/applications/composables/useCreateAppWizard";
import {
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const mockCreate = vi.mocked(createApplication);
const mockCreateKey = vi.mocked(createDeployKey);
const mockDeleteApp = vi.mocked(deleteApplication);
const mockSetCred = vi.mocked(setGitCredential);
const mockProbe = vi.mocked(testConnection);

function testApp() {
  return { id: "app1", name: "storefront" };
}

async function harness() {
  let wiz: ReturnType<typeof useCreateAppWizard> | null = null;
  const emitted: Array<{ event: string; value: unknown }> = [];
  const emit = ((event: string, value: unknown) => {
    emitted.push({ event, value });
  }) as unknown as WizardEvents;
  const Harness = defineComponent({
    setup() {
      wiz = useCreateAppWizard(ref(true), emit, { projectId: "p", environmentId: "e" });
      return () => h("div");
    },
  });
  const wrapper = mount({
    render: () => h(NMessageProvider, null, { default: () => h(Harness) }),
  });
  await flushPromises();
  await nextTick();
  return { wrapper, wiz: wiz!, emitted };
}

async function setSource(wiz: ReturnType<typeof useCreateAppWizard>, patch: Record<string, unknown>) {
  // Assign the type first and flush, so the watcher's field reset lands
  // before the type's own fields (as in the real UI flow).
  wiz.form.sourceType = patch.sourceType as typeof wiz.form.sourceType;
  await nextTick();
  Object.assign(wiz.form, patch);
}

async function sshForm(wiz: ReturnType<typeof useCreateAppWizard>) {
  await setSource(wiz, {
    sourceType: "git_private",
    privateCloneUrl: "git@h:o/r.git",
    privateAuth: "ssh",
    branch: "main",
    name: "storefront",
    serverId: "s1",
  });
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  setActivePinia(createPinia());
  vi.clearAllMocks();
  mockCreate.mockResolvedValue({ application: testApp(), webhook: undefined });
  mockCreateKey.mockResolvedValue({ public_key: "ssh-ed25519 AAAA key" });
  mockSetCred.mockResolvedValue({ has_credential: true, username: "" });
  mockProbe.mockResolvedValue({ ok: true, message: "connection succeeded", host: "h" });
  mockDeploy.mockResolvedValue({ id: "dep1" });
});

describe("wizard SSH post-create flow", () => {
  it("creates the key without deploying and gates Deploy on test-or-confirm", async () => {
    const { wrapper, wiz, emitted } = await harness();
    await sshForm(wiz);

    await wiz.handleSubmit();
    expect(mockCreate).toHaveBeenCalledOnce();
    expect(mockCreateKey).toHaveBeenCalledWith("app1");
    // No blind first deploy: the operator has not registered the key yet.
    expect(mockDeploy).not.toHaveBeenCalled();
    expect(wiz.createdKey.value?.publicKey).toBe("ssh-ed25519 AAAA key");
    expect(emitted).toEqual([]);

    // Deploy stays disabled until a test passes or the operator confirms.
    expect(wiz.canDeployCreated.value).toBe(false);
    await wiz.runCreatedKeyTest();
    expect(mockProbe).toHaveBeenCalledWith("app1");
    expect(wiz.keyTestPassed.value).toBe(true);
    expect(wiz.canDeployCreated.value).toBe(true);

    await wiz.deployCreatedKey();
    expect(mockDeploy).toHaveBeenCalledWith("app1");
    expect(emitted).toContainEqual({ event: "created", value: testApp() });
    expect(emitted).toContainEqual({ event: "update:show", value: false });
    expect(wiz.createdKey.value).toBeNull();
    wrapper.unmount();
  });

  it("lets an explicit confirmation stand in for the test", async () => {
    const { wrapper, wiz } = await harness();
    await sshForm(wiz);

    await wiz.handleSubmit();
    expect(wiz.createdKey.value).not.toBeNull();
    expect(wiz.canDeployCreated.value).toBe(false);
    wiz.keyConfirmed.value = true;
    expect(wiz.canDeployCreated.value).toBe(true);
    wrapper.unmount();
  });

  it("surfaces a key failure with recovery instead of a blind retry", async () => {
    const { wrapper, wiz, emitted } = await harness();
    await sshForm(wiz);
    mockCreateKey.mockRejectedValueOnce({ status: 500, message: "boom" });

    await wiz.handleSubmit();
    expect(wiz.createdKey.value).toBeNull();
    expect(mockDeploy).not.toHaveBeenCalled();
    expect(wiz.errorMessage.value).not.toBe("");
    expect(wiz.keyRecovery.value?.application).toEqual(testApp());
    expect(emitted).toEqual([]);

    // Retry lands on the key step without recreating the application.
    mockCreateKey.mockResolvedValueOnce({ public_key: "ssh-ed25519 AAAA key" });
    await wiz.retryKeyCreation();
    expect(mockCreate).toHaveBeenCalledOnce();
    expect(wiz.createdKey.value?.publicKey).toBe("ssh-ed25519 AAAA key");
    expect(wiz.keyRecovery.value).toBeNull();
    wrapper.unmount();
  });

  it("rolls the parked application back on delete", async () => {
    const { wrapper, wiz } = await harness();
    await sshForm(wiz);
    mockCreateKey.mockRejectedValueOnce({ status: 500, message: "boom" });
    mockDeleteApp.mockResolvedValueOnce(undefined);

    await wiz.handleSubmit();
    expect(wiz.keyRecovery.value).not.toBeNull();
    await wiz.deleteRecoveryApp();
    expect(mockDeleteApp).toHaveBeenCalledWith("app1");
    expect(wiz.keyRecovery.value).toBeNull();
    expect(wiz.form.name).toBe("");
    wrapper.unmount();
  });

  it("emits created when the key step closes without deploying", async () => {
    const { wrapper, wiz, emitted } = await harness();
    await sshForm(wiz);

    await wiz.handleSubmit();
    expect(wiz.createdKey.value).not.toBeNull();
    wiz.handleShowChange(false);
    expect(emitted).toContainEqual({ event: "created", value: testApp() });
    expect(wiz.createdKey.value).toBeNull();
    wrapper.unmount();
  });

  it("keeps the HTTPS auto-deploy (token needs no operator action)", async () => {
    const { wrapper, wiz, emitted } = await harness();
    await setSource(wiz, {
      sourceType: "git_private",
      privateCloneUrl: "https://h/o/r.git",
      privateAuth: "https",
      httpsUsername: "bob",
      httpsToken: "tok",
      branch: "main",
      name: "storefront",
      serverId: "s1",
    });

    await wiz.handleSubmit();
    expect(mockSetCred).toHaveBeenCalledWith("app1", "bob", "tok");
    expect(mockCreateKey).not.toHaveBeenCalled();
    expect(mockDeploy).toHaveBeenCalledWith("app1");
    expect(wiz.createdKey.value).toBeNull();
    expect(emitted).toContainEqual({ event: "created", value: testApp() });
    wrapper.unmount();
  });
});
