import { useCopyText } from "@/shared/composables/useCopyText";
import { useMessage } from "naive-ui";
import { computed, reactive, ref, toValue, watch } from "vue";
import type { InjectionKey, Ref } from "vue";
import { i18n } from "@/shared/i18n";

/** t resolves a databases/common message in the current locale. */
function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

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

/**
 * wizardStepKeys are the i18n keys for the step names above, in the same
 * order. The legacy names stay for length/step logic; display uses the
 * localized stepNames below.
 */
export const wizardStepKeys = [
  "databases.wizard.steps.engine",
  "databases.wizard.steps.configure",
  "databases.wizard.steps.review",
] as const;

/** Injection key for the wizard form shared with the step components. */
export const wizardFormKey: InjectionKey<WizardForm> =
  Symbol("wizard-form");

interface WizardOptions {
  show: Ref<boolean>;
  onCreated: (_created: CreatedDatabase) => void;
  onUpdateShow: (_value: boolean) => void;
  /**
   * Preselected scope (the host passes its route by ref so a route change
   * while the wizard is mounted re-seeds the form).
   */
  projectId?: string | Ref<string>;
  environmentId?: string | Ref<string>;
  /** Preselected engine (the Add-resource picker passes its card). */
  engine?: string | Ref<string>;
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
  const created = ref<CreatedDatabase | null>(null);

  /** SubmitIssue retains the submit failure so the alert refreshes on switch. */
  type SubmitIssue = { kind: "scope" } | { kind: "failure"; error: unknown };
  const submitIssue = ref<SubmitIssue | null>(null);
  const errorMessage = computed<string>(() => {
    if (submitIssue.value === null) {
      return "";
    }
    if (submitIssue.value.kind === "scope") {
      return t("databases.wizard.scopeError");
    }
    return describeDatabaseError(submitIssue.value.error);
  });

  const form = reactive<WizardForm>({
    engine: engineByValue(toValue(options.engine ?? "")).value,
    // Preselected so the Version select shows the engine default instead
    // of a blank value (the empty string only renders as a placeholder).
    version: engineByValue(toValue(options.engine ?? "")).defaultVersion,
    projectId: toValue(options.projectId ?? ""),
    environmentId: toValue(options.environmentId ?? ""),
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

  /** stepNames renders the localized wizard step names in step order. */
  const stepNames = computed<readonly string[]>(() =>
    wizardStepKeys.map((key) => String(t(key))),
  );

  /** credentialRows renders the generated credentials as label/value pairs. */
  function credentialRows(
    credentials: DatabaseCredentials,
  ): Array<{ label: string; value: string; secret: boolean }> {
    const rows: Array<{ label: string; value: string; secret: boolean }> = [
      {
        label: String(t("databases.detail.credentials.username")),
        value: credentials.username,
        secret: false,
      },
      {
        label: String(t("databases.detail.credentials.password")),
        value: credentials.password,
        secret: true,
      },
      {
        label: String(t("databases.detail.credentials.database")),
        value: credentials.database,
        secret: false,
      },
    ];
    if (credentials.root_password) {
      rows.push({
        label: String(t("databases.detail.credentials.rootPassword")),
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
    submitIssue.value = null;
    created.value = null;
    seedEngine();
    seedScope();
    form.serverId = "";
    form.name = "";
    form.exposePublic = false;
    form.publicPort = null;
  }

  /** seedEngine copies the preselected engine into the form. */
  function seedEngine(): void {
    const engine = engineByValue(toValue(options.engine ?? ""));
    form.engine = engine.value;
    form.version = engine.defaultVersion;
  }

  /** seedScope copies the live route scope into the form. */
  function seedScope(): void {
    form.projectId = toValue(options.projectId ?? "");
    form.environmentId = toValue(options.environmentId ?? "");
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
    submitIssue.value = null;
    if (form.environmentId === "") {
      submitIssue.value = { kind: "scope" };
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
      message.success(
        t("databases.wizard.created", {
          name: created.value.database.name,
        }),
      );
      options.onCreated(created.value);
    } catch (error) {
      submitIssue.value = { kind: "failure", error };
    } finally {
      submitting.value = false;
    }
  }

  /** handleClose emits the visibility update; the watcher resets the form. */
  function handleClose(value: boolean): void {
    options.onUpdateShow(value);
  }

  // Entering the wizard loads the node list and re-seeds the live scope;
  // closing resets the form.
  watch(
    () => options.show.value,
    (visible) => {
      if (visible) {
        seedScope();
        seedEngine();
        void serversStore.fetchServers().catch(() => undefined);
      } else {
        resetWizard();
      }
    },
  );

  // A route change while the wizard is mounted re-seeds the scope, so the
  // summary and the payload always name the current environment.
  watch(
    [() => toValue(options.projectId ?? ""), () => toValue(options.environmentId ?? "")],
    () => seedScope(),
  );

  // Switching engine reselects the new engine's default version.
  watch(
    () => form.engine,
    () => {
      form.version = engineByValue(form.engine).defaultVersion;
    },
  );

  return {
    databasesStore,
    serversStore,
    step,
    stepNames,
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
