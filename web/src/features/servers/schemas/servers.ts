import type { FormRules } from "naive-ui";
import { z } from "zod";

import {
  NAME_PATTERN,
  USER_PATTERN,
  isValidHost,
} from "@/features/servers/utils/serverValidation";
import { ruleFrom } from "@/shared/validation/naiveAdapter";

export const serverMessages = {
  nameRequired: "servers.validation.nameRequired",
  namePattern: "servers.validation.namePattern",
  hostRequired: "servers.validation.hostRequired",
  hostInvalid: "servers.validation.hostInvalid",
  portRequired: "servers.validation.portRequired",
  portRange: "servers.validation.portRange",
  sshUserRequired: "servers.validation.sshUserRequired",
  sshUserPattern: "servers.validation.sshUserPattern",
  keyNameRequired: "servers.validation.keyNameRequired",
  privateKeyRequired: "servers.validation.privateKeyRequired",
  wizardKeyIdRequired: "servers.validation.wizardKeyIdRequired",
  editKeyIdRequired: "servers.validation.editKeyIdRequired",
  nodePasswordRequired: "servers.validation.nodePasswordRequired",
} as const;

export const serverNameSchema = z
  .string({
    required_error: serverMessages.nameRequired,
    invalid_type_error: serverMessages.nameRequired,
  })
  .min(1, serverMessages.nameRequired)
  .refine((value) => value.trim() === "" || NAME_PATTERN.test(value.trim()), {
    message: serverMessages.namePattern,
  });

export const serverHostSchema = z
  .string({
    required_error: serverMessages.hostRequired,
    invalid_type_error: serverMessages.hostRequired,
  })
  .min(1, serverMessages.hostRequired)
  .refine((value) => isValidHost(value), {
    message: serverMessages.hostInvalid,
  });

export const serverPortSchema = z
  .number({
    required_error: serverMessages.portRequired,
    invalid_type_error: serverMessages.portRequired,
  })
  .int(serverMessages.portRange)
  .min(1, serverMessages.portRange)
  .max(65535, serverMessages.portRange);

export const serverUserSchema = z
  .string({
    required_error: serverMessages.sshUserRequired,
    invalid_type_error: serverMessages.sshUserRequired,
  })
  .min(1, serverMessages.sshUserRequired)
  .refine((value) => value.trim() === "" || USER_PATTERN.test(value.trim()), {
    message: serverMessages.sshUserPattern,
  });

export function requiredField(message: string): z.ZodString {
  return z
    .string({ required_error: message, invalid_type_error: message })
    .min(1, message);
}

export interface ConnectionModes {
  authMode: string;
  keyMode: string;
}

export function connectionRules(modes: ConnectionModes): FormRules {
  const isNewKey = modes.authMode === "key" && modes.keyMode === "new";
  const isExistingKey = modes.authMode === "key" && modes.keyMode === "existing";
  const isPassword = modes.authMode === "password";
  return {
    name: [{ ...ruleFrom(serverNameSchema, { required: true }), trigger: ["input", "blur"] }],
    ip: [{ ...ruleFrom(serverHostSchema, { required: true }), trigger: ["input", "blur"] }],
    port: [
      {
        type: "number",
        ...ruleFrom(serverPortSchema, { required: true }),
        trigger: ["input", "blur"],
      },
    ],
    sshUser: [{ ...ruleFrom(serverUserSchema, { required: true }), trigger: ["input", "blur"] }],
    keyName: [
      {
        ...ruleFrom(requiredField(serverMessages.keyNameRequired), {
          when: () => isNewKey,
          required: isNewKey,
        }),
        trigger: ["input", "blur"],
      },
    ],
    privateKey: [
      {
        ...ruleFrom(requiredField(serverMessages.privateKeyRequired), {
          when: () => isNewKey,
          required: isNewKey,
        }),
        trigger: ["input", "blur"],
      },
    ],
    keyId: [
      {
        ...ruleFrom(requiredField(serverMessages.wizardKeyIdRequired), {
          when: () => isExistingKey,
          required: isExistingKey,
        }),
        trigger: ["input", "blur"],
      },
    ],
    password: [
      {
        ...ruleFrom(requiredField(serverMessages.nodePasswordRequired), {
          when: () => isPassword,
          required: isPassword,
        }),
        trigger: ["input", "blur"],
      },
    ],
  };
}

export function editRules(authMode: string): FormRules {
  const isKey = authMode === "key";
  return {
    name: [{ ...ruleFrom(serverNameSchema, { required: true }), trigger: ["input", "blur"] }],
    ip: [{ ...ruleFrom(serverHostSchema, { required: true }), trigger: ["input", "blur"] }],
    port: [
      {
        type: "number",
        ...ruleFrom(serverPortSchema, { required: true }),
        trigger: ["input", "blur"],
      },
    ],
    sshUser: [{ ...ruleFrom(serverUserSchema, { required: true }), trigger: ["input", "blur"] }],
    keyId: [
      {
        ...ruleFrom(requiredField(serverMessages.editKeyIdRequired), {
          when: () => isKey,
          required: isKey,
        }),
        trigger: ["input", "blur"],
      },
    ],
  };
}
