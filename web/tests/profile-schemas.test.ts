// Profile schemas (JUS-27): catalog byte-exactness, field behaviour,
// rule builders, required marks, and API envelope shapes.
import { mount } from "@vue/test-utils";
import type { FormRules } from "naive-ui";
import { NForm, NFormItem, NInput } from "naive-ui";
import { beforeEach, describe, expect, it } from "vitest";
import { h } from "vue";

import { authMessages } from "@/features/auth/schemas/auth";
import {
  changePasswordRules,
  currentPasswordSchema,
  displayNameFieldSchema,
  displayNameRules,
  meEnvelopeSchema,
  passwordChangeEnvelopeSchema,
  profileMessages,
} from "@/features/profile/schemas/profile";
import { parseWith } from "@/shared/validation/parse";
import {
  registerDiscoveredCatalogs,
  resetLocaleState,
  resolveValidationMessage,
  syncComposerLocale,
} from "@/shared/i18n";

// Schema messages are keys resolved at display time; registration plus the
// default locale keeps every display assertion on the original English text.
beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
});

describe("profileMessages are message keys with byte-exact English display", () => {
  it("pins every catalog key and its resolved English text", () => {
    expect(profileMessages.displayNameLength).toBe(
      "profile.validation.displayNameLength",
    );
    expect(profileMessages.currentPasswordRequired).toBe(
      "profile.validation.currentPasswordRequired",
    );
    // Behavior preserved: the keys resolve to the exact previous strings in
    // the default locale, so visible feedback is unchanged in English.
    expect(resolveValidationMessage(profileMessages.displayNameLength)).toBe(
      "Display name must be 1-64 characters",
    );
    expect(
      resolveValidationMessage(profileMessages.currentPasswordRequired),
    ).toBe("Current password is required");
  });
});

describe("displayNameFieldSchema", () => {
  it("accepts blank input (clears the name)", () => {
    expect(displayNameFieldSchema.safeParse("").success).toBe(true);
    expect(displayNameFieldSchema.safeParse("   ").success).toBe(true);
  });

  it("accepts 1-64 characters after trimming", () => {
    expect(displayNameFieldSchema.safeParse("Ada").success).toBe(true);
    expect(displayNameFieldSchema.safeParse("  Ada  ").success).toBe(true);
    expect(displayNameFieldSchema.safeParse("x".repeat(64)).success).toBe(true);
  });

  it("rejects 65+ characters with the catalog message", () => {
    const result = displayNameFieldSchema.safeParse("x".repeat(65));
    expect(result.success).toBe(false);
    if (!result.success) {
      expect(result.error.issues[0]?.message).toBe(
        profileMessages.displayNameLength,
      );
      expect(resolveValidationMessage(result.error.issues[0]?.message ?? "")).toBe(
        "Display name must be 1-64 characters",
      );
    }
  });

  it("rejects absent values with the catalog message, never a zod default", () => {
    for (const value of [undefined, null]) {
      const result = displayNameFieldSchema.safeParse(value);
      expect(result.success).toBe(false);
      if (!result.success) {
        expect(result.error.issues[0]?.message).toBe(
          profileMessages.displayNameLength,
        );
        expect(resolveValidationMessage(result.error.issues[0]?.message ?? "")).toBe(
          "Display name must be 1-64 characters",
        );
      }
    }
  });
});

describe("currentPasswordSchema", () => {
  it("rejects blank input with the catalog message", () => {
    const result = currentPasswordSchema.safeParse("");
    expect(result.success).toBe(false);
    if (!result.success) {
      expect(result.error.issues[0]?.message).toBe(
        profileMessages.currentPasswordRequired,
      );
      expect(resolveValidationMessage(result.error.issues[0]?.message ?? "")).toBe(
        "Current password is required",
      );
    }
  });

  it("is required-only: whitespace passes, as on login", () => {
    expect(currentPasswordSchema.safeParse(" ").success).toBe(true);
    expect(currentPasswordSchema.safeParse("secret").success).toBe(true);
  });
});

describe("password rules are reused from auth, not duplicated", () => {
  it("keeps the register policy message for the new password", () => {
    expect(authMessages.passwordPolicy).toBe("auth.validation.passwordPolicy");
    // Behavior preserved: the key resolves to the exact previous string in
    // the default locale.
    expect(resolveValidationMessage(authMessages.passwordPolicy)).toBe(
      "Use at least 10 characters with 2 character classes (lowercase, uppercase, digits, symbols)",
    );
  });

  it("changePasswordRules omits the current entry without a password", () => {
    const rules = changePasswordRules(false, () => "");
    expect("currentPassword" in rules).toBe(false);
    expect("newPassword" in rules).toBe(true);
    expect("confirmPassword" in rules).toBe(true);
  });

  it("changePasswordRules includes the current entry with a password", () => {
    const rules = changePasswordRules(true, () => "");
    expect("currentPassword" in rules).toBe(true);
  });
});

describe("profile required marks", () => {
  function mountFields(
    model: Record<string, unknown>,
    rules: FormRules,
    fields: string[],
  ): ReturnType<typeof mount> {
    return mount(
      {
        render: () =>
          h(NForm, { model, rules }, () =>
            fields.map((path) =>
              h(NFormItem, { label: path, path, key: path }, () =>
                h(NInput, { value: "" }),
              ),
            ),
          ),
      },
      { global: { stubs: { transition: false } } },
    );
  }

  function fieldMarks(
    wrapper: ReturnType<typeof mount>,
    count: number,
  ): boolean[] {
    const items = wrapper.findAll(".n-form-item");
    expect(items.length).toBe(count);
    return items.map((item) =>
      item.find(".n-form-item-label__asterisk").exists(),
    );
  }

  it("leaves the optional display-name field unmarked", () => {
    const wrapper = mountFields({ displayName: "" }, displayNameRules(), [
      "displayName",
    ]);
    expect(fieldMarks(wrapper, 1)).toEqual([false]);
    wrapper.unmount();
  });

  it("marks every change-password field required", () => {
    const model = { currentPassword: "", newPassword: "", confirmPassword: "" };
    const wrapper = mountFields(model, changePasswordRules(true, () => ""), [
      "currentPassword",
      "newPassword",
      "confirmPassword",
    ]);
    expect(fieldMarks(wrapper, 3)).toEqual([true, true, true]);
    wrapper.unmount();
  });
});

describe("profile API envelopes", () => {
  const user = {
    id: "u-1",
    email: "ada@gotham.dev",
    created_at: "2026-01-01T00:00:00Z",
    display_name: "Ada",
    has_password: true,
    is_platform_admin: false,
  };

  it("accepts the GET/PATCH /me envelope strictly", () => {
    expect(() =>
      parseWith(meEnvelopeSchema, { user }, { strict: true }),
    ).not.toThrow();
  });

  it("accepts the POST /me/password AuthResult strictly", () => {
    const payload = {
      user,
      access_token: "a",
      token_type: "Bearer",
      expires_in: 900,
      refresh_token: "r",
    };
    expect(() =>
      parseWith(passwordChangeEnvelopeSchema, payload, { strict: true }),
    ).not.toThrow();
  });
});
