// I18N-7 (JUS-48) focused checks: services/templates catalog parity plus
// the live language-switch behavior the package promises — schema-owned
// validation feedback switches without accepting invalid values, template
// IDs/keys/compose/credentials/payloads stay unchanged, missing overlay
// entries fall back to provider metadata, and wizard/import drafts survive
// the switch.
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { defineComponent, h, ref } from "vue";
import type { Ref } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  deployStateLabel,
  describeServiceError,
  serviceStatusLabel,
} from "@/features/services/api/services";
import { useImportService } from "@/features/services/composables/useImportService";
import TemplateGallery from "@/features/templates/components/TemplateGallery.vue";
import {
  checkTemplateValue,
  describeTemplateError,
  templateOverlayDescription,
  templateOverlayFieldHelp,
  validateTemplateValues,
} from "@/features/templates/api/templates";
import type {
  TemplateField,
  TemplateSummary,
} from "@/features/templates/api/templates";
import { useTemplateWizard } from "@/features/templates/composables/useTemplateWizard";
import servicesEn from "@/features/services/locales/en";
import servicesVi from "@/features/services/locales/vi";
import templatesEn from "@/features/templates/locales/en";
import templatesVi from "@/features/templates/locales/vi";
import { checkCatalogParity } from "@/shared/i18n/catalog";
import {
  i18n,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";
import { useTemplatesStore } from "@/features/templates/stores/templates";
import { useServersStore } from "@/features/servers/stores/servers";

beforeEach(() => {
  i18n.global.mergeLocaleMessage("en", { services: servicesEn });
  i18n.global.mergeLocaleMessage("vi", { services: servicesVi });
  i18n.global.mergeLocaleMessage("en", { templates: templatesEn });
  i18n.global.mergeLocaleMessage("vi", { templates: templatesVi });
  resetLocaleState();
  syncComposerLocale("en");
  setActivePinia(createPinia());
});

afterEach(() => {
  setLocale("en", null);
});

describe("services/templates catalog parity", () => {
  it("ships equal en/vi leaf keys, params and compiler-clean syntax", () => {
    expect(checkCatalogParity(servicesEn, servicesVi)).toEqual([]);
    expect(checkCatalogParity(templatesEn, templatesVi)).toEqual([]);
  });
});

describe("service errors keep raw diagnostics", () => {
  it("passes actionable server text through in both locales", () => {
    const refusal = {
      status: 409,
      message: "services: a deploy is in progress",
      cause: null,
    };
    expect(describeServiceError(refusal)).toBe("a deploy is in progress");
    setLocale("vi", null);
    // Status-classified raw refusals pair a minimal localized frame with the
    // intact raw detail; refusal distinctions survive the frame.
    expect(describeServiceError(refusal)).toBe("Xung đột: a deploy is in progress");
    // The heading localizes; the raw detail stays byte-identical.
    expect(
      describeServiceError({ status: 502, message: "services: down", cause: null }),
    ).toBe("Lỗi node agent: down");
    setLocale("en", null);
    expect(
      describeServiceError({ status: 502, message: "services: down", cause: null }),
    ).toBe("Node agent error: down");
  });

  it("translates curated fallbacks while English stays byte-identical", () => {
    expect(describeServiceError({ status: 401, message: "", cause: null })).toBe(
      "Your session expired. Please sign in again.",
    );
    expect(describeServiceError(null)).toBe(
      "Something went wrong. Please try again.",
    );
    expect(
      describeServiceError({ status: 503, message: "", cause: null }),
    ).toBe("Services are disabled on the control plane (FEATURE_SERVICES=false).");
    setLocale("vi", null);
    expect(describeServiceError({ status: 401, message: "", cause: null })).toBe(
      "Phiên đăng nhập đã hết hạn. Vui lòng đăng nhập lại.",
    );
    expect(describeServiceError(null)).toBe("Đã xảy ra lỗi. Vui lòng thử lại.");
    // The UI frame translates; the FEATURE_SERVICES=false token is preserved.
    expect(describeServiceError({ status: 503, message: "", cause: null })).toBe(
      "Dịch vụ bị tắt trên control plane (FEATURE_SERVICES=false).",
    );
    expect(
      describeTemplateError({ status: 503, message: "", cause: null }),
    ).toBe("Dịch vụ bị tắt trên control plane (FEATURE_SERVICES=false).");
  });

  it("frames status-classified raw refusals without touching the EN baseline", () => {
    const badRequest = { status: 400, message: "services: compose invalid", cause: null };
    const missing = { status: 404, message: "services: gone", cause: null };
    expect(describeServiceError(badRequest)).toBe("compose invalid");
    expect(describeServiceError(missing)).toBe("gone");
    expect(
      describeTemplateError({ status: 404, message: "templates: gone", cause: null }),
    ).toBe("gone");
    setLocale("vi", null);
    expect(describeServiceError(badRequest)).toBe("Yêu cầu không hợp lệ: compose invalid");
    expect(describeServiceError(missing)).toBe("Không tìm thấy: gone");
    expect(
      describeTemplateError({ status: 404, message: "templates: gone", cause: null }),
    ).toBe("Không tìm thấy: gone");
    // Generic and unknown diagnostics keep their pinned raw passthrough.
    expect(describeServiceError({ status: 500, message: "services: down", cause: null })).toBe(
      "down",
    );
    expect(describeTemplateError({ status: 500, message: "templates: down", cause: null })).toBe(
      "down",
    );
  });

  it("pairs a localized 502 heading with the byte-identical raw detail", () => {
    const failure = { status: 502, message: "services: down", cause: null };
    expect(describeServiceError(failure)).toBe("Node agent error: down");
    setLocale("vi", null);
    expect(describeServiceError(failure)).toBe("Lỗi node agent: down");
    // Unknown diagnostics keep their raw text under the localized heading.
    const unknown = { status: 502, message: "services: dial 10.0.0.9:2375", cause: null };
    expect(describeServiceError(unknown)).toBe("Lỗi node agent: dial 10.0.0.9:2375");
  });

  it("renders status labels in both locales without touching wire values", () => {
    expect(serviceStatusLabel("running")).toBe("running");
    expect(deployStateLabel("failed")).toBe("failed");
    setLocale("vi", null);
    expect(serviceStatusLabel("running")).toBe("đang chạy");
    expect(deployStateLabel("failed")).toBe("thất bại");
  });

  it("translates template fallbacks while passing raw text through", () => {
    expect(
      describeTemplateError({ status: 400, message: "templates: bad values", cause: null }),
    ).toBe("bad values");
    expect(describeTemplateError({ status: 400, message: "", cause: null })).toBe(
      "Invalid template values. Check the highlighted fields.",
    );
    setLocale("vi", null);
    expect(
      describeTemplateError({ status: 400, message: "templates: bad values", cause: null }),
    ).toBe("Giá trị không hợp lệ: bad values");
    expect(describeTemplateError({ status: 400, message: "", cause: null })).toBe(
      "Giá trị mẫu không hợp lệ. Kiểm tra các trường được đánh dấu.",
    );
  });
});

describe("template validation switches without accepting invalid values", () => {
  const count: TemplateField = {
    key: "count",
    label: "Count",
    type: "number",
    required: true,
    min: 1,
    max: 10,
  };
  const mode: TemplateField = {
    key: "mode",
    label: "Mode",
    type: "select",
    required: true,
    options: ["slow", "fast"],
  };

  it("re-renders required/range/option feedback in Vietnamese", () => {
    expect(checkTemplateValue(count, "")).toBe("This field is required.");
    expect(checkTemplateValue(count, "abc")).toBe("Must be a whole number.");
    expect(checkTemplateValue(count, "99")).toBe("Must be at most 10.");
    expect(checkTemplateValue(mode, "turbo")).toBe("Must be one of: slow, fast.");
    setLocale("vi", null);
    expect(checkTemplateValue(count, "")).toBe("Trường này là bắt buộc.");
    expect(checkTemplateValue(count, "abc")).toBe("Phải là số nguyên.");
    expect(checkTemplateValue(count, "99")).toBe("Tối đa 10.");
    expect(checkTemplateValue(mode, "turbo")).toBe("Phải là một trong: slow, fast.");
    // Valid values stay valid across the switch.
    expect(checkTemplateValue(count, "7")).toBeNull();
    expect(checkTemplateValue(mode, "slow")).toBeNull();
    expect(
      validateTemplateValues([count, mode], { count: "", mode: "turbo" }),
    ).toEqual({ count: "Trường này là bắt buộc.", mode: "Phải là một trong: slow, fast." });
  });
});

describe("template overlay falls back to provider metadata", () => {
  const bundled: TemplateSummary = {
    slug: "n8n",
    name: "n8n",
    icon: "n8n",
    description: "n8n workflow automation with a persistent named volume and a routed domain.",
  };
  const foreign: TemplateSummary = {
    slug: "acme-custom",
    name: "Acme custom",
    icon: "box",
    description: "Operator-provided template copy stays untouched.",
  };

  it("translates bundled descriptions and keeps unknown slugs on provider copy", () => {
    expect(templateOverlayDescription(bundled)).toBe(bundled.description);
    expect(templateOverlayDescription(foreign)).toBe(foreign.description);
    setLocale("vi", null);
    expect(templateOverlayDescription(bundled)).toBe(
      "Tự động hóa quy trình n8n với volume có tên bền vững và tên miền được định tuyến.",
    );
    expect(templateOverlayDescription(foreign)).toBe(foreign.description);
  });

  it("translates curated field help and falls back per field", () => {
    const domain: TemplateField = {
      key: "domain",
      label: "Domain",
      type: "text",
      required: true,
      help: "The public host served by the node's Traefik proxy.",
    };
    const custom: TemplateField = {
      key: "custom_flag",
      label: "Custom flag",
      type: "text",
      required: false,
      help: "Operator help stays untouched.",
    };
    expect(templateOverlayFieldHelp("n8n", domain)).toBe(domain.help);
    expect(templateOverlayFieldHelp("n8n", custom)).toBe(custom.help);
    expect(templateOverlayFieldHelp("acme-custom", domain)).toBe(domain.help);
    setLocale("vi", null);
    expect(templateOverlayFieldHelp("n8n", domain)).toBe(
      "Host công khai do Traefik proxy của node phục vụ.",
    );
    expect(templateOverlayFieldHelp("n8n", custom)).toBe(custom.help);
    expect(templateOverlayFieldHelp("acme-custom", domain)).toBe(domain.help);
  });

  it("renders gallery descriptions from the overlay with provider fallback", () => {
    const wrapper = mount(TemplateGallery, {
      props: { templates: [bundled, foreign], loading: false, error: null },
    });
    const cards = wrapper.findAll(".tpl-card");
    expect(cards[0].find(".tpl-desc").text()).toBe(bundled.description);
    expect(cards[1].find(".tpl-desc").text()).toBe(foreign.description);
    // Template IDs, names and icon markers are never translated.
    expect(cards[0].attributes("data-template")).toBe("n8n");
    expect(cards[0].find(".tpl-name").text()).toBe("n8n");
    setLocale("vi", null);
    return wrapper.vm.$nextTick().then(() => {
      const updated = wrapper.findAll(".tpl-card");
      expect(updated[0].find(".tpl-desc").text()).toBe(
        "Tự động hóa quy trình n8n với volume có tên bền vững và tên miền được định tuyến.",
      );
      expect(updated[1].find(".tpl-desc").text()).toBe(foreign.description);
      expect(updated[0].attributes("data-template")).toBe("n8n");
    });
  });
});

describe("wizard draft and deploy state survive the language change", () => {
  const showRef: Ref<boolean> = ref(false);
  const slugRef: Ref<string> = ref("n8n");
  let wizard: ReturnType<typeof useTemplateWizard> | null = null;

  const Harness = defineComponent({
    setup() {
      wizard = useTemplateWizard(showRef, slugRef);
      return () => h("div");
    },
  });

  const detail = {
    slug: "n8n",
    name: "n8n",
    icon: "n8n",
    description: "n8n workflow automation.",
    fields: [
      { key: "domain", label: "Domain", type: "text", required: true },
      { key: "encryption_key", label: "Encryption key", type: "secret", required: true },
    ],
  };

  let wrapper: ReturnType<typeof mount> | null = null;

  async function openWizard(): Promise<void> {
    wrapper?.unmount();
    wrapper = null;
    showRef.value = false;
    slugRef.value = "n8n";
    wizard = null;
    const templates = useTemplatesStore();
    vi.spyOn(templates, "fetchDetail").mockResolvedValue(
      globalThis.structuredClone(detail) as never,
    );
    const servers = useServersStore();
    vi.spyOn(servers, "fetchServers").mockResolvedValue(undefined);
    wrapper = mount({
      render: () => h(NMessageProvider, null, { default: () => h(Harness) }),
    });
    showRef.value = true;
    await flushPromises();
  }

  it("keeps values, step and payloads while feedback switches", async () => {
    await openWizard();
    if (wizard) {
      wizard.values.value = { domain: "", encryption_key: "s3cret" };
    }
    wizard?.next();
    expect(wizard?.step.value).toBe(1);
    expect(wizard?.formErrors.value).toEqual({ domain: "This field is required." });
    setLocale("vi", null);
    // Draft, step and gating survive; visible feedback switches.
    expect(wizard?.values.value).toEqual({ domain: "", encryption_key: "s3cret" });
    expect(wizard?.step.value).toBe(1);
    expect(wizard?.formErrors.value).toEqual({ domain: "Trường này là bắt buộc." });
    // Pristine untouched fields stay quiet: only the revealed step-1 errors show.
    expect(Object.keys(wizard?.formErrors.value ?? {})).toEqual(["domain"]);
    setLocale("en", null);
    expect(wizard?.formErrors.value).toEqual({ domain: "This field is required." });
  });

  it("keeps an unsubmitted wizard pristine across the switch", async () => {
    await openWizard();
    if (wizard) {
      wizard.values.value = { domain: "", encryption_key: "" };
    }
    // Invalid but never submitted: no feedback in either locale.
    expect(wizard?.formErrors.value).toEqual({});
    setLocale("vi", null);
    expect(wizard?.formErrors.value).toEqual({});
    expect(wizard?.step.value).toBe(1);
    expect(wizard?.values.value).toEqual({ domain: "", encryption_key: "" });
  });

  it("re-derives retained wizard failures in the current locale", async () => {
    await openWizard();
    const templates = useTemplatesStore();
    vi.spyOn(templates, "fetchDetail").mockRejectedValueOnce({
      status: 404,
      message: "",
      cause: null,
    });
    showRef.value = false;
    await flushPromises();
    showRef.value = true;
    await flushPromises();
    expect(wizard?.detailError.value).toBe(
      "Template not found. The catalog may have changed — reload the page.",
    );
    setLocale("vi", null);
    expect(wizard?.detailError.value).toBe(
      "Không tìm thấy mẫu. Danh mục có thể đã đổi — tải lại trang.",
    );
  });
});

describe("import dialog draft survives the language change", () => {
  const showRef: Ref<boolean> = ref(false);
  let dialog: ReturnType<typeof useImportService> | null = null;

  const Harness = defineComponent({
    setup() {
      dialog = useImportService(showRef);
      return () => h("div");
    },
  });

  it("keeps typed values while scope feedback switches", async () => {
    const wrapper = mount({
      render: () => h(NMessageProvider, null, { default: () => h(Harness) }),
    });
    showRef.value = true;
    await flushPromises();
    if (dialog) {
      dialog.name.value = "blog-staging";
      dialog.yaml.value = "services:\n  web: {}";
    }
    await dialog?.handleImport();
    // No environment scope: the submit gate holds and the scope alert shows.
    expect(dialog?.scopeError.value).toBe("Select a project and environment.");
    setLocale("vi", null);
    expect(dialog?.name.value).toBe("blog-staging");
    expect(dialog?.yaml.value).toBe("services:\n  web: {}");
    expect(dialog?.scopeError.value).toBe("Chọn dự án và môi trường.");
    wrapper.unmount();
    showRef.value = false;
  });

  it("switches name/node feedback without changing the shared gating", async () => {
    const wrapper = mount({
      render: () => h(NMessageProvider, null, { default: () => h(Harness) }),
    });
    showRef.value = true;
    await flushPromises();
    if (dialog) {
      dialog.name.value = "  ";
      dialog.serverId.value = "";
    }
    await dialog?.handleImport();
    expect(dialog?.nameError.value).toBe("Enter a service name.");
    expect(dialog?.nodeError.value).toBe("Select a node.");
    setLocale("vi", null);
    expect(dialog?.name.value).toBe("  ");
    expect(dialog?.nameError.value).toBe("Nhập tên dịch vụ.");
    expect(dialog?.nodeError.value).toBe("Chọn một node.");
    wrapper.unmount();
    showRef.value = false;
  });

  it("keeps pre-submit forms pristine across the switch and clears on reset", async () => {
    const wrapper = mount({
      render: () => h(NMessageProvider, null, { default: () => h(Harness) }),
    });
    showRef.value = true;
    await flushPromises();
    // Pre-submit: invalid values entered but never submitted stay quiet,
    // in both locales.
    if (dialog) {
      dialog.name.value = "  ";
      dialog.serverId.value = "";
    }
    expect(dialog?.nameError.value).toBe("");
    expect(dialog?.nodeError.value).toBe("");
    expect(dialog?.scopeError.value).toBe("");
    setLocale("vi", null);
    expect(dialog?.nameError.value).toBe("");
    expect(dialog?.nodeError.value).toBe("");
    expect(dialog?.scopeError.value).toBe("");
    // After an attempt the feedback shows; reset restores pristine quiet.
    await dialog?.handleImport();
    expect(dialog?.nameError.value).toBe("Nhập tên dịch vụ.");
    dialog?.reset();
    expect(dialog?.nameError.value).toBe("");
    expect(dialog?.nodeError.value).toBe("");
    expect(dialog?.scopeError.value).toBe("");
    expect(dialog?.name.value).toBe("");
    wrapper.unmount();
    showRef.value = false;
  });
});
