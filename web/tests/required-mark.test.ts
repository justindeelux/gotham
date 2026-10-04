// Require-mark (asterisk) contract for the zod migration (JUS-23 fix r1).
//
// Naive UI derives the red `*` label mark from
// `rules.some((rule) => rule.required)`; the schemas carry it via ruleFrom's
// `required` option (validation still goes through `validator`). These tests
// mount real NForm/NFormItem trees with the ACTUAL rule builders the pages
// use and assert asterisk presence per field and per authMode/keyMode,
// including unexpected mode values.

import { mount } from "@vue/test-utils";
import type { FormRules } from "naive-ui";
import { NForm, NFormItem, NInput } from "naive-ui";
import { describe, expect, it } from "vitest";
import { h } from "vue";

import { loginRules, registerRules } from "@/features/auth/schemas/auth";
import {
  connectionRules,
  editRules,
} from "@/features/servers/schemas/servers";

interface Field {
  path: string;
  label: string;
}

function mountFields(
  model: Record<string, unknown>,
  rules: FormRules,
  fields: Field[],
): ReturnType<typeof mount> {
  return mount(
    {
      render: () =>
        h(NForm, { model, rules }, () =>
          fields.map((field) =>
            h(
              NFormItem,
              { label: field.label, path: field.path, key: field.path },
              () => h(NInput, { value: "" }),
            ),
          ),
        ),
    },
    { global: { stubs: { transition: false } } },
  );
}

/** fieldMarks reports asterisk presence per field, in mount order. */
function fieldMarks(
  wrapper: ReturnType<typeof mount>,
  fields: Field[],
): boolean[] {
  const items = wrapper.findAll(".n-form-item");
  expect(items.length).toBe(fields.length);
  return items.map((item) =>
    item.find(".n-form-item-label__asterisk").exists(),
  );
}

describe("login require marks", () => {
  it("marks email and password", () => {
    const fields = [
      { path: "email", label: "Email" },
      { path: "password", label: "Password" },
    ];
    const wrapper = mountFields({ email: "", password: "" }, loginRules(), fields);
    expect(fieldMarks(wrapper, fields)).toEqual([true, true]);
  });
});

describe("register require marks", () => {
  it("marks email, password and confirm but not terms", () => {
    const fields = [
      { path: "email", label: "Email" },
      { path: "password", label: "Password" },
      { path: "confirmPassword", label: "Confirm password" },
      { path: "terms", label: "Terms" },
    ];
    const wrapper = mountFields(
      { email: "", password: "", confirmPassword: "", terms: false },
      registerRules(() => ""),
      fields,
    );
    expect(fieldMarks(wrapper, fields)).toEqual([true, true, true, false]);
  });
});

const wizardFields: Field[] = [
  { path: "name", label: "Node name" },
  { path: "ip", label: "IP address / hostname" },
  { path: "port", label: "SSH port" },
  { path: "sshUser", label: "SSH user" },
  { path: "keyName", label: "Key name" },
  { path: "privateKey", label: "Private key (PEM)" },
  { path: "keyId", label: "Key ID" },
  { path: "password", label: "Node password" },
];

const wizardModel = (): Record<string, unknown> => ({
  name: "",
  ip: "",
  port: 22,
  sshUser: "root",
  keyName: "",
  privateKey: "",
  keyId: "",
  password: "",
});

describe("wizard require marks follow authMode/keyMode", () => {
  const authModes = ["key", "password", "bogus"];
  const keyModes = ["new", "existing", "bogus"];
  for (const authMode of authModes) {
    for (const keyMode of keyModes) {
      const label = `authMode=${authMode} keyMode=${keyMode}`;
      it(`marks exactly the matching fields (${label})`, () => {
        const wrapper = mountFields(
          wizardModel(),
          connectionRules({ authMode, keyMode }),
          wizardFields,
        );
        expect(fieldMarks(wrapper, wizardFields), label).toEqual([
          true,
          true,
          true,
          true,
          authMode === "key" && keyMode === "new",
          authMode === "key" && keyMode === "new",
          authMode === "key" && keyMode === "existing",
          authMode === "password",
        ]);
      });
    }
  }
});

const editFields: Field[] = [
  { path: "name", label: "Node name" },
  { path: "ip", label: "IP address / hostname" },
  { path: "port", label: "SSH port" },
  { path: "sshUser", label: "SSH user" },
  { path: "keyId", label: "Key ID" },
];

describe("edit modal require marks follow the credential mode", () => {
  for (const authMode of ["keep", "key", "password", "bogus"]) {
    it(`marks keyId only for key mode (authMode=${authMode})`, () => {
      const wrapper = mountFields(
        { name: "", ip: "", port: 22, sshUser: "", keyId: "" },
        editRules(authMode),
        editFields,
      );
      expect(fieldMarks(wrapper, editFields)).toEqual([
        true,
        true,
        true,
        true,
        authMode === "key",
      ]);
    });
  }
});

describe("number-typed port rule still validates through the schema", () => {
  it("reports the schema message for a non-numeric port", async () => {
    const wrapper = mountFields(
      { ...wizardModel(), port: "22" },
      connectionRules({ authMode: "key", keyMode: "new" }),
      wizardFields,
    );
    const form = wrapper.findComponent(NForm);
    const errors = await form.vm
      .validate()
      .then(() => null, (caught: unknown) => caught);
    expect(JSON.stringify(errors)).toContain("Enter an SSH port (1-65535).");
  });

  it("reports the range message for an out-of-range port", async () => {
    const wrapper = mountFields(
      { ...wizardModel(), port: 0 },
      editRules("keep"),
      editFields,
    );
    const form = wrapper.findComponent(NForm);
    const errors = await form.vm
      .validate()
      .then(() => null, (caught: unknown) => caught);
    expect(JSON.stringify(errors)).toContain(
      "Port must be a number from 1 to 65535.",
    );
  });
});
