import { useMessage } from "naive-ui";
import { computed, onMounted, onUnmounted, ref, watch, type Ref } from "vue";

import {
  describeApplicationError,
  getDomainMonitoring,
  testDomainMonitoring,
  updateDomainMonitoring,
  type Application,
  type ApplicationDomain,
  type WebsiteCheck,
  type WebsiteCheckInput,
  type WebsiteCheckTestResult,
} from "@/features/applications/api/applications";
import { websiteCheckSchema } from "@/features/applications/schemas/websiteCheck";
import { activeLocale, i18n } from "@/shared/i18n";
import { fieldErrors } from "@/shared/validation/naiveAdapter";

export function useWebsiteChecks(
  application: Ref<Application>,
  domains: Ref<ApplicationDomain[]>,
) {
  const message = useMessage();
  const checks = ref<Record<string, WebsiteCheck>>({});
  const loading = ref(false);
  const error = ref<string | null>(null);
  const busyDomainId = ref<string | null>(null);
  const testResults = ref<Record<string, WebsiteCheckTestResult>>({});
  const testingDomainId = ref<string | null>(null);
  let timer: ReturnType<typeof setInterval> | null = null;

  function tr(key: string): string {
    void activeLocale.value;
    return String(i18n.global.t(key));
  }

  const domainIDs = computed<string[]>(() =>
    domains.value.filter((row) => !row.disabled).map((row) => row.id),
  );

  async function refresh(): Promise<void> {
    const appID = application.value.id;
    const ids = domainIDs.value;
    if (ids.length === 0) {
      checks.value = {};
      error.value = null;
      return;
    }
    loading.value = Object.keys(checks.value).length === 0;
    try {
      const results = await Promise.all(
        ids.map((id) => getDomainMonitoring(appID, id)),
      );
      const next: Record<string, WebsiteCheck> = {};
      for (const check of results) {
        next[check.domain_id] = check;
      }
      checks.value = next;
      error.value = null;
    } catch (errorValue) {
      error.value = describeApplicationError(errorValue);
    } finally {
      loading.value = false;
    }
  }

  function validate(input: WebsiteCheckInput): string | null {
    const issues = fieldErrors(websiteCheckSchema, input);
    return issues[0] ?? null;
  }

  async function save(
    domainID: string,
    input: WebsiteCheckInput,
  ): Promise<boolean> {
    const issue = validate(input);
    if (issue !== null) {
      error.value = issue;
      return false;
    }
    busyDomainId.value = domainID;
    error.value = null;
    try {
      const check = await updateDomainMonitoring(
        application.value.id,
        domainID,
        input,
      );
      checks.value = { ...checks.value, [check.domain_id]: check };
      message.success(tr("applications.websiteCheck.saved"));
      return true;
    } catch (errorValue) {
      error.value = describeApplicationError(errorValue);
      return false;
    } finally {
      busyDomainId.value = null;
    }
  }

  async function test(domainID: string): Promise<void> {
    testingDomainId.value = domainID;
    error.value = null;
    try {
      const result = await testDomainMonitoring(application.value.id, domainID);
      testResults.value = { ...testResults.value, [domainID]: result };
    } catch (errorValue) {
      error.value = describeApplicationError(errorValue);
    } finally {
      testingDomainId.value = null;
    }
  }

  function poll(): void {
    if (typeof document !== "undefined" && document.visibilityState === "hidden") {
      return;
    }
    void refresh();
  }

  function onVisibility(): void {
    if (document.visibilityState === "visible") {
      void refresh();
    }
  }

  watch(
    () => application.value.id,
    () => {
      checks.value = {};
      testResults.value = {};
      void refresh();
    },
  );

  watch(domainIDs, () => {
    void refresh();
  });

  onMounted(() => {
    void refresh();
    timer = setInterval(poll, 60_000);
    document.addEventListener("visibilitychange", onVisibility);
  });

  onUnmounted(() => {
    if (timer !== null) {
      clearInterval(timer);
    }
    document.removeEventListener("visibilitychange", onVisibility);
  });

  return {
    checks,
    loading,
    error,
    busyDomainId,
    testResults,
    testingDomainId,
    refresh,
    save,
    test,
  };
}
