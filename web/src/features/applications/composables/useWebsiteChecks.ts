import { useMessage } from "naive-ui";
import { computed, onMounted, onUnmounted, ref, watch, type Ref } from "vue";

import {
  describeApplicationError,
  getDomainMonitoring,
  testDomainMonitoring,
  updateDomainMonitoring,
  type WebsiteCheck,
  type WebsiteCheckDomain,
  type WebsiteCheckInput,
  type WebsiteCheckOwner,
  type WebsiteCheckTestResult,
} from "@/features/applications/api/applications";
import { websiteCheckSchema } from "@/features/applications/schemas/websiteCheck";
import { activeLocale, i18n } from "@/shared/i18n";
import { fieldErrors } from "@/shared/validation/naiveAdapter";

export function useWebsiteChecks(
  owner: Ref<WebsiteCheckOwner>,
  domains: Ref<WebsiteCheckDomain[]>,
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
    const ids = domainIDs.value;
    if (owner.value.id === "" || ids.length === 0) {
      checks.value = {};
      error.value = null;
      return;
    }
    loading.value = Object.keys(checks.value).length === 0;
    try {
      const results = await Promise.all(
        ids.map((id) => getDomainMonitoring(owner.value, id)),
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
        owner.value,
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
      const result = await testDomainMonitoring(owner.value, domainID);
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
    () => `${owner.value.kind}:${owner.value.id}`,
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
