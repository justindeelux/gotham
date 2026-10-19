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
 *
 * Only list entries under `ports:`/`expose:` count: an unrelated list under a
 * service (volumes, command, environment) must never be mistaken for a port.
 * Interpolated or ranged entries that cannot yield one concrete number are
 * skipped rather than guessed.
 */
export function extractComposeServicePorts(content: string): Record<string, number> {
  const ports: Record<string, number> = {};
  const lines = content.split("\n");
  let inServices = false;
  let service = "";
  // section is the service-level key currently being read (ports/expose) and
  // sectionIndent its indentation; anything else disables port parsing until
  // the next service-level key.
  let section = "";
  let sectionIndent = 0;
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
    // A following top-level mapping ends the services block.
    if (indent === 0) {
      break;
    }
    const keyMatch = /^([A-Za-z0-9][A-Za-z0-9_.-]*)\s*:/.exec(trimmed);
    if (keyMatch && indent === 2) {
      service = keyMatch[1];
      section = "";
      if (!(service in ports)) {
        ports[service] = 0;
      }
      continue;
    }
    if (service === "") {
      continue;
    }
    if (keyMatch && indent === 4) {
      section = keyMatch[1] === "ports" || keyMatch[1] === "expose" ? keyMatch[1] : "";
      sectionIndent = indent;
      continue;
    }
    if (section === "" || indent <= sectionIndent) {
      continue;
    }
    // Long syntax: "- target: 8000".
    const target = /^-\s*target\s*:\s*(\d+)/.exec(trimmed);
    if (target) {
      if (ports[service] === 0) {
        ports[service] = Number(target[1]);
      }
      continue;
    }
    // Short syntax: "80", "8080:80", "[::1]:8080:80".
    const short = /^-\s*"?([^"#\s]+)"?/.exec(trimmed);
    if (!short) {
      continue;
    }
    // Drop a protocol suffix and take the container part.
    const parts = short[1].split("/")[0].split(":");
    const container = parts[parts.length - 1];
    if (!/^\d+$/.test(container)) {
      // A container range ("3000-3005") or an interpolated value is not a
      // single target port.
      continue;
    }
    if (ports[service] === 0) {
      ports[service] = Number(container);
    }
  }
  return ports;
}
