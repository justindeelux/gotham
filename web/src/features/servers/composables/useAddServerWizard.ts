
import type { FormInst, FormRules } from "naive-ui";
import { useMessage } from "naive-ui";
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import type { ComputedRef, InjectionKey, Ref } from "vue";
import { useI18n } from "vue-i18n";

import {
  createPrivateKey,
  failureText,
} from "@/features/servers/api/servers";
import type { CheckResult, Server, ServerCheckName } from "@/features/servers/api/servers";
import {
  connectionRules,
} from "@/features/servers/schemas/servers";
import { useServersStore } from "@/features/servers/stores/servers";
import { useDockerInstall } from "@/features/servers/composables/useDockerInstall";
import type { DockerInstallState } from "@/features/servers/composables/useDockerInstall";
import { useInFlightGuard } from "@/shared/composables/useInFlightGuard";
import { onLocaleChange } from "@/shared/i18n";
import { formatBytes } from "@/shared/utils/format";

export interface ConnectionForm {
  name: string;
  ip: string;
  port: number | null;
  sshUser: string;
  authMode: "key" | "password";
  keyMode: "new" | "existing";
  keyName: string;
  privateKey: string;
  passphrase: string;
  keyId: string;
  password: string;
  trustHostKey: boolean;
}

export type CheckState = "idle" | "running" | "ok" | "fail";

export interface FixedCheck {
  name: ServerCheckName;
  label: string;
  state: CheckState;
  detail: string;
  installable: boolean;
}

export interface AddServerWizardContext {
  step: Ref<number>;
  stepNames: ComputedRef<string[]>;
  form: ConnectionForm;
  formRef: Ref<FormInst | null>;
  setFormRef: (_instance: FormInst | null) => void;
  rules: ComputedRef<FormRules>;
  errorMessage: Ref<string>;
  validateMessage: Ref<string>;
  validationPassed: Ref<boolean>;
  fixedChecks: Ref<FixedCheck[]>;
  createdServer: Ref<Server | null>;
  creating: Ref<boolean>;
  validating: Ref<boolean>;
  hasCreatedServer: ComputedRef<boolean>;
  currentServer: ComputedRef<Server | null>;
  isReady: ComputedRef<boolean>;
  sshStatusLine: ComputedRef<string>;
  passedCount: ComputedRef<number>;
  checkSummary: ComputedRef<string>;
  hasRunChecks: ComputedRef<boolean>;
  nodeInitials: ComputedRef<string>;
  resourceSummary: ComputedRef<string>;
  handleCreate: () => Promise<void>;
  handleValidate: () => Promise<void>;
  closeWizard: () => void;
  handleShowChange: (_value: boolean) => void;
  dockerInstall: DockerInstallState;
  dockerInstallOpen: Ref<boolean>;
  dockerInstallOffer: ComputedRef<boolean>;
  openDockerInstall: () => void;
  closeDockerInstall: () => void;
  recheckAfterDockerInstall: () => Promise<void>;
}

export const WizardKey: InjectionKey<AddServerWizardContext> = Symbol("add-server-wizard");

const FIXED_CHECK_NAMES: ServerCheckName[] = ["docker", "cpu", "ram", "disk"];

type WizardEmit = {
  (_event: "update:show", _value: boolean): void;
  (_event: "created", _server: Server): void;
};

export function useAddServerWizard(emit: WizardEmit): AddServerWizardContext {
  const serversStore = useServersStore();
  const message = useMessage();
  const { locale, t } = useI18n();

  const stepNames = computed<string[]>(() => [
    t("servers.wizard.stepConnect"),
    t("servers.wizard.stepValidate"),
    t("servers.wizard.stepInstall"),
    t("servers.wizard.stepFinish"),
  ]);

  const step = ref(0);
  const formRef = ref<FormInst | null>(null);
  const createFailure = ref<unknown>(null);
  const probeFailure = ref<unknown>(null);
  const errorMessage = computed<string>(() =>
    createFailure.value === null || createFailure.value === undefined
      ? ""
      : failureText(createFailure.value, locale.value),
  );
  const validateMessage = computed<string>(() => {
    const failure = probeFailure.value;
    if (failure === null || failure === undefined || failure === "") {
      return "";
    }
    if (typeof failure === "string") {
      return failure;
    }
    return failureText(failure, locale.value);
  });
  const validationAttempted = ref(false);
  const validationPassed = ref(false);
  const fixedChecks = ref<FixedCheck[]>(makeIdleChecks());
  const createdServer = ref<Server | null>(null);

  const inFlight = useInFlightGuard();
  const creating = inFlight.creating;
  const validating = inFlight.validating;

  const form = reactive<ConnectionForm>({
    name: "",
    ip: "",
    port: 22,
    sshUser: "root",
    authMode: "key",
    keyMode: "new",
    keyName: "",
    privateKey: "",
    passphrase: "",
    keyId: "",
    password: "",
    trustHostKey: false,
  });

  const rules = computed<FormRules>(() => connectionRules(form));

  const dockerInstall = useDockerInstall({
    serverId: computed<string>(() => createdServer.value?.id ?? ""),
    passphrase: computed<string>(() => form.passphrase),
    trustHostKey: computed<boolean>(() => form.trustHostKey),
  });
  const dockerInstallOpen = ref(false);
  const dockerInstallOffer = computed<boolean>(() => {
    if (createdServer.value === null || validating.value) {
      return false;
    }
    const docker = fixedChecks.value.find((check) => check.name === "docker");
    return docker?.state === "fail" && docker.installable;
  });

  function openDockerInstall(): void {
    dockerInstallOpen.value = true;
    void dockerInstall.start();
  }

  function closeDockerInstall(): void {
    dockerInstallOpen.value = false;
    dockerInstall.stop();
  }

  async function recheckAfterDockerInstall(): Promise<void> {
    closeDockerInstall();
    await handleValidate();
  }

  const hasCreatedServer = computed<boolean>(() => createdServer.value !== null);

  const currentServer = computed<Server | null>(() => {
    const created = createdServer.value;
    if (!created) {
      return null;
    }
    return serversStore.servers.find((item) => item.id === created.id) ?? created;
  });

  const isReady = computed<boolean>(() => currentServer.value?.status === "ready");

  const sshStatusLine = computed<string>(() => {
    const server = currentServer.value;
    if (!server) {
      return "";
    }
    return `SSH ${server.ip}:${server.port} · user ${server.ssh_user}`;
  });

  const passedCount = computed<number>(
    () => fixedChecks.value.filter((check) => check.state === "ok").length,
  );

  function checkLabel(name: ServerCheckName): string {
    switch (name) {
      case "docker":
        return t("servers.wizard.checkDocker");
      case "cpu":
        return t("servers.wizard.checkCpu");
      case "ram":
        return t("servers.wizard.checkRam");
      case "disk":
        return t("servers.wizard.checkDisk");
    }
  }

  const checkSummary = computed<string>(() => {
    if (validating.value) {
      return t("servers.wizard.summaryRunning");
    }
    if (!hasRunChecks.value) {
      return t("servers.wizard.summaryIdle");
    }
    const total = fixedChecks.value.length;
    const passed = passedCount.value;
    if (validationPassed.value) {
      return t("servers.wizard.summaryPassed", { passed, total });
    }
    return t("servers.wizard.summaryFailed", { passed, total });
  });

  const hasRunChecks = computed<boolean>(
    () => fixedChecks.value.some((check) => check.state === "ok" || check.state === "fail"),
  );

  const nodeInitials = computed<string>(() => {
    const name = (currentServer.value?.name ?? form.name).trim();
    if (name === "") {
      return "··";
    }
    const compact = name.replace(/[^a-zA-Z0-9]/g, "");
    return (compact.slice(0, 2) || name.slice(0, 2)).toUpperCase();
  });

  const resourceSummary = computed<string>(() => {
    const server = currentServer.value;
    if (!server) {
      return "";
    }
    const parts: string[] = [];
    if (server.total_mem !== null) {
      parts.push(formatBytes(server.total_mem));
    }
    if (server.total_disk !== null) {
      parts.push(formatBytes(server.total_disk));
    }
    if (server.arch !== null && server.arch !== "") {
      parts.push(server.arch);
    }
    return parts.join(" · ");
  });

  watch(step, (value) => {
    if (value === 1 && createdServer.value && !hasRunChecks.value && !validating.value) {
      void handleValidate();
    }
  });

  const connectionSnapshot = computed<string>(() =>
    JSON.stringify([
      form.name,
      form.ip,
      form.port,
      form.sshUser,
      form.authMode,
      form.keyMode,
      form.keyName,
      form.privateKey,
      form.keyId,
      form.password !== "",
    ]),
  );
  watch(connectionSnapshot, () => {
    if (!createdServer.value) {
      return;
    }
    validationPassed.value = false;
    probeFailure.value = null;
    fixedChecks.value = makeIdleChecks();
  });

  function makeIdleChecks(): FixedCheck[] {
    return FIXED_CHECK_NAMES.map((name) => ({
      name,
      label: checkLabel(name),
      state: "idle" as CheckState,
      detail: t("servers.wizard.checkPending"),
      installable: false,
    }));
  }

  function applyCheckResults(results: CheckResult[]): void {
    const byName = new Map(results.map((item) => [item.name, item]));
    fixedChecks.value = FIXED_CHECK_NAMES.map((name) => {
      const result = byName.get(name);
      if (!result) {
        return {
          name,
          label: checkLabel(name),
          state: "idle" as CheckState,
          detail: t("servers.wizard.checkPending"),
          installable: false,
        };
      }
      return {
        name,
        label: checkLabel(name),
        state: (result.ok ? "ok" : "fail") as CheckState,
        detail: result.detail,
        installable: result.installable === true,
      };
    });
  }

  function relabelChecks(): void {
    fixedChecks.value = fixedChecks.value.map((check) => {
      let detail = check.detail;
      if (check.state === "idle") {
        detail = t("servers.wizard.checkPending");
      } else if (check.state === "running") {
        detail = t("servers.wizard.checkRunning");
      }
      return { ...check, label: checkLabel(check.name), detail };
    });
  }

  async function handleCreate(): Promise<void> {
    if (createdServer.value) {
      step.value = 1;
      return;
    }

    createFailure.value = null;
    validationAttempted.value = true;
    try {
      await formRef.value?.validate();
    } catch {
      return;
    }

    const token = inFlight.begin();
    creating.value = true;
    try {
      let keyId: string | null = null;
      let password: string | undefined;
      if (form.authMode === "password") {
        password = form.password;
      } else if (form.keyMode === "new") {
        const key = await createPrivateKey({
          name: form.keyName.trim(),
          private_key: form.privateKey,
        });
        keyId = key.id;
      } else {
        keyId = form.keyId.trim() || null;
      }

      const server = await serversStore.addServer({
        name: form.name.trim(),
        ip: form.ip.trim(),
        port: form.port ?? 22,
        ssh_user: form.sshUser.trim(),
        ssh_key_id: keyId,
        ...(password ? { password } : {}),
      });

      if (!inFlight.isCurrent(token)) {
        return;
      }
      createdServer.value = server;
      emit("created", server);
      step.value = 1;
    } catch (error) {
      if (!inFlight.isCurrent(token)) {
        return;
      }
      createFailure.value = error;
    } finally {
      if (inFlight.isCurrent(token)) {
        creating.value = false;
      }
    }
  }

  async function handleValidate(): Promise<void> {
    const server = createdServer.value;
    if (!server) {
      return;
    }

    const token = inFlight.begin();
    validating.value = true;
    probeFailure.value = null;
    validationPassed.value = false;
    fixedChecks.value = FIXED_CHECK_NAMES.map((name) => ({
      name,
      label: checkLabel(name),
      state: "running" as CheckState,
      detail: t("servers.wizard.checkRunning"),
      installable: false,
    }));
    try {
      const outcome = await serversStore.validate(server.id, form.passphrase || undefined, {
        trustHostKey: form.trustHostKey,
      });
      if (!inFlight.isCurrent(token)) {
        return;
      }
      applyCheckResults(outcome.checks);
      probeFailure.value = outcome.message;
      validationPassed.value = outcome.ok;
      if (outcome.ok) {
        message.success(t("servers.toasts.validationOk"));
      }
    } catch (error) {
      if (!inFlight.isCurrent(token)) {
        return;
      }
      fixedChecks.value = makeIdleChecks();
      probeFailure.value = error;
      validationPassed.value = false;
    } finally {
      if (inFlight.isCurrent(token)) {
        validating.value = false;
      }
    }
  }

  function closeWizard(): void {
    emit("update:show", false);
    resetWizard();
  }

  function handleShowChange(value: boolean): void {
    emit("update:show", value);
    if (!value) {
      resetWizard();
    }
  }

  function resetWizard(): void {
    inFlight.reset();
    step.value = 0;
    form.name = "";
    form.ip = "";
    form.port = 22;
    form.sshUser = "root";
    form.authMode = "key";
    form.keyMode = "new";
    form.keyName = "";
    form.privateKey = "";
    form.passphrase = "";
    form.keyId = "";
    form.password = "";
    form.trustHostKey = false;
    createFailure.value = null;
    probeFailure.value = null;
    validationPassed.value = false;
    validationAttempted.value = false;
    fixedChecks.value = makeIdleChecks();
    createdServer.value = null;
    dockerInstallOpen.value = false;
    dockerInstall.reset();
    formRef.value?.restoreValidation();
  }

  function setFormRef(instance: FormInst | null): void {
    formRef.value = instance;
  }

  const stopLocaleWatch = onLocaleChange(() => {
    relabelChecks();
    if (validationAttempted.value && formRef.value) {
      void formRef.value.validate().catch(() => {});
    }
  });
  onBeforeUnmount(stopLocaleWatch);

  return {
    step,
    stepNames,
    form,
    formRef,
    setFormRef,
    rules,
    errorMessage,
    validateMessage,
    validationPassed,
    fixedChecks,
    createdServer,
    creating,
    validating,
    hasCreatedServer,
    currentServer,
    isReady,
    sshStatusLine,
    passedCount,
    checkSummary,
    hasRunChecks,
    nodeInitials,
    resourceSummary,
    handleCreate,
    handleValidate,
    closeWizard,
    handleShowChange,
    dockerInstall,
    dockerInstallOpen,
    dockerInstallOffer,
    openDockerInstall,
    closeDockerInstall,
    recheckAfterDockerInstall,
  };
}
