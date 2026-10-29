export function extractComposeServiceNames(content: string): string[] {
  return Object.keys(extractComposeServicePorts(content));
}

export function extractComposeServicePorts(content: string): Record<string, number> {
  const ports: Record<string, number> = {};
  const lines = content.split("\n");
  let inServices = false;
  let service = "";
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
    const target = /^-\s*target\s*:\s*(\d+)/.exec(trimmed);
    if (target) {
      if (ports[service] === 0) {
        ports[service] = Number(target[1]);
      }
      continue;
    }
    const short = /^-\s*"?([^"#\s]+)"?/.exec(trimmed);
    if (!short) {
      continue;
    }
    const parts = short[1].split("/")[0].split(":");
    const container = parts[parts.length - 1];
    if (!/^\d+$/.test(container)) {
      continue;
    }
    if (ports[service] === 0) {
      ports[service] = Number(container);
    }
  }
  return ports;
}
