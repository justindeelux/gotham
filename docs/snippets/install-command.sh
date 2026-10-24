# region plain
curl -fsSL https://gotham.dev/install.sh | sudo sh
# endregion plain

# region unattended
curl -fsSL https://gotham.dev/install.sh | sudo \
  GOTHAM_ADMIN_EMAIL=ops@example.com \
  GOTHAM_ADMIN_PASSWORD_FILE=/root/gotham-admin-password \
  sh
# endregion unattended

# region dry-run
curl -fsSL https://gotham.dev/install.sh | sudo sh -s -- --dry-run
# endregion dry-run
