import type { McpMessages } from "./en";

const vi: McpMessages = {
  page: {
    eyebrow: "Cài đặt · Kết nối",
    title: "Thiết lập MCP",
    description:
      "Kết nối Claude Code, Claude Desktop, Cursor hoặc ứng dụng MCP khác với bảng điều khiển này. Chọn mức truy cập, tạo token và dán cấu hình có sẵn vào ứng dụng.",
  },
  access: {
    title: "1. Chọn quyền cho ứng dụng",
    hint: "Token giữ nguyên mức truy cập này. Hãy tạo token mới nếu muốn đổi sau này.",
    memberHint:
      "Token giữ nguyên mức truy cập này và ứng dụng chỉ chạm tới những dự án bạn được cấp quyền. Hãy tạo token mới nếu muốn đổi sau này.",
    read: {
      name: "Chỉ đọc",
      description:
        "Xem dự án, máy chủ, ứng dụng và tài nguyên. Không thể tạo, thay đổi hay triển khai gì.",
    },
    deploy: {
      name: "Đọc và triển khai",
      description:
        "Mọi quyền của chỉ đọc, cộng thêm công cụ tạo, thay đổi và triển khai. Chọn mức này khi muốn trợ lý thực hiện thay đổi.",
    },
    create: "Tạo token",
  },
  clients: {
    title: "2. Sao chép cấu hình cho ứng dụng",
    hint: "Tạo token để điền giá trị thật vào đây. Không có gì được lưu trong trình duyệt; hãy sao chép trước khi rời trang.",
    copy: "Sao chép",
    claudeCode: "Claude Code (dòng lệnh)",
    claudeDesktop: "Claude Desktop / Cursor",
    urlToken: "Ứng dụng khác (URL và token)",
  },
  https: {
    title: "Kết nối này không được mã hóa",
    owner:
      "URL bảng điều khiển đang dùng HTTP thường, nên token và dữ liệu công cụ truyền đi không được mã hóa. Hãy đặt URL bảng điều khiển thành địa chỉ HTTPS trong cài đặt hệ thống.",
    member:
      "URL bảng điều khiển đang dùng HTTP thường, nên token và dữ liệu công cụ truyền đi không được mã hóa. Hãy đề nghị chủ sở hữu đặt URL bảng điều khiển thành địa chỉ HTTPS.",
    open: "Mở cài đặt hệ thống",
  },
  reveal: {
    title: "Cấu hình MCP của bạn",
    warning:
      "Hãy sao chép một đoạn cấu hình hoặc token ngay. Token chỉ hiện một lần và không thể hiện lại; các đoạn cấu hình chỉ giữ token khi hộp thoại này mở.",
    aria: "Token MCP mới",
    copy: "Sao chép token",
    done: "Xong",
  },
  toast: {
    created: "Đã tạo token.",
  },
  errors: {
    loadFailed: "Không tải được kết nối MCP. Thử lại.",
    createFailed: "Không tạo được token. Thử lại.",
    retry: "Thử lại",
  },
};

export default vi;
