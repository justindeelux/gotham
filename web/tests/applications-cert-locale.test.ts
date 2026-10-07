// F2: the certificate dialog retains the raw failure and derives its display
// text in the current locale. The shared domains formatter is mocked with a
// genuinely locale-aware substitute to prove the consumer caches no string;
// the real baseline helper stays untouched for I18N-8.
import { mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, ref } from "vue";

import { useCertificateConfig } from "@/features/applications/composables/useCertificateConfig";
import {
  registerDiscoveredCatalogs,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";

vi.mock("@/features/domains", async () => {
  const { activeLocale } = await import("@/shared/i18n/locale");
  return {
    describeProxyError: (error: unknown) =>
      `${activeLocale.value === "vi" ? "LỖI" : "ERROR"}: ${(error as Error)?.message ?? String(error)}`,
    draftFromCertificate: (certificate: unknown) => ({ ...(certificate as object) }),
    providerLabel: (provider: string) => provider,
    toCertificateInput: (draft: unknown) => draft,
    useProxyStore: () => ({
      certificateOf: () => null,
      providerOf: () => null,
      fetchProviders: async () => undefined,
      fetchCertificates: async () => undefined,
      createCertificateConfig: async () => {
        throw new Error("node down");
      },
      updateCertificateConfig: async () => undefined,
      removeCertificate: async () => undefined,
      certificatesError: null,
      providers: [],
    }),
  };
});

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
});

describe("certificate failure follows the locale without resubmitting", () => {
  it("re-derives the retained failure and fallback frames on switch", async () => {
    let certs: ReturnType<typeof useCertificateConfig> | null = null;
    const Harness = defineComponent({
      setup() {
        certs = useCertificateConfig(ref({ id: "app-1" }) as never);
        return () => h("div");
      },
    });
    const wrapper = mount({ render: () => h(NMessageProvider, null, { default: () => h(Harness) }) });
    const c = certs!;
    await c.handleSaveCertificate();
    expect(c.certificateError.value).toBe("ERROR: node down");
    expect(c.providerName("")).toBe("shared HTTP resolver");
    expect(c.providerName("missing")).toBe("unknown provider");
    setLocale("vi", null);
    await wrapper.vm.$nextTick();
    expect(c.certificateError.value).toBe("LỖI: node down");
    expect(c.providerName("")).toBe("bộ giải HTTP dùng chung");
    expect(c.providerName("missing")).toBe("nhà cung cấp không xác định");
    wrapper.unmount();
  });
});
