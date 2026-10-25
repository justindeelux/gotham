import type { ApiTokensMessages } from "./en";

const vi: ApiTokensMessages = {
  page: {
    eyebrow: "Cài đặt · Tài khoản",
    title: "API token",
    description:
      "Token bearer cho tác vụ CI, script và tích hợp gọi API Gotham. Mỗi token thuộc tài khoản của bạn và chỉ làm được những gì phạm vi của nó cho phép.",
    create: "Tạo token",
  },
  list: {
    title: "Token của bạn",
    empty: "Chưa có API token nào. Hãy tạo một token cho tự động hóa của bạn.",
    retry: "Thử lại",
    columns: {
      name: "Tên",
      scopes: "Phạm vi",
      created: "Đã tạo",
      lastUsed: "Dùng gần nhất",
      status: "Trạng thái",
      actions: "Thao tác",
    },
    neverUsed: "Chưa dùng",
    active: "Đang hoạt động",
    revoked: "Đã thu hồi",
    revokedOn: "Thu hồi {date}",
    rotate: "Xoay vòng",
    revoke: "Thu hồi",
    rotateConfirm:
      "Xoay vòng {name}? Bí mật hiện tại ngừng hoạt động ngay, hãy cập nhật hệ thống đang dùng nó.",
    rotatePositive: "Xoay vòng",
    revokeConfirm:
      "Thu hồi {name}? Không thể hoàn tác và các yêu cầu dùng token này sẽ thất bại.",
    revokePositive: "Thu hồi",
  },
  create: {
    title: "Tạo API token",
    name: "Tên",
    namePlaceholder: "ci-deploy",
    nameHint:
      "Hiện trong danh sách này để bạn phân biệt các token. Tối đa 64 ký tự.",
    scope: "Phạm vi",
    submit: "Tạo token",
  },
  reveal: {
    title: "API token mới của bạn",
    warning:
      "Hãy sao chép token ngay. Token chỉ hiện một lần và không thể hiện lại; hãy lưu như mật khẩu.",
    aria: "API token mới",
    copy: "Sao chép token",
    copyLabel: "API token",
    done: "Xong",
  },
  scopes: {
    read: {
      name: "Đọc",
      description:
        "Xem dự án, máy chủ và ứng dụng. Không thể thay đổi gì.",
    },
    deploy: {
      name: "Triển khai",
      description:
        "Quyền đọc, cộng thêm triển khai, thay đổi tài nguyên dự án và đọc bí mật.",
    },
    admin: {
      name: "Quản trị",
      description:
        "Mọi quyền, gồm các thao tác chỉ chủ sở hữu: quyền người dùng, quản lý tệp, tên miền và cập nhật.",
    },
  },
  toast: {
    created: "Đã tạo token.",
    rotated: "Đã xoay vòng token.",
    revoked: "Đã thu hồi token.",
  },
  errors: {
    loadFailed: "Không tải được token. Thử lại.",
    createFailed: "Không tạo được token. Thử lại.",
    rotateFailed: "Không xoay vòng được token. Thử lại.",
    revokeFailed: "Không thu hồi được token. Thử lại.",
    sessionExpired: "Phiên đã hết hạn. Hãy đăng nhập lại.",
    forbidden: "Bạn không có quyền thực hiện thao tác đó.",
    notFound: "Token đó không còn tồn tại.",
  },
  validation: {
    nameRequired: "Tên là bắt buộc",
    nameMaxLength: "Tên phải có tối đa 64 ký tự",
  },
};

export default vi;
