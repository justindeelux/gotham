import { expect, test } from "./fixtures";
import { storageStatePath } from "./support";

test.use({ storageState: storageStatePath });

test("URL status follows edits and warns when localhost is missing", async ({ page }) => {
  const field = (value: string) => ({ value, source: "db", locked: false });
  const settings = {
    general: {
      control_plane_url: field("https://panel.example.com"),
      instance_name: field("Gotham"),
    },
    pending_control_plane_url: null,
  };
  await page.route("**/api/v1/instance/settings", (route) =>
    route.fulfill({ json: { settings } }),
  );
  let missingLocalhost = false;
  let trial = false;
  let certificateStatus = "issued";
  await page.route("**/api/v1/proxy/routers*", (route) => route.fulfill({ json: {
    routers: [{
      host: trial ? "new.example.com" : "panel.example.com",
      kind: "control_plane", server_id: "local",
      rule: "Host(`panel.example.com`)", service: "gotham-panel", owner_id: "",
      entrypoints: ["web", "websecure"],
      certificate: { status: certificateStatus,
        ...(certificateStatus === "issued" ? { not_after: "2027-01-14T12:00:00Z" } : {}),
      },
    }],
    nodes: missingLocalhost ? [] : [{
      server_id: "local", server_name: "localhost", sync_status: "synced",
    }],
  } }));
  await page.goto("/settings/instance");
  const status = page.getByRole("status", { name: "Control-plane domain status" });
  await expect(status).toContainText("Router: synced");
  await expect(status).toContainText("DNS: not reported");
  await expect(status).toContainText("Certificate: issued");
  await expect(status).toContainText("Expires");
  for (const state of ["pending", "failed"]) {
    certificateStatus = state;
    await status.getByRole("button", { name: "Refresh status" }).click();
    await expect(status).toContainText(`Certificate: ${state}`);
    await expect(status).not.toContainText("Expires");
  }
  await page.getByPlaceholder("https://gotham.example.com").fill("http://new.example.com");
  await expect(status).toContainText("Router: sync pending");
  await expect(status).toContainText("Save the URL");
  await expect(status).toContainText("Certificate: not applicable (HTTP)");
  trial = true;
  certificateStatus = "pending";
  await page.getByPlaceholder("https://gotham.example.com").fill("https://new.example.com");
  await expect(status).toContainText("Router: synced");
  await expect(status).toContainText("Certificate: pending");
  await expect(status).not.toContainText("Save the URL");
  missingLocalhost = true;
  await status.getByRole("button", { name: "Refresh status" }).click();
  await expect(status).toContainText("The localhost node is not registered.");
  await expect(status).not.toContainText("Router: synced");
  await expect(status).toContainText("Certificate: not reported");
});
