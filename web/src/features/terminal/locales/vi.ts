/**
 * Terminal Vietnamese catalog (JUS-245). Same key shape and named parameters
 * as the English reference; see `terminal/locales/en.ts`.
 */
const vi = {
  window: {
    label: "Cửa sổ terminal",
    heading: "Terminal",
    minimize: "Thu nhỏ cửa sổ terminal",
    close: "Đóng cửa sổ terminal",
    resize: "Thay đổi kích thước cửa sổ terminal",
    tabs: "Các phiên terminal",
    activeTarget: "Đích",
    activeCwd: "Thư mục làm việc",
  },
  rail: {
    openWithCount: "Cửa sổ terminal, {count} đang mở",
  },
  pane: {
    openInWindow: "Mở trong cửa sổ",
    movedTitle: "Terminal này đang mở trong cửa sổ",
    movedBody:
      "Phiên, bộ đệm và thư mục làm việc đã chuyển sang cửa sổ nổi. Phiên này không thể quay lại trang.",
    showWindow: "Hiện cửa sổ",
    close: "Đóng terminal",
    host: "Shell trên máy chủ",
    target: "Đích",
    cwd: "Thư mục làm việc",
    shell: "Shell",
    size: "{cols} × {rows}",
    serviceUser: "Chạy dưới {user}",
    privilege: "Quản trị toàn bộ máy chủ (thực tế là root).",
    detailLabel: "Terminal cho {title}",
  },
  status: {
    connecting: "Đang kết nối…",
    connected: "Đã kết nối",
    exited: "Đã thoát",
    disconnected: "Mất kết nối",
    error: "Lỗi",
  },
  exited: {
    title: "Phiên đã kết thúc",
    withCode: "Shell đã thoát với mã {code}.",
    withSignal: "Shell bị dừng bởi {signal}.",
    ended: "Phiên shell đã kết thúc.",
  },
  disconnected: {
    title: "Mất kết nối",
    body: "Luồng dữ liệu đã đóng. Tạo phiên mới sẽ không phát lại dữ liệu đã nhập trước đó.",
  },
  errors: {
    title: "Không thể mở terminal",
    auth: "Phiên đăng nhập đã kết thúc. Hãy đăng nhập lại để mở terminal.",
    forbidden: "Chỉ quản trị viên nền tảng mới có thể mở terminal máy chủ.",
    node: "Không tìm thấy máy chủ hoặc máy chủ không còn được quản lý.",
    quota: "Đã đạt giới hạn phiên terminal. Hãy đóng một phiên và thử lại.",
    unsupported: "Agent trên máy chủ này không hỗ trợ phiên terminal.",
    unavailable: "Agent trên máy chủ không khả dụng. Hãy thử lại khi agent kết nối lại.",
    network: "Không thể kết nối tới bảng điều khiển.",
    generic: "Không thể khởi động terminal.",
  },
  actions: {
    retry: "Tạo phiên mới",
    show: "Hiện terminal",
    minimize: "Thu nhỏ",
    close: "Đóng",
  },
  close: {
    titleTab: "Đóng terminal này?",
    titleWindow: "Đóng tất cả terminal?",
    messageTab: "Shell cho {title} sẽ kết thúc và tiến trình đang chạy sẽ bị dừng.",
    messageWindow: "Các phiên này sẽ kết thúc và tiến trình đang chạy sẽ bị dừng:",
    listLabel: "Các phiên sẽ đóng",
    cancel: "Giữ terminal",
    confirm: "Đóng terminal",
    confirmAll: "Đóng tất cả",
  },
  shortcuts: {
    closeTab: "Nhấn Delete trên tab đang chọn để đóng terminal đó sau khi xác nhận.",
    closeWindow: "Alt+Shift+W đóng tất cả terminal sau khi xác nhận.",
    minimize: "Alt+M thu nhỏ cửa sổ.",
  },
};

export default vi;
