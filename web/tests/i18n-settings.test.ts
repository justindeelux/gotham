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

vi.mock("@/features/notifications/api/notifications", async (importOriginal) => {
  const actual =
    await importOriginal<
      typeof import("@/features/notifications/api/notifications")
    >();
  return { ...actual, testChannel: vi.fn() };
});

vi.mock("@/features/teams/api/teams", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/teams/api/teams")>();
  return {
    ...actual,
    listTeams: vi.fn(),
    listMembers: vi.fn(),
    listInvites: vi.fn(),
    renameTeam: vi.fn(),
  };
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
  testChannel,
} from "@/features/notifications/api/notifications";
import ChannelCard from "@/features/notifications/components/ChannelCard.vue";
import { provideChannelDialog } from "@/features/notifications/composables/useChannelDialog";
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
  listInvites,
  listMembers,
  listTeams,
  meRoleLabel,
  renameTeam,
  roleLabel,
} from "@/features/teams/api/teams";
import { useTeamsPage } from "@/features/teams/composables/useTeamsPage";
import teamsEn from "@/features/teams/locales/en";
import teamsVi from "@/features/teams/locales/vi";
import { useTeamsStore } from "@/features/teams";
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

  it("pairs unknown failures with a localized summary plus the raw diagnostic", () => {
    const raw = { message: "proxy: dial tcp 10.0.0.9:443: connect: refused", status: 500 };
    expect(describeProxyError(raw)).toBe(
      "Request failed: dial tcp 10.0.0.9:443: connect: refused",
    );
    setLocale("vi", null);
    expect(describeProxyError(raw)).toBe(
      "Yêu cầu thất bại: dial tcp 10.0.0.9:443: connect: refused",
    );
    expect(describeProxyError({ message: "", status: 500 })).toBe(
      "Yêu cầu thất bại",
    );
    expect(describeProxyError(new Error("proxy: boom"))).toBe(
      "Đã xảy ra lỗi. Vui lòng thử lại: boom",
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

  it("renders unknown future roles verbatim and null roles neutral", () => {
    const billing = "billing" as unknown as "owner";
    expect(roleLabel(billing)).toBe("billing");
    expect(meRoleLabel(billing)).toBe("billing");
    expect(roleLabel(null as unknown as "owner")).toBe("Team member");
    expect(roleLabel(undefined as unknown as "owner")).toBe("Team member");
    setLocale("vi", null);
    expect(roleLabel(billing)).toBe("billing");
    expect(meRoleLabel(billing)).toBe("billing");
    expect(roleLabel(null as unknown as "owner")).toBe("Thành viên nhóm");
    expect(roleLabel(undefined as unknown as "owner")).toBe("Thành viên nhóm");
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
    // Unknown failures pair the localized summary with the raw diagnostic.
    expect(describeTeamError({ message: "teams: boom", status: 500 })).toBe(
      "Request failed: boom",
    );
    setLocale("vi", null);
    expect(describeTeamError({ message: "", status: 403 })).toBe(
      "Vai trò nhóm của bạn không cho phép hành động này.",
    );
    expect(describeTeamError({ message: "", status: 410 })).toBe(
      "Lời mời này đã hết hạn. Hãy tạo lời mời mới.",
    );
    expect(describeTeamError({ message: "teams: boom", status: 500 })).toBe(
      "Yêu cầu thất bại: boom",
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
    expect(
      describeChannelError({ message: "notifications: down", status: 500 }),
    ).toBe("Request failed: down");
    setLocale("vi", null);
    expect(describeChannelError({ message: "", status: 400 })).toBe(
      "Cấu hình kênh không hợp lệ.",
    );
    expect(
      describeChannelError({ message: "notifications: down", status: 500 }),
    ).toBe("Yêu cầu thất bại: down");
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

describe("I18N-8 retained channel test failure", () => {
  const channel = {
    id: "ch-1",
    team_id: "team-1",
    name: "Deploy alerts",
    kind: "discord",
    enabled: true,
    events: ["deploy_success"],
    config: { webhook_url: "https://…/••••" },
    secrets_configured: true,
    created_at: "",
    updated_at: "",
  } as NotificationChannel;

  const ownerTeam = {
    id: "team-1",
    name: "Acme",
    is_personal: false,
    role: "owner",
    created_at: "",
    updated_at: "",
  };

  /** cardHarness mounts a real ChannelCard with dialog state provided. */
  async function cardHarness() {
    setActivePinia(createPinia());
    const teams = useTeamsStore();
    teams.teams = [ownerTeam] as never;
    teams.activeTeamId = "team-1";
    const Inner = defineComponent({
      setup() {
        provideChannelDialog();
        return () => h(ChannelCard, { channel });
      },
    });
    const wrapper = mount(
      defineComponent({
        render: () => h(NMessageProvider, null, { default: () => h(Inner) }),
      }),
      { global: { plugins: [i18n] } },
    );
    await flushPromises();
    return wrapper;
  }

  async function clickSend(wrapper: ReturnType<typeof mount>) {
    const buttons = wrapper.findAll("button");
    const target = buttons.find((button) => button.text() === "Send test");
    expect(target, "expected a Send test button").toBeDefined();
    await target!.trigger("click");
    await flushPromises();
    await nextTick();
  }

  const resultText = (wrapper: ReturnType<typeof mount>): string =>
    wrapper.find("[data-test-channel-result]").text();

  it("rederives empty 403/404 failures with exactly one POST each, no resend on switch", async () => {
    const wrapper = await cardHarness();
    expect(wrapper.text()).toContain("No test sent yet.");

    vi.mocked(testChannel).mockRejectedValueOnce({ message: "", status: 403 });
    await clickSend(wrapper);
    expect(vi.mocked(testChannel)).toHaveBeenCalledTimes(1);
    expect(resultText(wrapper)).toBe(
      "Test failed: Your team role does not allow this action.",
    );

    // The switch sends nothing: the retained failure rederives in place.
    setLocale("vi", null);
    await nextTick();
    expect(resultText(wrapper)).toBe(
      "Gửi thử thất bại: Vai trò nhóm của bạn không cho phép hành động này.",
    );
    expect(vi.mocked(testChannel)).toHaveBeenCalledTimes(1);
    setLocale("en", null);
    await nextTick();
    expect(resultText(wrapper)).toBe(
      "Test failed: Your team role does not allow this action.",
    );
    expect(vi.mocked(testChannel)).toHaveBeenCalledTimes(1);

    vi.mocked(testChannel).mockRejectedValueOnce({ message: "", status: 404 });
    await clickSend(wrapper);
    expect(vi.mocked(testChannel)).toHaveBeenCalledTimes(2);
    expect(resultText(wrapper)).toBe(
      "Test failed: Notification channels are not enabled on this control " +
        "plane (FEATURE_NOTIFICATIONS=false).",
    );
    setLocale("vi", null);
    await nextTick();
    expect(resultText(wrapper)).toBe(
      "Gửi thử thất bại: Kênh thông báo chưa được bật trên control plane " +
        "này (FEATURE_NOTIFICATIONS=false).",
    );
    expect(vi.mocked(testChannel)).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });
});

describe("I18N-8 channel draft preservation", () => {
  it("keeps typed name/kind/scope/secrets across a switch with no API call", async () => {
    setActivePinia(createPinia());
    let state!: ReturnType<typeof provideChannelDialog>;
    const Inner = defineComponent({
      setup() {
        state = provideChannelDialog();
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

    state.openCreate();
    state.form.value.name = "Typed channel";
    state.form.value.kind = "discord";
    state.form.value.webhook_url = "https://typed/hook";
    state.selectScope("application");
    state.form.value.resourceId = "app-9";
    expect(state.canSubmit.value).toBe(true);

    setLocale("vi", null);
    await nextTick();
    // Draft, scope pick, secrets and gating survive; labels refresh.
    expect(state.form.value.name).toBe("Typed channel");
    expect(state.form.value.kind).toBe("discord");
    expect(state.form.value.webhook_url).toBe("https://typed/hook");
    expect(state.form.value.resourceType).toBe("application");
    expect(state.form.value.resourceId).toBe("app-9");
    expect(state.formOpen.value).toBe(true);
    expect(state.canSubmit.value).toBe(true);
    expect(state.scopeOptions.value[1]!.label).toBe("Ứng dụng");
    wrapper.unmount();
  });
});

describe("I18N-8 retained team 409", () => {
  it("keeps the distinct raw refusal verbatim in both locales", async () => {
    setActivePinia(createPinia());
    const teams = useTeamsStore();
    const ownerTeam = {
      id: "team-1",
      name: "Acme",
      is_personal: false,
      role: "owner",
      created_at: "",
      updated_at: "",
    };
    vi.mocked(listTeams).mockResolvedValue([ownerTeam] as never);
    vi.mocked(listMembers).mockResolvedValue([]);
    vi.mocked(listInvites).mockResolvedValue([]);
    let state!: ReturnType<typeof useTeamsPage>;
    const Inner = defineComponent({
      setup() {
        state = useTeamsPage();
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
    expect(teams.activeTeamId).toBe("team-1");

    state.openRename();
    state.renameName.value = "Taken";
    vi.mocked(renameTeam).mockRejectedValueOnce({
      message: "teams: name taken",
      status: 409,
    });
    await state.handleRename();
    expect(vi.mocked(renameTeam)).toHaveBeenCalledTimes(1);
    // A known refusal keeps its raw actionable text in both locales.
    expect(state.renameError.value).toBe("name taken");
    setLocale("vi", null);
    await nextTick();
    expect(state.renameError.value).toBe("name taken");
    expect(state.renameName.value).toBe("Taken");
    expect(state.renameOpen.value).toBe(true);
    wrapper.unmount();
  });
});

describe("I18N-8 library plural counts", () => {
  const enRuleOne =
    "1 rule · 1 enabled. GET answers the stored code; other methods " +
    "answer 308/307 so they keep their method.";
  const enRuleMany =
    "3 rules · 2 enabled. GET answers the stored code; other methods " +
    "answer 308/307 so they keep their method.";
  const viRuleOne =
    "1 quy tắc · 1 đang bật. GET trả lời mã đã lưu; các phương thức khác " +
    "trả lời 308/307 để giữ phương thức.";
  const viRuleMany =
    "3 quy tắc · 2 đang bật. GET trả lời mã đã lưu; các phương thức khác " +
    "trả lời 308/307 để giữ phương thức.";

  it("renders 0/1/many team counts exactly in English", () => {
    const t = i18n.global.t;
    expect(t("teams.list.count", { count: 0 }, { plural: 0 })).toBe("0 teams");
    expect(t("teams.list.count", { count: 1 }, { plural: 1 })).toBe("1 team");
    expect(t("teams.list.count", { count: 5 }, { plural: 5 })).toBe("5 teams");
  });

  it("renders meaningful Vietnamese team counts", () => {
    setLocale("vi", null);
    const t = i18n.global.t;
    expect(t("teams.list.count", { count: 0 }, { plural: 0 })).toBe("0 nhóm");
    expect(t("teams.list.count", { count: 1 }, { plural: 1 })).toBe("1 nhóm");
    expect(t("teams.list.count", { count: 5 }, { plural: 5 })).toBe("5 nhóm");
  });

  it("renders redirect summaries with named total/enabled in both locales", () => {
    const t = i18n.global.t;
    expect(
      t("domains.redirects.rulesSummary", { total: 1, enabled: 1 }, { plural: 1 }),
    ).toBe(enRuleOne);
    expect(
      t("domains.redirects.rulesSummary", { total: 3, enabled: 2 }, { plural: 3 }),
    ).toBe(enRuleMany);
    setLocale("vi", null);
    expect(
      t("domains.redirects.rulesSummary", { total: 1, enabled: 1 }, { plural: 1 }),
    ).toBe(viRuleOne);
    expect(
      t("domains.redirects.rulesSummary", { total: 3, enabled: 2 }, { plural: 3 }),
    ).toBe(viRuleMany);
  });
});
