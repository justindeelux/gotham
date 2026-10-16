// Two-factor profile dialogs (JUS-196 fix round 1): the one-time recovery codes
// are not dismissable until acknowledged, both dialogs offer a .txt download,
// and enabling the second factor refreshes the card (emits done) and toasts.
/* global HTMLAnchorElement, Blob, setTimeout: readonly */
import { NMessageProvider, NModal } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { defineComponent, h } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import type { VueWrapper } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/profile/api/totp", () => ({
  beginTotpEnrollment: vi.fn(),
  enableTotp: vi.fn(),
  disableTotp: vi.fn(),
  regenerateTotpRecoveryCodes: vi.fn(),
  fetchTotpStatus: vi.fn(),
}));

import {
  beginTotpEnrollment,
  disableTotp,
  enableTotp,
  regenerateTotpRecoveryCodes,
} from "@/features/profile/api/totp";
import TotpSetupDialog from "@/features/profile/components/TotpSetupDialog.vue";
import TotpConfirmDialog from "@/features/profile/components/TotpConfirmDialog.vue";
import { useAuthStore } from "@/features/auth";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const createObjectURL = vi.fn(() => "blob:mock");
const revokeObjectURL = vi.fn();

beforeEach(() => {
  setActivePinia(createPinia());
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  vi.clearAllMocks();
  // jsdom has no Blob object URLs; supply them for the download path.
  Object.defineProperty(globalThis.URL, "createObjectURL", {
    value: createObjectURL,
    configurable: true,
    writable: true,
  });
  Object.defineProperty(globalThis.URL, "revokeObjectURL", {
    value: revokeObjectURL,
    configurable: true,
    writable: true,
  });
  vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
  useAuthStore().user = {
    id: "u-1",
    email: "ada@gotham.dev",
    created_at: "2026-01-01T00:00:00Z",
    has_password: true,
  };
  vi.mocked(beginTotpEnrollment).mockResolvedValue({
    secret: "JBSWY3DPEHPK3PXP",
    otpauth_url: "otpauth://totp/Gotham:ada?secret=JBSWY3DPEHPK3PXP",
  });
  vi.mocked(enableTotp).mockResolvedValue(["ABCDE-FGHJK", "LMNPQ-RSTUV"]);
  vi.mocked(regenerateTotpRecoveryCodes).mockResolvedValue(["NEW-CODE-01"]);
  vi.mocked(disableTotp).mockResolvedValue(undefined);
});

/** openDialog mounts a dialog behind the message provider, then opens it. */
async function openDialog(
  child: object,
  props: Record<string, unknown> = {},
): Promise<VueWrapper> {
  const host = defineComponent({
    props: { show: { type: Boolean, default: false } },
    render() {
      return h(NMessageProvider, null, {
        default: () => h(child as never, { show: this.show, ...props }),
      });
    },
  });
  const wrapper = mount(host, {
    global: { plugins: [i18n], stubs: { teleport: true } },
  });
  await wrapper.setProps({ show: true });
  await flushPromises();
  return wrapper.findComponent(child as never);
}

function buttonByText(wrapper: VueWrapper, text: string) {
  const button = wrapper
    .findAll("button")
    .find((candidate) => candidate.text().includes(text));
  expect(button, `expected a button containing ${text}`).toBeDefined();
  return button!;
}

describe("TotpSetupDialog", () => {
  it("shows the codes with the modal locked, then closes on acknowledge", async () => {
    const dialog = await openDialog(TotpSetupDialog);
    // The scan step is dismissable.
    expect(dialog.findComponent(NModal).props("closable")).toBe(true);
    expect(dialog.findComponent(NModal).props("maskClosable")).toBe(true);

    await dialog.find("input#totp-enroll-password").setValue("password123");
    await dialog.find("input#totp-enroll-code").setValue("123456");
    await buttonByText(dialog, "Turn on").trigger("click");
    await flushPromises();

    // Enabling refreshed the card and the codes are on screen.
    expect(dialog.emitted("done")).toBeTruthy();
    expect(dialog.text()).toContain("ABCDE-FGHJK");
    const modal = dialog.findComponent(NModal);
    // Every dismiss path is off while the one-time codes are unacknowledged,
    // including the header close "X".
    expect(modal.props("closable")).toBe(false);
    expect(modal.props("maskClosable")).toBe(false);
    expect(modal.props("closeOnEsc")).toBe(false);

    await buttonByText(dialog, "I saved them").trigger("click");
    expect(dialog.emitted("update:show")).toContainEqual([false]);
  });

  it("downloads the codes as a text file", async () => {
    const dialog = await openDialog(TotpSetupDialog);
    await dialog.find("input#totp-enroll-password").setValue("password123");
    await dialog.find("input#totp-enroll-code").setValue("123456");
    await buttonByText(dialog, "Turn on").trigger("click");
    await flushPromises();

    await buttonByText(dialog, "Download .txt").trigger("click");
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(createObjectURL).toHaveBeenCalledTimes(1);
    expect(createObjectURL.mock.calls[0]![0]).toBeInstanceOf(Blob);
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:mock");
  });

  it("cancels before enabling without emitting done", async () => {
    const dialog = await openDialog(TotpSetupDialog);
    await buttonByText(dialog, "Cancel").trigger("click");
    expect(dialog.emitted("update:show")).toContainEqual([false]);
    expect(dialog.emitted("done")).toBeFalsy();
  });
});

describe("TotpConfirmDialog", () => {
  it("locks the modal while the regenerated codes are shown", async () => {
    const dialog = await openDialog(TotpConfirmDialog, { mode: "regenerate" });
    expect(dialog.findComponent(NModal).props("closable")).toBe(true);
    expect(dialog.findComponent(NModal).props("maskClosable")).toBe(true);

    await dialog.find("input#totp-confirm-password").setValue("password123");
    await dialog.find("input#totp-confirm-code").setValue("123456");
    await buttonByText(dialog, "Regenerate").trigger("click");
    await flushPromises();

    expect(dialog.text()).toContain("NEW-CODE-01");
    const modal = dialog.findComponent(NModal);
    expect(modal.props("closable")).toBe(false);
    expect(modal.props("maskClosable")).toBe(false);
    expect(modal.props("closeOnEsc")).toBe(false);

    await buttonByText(dialog, "I saved them").trigger("click");
    expect(dialog.emitted("update:show")).toContainEqual([false]);
    expect(dialog.emitted("done")).toBeTruthy();
  });

  it("disables two-factor and reports the change", async () => {
    const dialog = await openDialog(TotpConfirmDialog, { mode: "disable" });
    await dialog.find("input#totp-confirm-password").setValue("password123");
    await dialog.find("input#totp-confirm-code").setValue("123456");
    await buttonByText(dialog, "Turn off").trigger("click");
    await flushPromises();

    expect(disableTotp).toHaveBeenCalledTimes(1);
    expect(dialog.emitted("done")).toBeTruthy();
    expect(dialog.emitted("update:show")).toContainEqual([false]);
  });
});
