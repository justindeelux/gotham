export function buildInstallCommand(enableFileManager: boolean): string {
  const fileManagerFlag = enableFileManager ? "--file-manager" : "--no-file-manager";
  return [
    "scp root@<cp-host>:/var/lib/gotham/ca/ca.crt .",
    "git clone --depth 1 https://github.com/justindeelux/gotham /tmp/gotham",
    "sudo GOTHAM_AGENT_CP_ADDR=<cp-host>:9442 GOTHAM_AGENT_NODE_ID=<node> \\",
    `  /tmp/gotham/deploy/install-agent.sh --ca ./ca.crt --full ${fileManagerFlag}`,
    "",
    "# --full installs Docker Engine + the compose plugin (Ubuntu/Debian).",
    "# install.sh already adds a localhost agent by default (--no-local-agent",
    "# opts out); use this snippet for every further node.",
    "# The installer verifies the signed manifest + digest, writes",
    "# /etc/gotham/ca.crt, then enables the systemd unit.",
    "systemctl status gotham-agent",
  ].join("\n");
}
