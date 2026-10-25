import type { ProfileMessages } from "./en";

const vi: ProfileMessages = {
  page: {
    eyebrow: "Cài đặt · Tài khoản",
    title: "Hồ sơ",
    description:
      "Thông tin tài khoản, tên hiển thị trong sidebar và mật khẩu của bạn. Địa chỉ email không thể đổi tại đây.",
  },
  identity: {
    title: "Tài khoản",
    avatarNote:
      "Ảnh đại diện lấy từ GitHub khi bạn đăng nhập bằng GitHub, nếu không sẽ hiện chữ cái đầu.",
    email: "Email",
    role: "Vai trò",
    owner: "Chủ sở hữu",
    member: "Thành viên",
    memberSince: "Tham gia từ",
    signedIn: "Đã đăng nhập",
  },
  displayName: {
    title: "Tên hiển thị",
    ariaLabel: "Tên hiển thị",
    placeholder: "Ada Lovelace",
    hint: "Hiện trong sidebar thay cho email. Xóa để quay lại dùng email. 1-64 ký tự.",
    submit: "Lưu tên hiển thị",
    updated: "Đã cập nhật tên hiển thị.",
  },
  password: {
    title: "Đổi mật khẩu",
    currentLabel: "Mật khẩu hiện tại",
    currentPlaceholder: "Mật khẩu hiện tại của bạn",
    newLabel: "Mật khẩu mới",
    newPlaceholder: "Ít nhất 10 ký tự",
    confirmLabel: "Xác nhận mật khẩu mới",
    confirmPlaceholder: "Nhập lại mật khẩu mới",
    hint: "Ít nhất 10 ký tự với 2 nhóm ký tự: chữ thường, chữ hoa, chữ số, ký hiệu.",
    submit: "Đổi mật khẩu",
    changed: "Đã đổi mật khẩu. Các thiết bị khác đã bị đăng xuất.",
  },
  sessions: {
    title: "Phiên đang hoạt động",
    intro:
      "Mọi thiết bị đang đăng nhập tài khoản của bạn. Kết thúc một phiên sẽ đăng xuất thiết bị đó; kết thúc thiết bị này sẽ đăng xuất bạn tại đây.",
    loading: "Đang tải phiên",
    empty: "Không có phiên nào.",
    retry: "Thử lại",
    thisDevice: "Thiết bị này",
    created: "Đã tạo",
    lastActive: "Hoạt động gần nhất",
    unknownIp: "không rõ",
    unknownTime: "không rõ",
    unknownDevice: "Thiết bị không xác định",
    deviceOn: "trên",
    signOut: "Đăng xuất",
    keep: "Giữ lại",
    signOutOthers: "Đăng xuất mọi thiết bị khác",
    confirmOthersPositive: "Đăng xuất các phiên khác",
    confirmCurrent:
      "Đăng xuất thiết bị này? Bạn sẽ bị đăng xuất tại đây và quay lại trang đăng nhập.",
    confirmOther: "Đăng xuất {label}? Thiết bị đó sẽ phải đăng nhập lại.",
    confirmOthers:
      "Đăng xuất {count} phiên khác? Các thiết bị đó sẽ phải đăng nhập lại. | Đăng xuất {count} phiên khác? Các thiết bị đó sẽ phải đăng nhập lại.",
    needsReauth:
      "Lần đăng nhập của bạn có trước quản lý phiên. Hãy đăng nhập lại để quản lý các phiên khác.",
    signInAgain: "Đăng nhập lại",
    loadFailed: "Không tải được phiên. Thử lại.",
    endFailed: "Không đăng xuất được phiên đó. Thử lại.",
    revokeOthersFailed: "Không đăng xuất được các phiên khác. Thử lại.",
    listStale: "Đã đăng xuất, nhưng danh sách phiên có thể đã cũ.",
    signedOut: "Các thiết bị khác đã bị đăng xuất.",
    sessionSignedOut: "Đã đăng xuất phiên.",
    signedOutHere: "Đã đăng xuất trên thiết bị này.",
  },
  totp: {
    title: "Xác thực hai lớp",
    intro:
      "Yêu cầu thêm mã theo thời gian từ ứng dụng xác thực bên cạnh mật khẩu khi đăng nhập.",
    statusOn: "Đang bật",
    statusOff: "Đang tắt",
    recoveryRemaining: "Số mã khôi phục còn lại: {count}",
    retry: "Thử lại",
    enable: "Thiết lập xác thực hai lớp",
    disable: "Tắt",
    regenerate: "Tạo lại mã khôi phục",
    setupTitle: "Thiết lập xác thực hai lớp",
    scanHint:
      "Quét mã này bằng ứng dụng xác thực của bạn (Google Authenticator, 1Password, Authy).",
    qrLabel: "Mã QR thiết lập ứng dụng xác thực",
    manualHint: "Hoặc nhập khóa này bằng tay:",
    secretLabel: "Khóa thiết lập",
    copySecret: "Sao chép khóa",
    passwordLabel: "Mật khẩu hiện tại",
    passwordPlaceholder: "Mật khẩu hiện tại của bạn",
    codeLabel: "Mã từ ứng dụng",
    codePlaceholder: "Mã 6 chữ số",
    codeHint: "Lấy từ ứng dụng xác thực, hoặc dùng một mã khôi phục.",
    confirmEnable: "Bật",
    disableTitle: "Tắt xác thực hai lớp",
    disableHint:
      "Nhập mật khẩu và một mã hiện tại từ ứng dụng hoặc một mã khôi phục.",
    disableSubmit: "Tắt",
    regenerateTitle: "Tạo lại mã khôi phục",
    regenerateHint:
      "Các mã khôi phục hiện tại sẽ hết hiệu lực. Nhập mật khẩu và một mã hiện tại.",
    regenerateSubmit: "Tạo lại",
    codesHint:
      "Hãy lưu các mã khôi phục ở nơi an toàn. Mỗi mã dùng được một lần nếu bạn mất thiết bị, và chỉ hiển thị lần này.",
    codesLabel: "Mã khôi phục",
    copyCodes: "Sao chép mã",
    downloadCodes: "Tải .txt",
    codesSaved: "Tôi đã lưu",
    cancel: "Hủy",
    enabledToast: "Đã bật xác thực hai lớp.",
    disabledToast: "Đã tắt xác thực hai lớp.",
    regeneratedToast: "Đã tạo lại mã khôi phục.",
  },
  validation: {
    displayNameLength: "Tên hiển thị phải từ 1-64 ký tự",
    currentPasswordRequired: "Mật khẩu hiện tại là bắt buộc",
  },
};

export default vi;
