<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NRadioButton,
  NRadioGroup,
  NSkeleton,
  NSpace,
  NSwitch,
  NTag,
  NText,
} from "naive-ui";
import { computed, ref, toRef } from "vue";
import { useI18n } from "vue-i18n";

import type {
  Application,
  ApplicationDomain,
  WebsiteCheck,
  WebsiteCheckInput,
  WebsiteCheckPoint,
  WebsiteCheckTestResult,
  WebsiteCheckWindow,
} from "@/features/applications/api/applications";
import WebsiteCheckModal from "@/features/applications/components/WebsiteCheckModal.vue";
import { useWebsiteChecks } from "@/features/applications/composables/useWebsiteChecks";
import { formatPercent, relativeTime } from "@/shared/utils/format";

interface Props {
  application: Application;
  domains: ApplicationDomain[];
}

const props = defineProps<Props>();
const { t } = useI18n();

const checks = useWebsiteChecks(
  toRef(props, "application"),
  toRef(props, "domains"),
);

const windowKey = ref<"24h" | "7d">("24h");
const editing = ref<ApplicationDomain | null>(null);
const modalOpen = ref(false);

const visibleDomains = computed<ApplicationDomain[]>(() =>
  props.domains.filter((row) => !row.disabled),
);

const windowOptions = computed(() => [
  { label: t("applications.websiteCheck.window24h"), value: "24h" },
  { label: t("applications.websiteCheck.window7d"), value: "7d" },
]);

function checkFor(domainID: string): WebsiteCheck | null {
  return checks.checks.value[domainID] ?? null;
}

function windowFor(check: WebsiteCheck): WebsiteCheckWindow | null {
  return check.windows.find((item) => item.key === windowKey.value) ?? null;
}

function statusLabel(status: string): string {
  switch (status) {
    case "healthy":
      return t("applications.websiteCheck.statusHealthy");
    case "failing":
      return t("applications.websiteCheck.statusFailing");
    case "unsupported":
      return t("applications.websiteCheck.statusUnsupported");
    case "disabled":
      return t("applications.websiteCheck.statusDisabled");
    default:
      return t("applications.websiteCheck.statusUnknown");
  }
}

function statusType(status: string): "success" | "error" | "warning" | "default" {
  switch (status) {
    case "healthy":
      return "success";
    case "failing":
      return "error";
    case "unsupported":
      return "warning";
    default:
      return "default";
  }
}

function reasonLabel(reason: string | undefined): string {
  switch (reason) {
    case "timeout":
      return t("applications.websiteCheck.reasons.timeout");
    case "dns_failure":
      return t("applications.websiteCheck.reasons.dns");
    case "tls_failure":
      return t("applications.websiteCheck.reasons.tls");
    case "transport_failure":
      return t("applications.websiteCheck.reasons.transport");
    case "unexpected_status":
      return t("applications.websiteCheck.reasons.status");
    case "unsafe_redirect":
      return t("applications.websiteCheck.reasons.redirect");
    case "blocked_destination":
      return t("applications.websiteCheck.reasons.blocked");
    default:
      return "";
  }
}

function windowCoverage(window: WebsiteCheckWindow): string {
  return t("applications.websiteCheck.coverage", {
    percent: formatPercent(window.coverage * 100),
  });
}

function windowAvailability(window: WebsiteCheckWindow): string {
  if (window.insufficient_coverage) {
    return t("applications.websiteCheck.insufficientCoverage", {
      percent: formatPercent(window.coverage * 100),
    });
  }
  return t("applications.websiteCheck.availability", {
    percent: formatPercent((window.availability ?? 0) * 100),
  });
}

function latencyText(check: WebsiteCheck): string {
  return check.last_latency_ms === undefined
    ? ""
    : t("applications.websiteCheck.latency", {
        ms: check.last_latency_ms,
      });
}

function pointTitle(point: WebsiteCheckPoint): string {
  return t("applications.websiteCheck.point", {
    time: relativeTime(point.at, null, { long: true }),
    outcome: t(`applications.websiteCheck.pointOutcome.${point.outcome}`),
  });
}

function testResultFor(domainID: string): WebsiteCheckTestResult | null {
  return checks.testResults.value[domainID] ?? null;
}

function testResultType(
  result: WebsiteCheckTestResult,
): "success" | "error" | "warning" {
  if (result.outcome === "healthy") {
    return "success";
  }
  if (result.outcome === "unsupported") {
    return "warning";
  }
  return "error";
}

function testResultText(result: WebsiteCheckTestResult): string {
  const parts = [statusLabel(result.outcome === "failed" ? "failing" : result.outcome)];
  if (result.reason) {
    parts.push(reasonLabel(result.reason));
  }
  if (result.status_code) {
    parts.push(`HTTP ${result.status_code}`);
  }
  if (result.latency_ms !== undefined) {
    parts.push(`${result.latency_ms} ms`);
  }
  return parts.filter(Boolean).join(" · ");
}

function openConfigure(domain: ApplicationDomain): void {
  editing.value = domain;
  modalOpen.value = true;
}

async function save(input: WebsiteCheckInput): Promise<void> {
  if (editing.value === null) {
    return;
  }
  const ok = await checks.save(editing.value.id, input);
  if (ok) {
    modalOpen.value = false;
  }
}

async function toggleEnabled(
  domain: ApplicationDomain,
  check: WebsiteCheck,
  value: boolean,
): Promise<void> {
  await checks.save(domain.id, {
    enabled: value,
    path: check.path,
    expected_min: check.expected_min,
    expected_max: check.expected_max,
    timeout_seconds: check.timeout_seconds,
  });
}

const editingCheck = computed<WebsiteCheck | null>(() =>
  editing.value === null ? null : checkFor(editing.value.id),
);
</script>

<template>
  <NCard :title="t('applications.websiteCheck.title')">
    <template #header-extra>
      <NRadioGroup v-model:value="windowKey" size="small" name="website-window">
        <NRadioButton
          v-for="option in windowOptions"
          :key="option.value"
          :value="option.value"
        >
          {{ option.label }}
        </NRadioButton>
      </NRadioGroup>
    </template>
    <NSpace vertical :size="12">
      <NText depth="3" class="hint">
        {{ t("applications.websiteCheck.hint") }}
      </NText>
      <NAlert v-if="checks.error.value" type="error" :show-icon="true">
        {{ checks.error.value }}
      </NAlert>
      <NEmpty
        v-if="visibleDomains.length === 0"
        :description="t('applications.websiteCheck.empty')"
      />
      <NSkeleton v-else-if="checks.loading.value" text :repeat="3" />
      <ul v-else class="check-list">
        <li v-for="domain in visibleDomains" :key="domain.id" class="check-row">
          <template v-if="checkFor(domain.id) !== null">
            <div class="check-head">
              <span class="mono check-domain">{{ domain.domain }}</span>
              <NTag
                size="small"
                :bordered="false"
                :type="statusType(checkFor(domain.id)!.status)"
              >
                {{ statusLabel(checkFor(domain.id)!.status) }}
              </NTag>
              <NText
                v-if="checkFor(domain.id)!.reason"
                depth="3"
                class="check-reason"
              >
                {{ reasonLabel(checkFor(domain.id)!.reason) }}
              </NText>
              <span class="check-spacer" />
              <NSwitch
                size="small"
                :value="checkFor(domain.id)!.enabled"
                :loading="checks.busyDomainId.value === domain.id"
                :aria-label="t('applications.websiteCheck.toggleAria', { domain: domain.domain })"
                @update:value="(value: boolean) => void toggleEnabled(domain, checkFor(domain.id)!, value)"
              />
            </div>
            <NText depth="3" class="check-meta">
              {{ checkFor(domain.id)!.url }}
              · {{ t("applications.websiteCheck.vantage") }}
            </NText>
            <NText depth="3" class="check-meta">
              <template v-if="checkFor(domain.id)!.last_checked_at">
                {{ t("applications.websiteCheck.lastChecked", {
                  time: relativeTime(checkFor(domain.id)!.last_checked_at!, null, { long: true }),
                }) }}
              </template>
              <template v-else>
                {{ t("applications.websiteCheck.neverChecked") }}
              </template>
              <template v-if="latencyText(checkFor(domain.id)!)">
                · {{ latencyText(checkFor(domain.id)!) }}
              </template>
              <template v-if="checkFor(domain.id)!.last_status">
                · HTTP {{ checkFor(domain.id)!.last_status }}
              </template>
            </NText>

            <template v-if="windowFor(checkFor(domain.id)!)">
              <div class="window-summary">
                <NText class="window-primary">
                  {{ windowAvailability(windowFor(checkFor(domain.id)!)!) }}
                </NText>
                <NText depth="3" class="window-secondary">
                  {{ windowCoverage(windowFor(checkFor(domain.id)!)!) }}
                </NText>
              </div>
              <div
                class="spark"
                role="img"
                :aria-label="t('applications.websiteCheck.sparkAria', {
                  window: t(windowKey === '24h' ? 'applications.websiteCheck.window24h' : 'applications.websiteCheck.window7d'),
                })"
              >
                <span
                  v-for="(point, index) in windowFor(checkFor(domain.id)!)!.points"
                  :key="index"
                  :class="['spark-bar', point.outcome]"
                  :title="pointTitle(point)"
                />
              </div>
            </template>

            <NSpace :size="8" class="check-actions">
              <NButton
                size="small"
                :loading="checks.testingDomainId.value === domain.id"
                @click="() => void checks.test(domain.id)"
              >
                {{ t("applications.websiteCheck.test") }}
              </NButton>
              <NButton size="small" @click="openConfigure(domain)">
                {{ t("applications.websiteCheck.configure") }}
              </NButton>
            </NSpace>

            <NAlert
              v-if="testResultFor(domain.id)"
              :type="testResultType(testResultFor(domain.id)!)"
              :show-icon="true"
              class="test-result"
            >
              {{ testResultText(testResultFor(domain.id)!) }}
            </NAlert>
          </template>
        </li>
      </ul>
    </NSpace>

    <WebsiteCheckModal
      v-if="editing !== null && editingCheck !== null"
      v-model:show="modalOpen"
      :domain="editing.domain"
      :check="editingCheck"
      :saving="checks.busyDomainId.value === editing.id"
      :error="checks.error.value"
      @save="(input: WebsiteCheckInput) => void save(input)"
    />
  </NCard>
</template>

<style scoped>
.hint {
  font-size: var(--text-xs);
  max-width: 80ch;
}

.check-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.check-row {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  padding: var(--space-3) 0;
  min-width: 0;
}

.check-row + .check-row {
  border-top: 1px solid var(--border);
}

.check-head {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
  min-width: 0;
}

.check-domain {
  font-size: var(--text-sm);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 40ch;
}

.check-reason {
  font-size: var(--text-xs);
}

.check-spacer {
  flex: 1 1 auto;
}

.check-meta {
  font-size: var(--text-xs);
  overflow-wrap: anywhere;
}

.window-summary {
  display: flex;
  align-items: baseline;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.window-primary {
  font-size: var(--text-sm);
  font-weight: 600;
}

.window-secondary {
  font-size: var(--text-xs);
}

.spark {
  display: flex;
  gap: 1px;
  width: 100%;
  height: 18px;
  align-items: stretch;
  background: var(--fg-soft);
  border-radius: var(--radius-sm);
  overflow: hidden;
}

.spark-bar {
  flex: 1 1 0;
  min-width: 1px;
}

.spark-bar.healthy {
  background: var(--success);
}

.spark-bar.failed {
  background: var(--danger);
}

.spark-bar.unknown {
  background: var(--surface);
}

.check-actions {
  flex-wrap: wrap;
}

.test-result {
  font-size: var(--text-xs);
}

@media (max-width: 640px) {
  .check-domain {
    max-width: 100%;
  }
}
</style>
