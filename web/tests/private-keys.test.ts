import { NMessageProvider } from "naive-ui";
import { defineComponent, h } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import type { VueWrapper } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/private-keys/api/keys", () => ({
  listPrivateKeys: vi.fn(),
  listServersUsingKeys: vi.fn(),
  createPrivateKey: vi.fn(),
  generatePrivateKey: vi.fn(),
  updatePrivateKey: vi.fn(),
  deletePrivateKey: vi.fn(),
  revealPrivateKey: vi.fn(),
  describePrivateKeyError: vi.fn((_error: unknown, fallback: string) => fallback),
}));

import {
  listPrivateKeys,
  listServersUsingKeys,
  revealPrivateKey,
} from "@/features/private-keys/api/keys";
import { usePrivateKeysPage } from "@/features/private-keys/composables/usePrivateKeysPage";
import {
  generatePrivateKeyFormSchema,
  importPrivateKeyFormSchema,
} from "@/features/private-keys/schemas/keys";
import type { PrivateKey } from "@/features/private-keys/schemas/keys";
import { randomKeyName } from "@/features/private-keys/utils/randomName";
import { checkCatalogParity } from "@/shared/i18n/catalog";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";
import type { Server } from "@/features/servers";

const mockList = vi.mocked(listPrivateKeys);
const mockServers = vi.mocked(listServersUsingKeys);
const mockReveal = vi.mocked(revealPrivateKey);

const namePattern = /^[a-z]+-[a-z0-9]{12}$/;

function privateKey(overrides: Partial<PrivateKey> = {}): PrivateKey {
  return {
    id: "key-1",
    name: "web-1-deploy",
    type: "ed25519",
    description: "Main web node",
    public_key: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI web-1",
    created_at: "2026-10-26T10:00:00Z",
    ...overrides,
  };
}

function server(id: string, sshKeyId: string | null): Server {
  return {
    id,
    name: id,
    ip: "10.0.0.2",
    port: 9442,
    ssh_user: "root",
    ssh_key_id: sshKeyId,
    has_password: false,
    status: "ready",
    node_id: null,
    os: null,
    docker_version: null,
    arch: null,
    total_mem: null,
    total_disk: null,
    cpu_usage: null,
    mem_usage: null,
    disk_usage: null,
    container_count: null,
    last_seen: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

async function mountProbe(): Promise<{
  page: ReturnType<typeof usePrivateKeysPage>;
  wrapper: VueWrapper;
}> {
  const holder: { page?: ReturnType<typeof usePrivateKeysPage> } = {};
  const Probe = defineComponent({
    setup() {
      holder.page = usePrivateKeysPage();
      return () => h("div");
    },
  });
  const wrapper = mount(
    defineComponent({
      render() {
        return h(NMessageProvider, null, { default: () => h(Probe) });
      },
    }),
    { global: { plugins: [i18n] } },
  );
  await flushPromises();
  return { page: holder.page!, wrapper };
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  vi.clearAllMocks();
  mockList.mockResolvedValue([]);
  mockServers.mockResolvedValue([]);
  globalThis.document.body.innerHTML = "";
});

describe("private key names", () => {
  it("generates a first-name suffix that matches the acceptance pattern", () => {
    const names = new Set(Array.from({ length: 20 }, () => randomKeyName()));
    expect(names.size).toBeGreaterThan(1);
    for (const name of names) {
      expect(name).toMatch(namePattern);
    }
  });

  it("regenerates the prefill every time the create modal opens", async () => {
    const { page, wrapper } = await mountProbe();
    page.openCreate();
    const first = page.createName.value;
    expect(first).toMatch(namePattern);
    expect(page.createType.value).toBe("ed25519");
    page.closeCreate();
    page.openCreate();
    expect(page.createName.value).toMatch(namePattern);
    expect(page.createName.value).not.toBe(first);
    wrapper.unmount();
  });
});

describe("private key forms", () => {
  it("requires a name for generate and a PEM for import", () => {
    expect(
      generatePrivateKeyFormSchema.safeParse({
        name: "",
        description: "",
        type: "ed25519",
      }).success,
    ).toBe(false);
    expect(
      generatePrivateKeyFormSchema.safeParse({
        name: "web-1",
        description: "",
        type: "ed25519",
      }).success,
    ).toBe(true);
    expect(
      importPrivateKeyFormSchema.safeParse({
        name: "web-1",
        description: "",
        private_key: "  ",
        public_key: "",
      }).success,
    ).toBe(false);
    expect(
      importPrivateKeyFormSchema.safeParse({
        name: "web-1",
        description: "",
        private_key: "-----BEGIN OPENSSH PRIVATE KEY-----",
        public_key: "",
      }).success,
    ).toBe(true);
  });

  it("keeps the EN and VI catalogs in parity", async () => {
    const en = (await import("@/features/private-keys/locales/en")).default;
    const vi = (await import("@/features/private-keys/locales/vi")).default;
    expect(checkCatalogParity(en, vi)).toEqual([]);
  });
});

describe("private key state", () => {
  it("counts the servers attached to each key", async () => {
    mockList.mockResolvedValue([privateKey()]);
    mockServers.mockResolvedValue([
      server("srv-1", "key-1"),
      server("srv-2", "key-1"),
      server("srv-3", "key-2"),
    ]);
    const { page, wrapper } = await mountProbe();
    expect(page.usageById.value["key-1"]).toBe(2);
    expect(page.usageById.value["key-2"]).toBe(1);
    wrapper.unmount();
  });

  it("only exposes the private half after an explicit reveal", async () => {
    const key = privateKey();
    mockList.mockResolvedValue([key]);
    mockReveal.mockResolvedValue("-----BEGIN OPENSSH PRIVATE KEY-----");
    const { page, wrapper } = await mountProbe();

    expect(page.showing.value).toBeNull();
    await page.reveal(key);
    expect(mockReveal).toHaveBeenCalledWith("key-1");
    expect(page.showing.value?.privateKey).toBe(
      "-----BEGIN OPENSSH PRIVATE KEY-----",
    );

    page.closeReveal();
    expect(page.showing.value).toBeNull();
    wrapper.unmount();
  });
});
