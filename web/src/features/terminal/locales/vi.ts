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
    movedOpenHint:
      "Thu nhỏ cửa sổ để bắt đầu phiên mới trên trang hoặc đóng terminal này tại đây.",
    showWindow: "Hiện cửa sổ",
    close: "Đóng terminal",
    host: "Shell trên máy chủ",
    target: "Đích",
    cwd: "Thư mục làm việc",
    shell: "Shell",
    size: "{cols} × {rows}",
    serviceUser: "Chạy dưới {user}",
    privilege: "Quản trị toàn bộ máy chủ (thực tế là root).",
    application: "Ứng dụng",
    containerNote: "Shell này chạy bên trong container ứng dụng đã chọn.",
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
    agentUpdate:
      "Agent trên máy chủ này cần được cập nhật để mở terminal mà không cần thư mục.",
    badResponse:
      "Bảng điều khiển đã trả về phản hồi không mong đợi. Hãy bắt đầu phiên mới để thử lại.",
    generic: "Không thể khởi động terminal.",
    originRefused:
      "Kết nối terminal đã bị từ chối trước khi mở. Phiên của bạn có thể đã hết hạn, hoặc proxy chặn origin của bảng điều khiển. Hãy đăng nhập lại rồi bắt đầu phiên mới.",
    sessionExpired:
      "Terminal đã đóng trước khi shell khởi động. Phiên có thể đã hết hạn; hãy bắt đầu phiên mới.",
    applicationForbidden:
      "Bạn cần quyền ghi vào nhóm của ứng dụng này để mở shell trong container.",
    applicationNotFound: "Ứng dụng này không còn tồn tại.",
    noDeployment:
      "Ứng dụng này chưa có đợt triển khai nào đang chạy. Hãy triển khai rồi thử lại.",
    deployInFlight:
      "Đang có một đợt triển khai. Hãy đợi đợt triển khai kết thúc rồi thử lại.",
    containerStopped:
      "Container đã chọn không chạy. Hãy khởi động ứng dụng rồi làm mới danh sách service.",
    containerGone:
      "Container đã chọn không còn khả dụng. Hãy làm mới danh sách service và chọn một container đang chạy.",
    containerNoShell: "Container này không có shell được hỗ trợ.",
    terminalDisabled:
      "Terminal container đang bị tắt trên bản điều khiển này. Hãy đề nghị quản trị viên nền tảng bật lại.",
  },
  actions: {
    retry: "Bắt đầu phiên mới",
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
    chooseSuggestion:
      "Ctrl+Shift+Space chọn gợi ý lệnh nhanh đang hiện; Enter chèn mà không chạy.",
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
      "Trang này không còn terminal đang hoạt động. Hệ thống sẽ không tự khởi động; hãy dùng nút bên dưới khi bạn sẵn sàng.",
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
  quickCommands: {
    open: "Lệnh nhanh",
    title: "Lệnh nhanh",
    count: "{count} lệnh đã lưu",
    add: "Thêm lệnh",
    atLimit: "Bạn đã đạt giới hạn 40 lệnh. Hãy xóa một lệnh để thêm lệnh khác.",
    addTitle: "Lệnh mới",
    editTitle: "Sửa lệnh",
    insert: "Chèn",
    insertLabel: "Chèn {label} vào terminal",
    editLabel: "Sửa {label}",
    deleteLabel: "Xóa {label}",
    save: "Lưu lệnh",
    cancel: "Hủy",
    label: "Nhãn",
    labelPlaceholder: "Deploy API",
    command: "Lệnh",
    commandPlaceholder: "docker compose up -d",
    abbreviation: "Viết tắt (tùy chọn)",
    abbreviationPlaceholder: "dcu",
    abbreviationHint:
      "Gõ tại dấu nhắc được hỗ trợ để thấy lệnh. Chỉ dùng chữ, số, dấu chấm, gạch ngang và gạch dưới.",
    empty: "Chưa có lệnh nào được lưu.",
    storageNote:
      "Được lưu trong trình duyệt này cho tài khoản này. Không mã hóa — không lưu bí mật.",
    saveFailed:
      "Trình duyệt không thể lưu thay đổi. Các lệnh vẫn dùng được cho đến khi tải lại.",
    saveRejected:
      "Không thể lưu lệnh. Hãy kiểm tra các trường và viết tắt.",
    deleteBody: "Xóa {label} khỏi trình duyệt này?",
    inserted: "Đã chèn {label}. Nhấn Enter trong terminal để chạy.",
    insertMultiline:
      "Lệnh nhiều dòng cần hỗ trợ bracketed paste; không có gì được chèn.",
    insertControl:
      "Lệnh chứa ký tự điều khiển; không có gì được chèn.",
    insertEmpty: "Lệnh trống; không có gì được chèn.",
    insertUnavailable: "Terminal chưa sẵn sàng; không có gì được chèn.",
    dismissNotice: "Bỏ qua",
    hintTitle: "Gợi ý viết tắt đang tắt",
    hintBody:
      "Shell này không phát dấu nhắc. Hãy thêm đoạn mã tùy chọn vào tệp rc của shell; Gotham không bao giờ tự chèn vào shell.",
    hintAlternate:
      "Ứng dụng toàn màn hình đang hoạt động; gợi ý sẽ tắt cho đến khi dấu nhắc shell trở lại.",
    hintUnsupported:
      "Gợi ý tự động hỗ trợ bash và zsh. Chèn thủ công hoạt động với mọi shell.",
    copySnippet: "Sao chép đoạn mã",
    copied: "Đã sao chép",
    validation: {
      labelRequired: "Hãy đặt nhãn cho lệnh này.",
      labelTooLong: "Nhãn tối đa 64 ký tự.",
      commandRequired: "Nhập nội dung lệnh cần chèn.",
      commandTooLong: "Lệnh tối đa 2.048 ký tự.",
      commandNul: "Lệnh không được chứa ký tự NUL.",
      commandControl:
        "Lệnh không được chứa ký tự điều khiển ngoài xuống dòng và tab.",
      commandBidi:
        "Lệnh không được chứa ký tự điều khiển hướng văn bản hai chiều.",
      abbreviationTooLong: "Viết tắt tối đa 24 ký tự.",
      abbreviationFormat:
        "Dùng chữ, số, dấu chấm, gạch ngang hoặc gạch dưới, bắt đầu bằng chữ hoặc số.",
      abbreviationDuplicate: "Lệnh khác đã dùng viết tắt này.",
      tooMany: "Bạn có thể lưu tối đa 40 lệnh.",
      idInvalid: "Mã lệnh không hợp lệ.",
    },
  },
  suggestions: {
    label: "Gợi ý lệnh nhanh",
    announce:
      "Gợi ý: {label} cho {abbreviation}. Nhấn Control+Shift+Space để chọn hoặc Escape để bỏ qua.",
    hint: "Ctrl+Shift+Space để chọn · Esc để bỏ qua",
    placeholder: "Gõ viết tắt để chèn một lệnh đã lưu.",
    placeholderEmpty: "Lưu lệnh nhanh có viết tắt để thấy gợi ý tại đây.",
    choose: "Chọn",
    dismiss: "Bỏ qua gợi ý",
  },
};

export default vi;
