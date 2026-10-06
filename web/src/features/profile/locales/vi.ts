import type { ProfileMessages } from "./en";

/**
 * Vietnamese profile catalog. Keys mirror `en.ts` exactly; only values are
 * translated. Technical tokens (JWT, email addresses, device/browser/OS
 * names) stay untranslated by design.
 */
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
    platformRole: "Vai trò nền tảng",
    admin: "Quản trị nền tảng",
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
    unknownDevice: "Thiết bị không xác định",
    deviceOn: "trên",
    signOut: "Đăng xuất",
    keep: "Giữ lại",
    signOutOthers: "Đăng xuất mọi thiết bị khác",
    confirmOthersPositive: "Đăng xuất các phiên khác",
    confirmCurrent:
      "Đăng xuất thiết bị này? Bạn sẽ bị đăng xuất tại đây và quay lại trang đăng nhập.",
    confirmOther: "Đăng xuất {label}? Thiết bị đó sẽ phải đăng nhập lại.",
    confirmOthersOne:
      "Đăng xuất {count} phiên khác? Các thiết bị đó sẽ phải đăng nhập lại.",
    confirmOthersMany:
      "Đăng xuất {count} phiên khác? Các thiết bị đó sẽ phải đăng nhập lại.",
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
  validation: {
    displayNameLength: "Tên hiển thị phải từ 1-64 ký tự",
    currentPasswordRequired: "Mật khẩu hiện tại là bắt buộc",
  },
};

export default vi;
