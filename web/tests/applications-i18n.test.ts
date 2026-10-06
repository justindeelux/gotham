// Focused I18N-4 checks: applications catalog parity, invocation-time error
// summaries in both locales with raw diagnostics untouched, live-switch
// behavior that keeps drafts, and 0/1/many counts with unchanged payloads.
import { mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it } from "vitest";
import { defineComponent, h, ref } from "vue";

import {
  describeApplicationError,
} from "@/features/applications/api/applications";
import { describePreviewError } from "@/features/applications/api/previews";
import { describeProviderError } from "@/features/applications/api/providers";
import { useCreateAppWizard } from "@/features/applications/composables/useCreateAppWizard";
import { useApplicationDomain } from "@/features/applications/composables/useApplicationDomain";
import en from "@/features/applications/locales/en";
import vi from "@/features/applications/locales/vi";
import { durationText } from "@/features/applications/utils/deploymentDuration";
import type { Deployment } from "@/features/applications/api/applications";
import { checkCatalogParity } from "@/shared/i18n/catalog";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
});

/** apiError builds the normalized shape isApiError accepts. */
function apiError(status: number, message = "") {
  return { status, message, cause: null };
}

describe("applications catalog", () => {
  it("has matching en/vi keys, params and message syntax", () => {
    expect(checkCatalogParity(en, vi)).toEqual([]);
  });

  it("renders 0/1/many deployment and preview counts", () => {
    for (const count of [0, 1, 12]) {
      expect(String(i18n.global.t("applications.tabs.deployments", { count }))).toBe(
        `Deployments (${count})`,
      );
    }
    setLocale("vi", null);
    for (const count of [0, 1, 12]) {
      expect(String(i18n.global.t("applications.tabs.deployments", { count }))).toBe(
        `Đợt triển khai (${count})`,
      );
      expect(String(i18n.global.t("applications.tabs.previews", { count }))).toBe(
        `Bản xem trước (${count})`,
      );
    }
  });

  it("interpolates destructive confirmations with the actual resource", () => {
    expect(
      String(i18n.global.t("applications.rollback.confirm", { id: "abc12345" })),
    ).toContain("abc12345");
    setLocale("vi", null);
    expect(
      String(i18n.global.t("applications.rollback.confirm", { id: "abc12345" })),
    ).toContain("abc12345");
    expect(
      String(
        i18n.global.t("applications.cert.deleteConfirm", { domain: "app.example.com" }),
      ),
    ).toContain("app.example.com");
  });
});

describe("describe*Error localization", () => {
  it("resolves curated summaries in English and Vietnamese", () => {
    expect(describeApplicationError(apiError(401))).toBe(
      "Your session expired. Please sign in again.",
    );
    expect(describeApplicationError(apiError(404))).toBe(
      "Application not found. It may have been deleted or belong to another account.",
    );
    expect(describePreviewError(apiError(404))).toBe(
      "Preview deployments are not enabled on this control plane (FEATURE_PREVIEWS=false).",
    );
    setLocale("vi", null);
    expect(describeApplicationError(apiError(401))).toBe(
      "Phiên đã hết hạn. Vui lòng đăng nhập lại.",
    );
    expect(describeApplicationError(apiError(404))).toBe(
      "Không tìm thấy ứng dụng. Nó có thể đã bị xóa hoặc thuộc tài khoản khác.",
    );
    expect(describePreviewError(apiError(404))).toBe(
      "Triển khai xem trước chưa bật trên control plane này (FEATURE_PREVIEWS=false).",
    );
    expect(describeProviderError(new Error("boom"))).toBe("boom");
  });

  it("never translates raw server diagnostics or provider data", () => {
    const raw = { message: "deploy: container exploded", status: 500 };
    expect(describeApplicationError(raw)).toBe("container exploded");
    expect(describePreviewError(raw)).toBe("container exploded");
    expect(describeProviderError(raw)).toBe("container exploded");
    setLocale("vi", null);
    expect(describeApplicationError(raw)).toBe("container exploded");
    expect(describePreviewError(raw)).toBe("container exploded");
    expect(describeProviderError(raw)).toBe("container exploded");
  });

  it("classifies on raw status, not on translated text", () => {
    setLocale("vi", null);
    expect(describeApplicationError(apiError(503))).toBe(
      "Ứng dụng đang tắt trên control plane (FEATURE_APPLICATIONS=false).",
    );
    expect(describeApplicationError(apiError(502))).toContain("node agent");
  });
});

describe("wizard live switch", () => {
  it("keeps the draft while step labels follow the locale", async () => {
    setActivePinia(createPinia());
    let wiz: ReturnType<typeof useCreateAppWizard> | null = null;
    const Harness = defineComponent({
      setup() {
        wiz = useCreateAppWizard(ref(false), (() => undefined) as never);
        return () => h("div");
      },
    });
    const wrapper = mount({ render: () => h(NMessageProvider, null, { default: () => h(Harness) }) });
    const w = wiz!;
    w.form.name = "storefront";
    w.form.branch = "main";
    expect(w.stepNames.value[0]).toBe("Source");
    setLocale("vi", null);
    await wrapper.vm.$nextTick();
    expect(w.stepNames.value[0]).toBe("Nguồn");
    expect(w.form.name).toBe("storefront");
    expect(w.form.branch).toBe("main");
    expect(w.buildPacks.value[0].label).toBe("Tự nhận diện (khuyến nghị)");
    setLocale("en", null);
    await wrapper.vm.$nextTick();
    expect(w.stepNames.value[0]).toBe("Source");
    expect(w.form.name).toBe("storefront");
    wrapper.unmount();
  });
});

describe("visible domain feedback follows the locale", () => {
  it("refreshes an already-visible validation error without touching the draft", async () => {
    setActivePinia(createPinia());
    let domain: ReturnType<typeof useApplicationDomain> | null = null;
    const Harness = defineComponent({
      setup() {
        domain = useApplicationDomain(
          ref({ id: "app-1", base_domain: "app.example.com" }) as never,
        );
        return () => h("div");
      },
    });
    const wrapper = mount({ render: () => h(NMessageProvider, null, { default: () => h(Harness) }) });
    const d = domain!;
    d.baseDomain.value = "not a domain";
    await d.handleSaveDomain();
    expect(d.domainError.value).toBe(
      "Enter a plain hostname such as app.example.com (letters, digits, hyphens and dots; no wildcard).",
    );
    setLocale("vi", null);
    await wrapper.vm.$nextTick();
    expect(d.domainError.value).toBe(
      "Nhập hostname thuần như app.example.com (chữ cái, chữ số, gạch ngang và dấu chấm; không dùng ký tự đại diện).",
    );
    expect(d.baseDomain.value).toBe("not a domain");
    wrapper.unmount();
  });
});

describe("locale-independent display helpers", () => {  it("keeps duration text identical in both locales", () => {
    const deployment = {
      started_at: "2026-01-01T00:00:00Z",
      finished_at: "2026-01-01T00:02:05Z",
    } as Deployment;
    expect(durationText(deployment)).toBe("2m5s");
    setLocale("vi", null);
    expect(durationText(deployment)).toBe("2m5s");
  });
});
