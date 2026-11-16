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
      "Use letters, numbers and . _ ( ) , : / + @ - only, up to 200 characters.",
    commentDash: "A comment cannot start with a dash.",
    outboundSource: "Outbound rules cannot have a source address.",
  },
  toasts: {
    enabled: "Firewall turned on",
    disabled: "Firewall turned off",
    saved: "Rule saved",
    deleted: "Rule deleted",
  },
  errors: {
    load: "Could not load the firewall.",
    toggle: "Could not change the firewall state.",
    save: "Could not save the rule.",
    delete: "Could not delete the rule.",
  },
};

export default en;
