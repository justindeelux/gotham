import type { CommonMessages } from "./en";

/**
 * Common Vietnamese catalog. Keys mirror `en.ts` exactly; only values are
 * translated. Technical units (B/KiB/MiB, compact m/h/d/mo/y suffixes) and
 * wire values stay untranslated by design.
 */
const vi: CommonMessages = {
  common: {
    actions: {
      save: "Lưu",
      cancel: "Hủy",
      close: "Đóng",
      back: "Quay lại",
      retry: "Thử lại",
      reload: "Tải lại",
      confirm: "Xác nhận",
      delete: "Xóa",
      create: "Tạo",
      edit: "Sửa",
    },
    roles: {
      owner: "chủ sở hữu",
      admin: "quản trị viên",
      readOnly: "chỉ đọc",
      member: "Thành viên nhóm",
    },
    status: {
      unknown: "không rõ",
      never: "Chưa bao giờ",
    },
    errors: {
      requestFailed: "Yêu cầu thất bại",
      unexpected: "Đã xảy ra lỗi. Vui lòng thử lại.",
      rateLimited: "Quá nhiều lần thử, vui lòng chờ",
    },
    clipboard: {
      copied: "Đã sao chép {label}",
      copyFailed: "Không thể sao chép {label}",
    },
    chart: {
      seriesOf: "Biểu đồ chuỗi thời gian của {names}",
    },
  },
  validation: {
    invalid: "Giá trị không hợp lệ",
    required: "Trường này là bắt buộc",
  },
  language: {
    label: "Ngôn ngữ",
    names: {
      en: "English",
      vi: "Tiếng Việt",
    },
  },
  nav: {
    label: "Điều hướng sản phẩm",
    sections: {
      operations: "Vận hành",
      team: "Nhóm",
      system: "Hệ thống",
    },
    items: {
      dashboard: "Bảng điều khiển",
      projects: "Dự án",
      files: "Quản lý tệp",
      templates: "Thư viện mẫu",
      servers: "Máy chủ",
      domains: "Tên miền & SSL",
      toolchain: "Kho ứng dụng",
      teams: "Thành viên & vai trò",
      notifications: "Kênh thông báo",
      gitSources: "Nguồn Git",
      instance: "Hệ thống Gotham",
      updates: "Cập nhật & cài đặt",
    },
  },
  shell: {
    navToggle: "Bật/tắt điều hướng",
    serversToggle: "Bật/tắt máy chủ",
    envTitle: "Môi trường phục vụ: {env}",
    cpPortTitle: "Cổng HTTP control-plane",
    account: "Tài khoản",
    signIn: "Đăng nhập",
    profile: "Hồ sơ",
    signOut: "Đăng xuất",
    signedIn: "Đã đăng nhập",
  },
  titles: {
    login: "Đăng nhập",
    register: "Tạo tài khoản",
    oauthCallback: "Đang đăng nhập",
    providerCallback: "Đã kết nối nhà cung cấp",
    githubAppCallback: "GitHub App",
    dashboard: "Bảng điều khiển",
    servers: "Máy chủ",
    serverDetail: "Chi tiết máy chủ",
    serverContainers: "Container",
    serverFiles: "Tệp",
    files: "Quản lý tệp",
    projects: "Dự án",
    projectDetail: "Chi tiết dự án",
    environmentDetail: "Chi tiết môi trường",
    applicationDetail: "Chi tiết ứng dụng",
    databaseDetail: "Chi tiết cơ sở dữ liệu",
    domains: "Tên miền & SSL",
    serviceDetail: "Chi tiết dịch vụ",
    templates: "Thư viện mẫu",
    toolchain: "Kho ứng dụng",
    teams: "Nhóm",
    notifications: "Kênh thông báo",
    instance: "Cài đặt instance",
    gitSources: "Nguồn Git",
    profile: "Hồ sơ",
    updates: "Cập nhật",
    inviteAccept: "Lời mời vào nhóm",
    notFound: "Không tìm thấy trang",
  },
  time: {
    justNow: "vừa xong",
    inMoment: "trong giây lát",
    never: "Chưa bao giờ",
    unknown: "không rõ",
    expiresToday: "hết hạn hôm nay",
    expiredToday: "đã hết hạn hôm nay",
  },
};

export default vi;
