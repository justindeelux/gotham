/**
 * Compose helpers shared by the wizard Source step and the detail editor.
 * extractComposeServiceNames is a best-effort suggestion scan, not a YAML
 * parser: it collects the two-space-indented keys under a top-level
 * `services:` mapping (the overwhelmingly common compose style) so the web
 * service field can offer them. Anything unusual falls back to free text;
 * the server enforces membership either way.
 */
export function extractComposeServiceNames(content: string): string[] {
  return Object.keys(extractComposeServicePorts(content));
}

/**
 * extractComposeServicePorts returns each compose service's first declared
 * container port, best effort. The Domains tab uses it to pre-fill a domain's
 * port when the operator picks a service; the field stays editable and the
 * server re-validates the document.
 */
export function extractComposeServicePorts(content: string): Record<string, number> {
  const ports: Record<string, number> = {};
  const lines = content.split("\n");
  let inServices = false;
  let service = "";
  for (const line of lines) {
    const trimmed = line.trim();
    if (trimmed === "" || trimmed.startsWith("#")) {
      continue;
    }
    const indent = line.length - line.trimStart().length;
    if (!inServices) {
      if (indent === 0 && /^services\s*:\s*(#.*)?$/.test(trimmed)) {
        inServices = true;
      }
      continue;
    }
    // A following top-level mapping ends the services block; deeper keys
    // belong to a service body and are skipped.
    if (indent === 0) {
      break;
    }
    const keyMatch = /^([A-Za-z0-9][A-Za-z0-9_.-]*)\s*:/.exec(trimmed);
    if (keyMatch && indent === 2) {
      service = keyMatch[1];
      if (!(service in ports)) {
        ports[service] = 0;
      }
      continue;
    }
    if (service === "" || indent < 4) {
      continue;
    }
    // Short syntax ("8080:80", "80", "127.0.0.1:8080:80") or long syntax
    // target: 80. Only the container port is kept.
    const target = /^(?:-\s*)?target\s*:\s*(\d+)/.exec(trimmed);
    if (target) {
      if (ports[service] === 0) {
        ports[service] = Number(target[1]);
      }
      continue;
    }
    const short = /^-\s*"?([^"#\s]+)"?/.exec(trimmed);
    if (short) {
      const parts = short[1].split("/")[0].split(":");
      const port = Number(parts[parts.length - 1]);
      if (Number.isInteger(port) && port > 0 && ports[service] === 0) {
        ports[service] = port;
      }
    }
  }
  return ports;
}
