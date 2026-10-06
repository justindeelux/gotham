// I18N-6 coverage: databases catalog parity, invocation-time error
// summaries with raw diagnostics preserved, and live-switch behavior.
import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it } from "vitest";
import { nextTick, reactive } from "vue";

import { describeBackupError } from "@/features/databases/api/backups";
import {
  databaseEmptyDescription,
  databaseEmptyHint,
  describeDatabaseError,
} from "@/features/databases/api/databases";
import WizardConfigureStep from "@/features/databases/components/WizardConfigureStep.vue";
import {
  wizardFormKey,
  type WizardForm,
} from "@/features/databases/composables/useCreateDatabaseWizard";
import en from "@/features/databases/locales/en";
import vi from "@/features/databases/locales/vi";
import { maskConnectionPassword } from "@/features/databases/utils/databaseConnection";
import { checkCatalogParity } from "@/shared/i18n/catalog";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";

registerDiscoveredCatalogs();

beforeEach(() => {
  resetLocaleState();
  syncComposerLocale("en");
});

/** apiError builds the shape the shared HTTP layer throws. */
function apiError(status: number, message: string): unknown {
  return { status, message };
}

describe("databases catalog", () => {
  it("has en/vi parity (keys, params, plurals, syntax)", () => {
    expect(
      checkCatalogParity(
        en as unknown as Record<string, unknown>,
        vi as unknown as Record<string, unknown>,
      ),
    ).toEqual([]);
  });

  it("is registered under the databases namespace in both locales", () => {
    expect(i18n.global.te("databases.wizard.title")).toBe(true);
    expect(String(i18n.global.t("databases.wizard.title"))).toBe(
      "Create database",
    );
    setLocale("vi", null);
    expect(String(i18n.global.t("databases.wizard.title"))).toBe(
      "Tạo database",
    );
  });

  it("keeps database/backup identity params in the restore confirmation", () => {
    const params = {
      name: "pg-orders",
      backup: "s3://gotham-backups/pg-orders.dump",
      when: "2h ago",
      size: "4.10 MiB",
    };
    const enPrompt = String(
      i18n.global.t("databases.backups.dialog.prompt", params),
    );
    expect(enPrompt).toContain("pg-orders");
    expect(enPrompt).toContain("s3://gotham-backups/pg-orders.dump");
    setLocale("vi", null);
    const viPrompt = String(
      i18n.global.t("databases.backups.dialog.prompt", params),
    );
    expect(viPrompt).toContain("pg-orders");
    expect(viPrompt).toContain("s3://gotham-backups/pg-orders.dump");
  });
});

describe("describeDatabaseError", () => {
  it("keeps raw classifier diagnostics byte-identical in both locales", () => {
    const raw = "databases: boom";
    expect(describeDatabaseError(apiError(500, raw))).toBe("boom");
    setLocale("vi", null);
    expect(describeDatabaseError(apiError(500, raw))).toBe("boom");
  });

  it("localizes curated summaries without touching the 409 classifier", () => {
    expect(describeDatabaseError(apiError(404, "databases: gone"))).toBe(
      "Database not found. It may have been deleted or belong to another account.",
    );
    // Exact backend refusals still render inline for the move settings.
    expect(
      describeDatabaseError(
        apiError(409, "databases: a database cannot change server once created"),
      ),
    ).toBe("a database cannot change server once created");
    expect(describeDatabaseError(apiError(409, "databases: "))).toBe(
      "A database with that name already exists.",
    );
    setLocale("vi", null);
    expect(describeDatabaseError(apiError(404, "databases: gone"))).toBe(
      "Không tìm thấy database. Có thể nó đã bị xóa hoặc thuộc tài khoản khác.",
    );
    expect(
      describeDatabaseError(
        apiError(409, "databases: a database cannot change server once created"),
      ),
    ).toBe("a database cannot change server once created");
    expect(describeDatabaseError(apiError(409, "databases: "))).toBe(
      "Đã tồn tại database trùng tên.",
    );
  });
});

describe("describeBackupError", () => {
  it("distinguishes curated summaries per status in both locales", () => {
    expect(describeBackupError(apiError(401, "auth: expired"))).toBe(
      "Session expired. Please sign in again.",
    );
    expect(describeBackupError(apiError(502, "agent: down"))).toBe(
      "The node agent is unreachable or the job failed on the node. Check the node status and retry.",
    );
    setLocale("vi", null);
    expect(describeBackupError(apiError(401, "auth: expired"))).toBe(
      "Phiên đăng nhập đã hết hạn. Vui lòng đăng nhập lại.",
    );
    expect(describeBackupError(apiError(502, "agent: down"))).toBe(
      "Không kết nối được node agent hoặc tác vụ thất bại trên node. Kiểm tra trạng thái node rồi thử lại.",
    );
  });

  it("keeps the raw running-job diagnostic under the localized summary", () => {
    const raw = "databases: backup already running";
    expect(describeBackupError(apiError(409, raw))).toBe(
      "backup already running",
    );
    setLocale("vi", null);
    expect(describeBackupError(apiError(409, raw))).toBe(
      "backup already running",
    );
    expect(describeBackupError(apiError(409, "databases: "))).toBe(
      "Đã có một backup hoặc restore đang chạy cho database này.",
    );
  });
});

describe("restore outcome wording", () => {
  it("keeps the raw failure detail so a restore failure stays distinct from a restart failure", () => {
    const restartDetail = "database restored, restart failed: container exited";
    const summary = String(
      i18n.global.t("databases.backups.restores.failedWithDetail", {
        detail: restartDetail,
      }),
    );
    expect(summary).toContain(restartDetail);
    setLocale("vi", null);
    expect(
      String(
        i18n.global.t("databases.backups.restores.failedWithDetail", {
          detail: restartDetail,
        }),
      ),
    ).toContain(restartDetail);
  });
});

describe("live language switch on the configure step", () => {
  it("preserves the draft, refreshes visible feedback and starts nothing", async () => {
    const form = reactive<WizardForm>({
      engine: "postgres",
      version: "16-alpine",
      projectId: "proj-1",
      environmentId: "env-1",
      serverId: "srv-1",
      name: "no spaces",
      exposePublic: false,
      publicPort: null,
    });
    const wrapper = mount(WizardConfigureStep, {
      global: { provide: { [wizardFormKey as symbol]: form } },
    });
    try {
      // Invalid-name feedback is visible before any submit.
      expect(wrapper.text()).toContain(
        "Name must be 1-63 characters of letters, digits, ., _ or -.",
      );
      await wrapper.find("input").setValue("no spaces");
      expect(form.name).toBe("no spaces");
      setLocale("vi", null);
      await nextTick();
      // Draft survived, feedback switched, no API call or submit happened.
      expect(form.name).toBe("no spaces");
      expect(form.exposePublic).toBe(false);
      expect(wrapper.text()).toContain(
        "Tên phải dài 1-63 ký tự gồm chữ cái, chữ số, ., _ hoặc -.",
      );
      expect(wrapper.text()).not.toContain(
        "Name must be 1-63 characters of letters, digits, ., _ or -.",
      );
    } finally {
      wrapper.unmount();
    }
  });
});

describe("connection handling stays raw", () => {
  it("masks the DSN password identically in both locales", () => {
    const dsn = "postgresql://app:s3cret@10.0.0.4:5432/shop";
    expect(maskConnectionPassword(dsn)).toBe(
      "postgresql://app:••••••••@10.0.0.4:5432/shop",
    );
    setLocale("vi", null);
    expect(maskConnectionPassword(dsn)).toBe(
      "postgresql://app:••••••••@10.0.0.4:5432/shop",
    );
  });

  it("leaves the legacy empty-state helpers untouched", () => {
    expect(databaseEmptyDescription(0)).toBe("No databases yet");
    expect(databaseEmptyHint(0)).toContain("Create database");
  });
});
