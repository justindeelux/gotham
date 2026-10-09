<script setup lang="ts">
import { NButton, NCard, NSpace, NText } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import type { Deployment } from "@/features/applications/api/applications";
import {
  commitUrl,
  hasCommitSha,
  shortCommitSha,
} from "@/features/applications/utils/deploymentCommit";
import { useCopyText } from "@/shared/composables/useCopyText";
import { formatDate, relativeTime } from "@/shared/utils/format";

/** Commit details of the built revision; hidden while commit_sha is empty. */
interface Props {
  deployment: Deployment | null;
  repo?: string;
  cloneUrl?: string;
}

const props = withDefaults(defineProps<Props>(), { repo: "", cloneUrl: "" });

const { t } = useI18n();
const { copyText } = useCopyText();

/** show renders only when the deployment carries a commit hash. */
const show = computed<boolean>(() => hasCommitSha(props.deployment?.commit_sha));

const shortSha = computed<string>(() => shortCommitSha(props.deployment?.commit_sha));

const url = computed<string>(() =>
  commitUrl(props.repo, props.cloneUrl, props.deployment?.commit_sha),
);

const relative = computed<string>(() => relativeTime(props.deployment?.committed_at ?? ""));

const absolute = computed<string>(() => formatDate(props.deployment?.committed_at ?? ""));

/** copySha copies the full hash; the label comes from the commit catalog. */
function copySha(): void {
  if (props.deployment?.commit_sha) {
    void copyText(props.deployment.commit_sha, String(t("applications.commit.sha")));
  }
}
</script>

<template>
  <NCard v-if="show" size="small" data-testid="commit-card" :title="t('applications.commit.title')">
    <NSpace vertical :size="8">
      <NSpace align="center" :size="8">
        <a
          v-if="url"
          class="mono"
          :href="url"
          target="_blank"
          rel="noopener noreferrer"
          :title="props.deployment?.commit_sha ?? ''"
        >{{ shortSha }}</a>
        <span v-else class="mono" :title="props.deployment?.commit_sha ?? ''">{{ shortSha }}</span>
        <NButton
          size="tiny"
          quaternary
          :aria-label="t('applications.commit.copySha')"
          @click="copySha"
        >
          {{ t("applications.commit.copySha") }}
        </NButton>
      </NSpace>
      <NText v-if="props.deployment?.commit_message">{{ props.deployment.commit_message }}</NText>
      <NText depth="3">
        <span v-if="props.deployment?.commit_author">{{ props.deployment.commit_author }} · </span>
        <span :title="absolute">{{ relative }}</span>
      </NText>
    </NSpace>
  </NCard>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}
</style>
