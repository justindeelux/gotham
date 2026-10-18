<script setup lang="ts">
import { NButton, NFormItem, NInput, NModal } from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import type { ExplorerDialogMode } from "@/features/files/utils/explorer";
import {
  normalizeEntryName,
  validateEntryName,
} from "@/features/files/utils/files";
import { submitOnEnter } from "@/features/projects/utils/submitOnEnter";

interface Props {
  show: boolean;
  mode: ExplorerDialogMode;
  /** name is the entry being renamed/deleted; the initial value when renaming. */
  name: string;
  /** existing are the target folder's names (duplicate guard). */
  existing: string[];
  busy: boolean;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  "update:show": [value: boolean];
  /** submit carries the validated name (empty for a delete). */
  submit: [name: string];
}>();

const { t } = useI18n();

const draft = ref("");
const touched = ref(false);

// Every open starts from a clean field: a create starts empty, a rename starts
// with the current name, and a delete has no field at all.
watch(
  () => props.show,
  (open) => {
    if (open) {
      draft.value = props.mode === "rename" ? props.name : "";
      touched.value = false;
    }
  },
);

const isDelete = computed<boolean>(() => props.mode === "delete");
const isRename = computed<boolean>(() => props.mode === "rename");

const title = computed<string>(() => {
  switch (props.mode) {
    case "create-file":
      return String(t("files.editor.explorer.createFileTitle"));
    case "create-folder":
      return String(t("files.editor.explorer.createFolderTitle"));
    case "rename":
      return String(t("files.editor.explorer.renameTitle", { name: props.name }));
    default:
      return String(t("files.editor.explorer.deleteTitle", { name: props.name }));
  }
});

const label = computed<string>(() => {
  switch (props.mode) {
    case "create-file":
      return String(t("files.editor.explorer.fileLabel"));
    case "create-folder":
      return String(t("files.editor.explorer.folderLabel"));
    default:
      return String(t("files.editor.explorer.renameLabel"));
  }
});

const placeholder = computed<string>(() =>
  props.mode === "create-folder"
    ? String(t("files.editor.explorer.createFolderPlaceholder"))
    : String(t("files.editor.explorer.createFilePlaceholder")),
);

const confirmLabel = computed<string>(() => {
  switch (props.mode) {
    case "create-file":
      return String(t("files.editor.explorer.createFileConfirm"));
    case "create-folder":
      return String(t("files.editor.explorer.createFolderConfirm"));
    case "rename":
      return String(t("files.editor.explorer.rename"));
    default:
      return String(t("files.editor.explorer.deleteConfirm"));
  }
});

/** errorKey is the first rule the entered name breaks, or null when usable. */
const errorKey = computed<string | null>(() => {
  if (isDelete.value) {
    return null;
  }
  const clean = normalizeEntryName(draft.value);
  const invalid = validateEntryName(draft.value);
  if (invalid !== null) {
    return invalid;
  }
  if (clean === props.name && isRename.value) {
    return "files.editor.explorer.nameUnchanged";
  }
  const taken = props.existing.some(
    (existing) => existing === clean && !(isRename.value && clean === props.name),
  );
  return taken ? "files.editor.explorer.nameTaken" : null;
});

const feedback = computed<string>(() =>
  touched.value && errorKey.value ? String(t(errorKey.value, { name: normalizeEntryName(draft.value) })) : "",
);

const valid = computed<boolean>(() => errorKey.value === null);

/** submit validates the field and hands the normalized name to the caller. */
function submit(): void {
  if (props.busy) {
    return;
  }
  if (isDelete.value) {
    emit("submit", "");
    return;
  }
  touched.value = true;
  if (!valid.value) {
    return;
  }
  emit("submit", normalizeEntryName(draft.value));
}
</script>

<template>
  <NModal
    class="app-modal"
    :show="props.show"
    preset="card"
    style="width: 440px; max-width: 94vw"
    :title="title"
    @update:show="(value: boolean) => emit('update:show', value)"
  >
    <p v-if="isDelete" class="ex-dialog-text small">
      {{ t("files.editor.explorer.deleteMessage", { name: props.name }) }}
    </p>

    <NFormItem
      v-else
      :label="label"
      :validation-status="touched && errorKey ? 'error' : undefined"
      :feedback="feedback"
    >
      <NInput
        v-model:value="draft"
        class="mono"
        autofocus
        :placeholder="placeholder"
        @keydown.enter="(event: KeyboardEvent) => submitOnEnter(event, submit)"
      />
    </NFormItem>

    <template #footer>
      <div class="dialog-foot">
        <NButton @click="emit('update:show', false)">
          {{ t("common.actions.cancel") }}
        </NButton>
        <NButton
          :type="isDelete ? 'warning' : 'primary'"
          :loading="props.busy"
          :disabled="!isDelete && !valid"
          @click="submit"
        >
          {{ confirmLabel }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.ex-dialog-text {
  margin: 0;
}

.dialog-foot {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-2);
}
</style>
