/**
 * Vietnamese catalog for the toolchain surface. Same shape as the English
 * catalog; tool ids, versions, paths and API payloads stay raw parameters.
 */
import type en from "./en";

const vi: typeof en = {
  page: {
    eyebrow: "Vận hành · Công cụ máy chủ",
    title: "Kho ứng dụng",
    description:
      "Duyệt danh mục công cụ của một nút, cài đặt hoặc gỡ bỏ, và xếp hàng nhiều công cụ cùng lúc. Trạng thái cập nhật trực tiếp khi tác vụ chạy.",
    selectNode: "Chọn một nút",
    selectNodePlaceholder: "Chọn nút để quản lý công cụ",
    noNodes: "Chưa có nút nào. Hãy thêm máy chủ trước.",
    searchPlaceholder: "Tìm công cụ…",
    installSelected: "Cài các mục đã chọn",
    queue: "Hàng đợi",
    emptyTools: "Nút này chưa báo cáo công cụ nào",
    emptyToolsHint: "Agent trên nút này chưa báo cáo công cụ nào.",
    emptySearch: "Không có công cụ nào khớp tìm kiếm",
    emptySearchHint: "Hãy thử từ khóa khác.",
    detectOnlyNote: "Chỉ phát hiện: agent báo cáo công cụ này nhưng không thể cài đặt.",
    skipped: "Đã bỏ qua {tools}: đã có trong hàng đợi",
    enqueued: "Đã xếp hàng {count}",
    uncategorized: "Khác",
  },
  status: {
    installed: "Đã cài {version}",
    installedUnknown: "Đã cài đặt",
    missing: "Chưa cài đặt",
    outdated: "Có bản mới",
  },
  actions: {
    install: "Cài đặt",
    uninstall: "Gỡ bỏ",
    cancel: "Hủy",
    details: "Chi tiết",
  },
  queue: {
    queued: "Đang đợi",
    running: "Đang chạy",
    succeeded: "Thành công",
    failed: "Thất bại",
    canceled: "Đã hủy",
  },
  errors: {
    sessionExpired: "Phiên đăng nhập đã hết hạn. Vui lòng đăng nhập lại.",
    unexpected: "Đã xảy ra lỗi. Vui lòng thử lại.",
    notFound: "Không tìm thấy nút. Nó có thể đã bị xóa — hãy chọn nút khác.",
    conflict: "Tác vụ đã rời hàng đợi nên không thể hủy.",
  },
};

export default vi;
