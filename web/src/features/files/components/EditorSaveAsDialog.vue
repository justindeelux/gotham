<script setup lang="ts">
import { NButton, NFormItem, NInput, NModal, useMessage } from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { useEditorStore } from "@/features/files/stores/editor";
import { useFilesStore } from "@/features/files/stores/files";
import { normalizeEntryName, validateEntryName } from "@/features/files/utils/files";
import { submitOnEnter } from "@/features/projects/utils/submitOnEnter";
import { useTeamsStore } from "@/features/teams";

const { t } = useI18n();
const message = useMessage();
const editor = useEditorStore();
const files = useFilesStore();
const teams = useTeamsStore();

const name = ref("");
const touched = ref(false);

/** show follows the store's pending save-as prompt. */
const show = computed<boolean>(() => editor.saveAsTab !== null);
const tabName = computed<string>(() => editor.saveAsTab?.name ?? "");

/** directoryOf returns the parent path of a root-relative path. */
function directoryOf(path: string): string {
  const slash = path.lastIndexOf("/");
  return slash === -1 ? "" : path.slice(0, slash);
}

watch(show, (open) => {
  if (open) {
    name.value = `${tabName.value}.copy`;
    touched.value = false;
  }
});

/**
 * existsInFolder is an advisory hint for the current listing only (the store
 * performs the authoritative exclusive-create probe, so a stale or paginated
 * listing can never cause a clobber).
 */
const collides = computed<boolean>(() => {
  const tab = editor.saveAsTab;
  if (!tab || directoryOf(tab.path) !== files.path) {
    return false;
  }
  const target = normalizeEntryName(name.value);
  return files.entries.some((entry) => entry.name === target);
});

const errorKey = computed<string | null>(() =>
  touched.value ? validateEntryName(name.value) : null,
);
const errorText = computed<string>(() => {
  if (errorKey.value) {
    return String(t(errorKey.value));
  }
  if (touched.value && collides.value) {
    return String(t("files.editor.saveAsExists", { name: normalizeEntryName(name.value) }));
  }
  return "";
});
const valid = computed<boolean>(
  () => validateEntryName(name.value) === null && !collides.value,
);

/** submit writes the buffer to a sibling file and retargets the tab. */
async function submit(): Promise<void> {
  const tab = editor.saveAsTab;
  if (!tab || tab.saving) {
    return;
  }
  touched.value = true;
  if (!valid.value) {
    return;
  }
  const target = normalizeEntryName(name.value);
  const outcome = await editor.saveAsCopy(tab.id, teams.activeTeamId, target);
  if (outcome === "saved") {
    message.success(String(t("files.editor.saveAsSaved", { name: target })));
    // The copy is a new entry: refresh the listing so it appears.
    void files.refresh();
  } else if (outcome === "conflict") {
    message.error(String(t("files.editor.saveAsExists", { name: target })));
  } else if (outcome === "error") {
    message.error(tab.error || String(t("files.editor.saveFailed", { name: tab.name })));
  }
}
</script>

<template>
  <NModal
    class="app-modal"
    :show="show"
    preset="card"
    style="width: 440px; max-width: 94vw"
    :title="t('files.editor.saveAsTitle')"
    @update:show="(value: boolean) => { if (!value) editor.cancelSaveAs(); }"
  >
    <NFormItem
      :label="t('files.editor.saveAsLabel')"
      :validation-status="errorKey || (touched && collides) ? 'error' : undefined"
      :feedback="errorText"
    >
      <NInput
        v-model:value="name"
        class="mono"
        autofocus
        :placeholder="t('files.editor.saveAsPlaceholder')"
        @keydown.enter="(event: KeyboardEvent) => submitOnEnter(event, submit)"
      />
    </NFormItem>
    <template #footer>
      <div class="dialog-foot">
        <NButton @click="editor.cancelSaveAs()">
          {{ t("common.actions.cancel") }}
        </NButton>
        <NButton
          type="primary"
          :loading="editor.saveAsTab?.saving"
          :disabled="!valid"
          @click="() => void submit()"
        >
          {{ t("files.editor.saveAsConfirm") }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.dialog-foot {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-2);
}
</style>
