// I18N-8 focused checks: domains, teams and notification channels.
//
// Proves catalog parity for the three feature namespaces, current-locale
// labels from the shared feature helpers, raw classifier/error semantics,
// and that a locale switch preserves drafts while refreshing retained
// feedback. Wire values (event keys, kinds, URLs, roles, tokens) stay raw.
import { mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick } from "vue";
import { flushPromises } from "@vue/test-utils";

vi.mock("@/features/domains/api/proxy", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/domains/api/proxy")>();
  return { ...actual, updateDNSProvider: vi.fn() };
});

import {
  certificateStatusLabel,
  describeProxyError,
  providerLabel,
  updateDNSProvider,
} from "@/features/domains/api/proxy";
import CertificateForm from "@/features/domains/components/CertificateForm.vue";
import domainsEn from "@/features/domains/locales/en";
import domainsVi from "@/features/domains/locales/vi";
import {
  describeChannelError,
  eventLabel,
  kindLabel,
} from "@/features/notifications/api/notifications";
import notificationsEn from "@/features/notifications/locales/en";
import notificationsVi from "@/features/notifications/locales/vi";
import {
  configSummary,
  scopeLabel,
} from "@/features/notifications/utils/channelHelpers";
import type { NotificationChannel } from "@/features/notifications/api/notifications";
import {
  canManageMembers,
  describeTeamError,
  isFeatureDisabled,
  meRoleLabel,
  roleLabel,
} from "@/features/teams/api/teams";
import teamsEn from "@/features/teams/locales/en";
import teamsVi from "@/features/teams/locales/vi";
import { checkCatalogParity } from "@/shared/i18n/catalog";
import {
  i18n,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";

function registerAll(): void {
  i18n.global.mergeLocaleMessage("en", {
    domains: domainsEn,
    teams: teamsEn,
    notifications: notificationsEn,
  });
  i18n.global.mergeLocaleMessage("vi", {
    domains: domainsVi,
    teams: teamsVi,
    notifications: notificationsVi,
  });
}

beforeEach(() => {
  resetLocaleState();
  registerAll();
  syncComposerLocale("en");
});

describe("I18N-8 catalog parity", () => {
  it("matches en/vi keys, params and message syntax for all three features", () => {
    expect(checkCatalogParity(domainsEn, domainsVi)).toEqual([]);
    expect(checkCatalogParity(teamsEn, teamsVi)).toEqual([]);
    expect(checkCatalogParity(notificationsEn, notificationsVi)).toEqual([]);
  });
});

describe("I18N-8 domain labels", () => {
  it("keeps provider proper nouns identical in both locales", () => {
    setLocale("vi", null);
    expect(providerLabel("cloudflare")).toBe("Cloudflare");
    expect(providerLabel("digitalocean")).toBe("DigitalOcean");
  });

  it("localizes certificate status words and keeps the unreported fallback", () => {
    expect(certificateStatusLabel("present")).toBe("present");
    expect(certificateStatusLabel(undefined)).toBe("not reported");
    setLocale("vi", null);
    expect(certificateStatusLabel("present")).toBe("hiện có");
    expect(certificateStatusLabel("absent")).toBe("không có chứng chỉ");
    expect(certificateStatusLabel("unknown")).toBe("không rõ");
    expect(certificateStatusLabel(undefined)).toBe("chưa báo cáo");
  });

  it("keeps raw diagnostics on unknown failures in both locales", () => {
    const raw = { message: "proxy: dial tcp 10.0.0.9:443: connect: refused", status: 500 };
    expect(describeProxyError(raw)).toBe(
      "dial tcp 10.0.0.9:443: connect: refused",
    );
    setLocale("vi", null);
    expect(describeProxyError(raw)).toBe(
      "dial tcp 10.0.0.9:443: connect: refused",
    );
  });

  it("localizes curated summaries while classifying on raw status", () => {
    expect(describeProxyError({ message: "x", status: 401 })).toBe(
      "Your session expired. Please sign in again.",
    );
    setLocale("vi", null);
    expect(describeProxyError({ message: "x", status: 401 })).toBe(
      "Phiên đăng nhập đã hết hạn. Vui lòng đăng nhập lại.",
    );
    expect(describeProxyError({ message: "proxy: conflict", status: 409 })).toBe(
      "conflict",
    );
  });
});

describe("I18N-8 team roles and invite classification", () => {
  it("reads role wording from the shared catalog in the current locale", () => {
    expect(roleLabel("owner")).toBe("owner");
    expect(roleLabel("read_only")).toBe("read-only");
    expect(meRoleLabel(null)).toBe("Team member");
    setLocale("vi", null);
    expect(roleLabel("owner")).toBe("chủ sở hữu");
    expect(roleLabel("admin")).toBe("quản trị viên");
    expect(roleLabel("read_only")).toBe("chỉ đọc");
    expect(meRoleLabel(null)).toBe("Thành viên nhóm");
    expect(meRoleLabel("admin")).toBe("quản trị viên");
  });

  it("keeps permission gates identical across locales", () => {
    for (const locale of ["en", "vi"] as const) {
      setLocale(locale, null);
      expect(canManageMembers("owner")).toBe(true);
      expect(canManageMembers("admin")).toBe(true);
      expect(canManageMembers("read_only")).toBe(false);
      expect(canManageMembers(null)).toBe(false);
    }
  });

  it("classifies invites on raw status and localizes the summary", () => {
    const expired = { message: "teams: invite expired", status: 410 };
    expect(describeTeamError(expired)).toBe("invite expired");
    const forbidden = { message: "teams: need owner", status: 403 };
    expect(describeTeamError(forbidden)).toBe("need owner");
    setLocale("vi", null);
    expect(describeTeamError({ message: "", status: 403 })).toBe(
      "Vai trò nhóm của bạn không cho phép hành động này.",
    );
    expect(describeTeamError({ message: "", status: 410 })).toBe(
      "Lời mời này đã hết hạn. Hãy tạo lời mời mới.",
    );
    expect(isFeatureDisabled({ message: "x", status: 404 })).toBe(true);
    expect(isFeatureDisabled({ message: "x", status: 403 })).toBe(false);
  });
});

describe("I18N-8 notification labels", () => {
  it("localizes kind and event words, never the wire keys", () => {
    expect(kindLabel("discord")).toBe("Discord webhook");
    expect(eventLabel("deploy_success")).toBe("Deploy succeeded");
    setLocale("vi", null);
    expect(kindLabel("discord")).toBe("Webhook Discord");
    expect(eventLabel("deploy_success")).toBe("Triển khai thành công");
    expect(eventLabel("backup_failure")).toBe("Sao lưu thất bại");
  });

  it("summarizes routing facts with verbatim technical values", () => {
    const email = {
      kind: "email",
      config: { host: "smtp.example.com", port: 587, to: ["a@x.io"] },
    } as NotificationChannel;
    expect(configSummary(email)).toBe("smtp.example.com:587 → a@x.io");
    setLocale("vi", null);
    expect(configSummary(email)).toBe("smtp.example.com:587 → a@x.io");
    const missing = { kind: "discord", config: {} } as NotificationChannel;
    expect(configSummary(missing)).toBe("chưa cấu hình webhook");
    expect(scopeLabel(missing, () => undefined)).toBe("Toàn nhóm");
    const scoped = {
      kind: "slack",
      resource_type: "application",
      resource_id: "app-1",
      config: {},
    } as NotificationChannel;
    expect(scopeLabel(scoped, () => "Shop")).toBe("Ứng dụng: Shop");
  });

  it("localizes channel summaries while keeping raw 400 diagnostics", () => {
    expect(
      describeChannelError({ message: "notifications: bad hook", status: 400 }),
    ).toBe("bad hook");
    setLocale("vi", null);
    expect(describeChannelError({ message: "", status: 400 })).toBe(
      "Cấu hình kênh không hợp lệ.",
    );
  });
});

describe("I18N-8 live switch preserves drafts", () => {
  it("keeps a typed certificate draft while labels switch", async () => {
    setActivePinia(createPinia());
    const draft = {
      application_id: "app-1",
      challenge: "dns-01",
      dns_provider_id: "prov-1",
      wildcard: true,
      enabled: true,
    };
    const wrapper = mount(CertificateForm, {
      props: {
        modelValue: draft,
        applications: [{ id: "app-1", name: "Shop", base_domain: "shop.example.com" }],
        providers: [],
      },
      global: { plugins: [i18n] },
    });
    await nextTick();
    expect(wrapper.text()).toContain("Challenge");
    expect(wrapper.text()).toContain("shop.example.com");

    setLocale("vi", null);
    await nextTick();
    await flushPromises();
    // Labels refresh, the recorded domain and selections stay verbatim.
    expect(wrapper.text()).toContain("Thử thách");
    expect(wrapper.text()).toContain("shop.example.com");
    const emitted = wrapper.emitted("update:modelValue");
    expect(emitted).toBeUndefined();
    wrapper.unmount();
  });

  it("rederives a retained dialog error in the current locale", async () => {
    setActivePinia(createPinia());
    const { provideProviders } = await import(
      "@/features/domains/composables/useProviders"
    );
    let state!: ReturnType<typeof provideProviders>;
    const Inner = defineComponent({
      setup() {
        state = provideProviders();
        return () => h("div");
      },
    });
    const wrapper = mount(
      defineComponent({
        render: () => h(NMessageProvider, null, { default: () => h(Inner) }),
      }),
      { global: { plugins: [i18n] } },
    );
    await flushPromises();

    state.openProviderCreate();
    state.providerForm.value.name = "Typed name";
    vi.mocked(updateDNSProvider).mockRejectedValueOnce({ message: "", status: 403 });
    // Edit path (existing provider) exercises the raw-error write.
    state.openProviderEdit({
      id: "prov-1",
      provider: "cloudflare",
      name: "Typed name",
      zones: [],
      enabled: true,
      credentials_set: true,
      created_at: "",
      updated_at: "",
    });
    await state.handleSaveProvider();
    expect(state.providerError.value).toBe(
      "You need the admin scope to manage domains and SSL. Sign in with " +
        "an admin account or use an admin API token.",
    );

    setLocale("vi", null);
    await nextTick();
    // No refetch, no draft loss: the retained alert rederives in Vietnamese.
    expect(state.providerError.value).toBe(
      "Bạn cần phạm vi admin để quản lý tên miền và SSL. Hãy đăng nhập " +
        "bằng tài khoản admin hoặc dùng token API admin.",
    );
    expect(state.providerForm.value.name).toBe("Typed name");
    expect(state.providerOpen.value).toBe(true);
    wrapper.unmount();
  });
});
