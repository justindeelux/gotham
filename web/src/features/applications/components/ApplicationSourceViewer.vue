<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NIcon,
  NRadioButton,
  NRadioGroup,
  NSpace,
  NText,
} from "naive-ui";
import { ChevronDown, ChevronRight } from "@lucide/vue";
import { useClipboard } from "@vueuse/core";
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import type {
  Application,
  ApplicationSource,
} from "@/features/applications/api/applications";
import { getApplicationSource } from "@/features/applications/api/applications";
import CodeEditor from "@/features/files/components/CodeEditor.vue";

interface Props {
  application: Application;
}

const props = defineProps<Props>();

const { t } = useI18n();
const { copy, copied } = useClipboard({ legacy: true });

type SourceView = "raw" | "deployable";

const expanded = ref(false);
const view = ref<SourceView>("raw");
const source = ref<ApplicationSource | null>(null);
const loading = ref(false);
const failed = ref(false);

const text = computed<string>(() => {
  if (!source.value) {
    return "";
  }
  return view.value === "raw" ? source.value.raw : source.value.deployable;
});

const hasSource = computed<boolean>(() => source.value !== null && source.value.kind !== "");

const hint = computed<string>(() => {
  if (view.value === "raw") {
    return String(t("applications.source.rawHint"));
  }
  return source.value?.kind === "dockerfile"
    ? String(t("applications.source.dockerfileDeployableHint"))
    : String(t("applications.source.deployableHint"));
});

async function load(): Promise<void> {
  loading.value = true;
  failed.value = false;
  try {
    source.value = await getApplicationSource(props.application.id);
  } catch {
    failed.value = true;
    source.value = null;
  } finally {
    loading.value = false;
  }
}

onMounted(load);
watch(() => props.application.updated_at, load);

function copySource(): void {
  void copy(text.value);
}
</script>

<template>
  <NCard :title="t('applications.source.title')">
    <template #header-extra>
      <NSpace :size="8" align="center">
        <NButton v-if="hasSource && expanded" size="small" secondary @click="copySource">
          {{ copied ? t("applications.source.copied") : t("applications.source.copy") }}
        </NButton>
        <NButton
          size="small"
          quaternary
          :aria-expanded="expanded"
          :aria-label="expanded ? t('applications.source.collapse') : t('applications.source.expand')"
          @click="expanded = !expanded"
        >
          <NIcon :size="16">
            <ChevronDown v-if="expanded" />
            <ChevronRight v-else />
          </NIcon>
        </NButton>
      </NSpace>
    </template>
    <template v-if="expanded">
    <NAlert v-if="failed" type="error" :show-icon="true">
      {{ t("applications.source.loadError") }}
    </NAlert>
    <NText v-else-if="loading && !source" depth="3" class="small">
      {{ t("applications.source.loading") }}
    </NText>
    <NEmpty
      v-else-if="source && !hasSource"
      :description="t('applications.source.empty')"
    />
    <NSpace v-else-if="source" vertical :size="12">
      <NRadioGroup v-model:value="view" size="small">
        <NRadioButton value="raw">
          {{ t("applications.source.raw") }}
        </NRadioButton>
        <NRadioButton value="deployable">
          {{ t("applications.source.deployable") }}
        </NRadioButton>
      </NRadioGroup>
      <NText depth="3" class="small">{{ hint }}</NText>
      <CodeEditor
        :model-value="text"
        :preset="source.kind === 'dockerfile' ? 'dockerfile' : 'yaml'"
        read-only
        wrap
        height="46vh"
        :aria-label="t('applications.source.title')"
      />
    </NSpace>
    </template>
  </NCard>
</template>

<style scoped>
.small {
  font-size: var(--text-xs);
}
</style>
