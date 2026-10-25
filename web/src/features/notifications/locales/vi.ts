const vi = {
  page: {
    eyebrow: "Truy cập · Thông báo",
    title: "Kênh thông báo",
    description:
      "Sự kiện triển khai và sao lưu được gửi tới các kênh của bạn: " +
      "webhook Discord và Slack, bot Telegram, hoặc email SMTP. Kênh thuộc " +
      "phạm vi tài nguyên, và thông tin bí mật chỉ ghi — đọc chỉ thấy giá " +
      "trị đã che.",
    newChannel: "Kênh mới",
    unavailableTitle: "Thông báo không khả dụng",
    unavailableDesc:
      "Kênh thông báo đang tắt trên control plane này. " +
      "Hãy đề nghị chủ sở hữu bật.",
    empty: "Chưa có kênh nào.",
    createFirst: "Tạo kênh đầu tiên",
  },
  card: {
    enabledLabel: "Đang bật",
    enableAria: "Bật {name}",
    enabled: "đang bật",
    disabled: "đang tắt",
    secretConfigured: "đã đặt bí mật",
    sendTest: "Gửi thử",
    testDelivered: "Đã gửi thử: ",
    testFailed: "Gửi thử thất bại: ",
    noTest: "Chưa gửi thử lần nào.",
    edit: "Sửa",
    delete: "Xóa",
    deleteConfirm:
      'Xóa kênh "{name}"? Thông báo triển khai và sao lưu dừng ngay lập tức.',
    updated: "Cập nhật",
  },
  history: {
    show: "Lịch sử gửi",
    hide: "Ẩn lịch sử gửi",
    empty: "Chưa có lần gửi nào.",
    testEvent: "Tin nhắn thử",
    delivered: "Đã gửi",
    failed: "Thất bại",
  },
  kinds: {
    discord: "Webhook Discord",
    slack: "Webhook Slack",
    telegram: "Bot Telegram",
    email: "Email (SMTP)",
  },
  events: {
    deploy_success: "Triển khai thành công",
    deploy_failure: "Triển khai thất bại",
    backup_success: "Sao lưu thành công",
    backup_failure: "Sao lưu thất bại",
  },
  scopeLabel: {
    allResources: "Mọi tài nguyên",
    app: "Ứng dụng: {name}",
    database: "Cơ sở dữ liệu: {name}",
  },
  configSummary: {
    webhookMissing: "chưa cấu hình webhook",
    webhook: "webhook {value}",
    chat: "chat {id}",
    chatMissing: "—",
  },
  dialog: {
    newTitle: "Kênh thông báo mới",
    editTitle: "Sửa kênh thông báo",
    name: "Tên",
    namePlaceholder: "ví dụ Cảnh báo triển khai",
    nameAria: "Tên kênh",
    kind: "Loại",
    kindAria: "Loại kênh",
    webhookUrl: "URL webhook",
    webhookAria: "URL webhook",
    webhookPlaceholder: "https://discord.com/api/webhooks/…",
    webhookKeepPlaceholder: "Giữ nguyên giá trị đã che để dùng URL đã lưu",
    botToken: "Token bot",
    botTokenAria: "Token bot",
    botTokenPlaceholder: "123456:ABC-…",
    botTokenKeepPlaceholder: "Giữ nguyên giá trị đã che để dùng token đã lưu",
    chatId: "ID chat",
    chatIdAria: "ID chat",
    chatIdPlaceholder: "-100…",
    smtpHost: "Máy chủ SMTP",
    smtpHostAria: "Máy chủ SMTP",
    smtpHostPlaceholder: "smtp.example.com",
    smtpPort: "Cổng SMTP",
    smtpPortAria: "Cổng SMTP",
    smtpPortPlaceholder: "587",
    smtpUsername: "Tên đăng nhập SMTP",
    smtpUsernameAria: "Tên đăng nhập SMTP",
    smtpUsernamePlaceholder: "ops{'@'}example.com",
    smtpPassword: "Mật khẩu SMTP",
    smtpPasswordAria: "Mật khẩu SMTP",
    smtpPasswordPlaceholder: "Để trống cho relay mở",
    smtpPasswordKeepPlaceholder:
      "Giữ nguyên giá trị đã che để dùng mật khẩu đã lưu",
    fromAddress: "Địa chỉ gửi",
    fromAddressAria: "Địa chỉ gửi",
    fromAddressPlaceholder: "Gotham <ops{'@'}example.com>",
    recipients: "Người nhận",
    recipientsAria: "Người nhận",
    recipientsPlaceholder: "ops{'@'}example.com, oncall{'@'}example.com",
    events: "Sự kiện",
    eventsAria: "Sự kiện",
    selectEvents: "Chọn ít nhất một sự kiện",
    noEvents: "Chọn ít nhất một sự kiện.",
    outOfScope:
      "Kênh này đã đăng ký {names}, mà phạm vi tài nguyên không thể gửi. " +
      "Đăng ký đã lưu được giữ nguyên trừ khi bạn sửa sự kiện hoặc phạm vi.",
    eventsHintAll: "Kênh toàn cục nhận mọi sự kiện đã chọn.",
    eventsHintApp:
      "Ứng dụng gửi sự kiện triển khai; sự kiện sao lưu không tới được " +
      "kênh này.",
    eventsHintDb:
      "Cơ sở dữ liệu gửi sự kiện sao lưu; sự kiện triển khai không tới " +
      "được kênh này.",
    resourceScope: "Phạm vi tài nguyên",
    resourceScopeAria: "Phạm vi tài nguyên",
    scopeAll: "Mọi tài nguyên",
    scopeApp: "Ứng dụng",
    scopeAppUnavailable: "Ứng dụng (không khả dụng)",
    scopeDb: "Cơ sở dữ liệu",
    scopeDbUnavailable: "Cơ sở dữ liệu (không khả dụng)",
    resourceAria: "Tài nguyên",
    selectApp: "Chọn ứng dụng",
    selectDb: "Chọn cơ sở dữ liệu",
    missingResource: "{id} (thiếu)",
    unavailableBoth:
      "Ứng dụng và cơ sở dữ liệu không khả dụng trên control plane này; " +
      "kênh có thể giữ phạm vi toàn cục.",
    unavailableApps:
      "Ứng dụng đang tắt trên control plane này; " +
      "vẫn dùng được phạm vi cơ sở dữ liệu. Hãy đề nghị chủ sở hữu bật.",
    unavailableDbs:
      "Cơ sở dữ liệu đang tắt trên control plane này; " +
      "vẫn dùng được phạm vi ứng dụng. Hãy đề nghị chủ sở hữu bật.",
    enabled: "Đang bật",
    enabledAria: "Kênh đang bật",
    secretsNote:
      "Bí mật chỉ gửi một lần, niêm phong phía máy chủ và không bao giờ " +
      "hiển thị lại — đọc chỉ trả về mặt nạ. Kênh toàn cục nhận mọi sự " +
      "kiện đã chọn; kênh theo phạm vi chỉ nhận sự kiện của tài nguyên " +
      "đã chọn.",
    cancel: "Hủy",
    save: "Lưu",
    create: "Tạo kênh",
  },
  toast: {
    updated: "Đã cập nhật kênh",
    created: "Đã tạo kênh",
    deleted: "Đã xóa {name}",
  },
  errors: {
    sessionExpired: "Phiên đăng nhập đã hết hạn. Vui lòng đăng nhập lại.",
    forbiddenRole: "Bạn không có quyền thực hiện thao tác này.",
    featureDisabled:
      "Kênh thông báo đang tắt trên control plane này. " +
      "Hãy đề nghị chủ sở hữu bật.",
    invalidConfig: "Cấu hình kênh không hợp lệ.",
    requestFailed: "Yêu cầu thất bại",
    unexpected: "Đã xảy ra lỗi. Vui lòng thử lại.",
  },
};

export default vi;
