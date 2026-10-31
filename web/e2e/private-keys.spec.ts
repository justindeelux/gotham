import type { APIRequestContext, Page } from "@playwright/test";

import { expect, test } from "./fixtures";
import {
  loadAccount,
  storageStatePath,
  uniqueSuffix,
} from "./support";

test.use({
  storageState: storageStatePath,
  viewport: { width: 1366, height: 900 },
  permissions: ["clipboard-read", "clipboard-write"],
});

interface KeyRow {
  id: string;
  name: string;
}

function authHeaders(): Record<string, string> {
  return { Authorization: `Bearer ${loadAccount().accessToken}` };
}

async function isOwner(request: APIRequestContext): Promise<boolean> {
  const response = await request.get("/api/v1/auth/me", {
    headers: authHeaders(),
  });
  if (!response.ok()) {
    return false;
  }
  const body = (await response.json()) as { user?: { role?: string } };
  return body.user?.role === "owner";
}

async function openPrivateKeys(page: Page): Promise<void> {
  await page.goto("/dashboard");
  await page.getByRole("link", { name: "Private keys" }).click();
  await expect(page).toHaveURL(/\/security\/private-keys$/);
  await expect(
    page.getByRole("heading", { name: "Private keys", level: 1 }),
  ).toBeVisible();
}

function keyRow(page: Page, name: string) {
  return page
    .locator('[data-testid="private-keys-table"] tbody tr')
    .filter({ hasText: name });
}

async function confirmPopover(page: Page, action: string): Promise<void> {
  const popover = page.locator(".n-popconfirm").last();
  await expect(popover).toBeVisible();
  await popover.getByRole("button", { name: action, exact: true }).click();
}

test.afterEach(async ({ request }) => {
  const response = await request.get("/api/v1/private-keys", {
    headers: authHeaders(),
  });
  if (!response.ok()) {
    return;
  }
  const body = (await response.json()) as { private_keys?: KeyRow[] };
  for (const key of body.private_keys ?? []) {
    if (!key.name.startsWith("ui-e2e-key-")) {
      continue;
    }
    await request.delete(`/api/v1/private-keys/${key.id}`, {
      headers: authHeaders(),
    });
  }
});

test("creates, copies, reveals, edits and deletes a private key", async ({
  page,
  request,
  guardrails,
}) => {
  test.skip(!(await isOwner(request)), "the smoke account is not the instance owner");

  const name = `ui-e2e-key-${uniqueSuffix()}`;
  const description = `edited-${uniqueSuffix()}`;
  await openPrivateKeys(page);

  const createButton = page.getByTestId("private-keys-create");
  await createButton.click();
  let dialog = page.locator(".n-modal").last();
  const prefill = await dialog.locator("#private-key-name").inputValue();
  expect(prefill).toMatch(/^[a-z]+-[a-z0-9]{12}$/);
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await expect(dialog).toBeHidden();

  await createButton.click();
  dialog = page.locator(".n-modal").last();
  const secondPrefill = await dialog.locator("#private-key-name").inputValue();
  expect(secondPrefill).toMatch(/^[a-z]+-[a-z0-9]{12}$/);
  expect(secondPrefill).not.toBe(prefill);
  await dialog.locator("#private-key-name").fill(name);
  await dialog.getByTestId("private-keys-submit").click();
  await expect(dialog).toBeHidden();

  const row = keyRow(page, name);
  await expect(row).toBeVisible();
  await expect(row).toContainText("ed25519");
  await expect(
    page.locator('[data-testid="private-keys-table"]'),
  ).not.toContainText("BEGIN OPENSSH PRIVATE KEY");

  await row.getByTestId("private-key-copy-public").click();
  const publicKey = await page.evaluate(() => navigator.clipboard.readText());
  expect(publicKey).toMatch(/^ssh-ed25519 /);

  await row.getByTestId("private-key-reveal").click();
  await confirmPopover(page, "Reveal");
  const secret = page.locator("#private-key-secret");
  await expect(secret).toBeVisible();
  const privateKey = await secret.inputValue();
  expect(privateKey).toContain("BEGIN OPENSSH PRIVATE KEY");

  await page.getByTestId("private-keys-reveal-copy").click();
  const copiedPrivateKey = await page.evaluate(() => navigator.clipboard.readText());
  expect(copiedPrivateKey).toBe(privateKey);
  await page.getByTestId("private-keys-reveal-done").click();
  await expect(secret).toBeHidden();
  expect(
    await page.locator('[data-testid="private-keys-table"]').innerText(),
  ).not.toContain("BEGIN OPENSSH PRIVATE KEY");

  await row.getByTestId("private-key-edit").click();
  const editDialog = page.locator(".n-modal").last();
  await editDialog.locator("#private-key-edit-description").fill(description);
  await editDialog.getByTestId("private-keys-edit-save").click();
  await expect(editDialog).toBeHidden();
  await expect(row).toContainText(description);

  await row.getByTestId("private-key-delete").click();
  await confirmPopover(page, "Delete");
  await expect(row).toHaveCount(0);

  expect(
    guardrails.apiFailures,
    `failed API requests:\n${guardrails.apiFailures.join("\n")}`,
  ).toEqual([]);
});
