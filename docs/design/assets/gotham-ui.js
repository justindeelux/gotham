/* ═══════════════════════════════════════════════════════════════════════════
 * Gotham — UI behaviour layer
 * Shared by every screen: app chrome, tabs, dialogs, wizards, log streaming,
 * metric charts, filters and state feedback. No framework, no network calls.
 * ═════════════════════════════════════════════════════════════════════════ */
(function () {
  "use strict";

  /* ── Icon sprite ─────────────────────────────────────────────────────── */
  const ICONS = {
    "i-grid": '<path d="M4 4h6v6H4zM14 4h6v6h-6zM4 14h6v6H4zM14 14h6v6h-6z"/>',
    "i-box": '<path d="M12 3l8 4.5v9L12 21l-8-4.5v-9L12 3zM4 7.5l8 4.5 8-4.5M12 12v9"/>',
    "i-layers": '<path d="M12 3l9 5-9 5-9-5 9-5zM3 13l9 5 9-5M3 17l9 5 9-5"/>',
    "i-db": '<ellipse cx="12" cy="6" rx="8" ry="3"/><path d="M4 6v12c0 1.7 3.6 3 8 3s8-1.3 8-3V6M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3"/>',
    "i-server": '<rect x="3" y="4" width="18" height="7" rx="2"/><rect x="3" y="13" width="18" height="7" rx="2"/><path d="M7 7.5h.01M7 16.5h.01"/>',
    "i-globe": '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c2.5 2.6 3.8 5.6 3.8 9S14.5 18.4 12 21c-2.5-2.6-3.8-5.6-3.8-9S9.5 5.6 12 3z"/>',
    "i-users": '<circle cx="9" cy="8" r="3.2"/><path d="M3 20c0-3.3 2.7-5.4 6-5.4s6 2.1 6 5.4M16 5.5a3.2 3.2 0 010 6M18 20c0-2.4-.6-4-1.6-5.2"/>',
    "i-bell": '<path d="M6 9a6 6 0 1112 0c0 5 2 6 2 6H4s2-1 2-6zM10 20a2 2 0 004 0"/>',
    "i-search": '<circle cx="11" cy="11" r="6"/><path d="M20 20l-4.3-4.3"/>',
    "i-plus": '<path d="M12 5v14M5 12h14"/>',
    "i-play": '<path d="M8 5.5l10 6.5-10 6.5z"/>',
    "i-stop": '<rect x="6.5" y="6.5" width="11" height="11" rx="1.5"/>',
    "i-restart": '<path d="M20 12a8 8 0 11-2.6-5.9M20 4v4h-4"/>',
    "i-doc": '<path d="M6 3h8l4 4v14H6zM14 3v4h4M9 12h6M9 16h4"/>',
    "i-gear": '<circle cx="12" cy="12" r="3"/><path d="M12 3v3M12 18v3M3 12h3M18 12h3M5.6 5.6l2.1 2.1M16.3 16.3l2.1 2.1M18.4 5.6l-2.1 2.1M7.7 16.3l-2.1 2.1"/>',
    "i-shield": '<path d="M12 3l8 3v6c0 5-3.4 8-8 9-4.6-1-8-4-8-9V6l8-3z"/><path d="M9 12l2 2 4-4"/>',
    "i-clock": '<circle cx="12" cy="12" r="9"/><path d="M12 7.5V12l3 2"/>',
    "i-check": '<path d="M5 12.5l4.5 4.5L19 7"/>',
    "i-x": '<path d="M6 6l12 12M18 6L6 18"/>',
    "i-alert": '<path d="M12 4l9 16H3l9-16zM12 10v4M12 17h.01"/>',
    "i-info": '<circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 8h.01"/>',
    "i-branch": '<circle cx="6" cy="6" r="2.4"/><circle cx="6" cy="18" r="2.4"/><circle cx="18" cy="10" r="2.4"/><path d="M6 8.4v7.2M8.4 6h5.2c2.4 0 4.4 1.6 4.4 4v0"/>',
    "i-terminal": '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M7 9l3 3-3 3M13 15h4"/>',
    "i-copy": '<rect x="9" y="9" width="11" height="11" rx="2"/><path d="M15 5.5A2.5 2.5 0 0012.5 3H6a2 2 0 00-2 2v6.5A2.5 2.5 0 006.5 14"/>',
    "i-external": '<path d="M14 4h6v6M20 4l-8 8M18 14v5a1 1 0 01-1 1H5a1 1 0 01-1-1V7a1 1 0 011-1h5"/>',
    "i-trash": '<path d="M4 7h16M9 7V5h6v2M6 7l1 13h10l1-13M10 11v6M14 11v6"/>',
    "i-refresh": '<path d="M4 12a8 8 0 0113.7-5.6M20 12a8 8 0 01-13.7 5.6M20 4v4h-4M4 20v-4h4"/>',
    "i-chevron-right": '<path d="M9 6l6 6-6 6"/>',
    "i-chevron-down": '<path d="M6 9l6 6 6-6"/>',
    "i-download": '<path d="M12 4v11M8 11l4 4 4-4M4 20h16"/>',
    "i-lock": '<rect x="5" y="10" width="14" height="11" rx="2"/><path d="M8.5 10V7.5a3.5 3.5 0 017 0V10"/>',
    "i-cloud": '<path d="M7 18h10a4 4 0 000-8 6 6 0 00-11.6 1.6A3.6 3.6 0 007 18z"/>',
    "i-activity": '<path d="M3 12h4l2.5-6 4 13L16 12h5"/>',
    "i-cpu": '<rect x="7" y="7" width="10" height="10" rx="2"/><path d="M4 10h3M4 14h3M17 10h3M17 14h3M10 4v3M14 4v3M10 17v3M14 17v3"/>',
    "i-drive": '<rect x="3" y="5" width="18" height="6" rx="2"/><rect x="3" y="13" width="18" height="6" rx="2"/><path d="M7 8h.01M7 16h.01"/>',
    "i-wifi": '<path d="M4 9a12 12 0 0116 0M7 12.5a8 8 0 0110 0M10 16a4 4 0 014 0M12 19h.01"/>',
    "i-chat": '<path d="M4 5h16v11H9l-5 4V5z"/><path d="M9 9h6M9 12h4"/>',
    "i-hash": '<path d="M9 4L7 20M17 4l-2 16M4 9h16M3 15h16"/>',
    "i-send": '<path d="M4 12l16-8-6 16-3-6-7-2z"/>',
    "i-mail": '<rect x="3" y="5" width="18" height="14" rx="2"/><path d="M3.5 7l8.5 6 8.5-6"/>',
    "i-key": '<circle cx="8" cy="12" r="4"/><path d="M12 12h9M18 12v3M15 12v2.5"/>',
    "i-upload": '<path d="M12 20V9M8 13l4-4 4 4M4 5h16"/>',
    "i-rocket": '<path d="M14 4c4 1 6 3 6 3s-2 2-3 6c-1 3.5-4 6.5-7 7l-3-3c.5-3 3.5-6 7-7z"/><path d="M9 15l-4 4M7 11l-3 1 1-3M13 17l-1 3 3-1"/>',
    "i-warn-circle": '<circle cx="12" cy="12" r="9"/><path d="M12 8v5M12 16h.01"/>',
    "i-eye": '<path d="M2.5 12S6 6.5 12 6.5 21.5 12 21.5 12 18 17.5 12 17.5 2.5 12 2.5 12z"/><circle cx="12" cy="12" r="2.6"/>',
    "i-archive": '<rect x="3" y="4" width="18" height="5" rx="1.5"/><path d="M5 9v10h14V9M10 13h4"/>',
    "i-sliders": '<path d="M4 7h10M18 7h2M4 17h4M12 17h8"/><circle cx="16" cy="7" r="2"/><circle cx="10" cy="17" r="2"/>',
    "i-folder": '<path d="M3 7a2 2 0 012-2h4l2 2.4h6a2 2 0 012 2V17a2 2 0 01-2 2H5a2 2 0 01-2-2V7z"/>',
    "i-file": '<path d="M6 3h7l5 5v13H6z"/><path d="M13 3v5h5"/>',
    "i-image": '<rect x="3" y="5" width="18" height="14" rx="2"/><circle cx="8.5" cy="10" r="1.6"/><path d="M4 17l5-4.5 4 3.5 3-2.5 4 3.5"/>',
    "i-film": '<rect x="3" y="5" width="18" height="14" rx="2"/><path d="M8 5v14M16 5v14M3 12h18"/>',
    "i-pencil": '<path d="M4 20h4l11-11a2.5 2.5 0 00-3.5-3.5L4 16.5V20z"/><path d="M14.5 6.5l3 3"/>',
    "i-link": '<path d="M10 13a4 4 0 005.7 0l2.6-2.6a4 4 0 00-5.7-5.7L11 6.3"/><path d="M14 11a4 4 0 00-5.7 0l-2.6 2.6a4 4 0 005.7 5.7l1.6-1.6"/>',
    "i-share": '<circle cx="17" cy="6" r="2.4"/><circle cx="6" cy="12" r="2.4"/><circle cx="17" cy="18" r="2.4"/><path d="M8.2 10.8l6.6-3.6M8.2 13.2l6.6 3.6"/>',
    "i-more": '<circle cx="5.5" cy="12" r="1.4"/><circle cx="12" cy="12" r="1.4"/><circle cx="18.5" cy="12" r="1.4"/>',
    "i-list": '<path d="M4 7h16M4 12h16M4 17h16"/>',
    "i-cloud-down": '<path d="M7 17a4 4 0 01.6-8 5.5 5.5 0 0110.6 1.4A3.6 3.6 0 0118 17"/><path d="M12 12v7M9 16l3 3 3-3"/>'
  };

  function injectSprite() {
    const parts = Object.keys(ICONS).map(
      (id) =>
        '<symbol id="' + id + '" viewBox="0 0 24 24" fill="none" stroke="currentColor" ' +
        'stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">' + ICONS[id] + "</symbol>"
    );
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("aria-hidden", "true");
    svg.setAttribute("style", "position:absolute;width:0;height:0;overflow:hidden");
    svg.innerHTML = parts.join("");
    document.body.appendChild(svg);
  }

  const ic = (name, cls) => '<svg class="' + (cls || "") + '" aria-hidden="true"><use href="#' + name + '"></use></svg>';

  /* ── Sample estate (one source of truth across every screen) ─────────── */
  const SERVERS = [
    { id: "prod-01", initials: "P1", name: "gotham-prod-01", state: "online", label: "Sẵn sàng", meta: "203.0.113.18 · Ubuntu 22.04 · 4 vCPU / 7 GiB" },
    { id: "edge-sg-01", initials: "ES", name: "edge-sg-01", state: "online", label: "Sẵn sàng", meta: "198.51.100.24 · Ubuntu 24.04 · 2 vCPU / 4 GiB" },
    { id: "staging-01", initials: "ST", name: "staging-01", state: "idle", label: "Đang xác thực", meta: "203.0.113.42 · Debian 12 · 2 vCPU / 4 GiB" },
    { id: "build-02", initials: "B2", name: "build-node-02", state: "dnd", label: "Mất kết nối", meta: "198.51.100.77 · Ubuntu 22.04 · 8 vCPU / 16 GiB" }
  ];

  const SECTIONS = [
    {
      label: "Vận hành",
      items: [
        { nav: "dashboard", label: "Tổng quan", icon: "i-grid", href: "dashboard.html" },
        { nav: "projects", label: "Projects", icon: "i-layers", href: "projects.html", count: 3 },
        { nav: "files", label: "Quản lý file", icon: "i-folder", href: "files.html" },
        { nav: "templates", label: "Thư viện template", icon: "i-rocket", href: "services.html#templates" },
        { nav: "servers", label: "Máy chủ", icon: "i-server", href: "servers.html", count: 4 },
        { nav: "domains", label: "Tên miền & SSL", icon: "i-globe", href: "domains.html", count: 11 }
      ]
    },
    {
      label: "Nhóm",
      items: [
        { nav: "team", label: "Thành viên & quyền", icon: "i-users", href: "team-settings.html" },
        { nav: "notifications", label: "Kênh thông báo", icon: "i-bell", href: "team-settings.html#notifications" },
        { nav: "tokens", label: "API tokens", icon: "i-key", href: "team-settings.html#tokens" }
      ]
    },
    {
      label: "Hệ thống",
      items: [
        { nav: "settings", label: "Cập nhật & cài đặt", icon: "i-gear", href: "team-settings.html#update" },
        { nav: "map", label: "Bản đồ giao diện", icon: "i-sliders", href: "index.html" }
      ]
    }
  ];

  const ALERTS = [
    { kind: "danger", icon: "i-warn-circle", title: "Mất heartbeat · build-node-02", body: "Không nhận heartbeat trong 42 phút. Agent v0.9.3 đang chậm hơn CP một bản.", time: "42 phút" },
    { kind: "warn", icon: "i-alert", title: "Deploy thất bại · api-core", body: "Bước building dừng ở bước cài dependency. Xem log deploy 1181.", time: "1 giờ" },
    { kind: "success", icon: "i-check", title: "Chứng chỉ đã cấp · shop.gotham.dev", body: "Let's Encrypt HTTP-01 thành công, hết hạn sau 62 ngày.", time: "3 giờ" },
    { kind: "info", icon: "i-cloud", title: "Backup hoàn tất · pg-orders", body: "pg_dump 1.4 GiB đã đẩy lên Cloudflare R2 (bucket gotham-backups).", time: "5 giờ" }
  ];

  /* ── Chrome ──────────────────────────────────────────────────────────── */
  function renderRail() {
    const host = document.getElementById("rail");
    if (!host) return;
    const activeServer = document.body.dataset.server || "";
    const rows = SERVERS.map(
      (s) =>
        '<div class="rail-item' + (activeServer === s.id ? " is-active" : "") + '">' +
        '<a class="rail-btn" href="server-detail.html" aria-label="' + s.name + ' — ' + s.label + '">' +
        s.initials + '<span class="rail-dot dot--' + s.state + '"></span></a>' +
        '<span class="rail-tip" role="tooltip">' + s.name + " · " + s.label + "</span></div>"
    ).join("");

    host.innerHTML =
      '<div class="rail-item"><a class="rail-btn" href="dashboard.html" aria-label="Gotham — tổng quan">' +
      '<span class="brand-mark brand-mark--sm" style="background:transparent"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M5 19V11a7 7 0 0114 0v8"/><path d="M9.5 19v-6.5a2.5 2.5 0 015 0V19"/></svg></span></a>' +
      '<span class="rail-tip" role="tooltip">Gotham · tổng quan</span></div>' +
      '<div class="rail-sep"></div>' +
      '<nav class="rail-nav" aria-label="Máy chủ đang quản lý">' + rows + "</nav>" +
      '<div class="rail-item"><a class="rail-btn" href="servers.html#wizard" aria-label="Thêm máy chủ" style="background:var(--surface);color:var(--success)">' + ic("i-plus") + "</a>" +
      '<span class="rail-tip" role="tooltip">Thêm máy chủ</span></div>' +
      '<div class="rail-foot">' +
      '<div class="rail-sep"></div>' +
      '<div class="rail-item"><a class="rail-btn" href="index.html" aria-label="Bản đồ giao diện">' + ic("i-sliders") + "</a>" +
      '<span class="rail-tip" role="tooltip">Bản đồ giao diện</span></div>' +
      '<div class="rail-item"><button class="rail-btn" type="button" data-open="alerts" aria-label="Cảnh báo hệ thống">' + ic("i-bell") +
      '<span class="rail-badge">3</span></button><span class="rail-tip" role="tooltip">3 cảnh báo mới</span></div>' +
      "</div>";
  }

  function renderSidebar() {
    const host = document.getElementById("sidebar");
    if (!host) return;
    const active = document.body.dataset.nav || "dashboard";
    const groups = SECTIONS.map(function (group) {
      const items = group.items.map(function (item) {
        return '<a class="nav-item' + (item.nav === active ? " is-active" : "") + '" href="' + item.href + '">' +
          ic(item.icon) + "<span>" + item.label + "</span>" +
          (item.count ? '<span class="nav-count">' + item.count + "</span>" : "") + "</a>";
      }).join("");
      return '<p class="nav-label">' + group.label + "</p>" + items;
    }).join("");

    host.innerHTML =
      '<div class="sidebar-head">Gotham Labs<span class="tag">v0.9.4</span></div>' +
      '<div class="sidebar-body">' + groups + "</div>" +
      '<div class="sidebar-foot"><button class="me-card" type="button" data-open="account">' +
      '<span class="avatar-wrap"><span class="avatar avatar--sm">NA</span><span class="dot dot--online dot--ring"></span></span>' +
      '<span class="grow" style="text-align:left"><span class="truncate" style="display:block;font-size:var(--text-sm);color:var(--fg-2)">nga.tran@gotham.dev</span>' +
      '<span class="small muted">owner · Gotham Labs</span></span>' + ic("i-chevron-down") + "</button></div>";
  }

  function renderTopbar() {
    const host = document.getElementById("topbar");
    if (!host) return;
    host.innerHTML =
      '<button class="btn btn-ghost btn-icon nav-toggle" type="button" data-nav-toggle aria-label="Mở điều hướng">' + ic("i-grid") + "</button>" +
      '<span class="row gap-2"><span class="status-line">' + ic("i-shield") + 'production</span>' +
      '<span class="channel-chip">CP :8000</span><span class="channel-chip">gRPC :9442</span></span>' +
      '<div class="search" style="margin-left:auto" role="search">' + ic("i-search") +
      '<input type="search" placeholder="Tìm ứng dụng, máy chủ, database…" aria-label="Tìm kiếm" data-palette-open>' +
      '<span class="kbd">⌘K</span></div>' +
      '<div class="topbar-right">' +
      '<button class="btn btn-ghost btn-icon" type="button" data-open="alerts" aria-label="Cảnh báo">' + ic("i-bell") + "</button>" +
      '<a class="btn btn-ghost btn-icon" href="index.html" aria-label="Tài liệu thiết kế">' + ic("i-doc") + "</a>" +
      '<button class="btn btn-ghost btn-icon" type="button" data-open="account" aria-label="Tài khoản">' +
      '<span class="avatar avatar--sm">NA</span></button>' +
      "</div>";
    const toggle = host.querySelector("[data-nav-toggle]");
    if (toggle) toggle.style.display = "";
  }

  /* ── Global overlays injected on every screen ────────────────────────── */
  function injectOverlays() {
    const wrap = document.createElement("div");
    wrap.innerHTML =
      '<div class="overlay" id="alerts" hidden>' +
      '<div class="modal" role="dialog" aria-modal="true" aria-labelledby="alerts-title">' +
      '<div class="modal-head">' + ic("i-bell") + '<div><h3 id="alerts-title">Cảnh báo hệ thống</h3>' +
      '<p class="small muted">Heartbeat, deploy, chứng chỉ và backup — 4 sự kiện gần nhất</p></div>' +
      '<button class="btn btn-ghost btn-icon modal-close" type="button" data-close aria-label="Đóng">' + ic("i-x") + "</button></div>" +
      '<div class="modal-body stack gap-3">' +
      ALERTS.map(function (a) {
        return '<div class="embed embed--' + a.kind + '"><div class="between"><h4>' + a.title + '</h4><span class="small meta">' + a.time + ' trước</span></div>' +
          "<p>" + a.body + "</p></div>";
      }).join("") +
      "</div>" +
      '<div class="modal-foot"><span class="small muted grow">Kênh: Discord webhook · #gotham-alerts</span>' +
      '<button class="btn btn-outline" type="button" data-close>Đóng</button>' +
      '<button class="btn btn-primary" type="button" data-toast="Đã đánh dấu tất cả cảnh báo là đã đọc|success" data-close>Đánh dấu đã đọc</button></div>' +
      "</div></div>" +
      '<div class="overlay" id="account" hidden>' +
      '<div class="modal modal--sm" role="dialog" aria-modal="true" aria-labelledby="account-title">' +
      '<div class="modal-head"><span class="avatar avatar--lg">NA</span><div><h3 id="account-title">nga.tran@gotham.dev</h3>' +
      '<p class="small muted">owner · Gotham Labs · 2FA chưa bật</p></div>' +
      '<button class="btn btn-ghost btn-icon modal-close" type="button" data-close aria-label="Đóng">' + ic("i-x") + "</button></div>" +
      '<div class="modal-body stack gap-2">' +
      '<a class="nav-item" href="team-settings.html">' + ic("i-users") + "<span>Thành viên & quyền</span></a>" +
      '<a class="nav-item" href="team-settings.html#tokens">' + ic("i-key") + "<span>API tokens</span></a>" +
      '<a class="nav-item" href="team-settings.html#notifications">' + ic("i-bell") + "<span>Kênh thông báo</span></a>" +
      '<a class="nav-item" href="team-settings.html#update">' + ic("i-refresh") + "<span>Cập nhật control plane</span></a>" +
      "</div>" +
      '<div class="modal-foot"><span class="small muted grow">Phiên JWT còn 12 phút · refresh 30 ngày</span>' +
      '<a class="btn btn-outline" href="login.html">Đăng xuất</a></div>' +
      "</div></div>" +
      '<div class="overlay" id="confirm" hidden>' +
      '<div class="modal modal--sm" role="alertdialog" aria-modal="true" aria-labelledby="confirm-title">' +
      '<div class="modal-head">' + ic("i-alert") + '<div><h3 id="confirm-title" data-confirm-title>Xác nhận thao tác</h3>' +
      '<p class="small muted" data-confirm-body></p></div></div>' +
      '<div class="modal-foot"><span class="small muted grow">Thao tác được ghi vào audit log của team.</span>' +
      '<button class="btn btn-outline" type="button" data-close>Huỷ</button>' +
      '<button class="btn btn-danger" type="button" data-confirm-ok>Xoá</button></div>' +
      "</div></div>" +
      '<div class="overlay" id="palette" hidden>' +
      '<div class="modal modal--sm" style="align-self:start;margin-top:12vh" role="dialog" aria-modal="true" aria-label="Bảng lệnh">' +
      '<div class="modal-body"><div class="input-affix">' + ic("i-search") +
      '<input class="input" type="text" placeholder="Đi tới màn hình hoặc hành động…" data-palette-input aria-label="Bảng lệnh"></div>' +
      '<div class="stack gap-1 mt-3" data-palette-list></div></div></div></div>';
    while (wrap.firstChild) document.body.appendChild(wrap.firstChild);

    const toasts = document.createElement("div");
    toasts.className = "toast-stack";
    toasts.id = "toasts";
    document.body.appendChild(toasts);
  }

  /* ── Confirm dialog (destructive actions) ────────────────────────────── */
  let pendingConfirm = null;

  function askConfirm(btn) {
    const overlay = document.getElementById("confirm");
    if (!overlay) return;
    pendingConfirm = btn;
    overlay.querySelector("[data-confirm-title]").textContent = btn.dataset.confirmTitle || "Xác nhận thao tác";
    overlay.querySelector("[data-confirm-body]").textContent = btn.dataset.confirm;
    const ok = overlay.querySelector("[data-confirm-ok]");
    ok.textContent = btn.dataset.confirmLabel || "Xoá";
    const danger = btn.dataset.confirmKind !== "neutral";
    ok.className = "btn " + (danger ? "btn-danger" : "btn-primary");
    openOverlay("confirm");
  }

  function resolveConfirm() {
    const btn = pendingConfirm;
    closeOverlay(document.getElementById("confirm"));
    pendingConfirm = null;
    if (!btn) return;
    toast(btn.dataset.confirmToast || "Đã thực hiện thao tác.", btn.dataset.confirmKind === "neutral" ? "info" : "success");
    if (btn.dataset.remove) {
      const target = btn.closest(btn.dataset.remove);
      if (target) {
        target.style.transition = "opacity 180ms linear";
        target.style.opacity = "0";
        window.setTimeout(function () { target.remove(); }, 200);
      }
    }
  }

  /* ── Toast ───────────────────────────────────────────────────────────── */
  function toast(message, kind) {
    const host = document.getElementById("toasts");
    if (!host) return;
    const icons = { success: "i-check", danger: "i-warn-circle", warn: "i-alert", info: "i-info" };
    const k = kind || "info";
    const el = document.createElement("div");
    el.className = "toast toast--" + k;
    el.setAttribute("role", "status");
    el.innerHTML = ic(icons[k] || "i-info") + "<span>" + message + "</span>";
    host.appendChild(el);
    window.setTimeout(function () {
      el.style.transition = "opacity 200ms linear";
      el.style.opacity = "0";
      window.setTimeout(function () { el.remove(); }, 220);
    }, 3200);
  }

  /* ── Overlay open/close ──────────────────────────────────────────────── */
  let lastFocus = null;

  function openOverlay(id) {
    const el = document.getElementById(id);
    if (!el) return;
    lastFocus = document.activeElement;
    el.hidden = false;
    const focusable = el.querySelector("input, select, textarea, button:not([data-close])");
    if (focusable) focusable.focus();
    if (id === "palette") renderPalette("");
  }

  function closeOverlay(el) {
    if (!el) return;
    el.hidden = true;
    if (lastFocus && lastFocus.focus) lastFocus.focus();
  }

  function closeAllOverlays() {
    document.querySelectorAll(".overlay").forEach(function (o) { o.hidden = true; });
    document.querySelectorAll(".drawer").forEach(function (d) { d.hidden = true; });
  }

  /* ── Command palette ─────────────────────────────────────────────────── */
  const PALETTE = [
    { label: "Tổng quan", hint: "Màn hình", href: "dashboard.html", icon: "i-grid" },
    { label: "Ứng dụng", hint: "12 ứng dụng", href: "applications.html", icon: "i-box" },
    { label: "Tạo ứng dụng từ Git", hint: "Hành động", href: "applications.html#wizard", icon: "i-plus" },
    { label: "Services", hint: "4 compose project", href: "services.html", icon: "i-layers" },
    { label: "Thư viện template", hint: "WordPress, Nextcloud, n8n, Uptime Kuma", href: "services.html#templates", icon: "i-rocket" },
    { label: "Cơ sở dữ liệu", hint: "5 database", href: "databases.html", icon: "i-db" },
    { label: "Quản lý file", hint: "Volume ứng dụng · tải lên, xoá, đổi tên", href: "files.html", icon: "i-folder" },
    { label: "Máy chủ", hint: "4 node", href: "servers.html", icon: "i-server" },
    { label: "Thêm máy chủ (SSH + agent)", hint: "Hành động", href: "servers.html#wizard", icon: "i-plus" },
    { label: "Tên miền & SSL", hint: "9 router", href: "domains.html", icon: "i-globe" },
    { label: "Thành viên & quyền", hint: "Team", href: "team-settings.html", icon: "i-users" },
    { label: "Kênh thông báo", hint: "Discord · Slack · Telegram · Email", href: "team-settings.html#notifications", icon: "i-bell" },
    { label: "API tokens", hint: "scope read/deploy/admin", href: "team-settings.html#tokens", icon: "i-key" },
    { label: "Cập nhật control plane", hint: "v0.10.0 · Ed25519", href: "team-settings.html#update", icon: "i-refresh" },
    { label: "Bản đồ giao diện", hint: "Toàn bộ màn hình + token", href: "index.html", icon: "i-sliders" }
  ];

  function renderPalette(query) {
    const list = document.querySelector("[data-palette-list]");
    if (!list) return;
    const q = (query || "").toLowerCase();
    const hits = PALETTE.filter(function (p) { return (p.label + " " + p.hint).toLowerCase().indexOf(q) >= 0; });
    list.innerHTML = hits.length
      ? hits.map(function (p) {
          return '<a class="nav-item" href="' + p.href + '">' + ic(p.icon) + "<span>" + p.label + '</span><span class="nav-count">' + p.hint + "</span></a>";
        }).join("")
      : '<div class="empty">' + ic("i-search") + "<p>Không có kết quả cho “" + query + "”</p></div>";
  }

  /* ── Tabs ────────────────────────────────────────────────────────────── */
  function initTabs(root) {
    (root || document).querySelectorAll("[data-tabs]").forEach(function (group) {
      const key = "gotham:tab:" + (document.body.dataset.page || "page") + ":" + (group.dataset.tabs || "t");
      const buttons = group.querySelectorAll("[data-tab]");
      const host = group.parentElement || document;
      function activate(name, persist) {
        buttons.forEach(function (b) {
          const on = b.dataset.tab === name;
          b.setAttribute("aria-selected", on ? "true" : "false");
          b.tabIndex = on ? 0 : -1;
        });
        host.querySelectorAll("[data-panel]").forEach(function (p) {
          p.hidden = p.dataset.panel !== name;
        });
        /* Charts measured while hidden report width 0 — redraw once visible. */
        window.requestAnimationFrame(function () {
          document.dispatchEvent(new CustomEvent("gotham:rerender-charts"));
        });
        if (persist) { try { localStorage.setItem(key, name); } catch (e) { /* ignore */ } }
      }
      buttons.forEach(function (b) {
        b.addEventListener("click", function () { activate(b.dataset.tab, true); });
        b.addEventListener("keydown", function (e) {
          if (e.key !== "ArrowRight" && e.key !== "ArrowLeft") return;
          const arr = Array.prototype.slice.call(buttons);
          const i = arr.indexOf(b);
          const next = arr[(i + (e.key === "ArrowRight" ? 1 : arr.length - 1)) % arr.length];
          next.focus();
        });
      });
      let start = null;
      if (location.hash) {
        const h = location.hash.replace("#", "");
        if (Array.prototype.some.call(buttons, function (b) { return b.dataset.tab === h; })) start = h;
      }
      if (!start) { try { start = localStorage.getItem(key); } catch (e) { start = null; } }
      const valid = start && Array.prototype.some.call(buttons, function (b) { return b.dataset.tab === start; });
      activate(valid ? start : (group.dataset.default || buttons[0].dataset.tab), false);
    });
  }

  /* ── Filters + search ────────────────────────────────────────────────── */
  function initFilters(scope) {
    const root = scope || document;
    root.querySelectorAll("[data-filter-group]").forEach(function (group) {
      const targetSel = group.dataset.filterTarget;
      group.querySelectorAll("[data-filter]").forEach(function (btn) {
        btn.addEventListener("click", function () {
          group.querySelectorAll("[data-filter]").forEach(function (b) { b.classList.remove("is-active"); });
          btn.classList.add("is-active");
          const want = btn.dataset.filter;
          const items = targetSel ? document.querySelectorAll(targetSel) : [];
          items.forEach(function (item) {
            const tags = (item.dataset.tag || "").split(/\s+/);
            item.hidden = !(want === "all" || tags.indexOf(want) >= 0);
          });
          const empty = document.querySelector(group.dataset.filterEmpty || "#filter-empty");
          if (empty && items.length) {
            let visible = 0;
            items.forEach(function (i) { if (!i.hidden) visible++; });
            empty.hidden = visible > 0;
          }
        });
      });
    });
    root.querySelectorAll("[data-search]").forEach(function (input) {
      input.addEventListener("input", function () {
        const sel = input.dataset.search;
        const q = input.value.trim().toLowerCase();
        document.querySelectorAll(sel).forEach(function (item) {
          item.hidden = q.length > 0 && item.textContent.toLowerCase().indexOf(q) < 0;
        });
      });
    });
  }

  /* ── Switches, meters, copy, reveal ──────────────────────────────────── */
  function initControls(scope) {
    const root = scope || document;
    root.querySelectorAll("[data-switch]").forEach(function (sw) {
      sw.addEventListener("click", function () {
        const on = sw.getAttribute("aria-checked") !== "true";
        sw.setAttribute("aria-checked", on ? "true" : "false");
        const show = sw.dataset.whenOn ? document.querySelector(sw.dataset.whenOn) : null;
        if (show) show.hidden = !on;
        if (sw.dataset.switchToast) toast(sw.dataset.switchToast.replace("{state}", on ? "bật" : "tắt"), on ? "success" : "info");
      });
    });
    root.querySelectorAll("[data-meter]").forEach(function (m) {
      const v = Math.max(0, Math.min(100, parseFloat(m.dataset.meter) || 0));
      const fill = document.createElement("span");
      fill.className = "meter-fill" + (m.dataset.meterKind ? " meter-fill--" + m.dataset.meterKind : "");
      fill.style.width = v + "%";
      m.appendChild(fill);
    });
    root.querySelectorAll("[data-copy]").forEach(function (btn) {
      btn.addEventListener("click", function () {
        const raw = btn.dataset.copy;
        const src = raw.charAt(0) === "#" ? document.querySelector(raw) : null;
        const text = src ? (src.value || src.textContent).trim() : raw;
        const done = function () { toast("Đã sao chép: " + (text.length > 42 ? text.slice(0, 42) + "…" : text), "success"); };
        if (navigator.clipboard && navigator.clipboard.writeText) {
          navigator.clipboard.writeText(text).then(done, done);
        } else { done(); }
      });
    });
    root.querySelectorAll("[data-reveal]").forEach(function (btn) {
      btn.addEventListener("click", function () {
        const el = document.querySelector(btn.dataset.reveal);
        if (!el) return;
        const masked = el.dataset.masked !== "false";
        el.dataset.masked = masked ? "false" : "true";
        el.textContent = masked ? el.dataset.value : el.dataset.mask || "••••••••••••";
        btn.setAttribute("aria-label", masked ? "Ẩn giá trị" : "Hiện giá trị");
      });
    });
    root.querySelectorAll("[data-toast]").forEach(function (btn) {
      btn.addEventListener("click", function () {
        const parts = btn.dataset.toast.split("|");
        toast(parts[0], parts[1] || "info");
      });
    });
    root.querySelectorAll("[data-count]").forEach(function (el) {
      const target = parseFloat(el.dataset.count);
      if (isNaN(target)) return;
      const decimals = (el.dataset.count.split(".")[1] || "").length;
      let start = null;
      const step = function (ts) {
        if (!start) start = ts;
        const p = Math.min(1, (ts - start) / 700);
        const eased = 1 - Math.pow(1 - p, 3);
        el.textContent = (target * eased).toFixed(decimals);
        if (p < 1) requestAnimationFrame(step);
      };
      requestAnimationFrame(step);
    });
    root.querySelectorAll("[data-confirm]").forEach(function (btn) {
      btn.addEventListener("click", function () { askConfirm(btn); });
    });
  }

  /* ── Segmented control ───────────────────────────────────────────────── */
  function initSegments(scope) {
    (scope || document).querySelectorAll("[data-seg]").forEach(function (seg) {
      seg.querySelectorAll("button").forEach(function (btn) {
        btn.addEventListener("click", function () {
          seg.querySelectorAll("button").forEach(function (b) { b.classList.remove("is-active"); });
          btn.classList.add("is-active");
          const evt = new CustomEvent("gotham:segment", { detail: { value: btn.dataset.value, group: seg.dataset.seg }, bubbles: true });
          seg.dispatchEvent(evt);
        });
      });
    });
  }

  /* ── Sparklines ──────────────────────────────────────────────────────── */
  function initSparks(scope) {
    (scope || document).querySelectorAll("[data-spark]").forEach(function (host) {
      const values = host.dataset.spark.split(",").map(Number);
      const kind = host.dataset.sparkKind || "";
      const w = 96, h = 26, max = Math.max.apply(null, values) * 1.15, min = Math.min.apply(null, values) * 0.85;
      const step = w / (values.length - 1);
      const pts = values.map(function (v, i) {
        return [i * step, h - ((v - min) / (max - min || 1)) * (h - 4) - 2];
      });
      const line = pts.map(function (p, i) { return (i ? "L" : "M") + p[0].toFixed(1) + " " + p[1].toFixed(1); }).join(" ");
      const area = line + " L" + w + " " + h + " L0 " + h + " Z";
      host.innerHTML =
        '<svg class="spark' + (kind ? " spark--" + kind : "") + '" viewBox="0 0 ' + w + " " + h + '" preserveAspectRatio="none" aria-hidden="true">' +
        '<path class="spark-area" d="' + area + '"></path><path class="spark-line" d="' + line + '"></path></svg>';
    });
  }

  /* ── Area charts (filled data encoding + hover read-out) ─────────────── */
  function seriesFrom(seed, points, base, amp) {
    const out = [];
    let x = seed;
    for (let i = 0; i < points; i++) {
      x = (x * 9301 + 49297) % 233280;
      const r = x / 233280;
      out.push(Math.max(1, Math.min(99, base + Math.sin(i / 3.1 + seed) * amp + (r - 0.5) * amp * 0.9)));
    }
    return out;
  }

  const DATASETS = {
    "1h": { points: 30, labels: ["-60p", "-45p", "-30p", "-15p", "bây giờ"], cpu: seriesFrom(7, 30, 38, 12), mem: seriesFrom(19, 30, 61, 7), disk: seriesFrom(3, 30, 44, 9), net: seriesFrom(11, 30, 26, 14) },
    "24h": { points: 24, labels: ["-24h", "-18h", "-12h", "-6h", "bây giờ"], cpu: seriesFrom(23, 24, 41, 18), mem: seriesFrom(41, 24, 58, 10), disk: seriesFrom(5, 24, 39, 14), net: seriesFrom(31, 24, 33, 20) },
    "7d": { points: 28, labels: ["-7 ngày", "-5 ngày", "-3 ngày", "-1 ngày", "bây giờ"], cpu: seriesFrom(53, 28, 36, 21), mem: seriesFrom(61, 28, 55, 12), disk: seriesFrom(13, 28, 47, 17), net: seriesFrom(71, 28, 29, 23) }
  };

  function drawChart(host, values, opts) {
    const o = opts || {};
    const w = Math.max(240, host.clientWidth || 560);
    const h = o.height || 132;
    const pad = { t: 12, r: 10, b: 18, l: 34 };
    const kind = o.kind || "";
    const unit = o.unit || "%";
    const min = o.min !== undefined ? o.min : 0;
    const max = o.max !== undefined ? o.max : 100;
    const iw = w - pad.l - pad.r;
    const ih = h - pad.t - pad.b;
    const step = iw / (values.length - 1);
    const pts = values.map(function (v, i) {
      return [pad.l + i * step, pad.t + ih - ((v - min) / (max - min)) * ih];
    });
    const line = pts.map(function (p, i) { return (i ? "L" : "M") + p[0].toFixed(1) + " " + p[1].toFixed(1); }).join(" ");
    const area = line + " L" + (pad.l + iw) + " " + (pad.t + ih) + " L" + pad.l + " " + (pad.t + ih) + " Z";
    const grid = [0, 0.25, 0.5, 0.75, 1].map(function (f) {
      const y = pad.t + ih - f * ih;
      return '<line class="chart-grid" x1="' + pad.l + '" y1="' + y.toFixed(1) + '" x2="' + (pad.l + iw) + '" y2="' + y.toFixed(1) + '"></line>' +
        '<text class="chart-label" x="' + (pad.l - 6) + '" y="' + (y + 3).toFixed(1) + '" text-anchor="end">' + Math.round(min + f * (max - min)) + "</text>";
    }).join("");
    const labels = (o.labels || []).map(function (t, i, arr) {
      const x = pad.l + (iw * i) / (arr.length - 1);
      return '<text class="chart-label" x="' + x.toFixed(1) + '" y="' + (h - 5) + '" text-anchor="' + (i === 0 ? "start" : i === arr.length - 1 ? "end" : "middle") + '">' + t + "</text>";
    }).join("");
    host.innerHTML =
      '<svg class="chart" viewBox="0 0 ' + w + " " + h + '" preserveAspectRatio="none" role="img" aria-label="' + (o.title || "Biểu đồ") + '">' +
      grid +
      '<path class="chart-area' + (kind ? " chart-area--" + kind : "") + '" d="' + area + '"></path>' +
      '<path class="chart-line' + (kind ? " chart-line--" + kind : "") + '" d="' + line + '"></path>' +
      '<line class="chart-hover" x1="0" y1="' + pad.t + '" x2="0" y2="' + (pad.t + ih) + '"></line>' +
      '<circle class="chart-dot" r="2.6" cx="0" cy="0"></circle>' +
      labels +
      "</svg>" +
      '<div class="chart-tip" role="presentation"></div>';
    const tip = host.querySelector(".chart-tip");
    const hoverLine = host.querySelector(".chart-hover");
    const dot = host.querySelector(".chart-dot");
    host.addEventListener("mousemove", function (e) {
      const rect = host.getBoundingClientRect();
      const scale = w / rect.width;
      const x = (e.clientX - rect.left) * scale;
      const i = Math.max(0, Math.min(values.length - 1, Math.round((x - pad.l) / step)));
      const p = pts[i];
      hoverLine.setAttribute("x1", p[0]);
      hoverLine.setAttribute("x2", p[0]);
      hoverLine.style.opacity = "0.5";
      dot.setAttribute("cx", p[0]);
      dot.setAttribute("cy", p[1]);
      dot.style.opacity = "1";
      tip.style.opacity = "1";
      tip.style.left = (p[0] / scale) + "px";
      tip.style.top = (p[1] / scale) + "px";
      tip.textContent = values[i].toFixed(1) + unit + " · " + (o.points ? o.points[i] : "");
    });
    host.addEventListener("mouseleave", function () {
      hoverLine.style.opacity = "0";
      dot.style.opacity = "0";
      tip.style.opacity = "0";
    });
  }

  function initCharts(scope) {
    const hosts = (scope || document).querySelectorAll("[data-chart]");
    if (!hosts.length) return;
    let range = "1h";
    const render = function () {
      const ds = DATASETS[range];
      hosts.forEach(function (host) {
        const key = host.dataset.chart;
        drawChart(host, ds[key], {
          kind: host.dataset.chartKind || "",
          height: parseInt(host.dataset.chartHeight || "132", 10),
          labels: ds.labels,
          points: ds.points,
          title: host.dataset.chartTitle || key,
          unit: host.dataset.chartUnit || "%"
        });
      });
      document.querySelectorAll("[data-range-label]").forEach(function (el) { el.textContent = range; });
    };
    render();
    let t = null;
    window.addEventListener("resize", function () {
      window.clearTimeout(t);
      t = window.setTimeout(render, 160);
    });
    document.addEventListener("gotham:segment", function (e) {
      if (e.detail.group !== "metrics-range") return;
      range = e.detail.value;
      render();
    });
    document.addEventListener("gotham:rerender-charts", render);
  }

  /* ── Log streaming ───────────────────────────────────────────────────── */
  function initLogs(scope) {
    (scope || document).querySelectorAll("[data-log]").forEach(function (host) {
      const script = document.getElementById(host.dataset.log);
      if (!script) return;
      let lines = [];
      try { lines = JSON.parse(script.textContent); } catch (e) { lines = []; }
      const body = host.querySelector("[data-log-body]");
      const state = host.querySelector("[data-log-state]");
      const clock = host.querySelector("[data-log-clock]");
      let i = 0, paused = false, timer = null;
      const start = new Date();
      start.setHours(14, 2, 8, 0);

      function stamp(offset) {
        const d = new Date(start.getTime() + offset * 1000);
        return d.toTimeString().slice(0, 8);
      }
      function push() {
        if (i >= lines.length) {
          if (state) { state.textContent = "đã dừng"; state.classList.add("is-offline"); }
          window.clearInterval(timer);
          return;
        }
        const line = lines[i++];
        const row = document.createElement("div");
        row.className = "log-line";
        row.dataset.level = line.level || "info";
        row.innerHTML = '<span class="t">' + (line.t || stamp(i * 2)) + '</span><span class="lv">' + (line.level || "info").toUpperCase() + '</span><span class="m"></span>';
        row.querySelector(".m").textContent = line.m;
        body.appendChild(row);
        if (host.dataset.logAutoscroll !== "off") body.scrollTop = body.scrollHeight;
      }
      function play() {
        window.clearInterval(timer);
        timer = window.setInterval(function () { if (!paused) push(); }, 420);
      }
      play();

      const pauseBtn = host.querySelector("[data-log-pause]");
      if (pauseBtn) {
        pauseBtn.addEventListener("click", function () {
          paused = !paused;
          pauseBtn.innerHTML = paused ? ic("i-play") + "<span>Tiếp tục</span>" : ic("i-stop") + "<span>Tạm dừng</span>";
          if (state) state.classList.toggle("is-paused", paused);
          if (clock) clock.textContent = paused ? "tạm dừng" : "đang stream";
        });
      }
      const clearBtn = host.querySelector("[data-log-clear]");
      if (clearBtn) clearBtn.addEventListener("click", function () { body.innerHTML = ""; toast("Đã xoá buffer log hiển thị.", "info"); });
      const dl = host.querySelector("[data-log-download]");
      if (dl) dl.addEventListener("click", function () { toast("Đang tải " + host.dataset.logName + " (" + lines.length + " dòng)…", "info"); });
      const follow = host.querySelector("[data-log-follow]");
      if (follow) follow.addEventListener("click", function () {
        const off = host.dataset.logAutoscroll === "off";
        host.dataset.logAutoscroll = off ? "on" : "off";
        follow.classList.toggle("is-active", off);
        toast(off ? "Bật tự động cuộn theo log." : "Đã tắt tự động cuộn.", "info");
      });
    });
  }

  /* ── Wizard ──────────────────────────────────────────────────────────── */
  function validateField(field) {
    const input = field.querySelector("input, select, textarea");
    if (!input) return true;
    const value = (input.value || "").trim();
    let ok = true;
    if (input.required && !value) ok = false;
    if (ok && input.dataset.pattern && value) ok = new RegExp(input.dataset.pattern).test(value);
    if (ok && input.type === "email" && value) ok = /^[^@\s]+@[^@\s]+\.[a-z]{2,}$/i.test(value);
    field.classList.toggle("has-error", !ok);
    return ok;
  }

  function initWizards(scope) {
    (scope || document).querySelectorAll("[data-wizard]").forEach(function (wiz) {
      const steps = Array.prototype.slice.call(wiz.querySelectorAll(".wstep"));
      const railItems = Array.prototype.slice.call(wiz.querySelectorAll(".wizard-rail li"));
      const back = wiz.querySelector("[data-wizard-back]");
      const next = wiz.querySelector("[data-wizard-next]");
      const done = wiz.querySelector("[data-wizard-done]");
      let index = 0;

      function paint() {
        steps.forEach(function (s, i) { s.hidden = i !== index; });
        railItems.forEach(function (li, i) {
          li.classList.toggle("is-active", i === index);
          li.classList.toggle("is-done", i < index);
        });
        if (back) back.hidden = index === 0;
        const last = index === steps.length - 1;
        if (next) next.hidden = last;
        if (done) done.hidden = !last;
        const counter = wiz.querySelector("[data-wizard-counter]");
        if (counter) counter.textContent = "Bước " + (index + 1) + " / " + steps.length;
      }
      function go(target) {
        if (target > index) {
          const fields = steps[index].querySelectorAll(".field");
          let ok = true;
          fields.forEach(function (f) { if (!validateField(f)) ok = false; });
          if (!ok) { toast("Vui lòng kiểm tra lại các trường được đánh dấu.", "danger"); return; }
        }
        index = Math.max(0, Math.min(steps.length - 1, target));
        paint();
        const hook = steps[index].dataset.onEnter;
        if (hook && typeof window[hook] === "function") window[hook](wiz);
      }
      if (next) next.addEventListener("click", function () {
        const btn = next;
        if (steps[index].dataset.async === "true") {
          btn.classList.add("is-busy");
          window.setTimeout(function () { btn.classList.remove("is-busy"); go(index + 1); }, 900);
        } else { go(index + 1); }
      });
      if (back) back.addEventListener("click", function () { go(index - 1); });
      wiz.querySelectorAll(".field input, .field select").forEach(function (input) {
        input.addEventListener("input", function () { validateField(input.closest(".field")); });
        input.addEventListener("blur", function () { validateField(input.closest(".field")); });
      });
      paint();
    });
  }

  /* ── Pipeline runner (deploy state machine demo) ─────────────────────── */
  function initPipelines(scope) {
    (scope || document).querySelectorAll("[data-pipeline-run]").forEach(function (btn) {
      btn.addEventListener("click", function () {
        const target = document.querySelector(btn.dataset.pipelineRun);
        if (!target) return;
        const nodes = Array.prototype.slice.call(target.querySelectorAll(".node"));
        nodes.forEach(function (n, i) {
          n.classList.remove("is-active", "is-done");
          if (i > 0) { n.classList.add("is-idle"); }
        });
        let i = 0;
        const tick = function () {
          if (i > 0) nodes[i - 1].classList.replace("is-active", "is-done");
          if (i >= nodes.length) {
            toast("Deploy hoàn tất · container đang chạy.", "success");
            return;
          }
          nodes[i].classList.add("is-active");
          nodes[i].classList.remove("is-idle");
          document.dispatchEvent(new CustomEvent("gotham:deploy-step", { detail: { index: i, name: nodes[i].dataset.step } }));
          i++;
          window.setTimeout(tick, 1100);
        };
        tick();
      });
    });
  }

  /* ── Container row actions ───────────────────────────────────────────── */
  function initRowActions(scope) {
    (scope || document).querySelectorAll("[data-container-action]").forEach(function (btn) {
      btn.addEventListener("click", function () {
        const row = btn.closest("tr") || btn.closest("[data-container-row]");
        if (!row) return;
        const action = btn.dataset.containerAction;
        const stateCell = row.querySelector("[data-container-state]");
        const dot = row.querySelector("[data-container-dot]");
        const label = { start: "Đang khởi động", stop: "Đang dừng", restart: "Đang khởi động lại" }[action] || "Đang xử lý";
        if (stateCell) stateCell.textContent = label + "…";
        window.setTimeout(function () {
          const running = action !== "stop";
          if (stateCell) stateCell.textContent = running ? "running" : "exited";
          if (dot) dot.className = "dot dot--" + (running ? "online" : "offline");
          toast((row.dataset.name || "container") + ": " + (running ? "đang chạy" : "đã dừng") + " qua agent gRPC.", running ? "success" : "info");
        }, 850);
      });
    });
  }

  /* ── Log drawer ──────────────────────────────────────────────────────── */
  function initDrawers(scope) {
    (scope || document).querySelectorAll("[data-drawer-open]").forEach(function (btn) {
      btn.addEventListener("click", function (e) {
        e.preventDefault();
        const d = document.getElementById(btn.dataset.drawerOpen);
        if (!d) return;
        d.hidden = false;
        const title = d.querySelector("[data-drawer-title]");
        if (title && btn.dataset.drawerTitle) title.textContent = btn.dataset.drawerTitle;
        const sub = d.querySelector("[data-drawer-sub]");
        if (sub && btn.dataset.drawerSub) sub.textContent = btn.dataset.drawerSub;
      });
    });
  }

  /* ── Boot ────────────────────────────────────────────────────────────── */
  function mount() {
    injectSprite();
    renderRail();
    renderSidebar();
    renderTopbar();
    injectOverlays();

    document.querySelectorAll("[data-open]").forEach(function (btn) {
      btn.addEventListener("click", function (e) { e.preventDefault(); openOverlay(btn.dataset.open); });
    });
    document.querySelectorAll("[data-close]").forEach(function (btn) {
      btn.addEventListener("click", function () { closeOverlay(btn.closest(".overlay")); });
    });
    const confirmOk = document.querySelector("[data-confirm-ok]");
    if (confirmOk) confirmOk.addEventListener("click", resolveConfirm);
    document.querySelectorAll("[data-drawer-close]").forEach(function (btn) {
      btn.addEventListener("click", function () { btn.closest(".drawer").hidden = true; });
    });
    document.querySelectorAll(".overlay").forEach(function (o) {
      o.addEventListener("mousedown", function (e) { if (e.target === o) closeOverlay(o); });
    });
    document.addEventListener("keydown", function (e) {
      if (e.key === "Escape") closeAllOverlays();
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        openOverlay("palette");
      }
    });
    const paletteInput = document.querySelector("[data-palette-input]");
    if (paletteInput) paletteInput.addEventListener("input", function () { renderPalette(paletteInput.value); });
    document.addEventListener("click", function (e) {
      const opener = e.target.closest("[data-palette-open]");
      if (opener) { e.preventDefault(); openOverlay("palette"); }
    });
    const navToggle = document.querySelector("[data-nav-toggle]");
    if (navToggle) {
      const backdrop = document.createElement("div");
      backdrop.className = "nav-backdrop";
      backdrop.hidden = true;
      backdrop.addEventListener("click", function () {
        document.querySelector(".app").classList.remove("is-open");
        backdrop.hidden = true;
      });
      document.body.appendChild(backdrop);
      navToggle.addEventListener("click", function () {
        const open = document.querySelector(".app").classList.toggle("is-open");
        backdrop.hidden = !open;
      });
    }
    initTabs();
    initFilters();
    initControls();
    initSegments();
    initSparks();
    initCharts();
    initLogs();
    initWizards();
    initPipelines();
    initRowActions();
    initDrawers();

    /* Deep links from the rail, the sidebar or the command palette open the
       matching dialog, e.g. servers.html#wizard. No scrollIntoView anywhere. */
    if (location.hash === "#wizard") {
      const wiz = Array.prototype.find.call(document.querySelectorAll(".overlay"), function (o) {
        return o.querySelector("[data-wizard]");
      });
      if (wiz) openOverlay(wiz.id);
    }
  }

  window.Gotham = {
    toast: toast,
    open: openOverlay,
    close: closeOverlay,
    icon: ic,
    chart: drawChart,
    init: function (root) {
      initTabs(root); initFilters(root); initControls(root); initSegments(root);
      initSparks(root); initLogs(root); initWizards(root); initPipelines(root);
      initRowActions(root); initDrawers(root);
    }
  };

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", mount);
  else mount();
})();
