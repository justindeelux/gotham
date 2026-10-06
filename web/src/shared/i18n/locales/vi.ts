import type { CommonMessages } from "./en";

/**
 * Common Vietnamese catalog. Keys mirror `en.ts` exactly; only values are
 * translated. Technical units (B/KiB/MiB, compact m/h/d/mo/y suffixes) and
 * wire values stay untranslated by design.
 */
const vi: CommonMessages = {
  common: {
    actions: {
      save: "Lưu",
      cancel: "Hủy",
      close: "Đóng",
      back: "Quay lại",
      retry: "Thử lại",
      reload: "Tải lại",
      confirm: "Xác nhận",
      delete: "Xóa",
      create: "Tạo",
      edit: "Sửa",
    },
    roles: {
      owner: "chủ sở hữu",
      admin: "quản trị viên",
      readOnly: "chỉ đọc",
      member: "Thành viên",
    },
    status: {
      unknown: "không rõ",
      never: "không bao giờ",
    },
    errors: {
      requestFailed: "Yêu cầu thất bại",
      unexpected: "Đã xảy ra lỗi. Vui lòng thử lại.",
    },
  },
  validation: {
    invalid: "Giá trị không hợp lệ",
    required: "Trường này là bắt buộc",
  },
  language: {
    label: "Ngôn ngữ",
  },
  time: {
    justNow: "vừa xong",
    inMoment: "sắp tới",
    never: "không bao giờ",
    unknown: "không rõ",
    expiresToday: "hết hạn hôm nay",
    expiredToday: "đã hết hạn hôm nay",
  },
};

export default vi;
