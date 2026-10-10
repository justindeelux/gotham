import type en from "./en";

/** Vietnamese instance-settings catalog: same keys as `en.ts`, translated values. */
const vi: typeof en = {
  page: {
    eyebrow: "Cài đặt · Quản trị nền tảng",
    title: "Hệ thống Gotham",
    description:
      "Cấu hình chính instance Gotham: địa chỉ công khai, tên và múi giờ, mạng của máy chủ và một số tùy chọn Linux. Chỉ dành cho quản trị viên nền tảng.",
  },
  tabs: { general: "Chung", network: "Mạng", system: "Hệ thống" },
  forbidden: "Chỉ quản trị viên nền tảng mới xem hoặc thay đổi được cài đặt instance.",
  unsupported:
    "Máy này chưa hỗ trợ thay đổi cấu hình máy chủ: chưa cài helper hoặc máy không dùng systemd-networkd. Giá trị chỉ hiển thị, không sửa được. Xem deploy/README.md.",
  locked: "Bị khóa bởi biến môi trường {name}.",
  general: {
    title: "Chung",
    url: "URL control plane",
    urlHint: "URL công khai dùng cho đăng ký agent, OAuth redirect và webhook.",
    name: "Tên instance",
    timezone: "Múi giờ",
    timezoneHint: "Múi giờ IANA. Áp dụng cho lịch chạy, hiển thị log và sao lưu.",
    submit: "Lưu cài đặt chung",
    saved: "Đã lưu cài đặt chung.",
  },
  network: {
    title: "Mạng",
    dns: "Máy chủ DNS",
    dnsHint: "Tối đa 3 resolver, cách nhau bằng dấu phẩy.",
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
  },
  validation: {
    url: "Nhập URL http(s) có tên máy chủ, không kèm thông tin đăng nhập, query hay fragment.",
    name: "Tên phải dài 1-64 ký tự.",
    timezone: "Nhập múi giờ IANA, ví dụ Europe/Berlin.",
    dns: "Nhập tối đa 3 địa chỉ IPv4 hoặc IPv6 khác nhau.",
    ipv4Address: "Nhập địa chỉ IPv4 dạng CIDR, ví dụ 192.168.1.10/24.",
    ipv4Gateway: "Nhập gateway IPv4 hợp lệ.",
    ipv6Address: "Nhập địa chỉ IPv6 dạng CIDR, ví dụ 2001:db8::10/64.",
    ipv6Gateway: "Nhập gateway IPv6 hợp lệ.",
    hostname: "Dùng chữ cái, chữ số và dấu gạch ngang; dấu chấm ngăn cách các nhãn.",
    ntp: "Nhập tối đa 4 tên máy chủ hoặc địa chỉ IP khác nhau.",
  },
};

export default vi;
