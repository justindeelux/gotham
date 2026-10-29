const en = {
  validation: {
    required: "This field is required.",
    pattern: "Does not match the required format.",
    wholeNumber: "Must be a whole number.",
    bool: "Must be true or false.",
    maxLength: "Must be at most {max} characters.",
    minNumber: "Must be at least {min}.",
    maxNumber: "Must be at most {max}.",
    selectOptions: "Must be one of: {options}.",
  },
  errors: {
    sessionExpired: "Your session expired. Please sign in again.",
    invalidValues: "Invalid template values. Check the highlighted fields.",
    invalidValuesWithDetail: "{detail}",
    notFound: "Template not found. The catalog may have changed — reload the page.",
    notFoundWithDetail: "{detail}",
    disabled: "Services are turned off on this control plane. Ask the owner to enable them.",
  },
  field: {
    optionalLabel: "{label} (optional)",
  },
  wizard: {
    title: "Deploy template {name}",
    steps: {
      configure: "Configure",
      configureDesc: "Fill the template fields",
      preview: "Compose preview",
      previewDesc: "Rendered from the schema",
      create: "Create",
      createDesc: "Name the service and pick a node",
    },
    counter: "Step {step} / 3",
    back: "Back",
    next: "Next",
    close: "Close",
    createAndDeploy: "Create and deploy",
  },
  secret: {
    generate: "Generate",
    generateLabel: "Generate {label}",
    copy: "Copy",
    copyLabel: "Copy {label}",
    copied: "{label} copied to clipboard.",
    copyFailed: "Could not copy {label}; select the value and copy it manually.",
    generated: "{label} generated.",
    copiedAnnouncement: "Copied.",
  },
  preview: {
    servicesOne: "{count} service",
    servicesOther: "{count} services",
    volumesOne: "{count} named volume",
    volumesOther: "{count} named volumes",
    domainsOne: "{count} domain route",
    domainsOther: "{count} domain routes",
    secretNote:
      "The engine only substitutes strings. A secret field stays a {ref} reference in this document; its value travels separately in the service environment and is redacted from errors and deploy history.",
    heldSeparately: "Held separately: {keys}.",
  },
  target: {
    nameLabel: "Service name",
    scopeError: "Select a project and environment.",
  },
  toast: {
    deploying: "Service {name} created. The deploy is running in the background.",
  },
  overlay: {
    n8n: {
      description:
        "n8n workflow automation with a persistent named volume and a routed domain.",
      fields: {
        domain: {
          help: "Optional. Leave empty to add a domain later from the service page.",
        },
        encryption_key: {
          help: "Encrypts stored credentials; use Generate for a strong value. Losing it locks the credentials.",
        },
        timezone: {
          help: "IANA timezone name used for schedules and the container clock.",
        },
        diagnostics: {
          help: "Allow n8n to send anonymous usage diagnostics.",
        },
        executions_retention_hours: {
          help: "Executions older than this are pruned; 336 hours is 14 days.",
        },
      },
    },
    nextcloud: {
      description:
        "Nextcloud with PostgreSQL and Redis, persistent named volumes and a routed domain.",
      fields: {
        domain: {
          help: "Required for the first install; the official image only reads its trusted-domain list then. Add more domains from the service page after the install.",
        },
        admin_password: {
          help: "The initial Nextcloud administrator password; use Generate for a strong value.",
        },
        redis_password: {
          help: "Nextcloud always ships with Redis; use Generate to protect the cache instance.",
        },
      },
    },
    "uptime-kuma": {
      description:
        "Uptime Kuma monitoring with a persistent named volume and a routed domain.",
      fields: {
        domain: {
          help: "Optional. Leave empty to add a domain later from the service page.",
        },
        tz: {
          help: "IANA timezone name used by the container clock and notifications.",
        },
      },
    },
    wordpress: {
      description:
        "WordPress with a MySQL database, persistent named volumes and a routed domain.",
      fields: {
        domain: {
          help: "Optional. Leave empty to add a domain later from the service page.",
        },
        db_password: {
          help: "Used by WordPress and MySQL; use Generate for a strong value.",
        },
      },
    },
  },
};

export default en;
