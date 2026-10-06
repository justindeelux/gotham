/**
 * English catalog for the templates surface (I18N-7). Display copy only:
 * template slugs, field keys, rendered compose, secret values, domains and
 * API payloads stay raw values interpolated as parameters. Curated bundled
 * template descriptions and field help live under `overlay`, keyed by the
 * stable template slug; unknown or operator-provided metadata falls back
 * to the provider copy.
 */
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
    disabled: "Services are disabled on the control plane (FEATURE_SERVICES=false).",
  },
  gallery: {
    empty: "No templates in the catalog.",
  },
  page: {
    eyebrow: "Operations · one-click templates",
    title: "Template library",
    description:
      "Pick a template, fill the form, and the control plane renders the compose document before deploying it through the node agent. The form is generated from the template schema, so a new template needs no UI change.",
    calloutTitle: "How a template is structured",
    calloutYaml:
      "{file} declares the name, icon, description and the form fields (type, default, required, validation).",
    calloutComposeUses: "{file} uses {placeholder} placeholders.",
    calloutComposeNote:
      "The engine only substitutes strings and never executes code, so a template cannot run commands on a node. Editing a template does not affect a service that is already deployed.",
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
    create: "Create service",
    deploy: "Deploy now",
  },
  configure: {
    description:
      "Fields come from the template schema ({endpoint}); adding a template does not require a UI change. Secret values stay in this form until they are sent as the service environment.",
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
    description:
      "Creating stores the rendered document and its environment ({endpoint}); a service runs on exactly one node. Deploy sends the project to that node's agent and shows what the agent reports back.",
  },
  created: {
    title: "{name} created",
    description:
      "The service row exists; nothing runs yet. Deploy renders the document again on the node and records one deploy row.",
    open: "Open service detail",
    deployed: "deployed",
    stubTitle: "Deploy step timeline — backend pending",
    stubBody:
      "The API records one row per deploy (state, error, timestamps) and exposes no per-step progress, so no step timeline is shown here. The detail page lists the history instead.",
  },
  toast: {
    created: "Service {name} created.",
    deployed: "Deploy finished.",
  },
  overlay: {
    n8n: {
      description:
        "n8n workflow automation with a persistent named volume and a routed domain.",
      fields: {
        domain: {
          help: "The public host served by the node's Traefik proxy.",
        },
        encryption_key: {
          help: "Encrypts stored credentials; keep it safe, losing it locks the credentials.",
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
          help: "The public host served by the node's Traefik proxy.",
        },
        admin_password: {
          help: "The initial Nextcloud administrator password.",
        },
        redis_password: {
          help: "Nextcloud always ships with Redis; this protects the cache instance.",
        },
      },
    },
    "uptime-kuma": {
      description:
        "Uptime Kuma monitoring with a persistent named volume and a routed domain.",
      fields: {
        domain: {
          help: "The public host served by the node's Traefik proxy.",
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
          help: "The public host served by the node's Traefik proxy.",
        },
        db_password: {
          help: "Used by WordPress and MySQL; choose a strong value.",
        },
      },
    },
  },
};

export default en;
