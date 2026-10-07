/**
 * Vietnamese catalog for the teams feature. Keys mirror `en.ts` exactly;
 * only values are translated. Role words shown through the shared
 * `common.roles` catalog are not duplicated here; team names, emails,
 * tokens and URLs stay raw parameters.
 */
const vi = {
  page: {
    eyebrow: "Nhóm · Kiểm soát truy cập",
    title: "Nhóm",
    descriptionPre:
      "Mỗi tài nguyên thuộc về đúng một nhóm, và mỗi thành viên giữ một " +
      "trong ba vai trò —",
    descriptionPost:
      "Chủ sở hữu quản lý quyền sở hữu, quản trị viên quản lý tài nguyên " +
      "và thành viên, thành viên chỉ đọc chỉ được xem.",
    newTeam: "Nhóm mới",
    unavailableTitle: "Nhóm không khả dụng",
    unavailableDesc:
      "Quản lý nhóm chưa được bật trên control plane này (FEATURE_TEAMS=false).",
    yourRole: "vai trò của bạn: {role}",
    personal: "cá nhân",
    membersTab: "Thành viên ({count})",
    invitesTab: "Lời mời ({count})",
    refresh: "Tải lại",
  },
  list: {
    title: "Nhóm của bạn",
    /**
     * Team count with library pluralization (one | other): Vietnamese
     * repeats the identical segment so the strict en/vi segment parity holds.
     */
    count: "{count} nhóm | {count} nhóm",
    rename: "Đổi tên",
    delete: "Xóa",
    deleteConfirm:
      'Xóa nhóm "{name}"? Tài nguyên phải được chuyển đi trước — nhóm còn ' +
      "sở hữu tài nguyên sẽ bị từ chối.",
    empty: "Chưa có nhóm nào.",
  },
  members: {
    member: "Thành viên",
    role: "Vai trò",
    roleOfAria: "Vai trò của {email}",
    joined: "Tham gia",
    actions: "Hành động",
    personalOwner: "chủ sở hữu nhóm cá nhân",
    remove: "Gỡ",
    removeConfirm: "Gỡ {email} khỏi {team}?",
    footnote:
      "Một nhóm luôn giữ ít nhất một chủ sở hữu: hạ cấp hay gỡ người cuối " +
      "cùng sẽ bị từ chối kèm thông báo của backend. Tư cách chủ sở hữu của " +
      "nhóm cá nhân là bất biến.",
  },
  invites: {
    email: "Email",
    role: "Vai trò",
    sent: "Đã gửi",
    expires: "Hết hạn",
    actions: "Hành động",
    inviteMember: "Mời thành viên",
    singleUse: "Lời mời dùng một lần và gắn với địa chỉ email.",
    readOnlyNote: "Vai trò của bạn không liệt kê hay quản lý lời mời.",
    revoke: "Thu hồi",
    revokeConfirm: "Thu hồi lời mời tới {email}?",
  },
  dialogs: {
    newTitle: "Nhóm mới",
    newDesc:
      "Bạn trở thành chủ sở hữu của nhóm mới. Tài nguyên có thể được chuyển " +
      "vào từ các ứng dụng và máy chủ bạn đang quản lý.",
    teamNamePlaceholder: "Tên nhóm",
    teamNameAria: "Tên nhóm",
    createTeam: "Tạo nhóm",
    renameTitle: "Đổi tên nhóm",
    save: "Lưu",
    cancel: "Hủy",
    inviteTitle: "Mời thành viên",
    inviteEmailPlaceholder: "name{'@'}example.com",
    inviteEmailAria: "Email mời",
    inviteRoleAria: "Vai trò lời mời",
    inviteHint:
      "Lời mời chỉ cấp quản trị viên hoặc chỉ đọc — quyền sở hữu được " +
      "chuyển từ bảng thành viên, không bao giờ qua lời mời.",
    createInvite: "Tạo lời mời",
    tokenTitle: "Đã tạo lời mời — sao chép liên kết ngay",
    tokenWarning:
      "Liên kết này chỉ hiển thị một lần và không bao giờ lưu. Gửi email " +
      "tự động chưa được đấu nối (BE-8.3) — hãy tự gửi nó cho {email}.",
    newMemberLinkAria: "Liên kết thành viên mới",
    acceptLinkAria: "Liên kết chấp nhận",
    tokenText: "token: {token}",
    tokenBody:
      "Gửi liên kết đầu cho {email} để tạo tài khoản mới: đăng ký đã đóng " +
      "trên instance có sẵn tài khoản nên lời mời là đường vào duy nhất. " +
      "Liên kết thứ hai dành cho người đã có tài khoản và đang đăng nhập. " +
      "Dù cách nào thành viên cũng tham gia với vai trò {role}. " +
      "Hết hạn {expiry}.",
    copyLink: "Sao chép liên kết",
    copied: "Đã sao chép",
    done: "Xong",
  },
  toast: {
    createdTeam: "Đã tạo nhóm {name}",
    renamed: "Đã đổi tên nhóm",
    deletedTeam: "Đã xóa nhóm {name}",
    roleChanged: "{email} hiện là {role}",
    removedMember: "Đã gỡ {email}",
    revokedInvite: "Đã thu hồi lời mời tới {email}",
    linkCopied: "Đã sao chép liên kết mời",
    clipboardDenied:
      "Clipboard không khả dụng — hãy chọn liên kết và sao chép thủ công.",
  },
  errors: {
    sessionExpired: "Phiên đăng nhập đã hết hạn. Vui lòng đăng nhập lại.",
    forbiddenRole: "Vai trò nhóm của bạn không cho phép hành động này.",
    notFound: "Không tìm thấy. Có thể nó đã bị gỡ.",
    conflictChanged:
      "Nhóm đã thay đổi trong lúc bạn chỉnh sửa. Tải lại rồi thử lại.",
    inviteExpired: "Lời mời này đã hết hạn. Hãy tạo lời mời mới.",
    invalidRequest: "Yêu cầu không hợp lệ.",
    requestFailed: "Yêu cầu thất bại",
    unexpected: "Đã xảy ra lỗi. Vui lòng thử lại.",
  },
};

export default vi;
