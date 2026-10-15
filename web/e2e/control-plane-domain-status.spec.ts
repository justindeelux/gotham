import { expect, test } from "./fixtures";
import { storageStatePath } from "./support";

test.use({ storageState: storageStatePath });

test("URL status follows edits and warns when localhost is missing", async ({ page }) => {
  const field = (value: string) => ({ value, source: "db", locked: false });
  const settings = {
    general: {
      control_plane_url: field("https://panel.example.com"),
      instance_name: field("Gotham"), timezone: field("UTC"),
    },
    network: {
      dns_servers: ["1.1.1.1"],
      ipv4: { mode: "dhcp", address: "", gateway: "" },
      ipv6: { enabled: false, mode: "dhcp", address: "", gateway: "" },
    },
    system: { hostname: "gotham", ntp_enabled: true, ntp_servers: [] },
    capabilities: { network: false, system: false }, pending: null,
  };
  await page.route("**/api/v1/instance/settings", (route) =>
    route.fulfill({ json: { settings } }),
  );
  let missingLocalhost = false;
  await page.route("**/api/v1/proxy/routers*", (route) => route.fulfill({ json: {
    routers: [{
      host: "panel.example.com", kind: "control_plane", server_id: "local",
      rule: "Host(`panel.example.com`)", service: "gotham-panel", owner_id: "",
      entrypoints: ["web", "websecure"],
    }],
    nodes: missingLocalhost ? [] : [{
      server_id: "local", server_name: "localhost", sync_status: "synced",
    }],
  } }));
  await page.goto("/settings/instance");
  const status = page.getByRole("status", { name: "Control-plane domain status" });
  await expect(status).toContainText("Router: synced");
  await expect(status).toContainText("DNS: not reported");
  await expect(status).toContainText("Certificate: not reported");
  await page.getByPlaceholder("https://gotham.example.com").fill("http://new.example.com");
  await expect(status).toContainText("Router: sync pending");
  await expect(status).toContainText("Save the URL");
  await expect(status).toContainText("Certificate: not applicable (HTTP)");
  missingLocalhost = true;
  await status.getByRole("button", { name: "Refresh status" }).click();
  await expect(status).toContainText("The localhost node is not registered.");
  await expect(status).not.toContainText("Router: synced");
});
