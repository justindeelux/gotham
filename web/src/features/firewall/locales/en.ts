const en = {
  tab: "Firewall",
  title: "Firewall",
  description:
    "Open or close ports on this node. The rules are applied on the host itself; every other node keeps its own firewall.",
  status: {
    on: "On",
    off: "Off",
    backend: "Backend",
    enabled: "Firewall is on",
    disabled: "Firewall is off",
    accessHint:
      "SSH ({ssh}), the node agent ({agent}) and the control plane ({controlPlane}) stay allowed.",
    accessHintRemote:
      "SSH ({ssh}) and the node agent ({agent}) stay allowed. The control plane runs on another node.",
    turnOnHint:
      "Turning the firewall on keeps the access rules Gotham needs, so you cannot lock yourself out.",
    unsupportedTitle: "This node's firewall cannot be managed here",
    unsupportedBody:
      "Gotham manages ufw and firewalld. A node without either shows its state below.",
    install: "Install firewall",
    installing: "Installing the firewall…",
    installTitle: "Install a firewall?",
    installBody:
      "Gotham installs ufw on Debian/Ubuntu or firewalld on Fedora/RHEL, keeps SSH and Gotham's own ports open, then turns it on.",
    installConfirm: "Install",
    reload: "Reload",
  },
  rules: {
    title: "Rules",
    add: "Add rule",
    empty: "No firewall rules yet",
    emptyHint: "Add a rule to open or close a port.",
    any: "Any",
    enabled: "Enabled",
    disabled: "Disabled",
    required: "Required",
    protectedHint:
      "This rule keeps SSH, the node agent or the control-plane access open.",
    readOnly: "Managed on the node",
    readOnlyHint:
      "This rule is managed on the node and cannot be edited from Gotham.",
    edit: "Edit",
    delete: "Delete",
    deleteConfirm: "Delete this rule for {target}?",
    targetPort: "port {port}",
    targetAll: "all ports",
    columns: {
      action: "Action",
      direction: "Direction",
      protocol: "Protocol",
      port: "Port",
      source: "Address",
      comment: "Comment",
      status: "Status",
      actions: "Actions",
    },
    action: {
      allow: "Allow",
      deny: "Deny",
    },
    direction: {
      in: "In",
      out: "Out",
    },
    protocol: {
      tcp: "TCP",
      udp: "UDP",
      any: "Any",
    },
  },
  form: {
    addTitle: "Add firewall rule",
    editTitle: "Edit firewall rule",
    action: "Action",
    direction: "Direction",
    protocol: "Protocol",
    port: "Port or range",
    portPlaceholder: "22 or 8000-8100",
    portHint: "Leave empty for any port.",
    source: "Address",
    sourcePlaceholder: "10.0.0.0/8",
    sourceHint:
      "The source for inbound rules, the destination for outbound rules. Leave empty for any address.",
    comment: "Comment",
    commentPlaceholder: "Optional note",
    enabled: "Apply this rule",
    enabledHint: "A disabled rule is saved on the node but not applied.",
    save: "Save rule",
    cancel: "Cancel",
  },
  validation: {
    port: "Use a port from 1 to 65535, or a range like 8000-8100.",
    source: "Use an IP address or a CIDR range.",
    comment:
      "Use letters, numbers and . _ ( ) , : / + {'@'} - only, up to 200 characters.",
    commentDash: "A comment cannot start with a dash.",
    outboundSource: "Outbound rules cannot have a source address.",
  },
  toasts: {
    enabled: "Firewall turned on",
    disabled: "Firewall turned off",
    installed: "Firewall installed",
    saved: "Rule saved",
    deleted: "Rule deleted",
  },
  access: {
    ssh: "SSH",
    agent: "the node agent",
    control_plane: "the control plane",
  },
  errors: {
    details: "Details: {message}",
    load: "Could not load the firewall.",
    toggle: "Could not change the firewall state.",
    install: "Could not install a firewall.",
    save: "Could not save the rule.",
    delete: "Could not delete the rule.",
    no_firewall: "No supported firewall is installed on this node.",
    install_unsupported:
      "This distribution is not supported. Install ufw or firewalld on the node yourself.",
    install_failed:
      "The firewall could not be installed on this node. Nothing was changed; try again.",
    unsupported_backend:
      "This node uses {backend}. Gotham can detect it but cannot edit its rules yet; manage them on the node.",
    helper_missing:
      "The node's firewall helper did not answer. Update this node (run its Gotham update) and try again.",
    helper_failed:
      "The node's firewall helper failed; the change was not applied.",
    backend_changed:
      "The node's firewall backend changed. Reload the page.",
    state_read_failed:
      "The node could not read its saved firewall rules.",
    state_write_failed:
      "The change was applied, but the node could not save the rule state.",
    rule_not_found:
      "This rule no longer exists on the node. Reload the page.",
    server_not_found:
      "This server was not found. It may have been deleted.",
    read_only_rule:
      "This rule is managed on the node and cannot be changed from Gotham.",
    would_lock_out:
      "This change would block port {port}, which is needed for {access} access; keep an allow rule for that port.",
    invalid_action: "The action must be allow or deny.",
    invalid_direction: "The direction must be in or out.",
    invalid_protocol: "The protocol must be TCP, UDP or any.",
    invalid_port:
      "The port must be between 1 and 65535, or a range like 8000-8100.",
    invalid_source: "The address must be an IP address or a CIDR range.",
    invalid_outbound_source: "Outbound rules cannot have a source address.",
    invalid_comment:
      "The comment may only use letters, numbers and . _ ( ) , : / + {'@'} - , up to 200 characters.",
    invalid_rule:
      "The rule could not be read. Reload the page and try again.",
    invalid_request: "The request was not valid.",
    agent_unavailable:
      "The node agent did not answer. Check that the node is online and try again.",
    agent_outdated:
      "This node's agent is too old for firewall management. Update the agent and try again.",
  },
};

export default en;

export type FirewallMessages = typeof en;
