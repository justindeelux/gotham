<script setup lang="ts">
import { NAlert, NButton, NCard, NSpace, NSpin, NTag, NText } from "naive-ui";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";

import { describeAuthError } from "@/features/auth";
import { fetchTotpStatus, type TotpStatus } from "@/features/profile/api/totp";
import TotpConfirmDialog from "@/features/profile/components/TotpConfirmDialog.vue";
import TotpSetupDialog from "@/features/profile/components/TotpSetupDialog.vue";

const { t } = useI18n();

const status = ref<TotpStatus | null>(null);
const loading = ref(true);
const loadError = ref("");

const setupOpen = ref(false);
const confirmMode = ref<"disable" | "regenerate" | null>(null);

const enabled = computed<boolean>(() => status.value?.enabled ?? false);
const remaining = computed<number>(() => status.value?.recovery_codes_remaining ?? 0);

async function refresh(): Promise<void> {
  loadError.value = "";
  try {
    status.value = await fetchTotpStatus();
  } catch (error) {
    loadError.value = describeAuthError(error);
  } finally {
    loading.value = false;
  }
}

onMounted(() => {
  void refresh();
});
</script>

<template>
  <NCard :title="t('profile.totp.title')">
    <NSpace vertical :size="12">
      <NText depth="3">{{ t("profile.totp.intro") }}</NText>

      <NAlert v-if="loadError" type="error" :show-icon="true">
        {{ loadError }}
        <NButton size="tiny" quaternary @click="refresh">
          {{ t("profile.totp.retry") }}
        </NButton>
      </NAlert>

      <NSpin v-else-if="loading" size="small" />

      <template v-else>
        <div class="totp-status">
          <NTag :type="enabled ? 'success' : 'default'" :bordered="false" size="small">
            {{ enabled ? t("profile.totp.statusOn") : t("profile.totp.statusOff") }}
          </NTag>
          <NText v-if="enabled" depth="3" class="totp-remaining">
            {{ t("profile.totp.recoveryRemaining", { count: remaining }) }}
          </NText>
        </div>

        <NSpace :size="8">
          <NButton v-if="!enabled" type="primary" @click="setupOpen = true">
            {{ t("profile.totp.enable") }}
          </NButton>
          <template v-else>
            <NButton @click="confirmMode = 'regenerate'">
              {{ t("profile.totp.regenerate") }}
            </NButton>
            <NButton type="error" ghost @click="confirmMode = 'disable'">
              {{ t("profile.totp.disable") }}
            </NButton>
          </template>
        </NSpace>
      </template>
    </NSpace>

    <TotpSetupDialog
      v-model:show="setupOpen"
      @done="refresh"
    />

    <TotpConfirmDialog
      v-if="confirmMode !== null"
      :show="true"
      :mode="confirmMode"
      @update:show="confirmMode = null"
      @done="refresh"
    />
  </NCard>
</template>

<style scoped>
.totp-status {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.totp-remaining {
  font-size: var(--text-sm);
}
</style>
