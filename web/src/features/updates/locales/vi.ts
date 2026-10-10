/**
 * Vietnamese updates catalog. Keys mirror `en.ts` exactly; only values are
 * translated.
 */
const vi = {
  page: {
    eyebrow: "Cài đặt · Hệ thống",
    title: "Cập nhật",
    description:
      "Kiểm tra bản phát hành Gotham mới, cập nhật control plane và chọn thời điểm Gotham tự kiểm tra và áp dụng cập nhật.",
    unavailable: "Tính năng tự cập nhật đang tắt trên bản cài đặt này.",
  },
  actions: {
    check: "Kiểm tra cập nhật",
    update: "Cập nhật ngay",
    save: "Lưu lịch",
    reset: "Đặt lại",
    cancel: "Hủy",
  },
  stats: {
    current: "Phiên bản hiện tại",
    latest: "Bản mới nhất",
    lastChecked: "Kiểm tra lần cuối",
    nextCheck: "Lần kiểm tra tới {time}",
    upToDate: "Đã là bản mới nhất",
    updateAvailable: "Có bản cập nhật",
    notChecked: "chưa kiểm tra",
    unknown: "không rõ",
    channel: { stable: "ổn định", beta: "beta" },
  },
  notes: { title: "Ghi chú phát hành · {version}", empty: "Bản phát hành này không có ghi chú." },
  changelog: {
    title: "Nhật ký thay đổi",
    currentTitle: "Điểm mới trong {version}",
    github: "Xem trên GitHub",
    empty: "Không tìm thấy ghi chú phát hành cho phiên bản này.",
    loadError: "Không thể tải nhật ký thay đổi.",
  },
  confirm: {
    title: "Cập nhật Gotham lên {version}?",
    body: "Gotham tải và xác minh bản phát hành rồi khởi động lại. Bảng điều khiển tạm thời không truy cập được và sẽ tự kết nối lại. Nếu kiểm tra sức khỏe thất bại, phiên bản trước sẽ được khôi phục.",
  },
  progress: {
    title: "Đang cập nhật",
    installing: "Đang cài {version}",
    restarting: "Đang khởi động lại — trang sẽ tự cập nhật.",
  },
  last: {
    title: "Lần cập nhật gần nhất",
    ok: "Thành công",
    staged: "Đang tiến hành",
    rolled_back: "Đã hoàn tác",
    rollback_failed: "Hoàn tác thất bại",
    no_backup: "Thất bại",
    wrapper_failed: "Thất bại",
    resuming: "Đang tiếp tục",
    detail: "Chi tiết: {detail}",
  },
  errors: {
    check: "Không thể kiểm tra cập nhật: {message}",
    apply: "Cập nhật thất bại: {message}",
    save: "Không thể lưu lịch: {message}",
    backoff:
      "Cập nhật tự động bỏ qua {version} đến {time} sau một lần cập nhật thất bại. Bạn vẫn có thể thử lại bằng Cập nhật ngay.",
  },
  schedule: {
    title: "Lịch cập nhật",
    hint: "Thay đổi đã lưu có hiệu lực ngay — không cần khởi động lại.",
    timezone: "Giờ tính theo {timezone}.",
    adminOnly: "Chỉ quản trị viên nền tảng mới có thể kiểm tra, cập nhật hoặc đổi lịch.",
    checkEnabled: "Tự động kiểm tra cập nhật",
    checkHint: "Gotham tìm bản phát hành mới theo lịch bên dưới.",
    autoApply: "Tự động áp dụng cập nhật",
    autoApplyHint: "Cài bản phát hành tìm thấy mà không cần xác nhận. Mặc định tắt.",
    channel: "Kênh",
    frequency: "Tần suất",
    frequencies: { interval: "Mỗi N giờ", daily: "Hằng ngày", weekly: "Hằng tuần" },
    intervalHours: "Khoảng cách (giờ)",
    time: "Giờ",
    weekday: "Ngày trong tuần",
    weekdays: {
      "0": "Chủ nhật",
      "1": "Thứ hai",
      "2": "Thứ ba",
      "3": "Thứ tư",
      "4": "Thứ năm",
      "5": "Thứ sáu",
      "6": "Thứ bảy",
    },
    saved: "Đã lưu lịch.",
  },
  validation: {
    intervalRange: "Nhập số giờ nguyên từ 1 đến 720.",
    timeFormat: "Dùng định dạng 24 giờ HH:MM.",
    autoApplyNeedsCheck: "Tự động áp dụng cần bật tự động kiểm tra.",
  },
};

export default vi;
