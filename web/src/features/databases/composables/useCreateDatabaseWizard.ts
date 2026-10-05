import { useCopyText } from "@/shared/composables/useCopyText";
import { useMessage } from "naive-ui";
import { computed, reactive, ref, watch } from "vue";
import type { InjectionKey, Ref } from "vue";

import {
  describeDatabaseError,
} from "@/features/databases/api/databases";
import type {
  CreatedDatabase,
  DatabaseCredentials,
} from "@/features/databases/api/databases";
import {
  effectiveVersionFor,
  engineByValue,
  imagePreviewFor,
  versionOptionsFor,
} from "@/features/databases/utils/databaseEngines";
import { isWizardConfigureValid } from "@/features/databases/schemas/databases";
import { useDatabasesStore } from "@/features/databases/stores/databases";
import { useServersStore } from "@/features/servers";

export interface WizardForm {
  engine: string;
  version: string;
  /** Project the database is created in (read-only summary, changeable). */
  projectId: string;
  /** Environment the database is created in (required by the API). */
  environmentId: string;
  serverId: string;
  name: string;
  exposePublic: boolean;
  publicPort: number | null;
}

export const wizardStepNames = ["Engine", "Configure", "Review"];

/** Injection key for the wizard form shared with the step components. */
export const wizardFormKey: InjectionKey<WizardForm> =
  Symbol("wizard-form");

interface WizardOptions {
  show: Ref<boolean>;
  onCreated: (_created: CreatedDatabase) => void;
  onUpdateShow: (_value: boolean) => void;
  /** Preselected scope (the environment page passes its route). */
  projectId?: string;
  environmentId?: string;
}

/**
 * useCreateDatabaseWizard owns the create-database wizard state: the stepped
 * form, validation, provisioning and the generated-credentials success panel.
 * The modal shell stays in the component; the steps render from this state.
 */
export function useCreateDatabaseWizard(options: WizardOptions) {
  const databasesStore = useDatabasesStore();
  const serversStore = useServersStore();
  const message = useMessage();
  const { copyText } = useCopyText();

  const step = ref(0);
  const submitting = ref(false);
  const errorMessage = ref("");
  const created = ref<CreatedDatabase | null>(null);

  const form = reactive<WizardForm>({
    engine: "postgres",
    version: "",
    projectId: options.projectId ?? "",
    environmentId: options.environmentId ?? "",
    serverId: "",
    name: "",
    exposePublic: false,
    publicPort: null,
  });

  const selectedEngine = computed(() => engineByValue(form.engine));

  const versionOptions = computed(() => versionOptionsFor(selectedEngine.value));

  const effectiveVersion = computed<string>(() =>
    effectiveVersionFor(selectedEngine.value, form.version),
  );

  const imagePreview = computed<string>(() =>
    imagePreviewFor(selectedEngine.value, form.version),
  );

  const serverOptions = computed<Array<{ label: string; value: string }>>(() =>
    serversStore.servers.map((server) => ({
      label: `${server.name} · ${server.ip}`,
      value: server.id,
    })),
  );

  const serverLabel = computed<string>(
    () =>
      serverOptions.value.find((item) => item.value === form.serverId)
        ?.label ?? form.serverId,
  );

  /** engineValid gates the Engine step: a scope, an engine and a node. */
  const engineValid = computed<boolean>(
    () => form.environmentId !== "" && form.serverId !== "",
  );

  /** configureValid gates the Configure step: backend name rule + port range. */
  const configureValid = computed<boolean>(() =>
    isWizardConfigureValid({
      name: form.name,
      exposePublic: form.exposePublic,
      publicPort: form.publicPort,
    }),
  );

  const canContinue = computed<boolean>(() => {
    switch (step.value) {
      case 0:
        return engineValid.value;
      case 1:
        return configureValid.value;
      default:
        return true;
    }
  });

  /** credentialRows renders the generated credentials as label/value pairs. */
  function credentialRows(
    credentials: DatabaseCredentials,
  ): Array<{ label: string; value: string; secret: boolean }> {
    const rows: Array<{ label: string; value: string; secret: boolean }> = [
      { label: "Username", value: credentials.username, secret: false },
      { label: "Password", value: credentials.password, secret: true },
      { label: "Database", value: credentials.database, secret: false },
    ];
    if (credentials.root_password) {
      rows.push({
        label: "Root password",
        value: credentials.root_password,
        secret: true,
      });
    }
    return rows;
  }

  /** resetWizard clears the form back to its defaults. */
  function resetWizard(): void {
    step.value = 0;
    submitting.value = false;
    errorMessage.value = "";
    created.value = null;
    form.engine = "postgres";
    form.version = "";
    form.projectId = options.projectId ?? "";
    form.environmentId = options.environmentId ?? "";
    form.serverId = "";
    form.name = "";
    form.exposePublic = false;
    form.publicPort = null;
  }

  /** goNext advances one step, or submits on the review step. */
  function goNext(): void {
    if (step.value < wizardStepNames.length - 1) {
      step.value += 1;
      return;
    }
    void handleSubmit();
  }

  /** goBack returns to the previous step. */
  function goBack(): void {
    if (step.value > 0) {
      step.value -= 1;
    }
  }

  /**
   * handleSubmit provisions the database. Credentials are generated
   * server-side — the wizard never asks for a password, it shows the generated
   * set on the success step.
   */
  async function handleSubmit(): Promise<void> {
    submitting.value = true;
    errorMessage.value = "";
    if (form.environmentId === "") {
      errorMessage.value = "Select a project and environment first.";
      submitting.value = false;
      return;
    }
    try {
      created.value = await databasesStore.provision({
        name: form.name.trim(),
        engine: form.engine,
        version: form.version || undefined,
        environment_id: form.environmentId,
        server_id: form.serverId,
        public_port: form.exposePublic ? (form.publicPort ?? undefined) : undefined,
      });
      message.success(`Database "${created.value.database.name}" created`);
      options.onCreated(created.value);
    } catch (error) {
      errorMessage.value = describeDatabaseError(error);
    } finally {
      submitting.value = false;
    }
  }

  /** handleClose emits the visibility update; the watcher resets the form. */
  function handleClose(value: boolean): void {
    options.onUpdateShow(value);
  }

  // Entering the wizard loads the node list; closing resets the form.
  watch(
    () => options.show.value,
    (visible) => {
      if (visible) {
        void serversStore.fetchServers().catch(() => undefined);
      } else {
        resetWizard();
      }
    },
  );

  // Switching engine resets the version pick to the engine default.
  watch(
    () => form.engine,
    () => {
      form.version = "";
    },
  );

  return {
    databasesStore,
    serversStore,
    step,
    submitting,
    errorMessage,
    created,
    form,
    selectedEngine,
    versionOptions,
    effectiveVersion,
    imagePreview,
    serverOptions,
    serverLabel,
    engineValid,
    configureValid,
    canContinue,
    copyText,
    credentialRows,
    resetWizard,
    goNext,
    goBack,
    handleSubmit,
    handleClose,
  };
}

export type CreateDatabaseWizardState = ReturnType<typeof useCreateDatabaseWizard>;
