<script setup lang="ts">
import { NButton, NFormItem, NInput, NModal } from "naive-ui";
import { useMessage } from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { describeFilesError } from "@/features/files/api/files";
import { useFilesStore } from "@/features/files/stores/files";
import type { NameKind } from "@/features/files/utils/files";
import {
  normalizeEntryName,
  validateEntryName,
} from "@/features/files/utils/files";

interface Props {
  show: boolean;
  kind: NameKind;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  "update:show": [value: boolean];
}>();

const { t } = useI18n();
const message = useMessage();
const files = useFilesStore();

const name = ref("");
const touched = ref(false);

// Reset the field each time the dialog opens so a previous value never leaks
// into a new create.
watch(
  () => props.show,
  (open) => {
    if (open) {
      name.value = "";
      touched.value = false;
    }
  },
);

const errorKey = computed<string | null>(() =>
  touched.value ? validateEntryName(name.value) : null,
);
const nameError = computed<string>(() =>
  errorKey.value ? String(t(errorKey.value)) : "",
);
const valid = computed<boolean>(() => validateEntryName(name.value) === null);

/** submit creates the file or folder, surfacing any failure in a toast. */
async function submit(): Promise<void> {
  touched.value = true;
  if (!valid.value) {
    return;
  }
  try {
    await files.createEntry(normalizeEntryName(name.value), props.kind);
    message.success(
      String(
        t("files.dialogs.created", { name: normalizeEntryName(name.value) }),
      ),
    );
    emit("update:show", false);
  } catch (err) {
    message.error(describeFilesError(err));
  }
}
</script>

<template>
  <NModal
    class="app-modal"
    :show="props.show"
    preset="card"
    style="width: 440px; max-width: 94vw"
    :title="
      props.kind === 'directory'
        ? t('files.dialogs.createFolderTitle')
        : t('files.dialogs.createFileTitle')
    "
    @update:show="(value: boolean) => emit('update:show', value)"
  >
    <NFormItem
      :label="
        props.kind === 'directory'
          ? t('files.dialogs.createFolderLabel')
          : t('files.dialogs.createFileLabel')
      "
      :validation-status="errorKey ? 'error' : undefined"
      :feedback="nameError"
    >
      <NInput
        v-model:value="name"
        class="mono"
        autofocus
        :placeholder="
          props.kind === 'directory'
            ? t('files.dialogs.createFolderPlaceholder')
            : t('files.dialogs.createFilePlaceholder')
        "
        @keyup.enter="() => void submit()"
      />
    </NFormItem>
    <p class="hint small muted">{{ t("files.name.hint") }}</p>
    <template #footer>
      <div class="dialog-foot">
        <NButton @click="emit('update:show', false)">
          {{ t("common.actions.cancel") }}
        </NButton>
        <NButton
          type="primary"
          :loading="files.acting"
          :disabled="!valid"
          @click="() => void submit()"
        >
          {{
            props.kind === "directory"
              ? t("files.dialogs.createFolderConfirm")
              : t("files.dialogs.createFileConfirm")
          }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.hint {
  margin: 0;
}

.dialog-foot {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-2);
}
</style>
