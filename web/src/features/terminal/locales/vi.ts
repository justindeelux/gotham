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
    closeTab: "Đóng {title}",
    activeTarget: "Đích",
    activeCwd: "Thư mục làm việc",
  },
  rail: {
    openWithCount: "Cửa sổ terminal, {count} đang mở",
    pageWithCount: "Terminal trên trang, {count} đang mở",
  },
  pane: {
    openInWindow: "Mở trong cửa sổ",
    movedTitle: "Terminal này đang mở trong cửa sổ",
    movedBody:
      "Phiên, bộ đệm và thư mục làm việc đã chuyển sang cửa sổ nổi. Phiên này không thể quay lại trang.",
    showWindow: "Hiện cửa sổ",
    startNew: "Bắt đầu phiên mới",
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
    cwdInvalid:
      "Thư mục làm việc không hợp lệ. Hãy mở một thư mục bên trong vùng gốc được cho phép của máy chủ.",
    cwdMissing: "Thư mục đó không còn tồn tại trên máy chủ.",
    cwdUnreadable: "Agent trên máy chủ không thể đọc thư mục đó.",
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
    title: "Phím tắt terminal",
    show: "Phím tắt terminal",
    escape:
      "Ctrl+Shift+E rời khỏi vùng nhập terminal và đưa tiêu điểm về tab cửa sổ hoặc nút trên khung. Shift+Tab cũng rời khỏi shell.",
    closeTab:
      "Nhấn Delete trên tab đang chọn để đóng terminal đó sau khi xác nhận.",
    closeWindow:
      "Alt+Shift+W đóng tất cả terminal trong cửa sổ sau khi xác nhận khi tiêu điểm ở thanh tiêu đề hoặc tab.",
    minimize:
      "Alt+M thu nhỏ cửa sổ khi tiêu điểm ở thanh tiêu đề hoặc tab.",
    hint:
      "Ctrl+Shift+E hoặc Shift+Tab rời khỏi vùng nhập terminal. Trong shell, phím Alt và Option được gửi tới terminal (readline, vim, tmux); Alt+M và Alt+Shift+W hoạt động từ thanh cửa sổ.",
  },
  page: {
    navLabel: "Terminal",
    title: "Terminal",
    eyebrow: "Vận hành",
    description:
      "Mở shell trên máy chủ đã quản lý. Quyền terminal là quản trị toàn bộ máy chủ và chỉ dành cho quản trị viên nền tảng.",
    tabLabel: "Terminal",
    switchServer: "Máy chủ",
    chooseServer: "Chọn máy chủ",
    chooseHint: "Chọn một máy chủ sẵn sàng để mở terminal trên trang.",
    empty: "Chưa có máy chủ nào",
    emptyHint: "Hãy thêm và kiểm tra một máy chủ trước khi mở terminal.",
    addServer: "Thêm máy chủ",
    starting: "Đang khởi động terminal…",
    sessionClosedTitle: "Phiên đã đóng",
    sessionClosedBody:
      "Trang này không còn terminal đang hoạt động. Hãy bắt đầu phiên mới khi bạn sẵn sàng; hệ thống sẽ không tự khởi động.",
    startNew: "Bắt đầu phiên mới",
    notAvailableTitle: "Không thể dùng terminal",
    notAvailableBody:
      "Terminal chỉ dành cho quản trị viên nền tảng. Hãy đề nghị quyền truy cập rồi tải lại trang này.",
    notReadyTitle: "Không thể mở terminal",
    notReadyBody:
      "Máy chủ này đang ở trạng thái {status}. Terminal chỉ có thể bắt đầu trên node sẵn sàng.",
    unknownServer: "Máy chủ này không được quản lý bởi bảng điều khiển.",
  },
  dock: {
    label: "Terminal trên trang đang tạm dừng",
    heading: "Terminal trên trang",
    paused: "Tạm dừng",
    resume: "Tiếp tục",
    resumeTab: "Tiếp tục terminal {title}",
    openInWindow: "Mở trong cửa sổ",
    openInWindowTab: "Mở {title} trong cửa sổ",
    expand: "Hiện các terminal trên trang đang tạm dừng",
    collapse: "Ẩn các terminal trên trang đang tạm dừng",
    closeTab: "Đóng {title}",
  },
  appearance: {
    label: "Giao diện terminal",
    title: "Giao diện terminal",
    themes: {
      gothamDark: "Gotham Tối",
      gothamLight: "Gotham Sáng",
    },
  },
  banner: {
    server: "máy chủ",
  },
};

export default vi;
