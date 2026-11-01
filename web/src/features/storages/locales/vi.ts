import type { StoragesMessages } from "./en";

const vi: StoragesMessages = {
  page: {
    eyebrow: "Quản lý · Tích hợp",
    title: "Lưu trữ",
    description:
      "Nơi các bản sao lưu được ghi vào. Cấu hình một lần tại đây và mọi lần " +
      "sao lưu cùng lịch chạy đều dùng được.",
    add: "Thêm nơi lưu trữ",
  },
  title: "Nơi lưu trữ",
  empty: "Chưa có nơi lưu trữ nào",
  onlyBuiltin:
    "Bản sao lưu đang nằm trên đĩa của bảng điều khiển. Thêm một nơi lưu trữ " +
    "tương thích S3 để giữ bản sao ở chỗ khác.",
  builtin: "Có sẵn",
  usage: "{count} bản sao lưu · {size}",
  freeSpace: "còn trống {free} trên {total}",
  test: "Kiểm tra",
  testFailed: "Kiểm tra kết nối thất bại.",
  deleteConfirm:
    "Xóa {name}? Các bản sao lưu đang nằm ở đó sẽ không đọc lại được và " +
    "không thể khôi phục. Nơi lưu trữ còn được lịch chạy dùng sẽ không xóa được.",
  saved: "Đã lưu nơi lưu trữ “{name}” · thông tin đăng nhập đã được mã hóa",
  updated: "Đã cập nhật nơi lưu trữ “{name}”",
  deleted: "Đã xóa nơi lưu trữ “{name}”",
  addTitle: "Thêm nơi lưu trữ",
  editTitle: "Sửa nơi lưu trữ",
  save: "Lưu nơi lưu trữ",
  localNote:
    "Đĩa cục bộ giữ bản sao lưu trên bảng điều khiển. Không cần cấu hình và " +
    "không thể xóa.",
  secretsNote:
    "Khóa được mã hóa trên máy chủ và không bao giờ hiển thị lại — để trống " +
    "ô khóa để giữ khóa đã lưu.",
  kindS3: "Tương thích S3",
  kindLocal: "Đĩa cục bộ",
  controlPlaneDisk: "đĩa của bảng điều khiển",
  credsOk: "Đã cấu hình thông tin đăng nhập",
  fields: {
    name: "Tên",
    kind: "Loại",
    endpoint: "Endpoint",
    region: "Vùng",
    bucket: "Bucket",
    prefix: "Tiền tố",
    accessKey: "Access key",
    secretKey: "Secret key",
  },
  placeholders: {
    name: "Tên nơi lưu trữ",
    endpoint: "https://…endpoint",
    region: "Vùng (ví dụ: auto)",
    bucket: "Bucket",
    prefix: "Tiền tố khóa (không bắt buộc)",
    accessKey: "Access key",
    accessKeyEdit: "Access key · để trống giữ khóa đã lưu",
    secretKey: "Secret key",
    secretKeyEdit: "Secret key · để trống giữ khóa đã lưu",
  },
  aria: {
    name: "Tên nơi lưu trữ",
    kind: "Loại nơi lưu trữ",
    endpoint: "Endpoint",
    region: "Vùng",
    bucket: "Bucket",
    prefix: "Tiền tố khóa",
    accessKey: "Access key",
    secretKey: "Secret key",
    test: "Kiểm tra nơi lưu trữ {name}",
    edit: "Sửa nơi lưu trữ {name}",
    delete: "Xóa nơi lưu trữ {name}",
  },
  errors: {
    invalidRequest: "Cấu hình lưu trữ bị từ chối",
    notFound: "Không tìm thấy nơi lưu trữ",
    conflict: "Không thể thay đổi nơi lưu trữ",
    forbidden: "Chỉ chủ tài khoản có quyền admin mới quản lý được nơi lưu trữ",
    unavailable: "Quản lý lưu trữ hiện không khả dụng",
    agentUnreachable:
      "Không kết nối được node agent hoặc tác vụ thất bại trên node. Kiểm tra trạng thái node rồi thử lại.",
  },
  validation: {
    nameRequired: "Hãy nhập tên nơi lưu trữ.",
    s3LocationRequired: "Nơi lưu trữ tương thích S3 cần endpoint và bucket.",
    s3KeysRequired: "Hãy nhập cả access key và secret key.",
  },
};

export default vi;
