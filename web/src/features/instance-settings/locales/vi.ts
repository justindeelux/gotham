import type en from "./en";

const vi: typeof en = {
  page: {
    eyebrow: "Cài đặt · Chủ sở hữu",
    title: "Hệ thống Gotham",
    description:
      "Cấu hình chính instance Gotham: địa chỉ công khai, tên và múi giờ, mạng của máy chủ và một số tùy chọn Linux. Chỉ dành cho chủ sở hữu.",
  },
  tabs: { general: "Chung", network: "Mạng", system: "Hệ thống" },
  forbidden: "Chỉ chủ sở hữu mới xem hoặc thay đổi được cài đặt instance.",
  locked: "Bị khóa bởi biến môi trường {name}.",
  helper: {
    missing:
      "Helper máy chủ chưa được cài, nên các giá trị chỉ hiển thị. Hãy cài để quản lý cài đặt máy chủ từ Gotham.",
    restart:
      "Thay đổi máy chủ do trình cài đặt control plane thiết lập, nên chủ sở hữu phải chạy lại trình cài đặt (hoặc nâng cấp) để bật chúng.",
    install: "Cài helper máy chủ",
    installed: "Đã cài helper máy chủ. Các cài đặt này giờ có thể chỉnh sửa.",
    installFailed: "Không cài được helper máy chủ.",
    logTitleFailed: "Cài helper máy chủ thất bại",
    logHint: "Kết quả từ trình cài trên máy chủ này.",
    close: "Đóng",
  },
  general: {
    title: "Chung",
    url: "URL control plane",
    urlHint: "URL công khai dùng cho đăng ký agent, OAuth redirect và webhook.",
    name: "Tên instance",
    timezone: "Múi giờ",
    timezoneHint:
      "Danh sách IANA có thể tìm kiếm, kèm độ lệch UTC hiện tại. Áp dụng cho lịch chạy, hiển thị log và sao lưu. Bị vô hiệu khi khóa bởi môi trường.",
    submit: "Lưu cài đặt chung",
    saved: "Đã lưu cài đặt chung.",
  },
  domainStatus: {
    title: "Trạng thái tên miền control plane",
    empty: "Đặt URL control plane để xem trạng thái router.",
    invalid: "Nhập URL control plane hợp lệ để kiểm tra trạng thái.",
    dnsUnknown: "DNS: chưa báo cáo",
    certUnknown: "Chứng chỉ: chưa báo cáo",
    certState: {
      issued: "Chứng chỉ: đã cấp",
      pending: "Chứng chỉ: đang chờ cấp",
      failed: "Chứng chỉ: cấp thất bại",
    },
    certExpiry: "Hết hạn {date}",
    certNotApplicable: "Chứng chỉ: không áp dụng (HTTP)",
    observationHint:
      "API router không báo cáo kết quả phân giải DNS.",
    loading: "Router: đang kiểm tra…",
    synced: "Router: đã đồng bộ",
    pending: "Router: chờ đồng bộ",
    unknown: "Router: không rõ",
    missingLocalhost:
      "Node localhost chưa được đăng ký. " +
      "Hãy đăng ký node trước khi đồng bộ router control plane.",
    readFailed: "Không đọc được trạng thái router localhost. Hãy kiểm tra node và thử lại.",
    unsaved: "Lưu URL trước khi tạo và đồng bộ router tương ứng.",
    refresh: "Làm mới trạng thái",
  },
  network: {
    title: "Mạng",
    dnsPrimary: "Máy chủ DNS chính",
    dnsAlternate: "Máy chủ DNS phụ",
    dnsExtra: "Máy chủ DNS {n}",
    dnsAdd: "Thêm máy chủ DNS",
    dnsRemove: "Gỡ",
    dnsHint: "DNS chính bắt buộc, DNS phụ tùy chọn. Tối đa 3 resolver.",
    optional: "tùy chọn",
    mode: "Chế độ",
    dhcp: "DHCP",
    auto: "Tự động",
    static: "Tĩnh",
    address: "Địa chỉ (CIDR)",
    gateway: "Gateway",
    ipv6Enabled: "Bật IPv6",
    submit: "Áp dụng thay đổi mạng",
    applied: "Đã áp dụng thay đổi mạng. Hãy xác nhận trước khi đếm ngược kết thúc.",
    pendingNote: "Đang có thay đổi mạng chờ xác nhận. Hãy xử lý trước khi thay đổi tiếp.",
    dnsOnlyNote: "Chỉ đổi máy chủ DNS: cấu hình interface được giữ nguyên.",
    riskyWarning:
      "Thay đổi này can thiệp vào địa chỉ của interface bạn đang kết nối. Máy chủ có thể mất kết nối cho đến khi bạn xác nhận hoặc hoàn tác.",
    confirmLabel: "Tôi hiểu thay đổi này có thể làm gián đoạn kết nối",
    confirmRequired: "Hãy tích vào ô xác nhận để áp dụng thay đổi này.",
    stackUnsupported:
      "Máy chủ này không chạy systemd-networkd và systemd-resolved, nên Gotham không thể quản lý mạng. Giá trị chỉ hiển thị.",
  },
  confirm: {
    title: "Giữ các thay đổi mạng này?",
    body: "Cấu hình mới đang chạy. Nếu bạn không truy cập được Gotham ở địa chỉ mới, đừng làm gì: máy chủ sẽ tự khôi phục cấu hình cũ sau",
    keep: "Giữ thay đổi",
    revert: "Hoàn tác ngay",
  },
  system: {
    title: "Hệ thống",
    hostname: "Hostname",
    ntpEnabled: "Đồng bộ thời gian (NTP)",
    ntpServers: "Máy chủ NTP",
    ntpHint: "Tối đa 4, cách nhau bằng dấu phẩy. Để trống để dùng mặc định của hệ thống.",
    submit: "Lưu cài đặt hệ thống",
    saved: "Đã lưu cài đặt hệ thống.",
    stackUnsupported:
      "Máy chủ này thiếu các công cụ Gotham dùng cho cài đặt hệ thống. Giá trị chỉ hiển thị.",
  },
  validation: {
    url: "Nhập URL http(s) có tên máy chủ, không kèm thông tin đăng nhập, query hay fragment.",
    name: "Tên phải dài 1-64 ký tự.",
    timezone: "Chọn múi giờ IANA, ví dụ Europe/Berlin.",
    dns: "Nhập tối đa 3 địa chỉ IPv4 hoặc IPv6 khác nhau.",
    dnsSingle: "Nhập một địa chỉ IPv4 hoặc IPv6 hợp lệ.",
    ipv4Address: "Nhập địa chỉ IPv4 dạng CIDR, ví dụ 192.168.1.10/24.",
    ipv4Gateway: "Nhập gateway IPv4 hợp lệ.",
    ipv6Address: "Nhập địa chỉ IPv6 dạng CIDR, ví dụ 2001:db8::10/64.",
    ipv6Gateway: "Nhập gateway IPv6 hợp lệ.",
    hostname: "Dùng chữ cái, chữ số và dấu gạch ngang; dấu chấm ngăn cách các nhãn.",
    ntp: "Nhập tối đa 4 tên máy chủ hoặc địa chỉ IP khác nhau.",
  },
};

export default vi;
