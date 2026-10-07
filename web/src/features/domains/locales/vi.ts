/**
 * Vietnamese catalog for the domains feature. Keys mirror `en.ts` exactly;
 * only values are translated. Wire values (provider types, challenge modes,
 * redirect codes, domains, URLs, Traefik middleware names, product names)
 * stay untranslated by design.
 */
const vi = {
  page: {
    eyebrow: "Vận hành · Reverse proxy",
    title: "Tên miền & SSL",
    descriptionPre: "Traefik 3.1 chạy trên mọi node dưới dạng container",
    descriptionPost:
      "Control plane sinh cấu hình động từ trạng thái ứng dụng thông qua file " +
      "provider — dễ kiểm thử, lũy đẳng, và luôn giữ một phiên bản trước để " +
      "hoàn tác nhanh. Các chứng chỉ bên dưới là bản ghi ý định; node thực " +
      "hiện phát hành ACME.",
    addProvider: "Thêm nhà cung cấp DNS",
    addCertificate: "Thêm chứng chỉ",
  },
  tabs: {
    routers: "Bộ định tuyến",
    certificates: "Chứng chỉ",
    dns: "Nhà cung cấp DNS",
    redirects: "Chuyển hướng",
  },
  stats: {
    certConfigs: "Cấu hình chứng chỉ",
    certConfigsSub: "{count} đang bật · một cho mỗi ứng dụng",
    wildcardConfigs: "Cấu hình wildcard",
    wildcardSub: "yêu cầu thử thách dns-01",
    dnsProviders: "Nhà cung cấp DNS",
    dnsProvidersSub: "{count} đang bật · thông tin xác thực đã niêm phong",
    appsWithDomain: "Ứng dụng có tên miền",
    appsWithDomainSub: "chỉnh sửa trên từng trang ứng dụng",
  },
  providers: {
    title: "Nhà cung cấp DNS",
    add: "Thêm nhà cung cấp",
    sealedNote:
      "Thông tin xác thực được niêm phong phía máy chủ và API không bao " +
      "giờ trả về. Giao diện có thể đặt hoặc xoay vòng thông tin xác thực, " +
      "nhưng không thể hiển thị hay sao chép nó.",
    zones: "Vùng",
    credential: "Thông tin xác thực",
    credentialSet: "đã đặt",
    credentialUnset: "chưa đặt",
    credentialNeverReturned: "· API không bao giờ trả về token",
    updated: "Cập nhật",
    enableLabel: "Bật {name}",
    editRotate: "Sửa & xoay vòng thông tin xác thực",
    delete: "Xóa",
    deleteConfirm:
      "Xóa nhà cung cấp {provider} {name}? Các nhà cung cấp đang được cấu " +
      "hình chứng chỉ tham chiếu thì không thể xóa.",
    empty: "Chưa cấu hình nhà cung cấp DNS nào.",
    emptyHint:
      "Thử thách DNS-01 cần thông tin xác thực của nhà cung cấp. Chứng " +
      "chỉ wildcard yêu cầu thử thách DNS-01.",
    dns01Title: "DNS-01 yêu cầu vùng phải được ủy quyền cho nhà cung cấp",
    dns01BodyPre: "Let's Encrypt xác thực qua bản ghi TXT",
    dns01BodyPost:
      "do nhà cung cấp tạo. Nếu máy chủ tên của vùng không trỏ về " +
      "Cloudflare hoặc DigitalOcean, yêu cầu sẽ thất bại với NXDOMAIN. " +
      "Control plane chỉ lưu các vùng đã cấu hình; nó không kiểm tra ủy " +
      "quyền giúp bạn.",
    enabled: "đang bật",
    disabled: "đang tắt",
    unknownProvider: "nhà cung cấp không xác định",
    saved: "Đã lưu nhà cung cấp DNS.",
    created: "Đã tạo nhà cung cấp DNS.",
    enabledToast: "Đã bật nhà cung cấp.",
    disabledToast: "Đã tắt nhà cung cấp.",
    deleted: "Đã xóa nhà cung cấp DNS.",
  },
  providerDialog: {
    addTitle: "Thêm nhà cung cấp DNS",
    editTitle: "Sửa nhà cung cấp DNS",
    provider: "Nhà cung cấp",
    providerAria: "Loại nhà cung cấp",
    name: "Tên",
    nameAria: "Tên nhà cung cấp",
    namePlaceholder: "Nhãn tùy chọn, ví dụ Production Cloudflare",
    zones: "Vùng",
    zonesAria: "Vùng DNS",
    zonesPlaceholder: "Nhập vùng rồi nhấn Enter",
    credential: "Thông tin xác thực",
    rotateCredential: "Xoay vòng thông tin xác thực (tùy chọn)",
    credentialAria: "Thông tin xác thực của nhà cung cấp",
    credentialPlaceholder: "Token API",
    keepCredentialPlaceholder: "Để trống để giữ thông tin xác thực đã lưu",
    enabled: "Đang bật",
    enabledAria: "Nhà cung cấp đang bật",
    sealedNote:
      "Thông tin xác thực chỉ được gửi một lần qua API và niêm phong phía " +
      "máy chủ; nó không bao giờ hiển thị, ghi log hay lưu lại trong trình " +
      "duyệt.",
    cancel: "Hủy",
    save: "Lưu",
  },
  certificates: {
    title: "Cấu hình chứng chỉ",
    onePerApp: "một cho mỗi ứng dụng",
    add: "Thêm chứng chỉ",
    domain: "Tên miền",
    challenge: "Thử thách",
    dnsProvider: "Nhà cung cấp DNS",
    wildcard: "Wildcard",
    wildcardTag: "wildcard",
    no: "không",
    enabled: "Đang bật",
    enabledTag: "đang bật",
    disabledTag: "đang tắt",
    status: "Trạng thái",
    expires: "Hết hạn",
    updated: "Cập nhật",
    actions: "Hành động",
    edit: "Sửa",
    delete: "Xóa",
    deleteConfirm:
      "Xóa cấu hình chứng chỉ cho {domain}? Tuyến sẽ trở lại HTTP thường.",
    empty: "Chưa có cấu hình chứng chỉ nào.",
    statusPresent: "hiện có",
    statusAbsent: "không có chứng chỉ",
    statusUnknown: "không rõ",
    statusNotReported: "chưa báo cáo",
    expiryNotReported: "chưa báo cáo",
    baseDomainChanged:
      "{app} · tên miền gốc đã đổi thành {domain} — lưu lại để ghi nhận",
    statusTitle: "Trạng thái được quan sát trực tiếp từ node sở hữu",
    statusBody:
      "present nghĩa là bộ nhớ ACME của node đang giữ chứng chỉ cho tên " +
      "miền đã ghi và hiển thị ngày hết hạn; absent nghĩa là đã đọc bộ nhớ " +
      "nhưng không có; unknown nghĩa là không đọc được node. Trạng thái " +
      "được tính khi đọc — control plane chỉ lưu cấu hình mong muốn.",
    footer:
      "Tên miền đã ghi lấy từ tên miền gốc của ứng dụng tại thời điểm " +
      "lưu. Đổi tên miền gốc sau đó thì cần lưu lại cấu hình để ghi nhận.",
    saved: "Đã lưu cấu hình chứng chỉ.",
    created: "Đã tạo cấu hình chứng chỉ.",
    deleted: "Đã xóa cấu hình chứng chỉ.",
  },
  certificateDialog: {
    addTitle: "Thêm chứng chỉ",
    editTitle: "Sửa cấu hình chứng chỉ",
    note: "Tên miền đã ghi luôn lấy từ tên miền gốc của ứng dụng đã chọn — không chỉnh sửa tại đây.",
    cancel: "Hủy",
    save: "Lưu",
  },
  certificateForm: {
    application: "Ứng dụng",
    applicationAria: "Ứng dụng",
    selectApplication: "Chọn ứng dụng",
    domainFromApp: "Tên miền (từ ứng dụng)",
    noBaseDomain: "Chưa có tên miền gốc — hãy đặt trên ứng dụng trước.",
    appWithoutDomain: "{name} · chưa có tên miền gốc",
    challenge: "Thử thách",
    httpHint: "· bộ giải HTTP dùng chung",
    dnsHint: "· bản ghi TXT qua nhà cung cấp DNS",
    dnsProvider: "Nhà cung cấp DNS",
    dnsProviderAria: "Nhà cung cấp DNS",
    selectProvider: "Chọn nhà cung cấp DNS",
    providerDisabled: "{label} · {provider} (đang tắt)",
    httpNoProvider: "Bộ giải HTTP-01 dùng chung không cần nhà cung cấp.",
    wildcard: "Wildcard",
    wildcardAria: "Chứng chỉ wildcard",
    wildcardRequested: "đã yêu cầu",
    wildcardRequires: "Wildcard yêu cầu thử thách DNS-01.",
    enabled: "Đang bật",
    enabledAria: "Cấu hình chứng chỉ đang bật",
  },
  routers: {
    title: "Danh sách bộ định tuyến — backend đang chờ",
    empty: "Các bộ định tuyến Traefik đã sinh chưa được API công bố.",
    hint: "Control plane sinh cấu hình file-provider từ trạng thái ứng dụng (BE-6.1), nhưng chưa có endpoint đọc các bộ định tuyến thành phẩm. Bảng bộ định tuyến trực tiếp sẽ đến trong gói backend sau; tại đây không hiển thị gì thay vì bịa dữ liệu.",
    manageDomains: "Quản lý tên miền ứng dụng",
  },
  redirects: {
    rulesTitle: "Quy tắc chuyển hướng",
    middlewareTag: "middleware redirectregex",
    addTitle: "Thêm chuyển hướng",
    appliesAfter: "áp dụng sau lần đồng bộ cấu hình tới",
    application: "Ứng dụng",
    applicationAria: "Ứng dụng chuyển hướng",
    selectApplication: "Chọn ứng dụng",
    appWithoutDomain: "{name} · chưa có tên miền gốc",
    sourceDomain: "Tên miền nguồn",
    sourceAria: "Tên miền nguồn chuyển hướng",
    targetDomain: "Tên miền đích",
    targetAria: "Tên miền đích chuyển hướng",
    redirectCode: "Mã chuyển hướng",
    codeAria: "Mã chuyển hướng",
    codePermanent: "vĩnh viễn",
    codeTemporary: "tạm thời",
    codeOption: "{code} · {kind}",
    preservePath: "Giữ đường dẫn",
    preserveAria: "Giữ đường dẫn chuyển hướng",
    enabled: "Đang bật",
    enabledNowAria: "Bật chuyển hướng ngay",
    addRedirect: "Thêm chuyển hướng",
    ruleHint:
      "Nguồn và đích phải khác nhau. Mã áp dụng cho GET; HEAD và mọi " +
      "phương thức khác trả lời 308/307, giữ nguyên phương thức.",
    editTitle: "Sửa quy tắc chuyển hướng",
    owningApp: "Ứng dụng:",
    cannotMove: "— ứng dụng sở hữu không thể đổi sau khi tạo.",
    editSourceAria: "Sửa tên miền nguồn chuyển hướng",
    editTargetAria: "Sửa tên miền đích chuyển hướng",
    editCodeAria: "Sửa mã chuyển hướng",
    editPreserveAria: "Sửa giữ đường dẫn chuyển hướng",
    editEnabledAria: "Sửa trạng thái bật chuyển hướng",
    enableRedirectAria: "Bật chuyển hướng {source}",
    cancel: "Hủy",
    save: "Lưu",
    paused: "tạm dừng",
    keepsPath: "· giữ đường dẫn",
    edit: "Sửa",
    delete: "Xóa",
    deleteConfirm:
      "Xóa chuyển hướng {source} → {target}? Các yêu cầu tới nguồn sẽ " +
      "ngừng chuyển hướng.",
    empty: "Chưa có quy tắc chuyển hướng nào.",
    emptyHint:
      "Một quy tắc gửi một tên miền nguồn chính xác tới một tên miền đích " +
      "chính xác. Đích phải tự phục vụ chứng chỉ của nó.",
    /**
     * Rule count with library pluralization (one | other): Vietnamese
     * repeats the identical segment so the strict en/vi segment parity holds.
     */
    rulesSummary:
      "{total} quy tắc · {enabled} đang bật. GET trả lời mã đã lưu; các " +
      "phương thức khác trả lời 308/307 để giữ phương thức. | " +
      "{total} quy tắc · {enabled} đang bật. GET trả lời mã đã lưu; các " +
      "phương thức khác trả lời 308/307 để giữ phương thức.",
    footer:
      "Quy tắc do middleware redirectRegex của Traefik áp dụng ở lần đồng " +
      "bộ cấu hình động tiếp theo — không cần triển khai lại.",
    created: "Đã tạo quy tắc chuyển hướng.",
    saved: "Đã lưu quy tắc chuyển hướng.",
    enabledToast: "Đã bật quy tắc chuyển hướng.",
    pausedToast: "Đã tạm dừng quy tắc chuyển hướng.",
    deleted: "Đã xóa quy tắc chuyển hướng.",
  },
  errors: {
    sessionExpired: "Phiên đăng nhập đã hết hạn. Vui lòng đăng nhập lại.",
    adminScope:
      "Bạn cần phạm vi admin để quản lý tên miền và SSL. Hãy đăng nhập " +
      "bằng tài khoản admin hoặc dùng token API admin.",
    invalidRequest:
      "Yêu cầu không hợp lệ. Kiểm tra các trường được đánh dấu rồi thử lại.",
    notFound: "Không tìm thấy. Có thể nó đã bị xóa.",
    conflict: "Thay đổi xung đột với trạng thái hiện tại.",
    nodeUnreachable:
      "Không kết nối được node agent. Kiểm tra trạng thái node rồi thử lại.",
    secretNotConfigured:
      "Chưa cấu hình khóa bí mật triển khai nên không thể lưu thông tin " +
      "xác thực (hãy đặt GOTHAM_SECRET_KEY).",
    requestFailed: "Yêu cầu thất bại",
    unexpected: "Đã xảy ra lỗi. Vui lòng thử lại.",
  },
};

export default vi;
