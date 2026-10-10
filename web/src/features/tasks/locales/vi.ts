import type { TasksMessages } from "./en";

/**
 * Tasks Vietnamese catalog: translated message values only.
 * Keys stay English per the repo language rule.
 */
const vi: TasksMessages = {
  card: {
    title: "Tác vụ nền",
    viewLogs: "Xem nhật ký",
    dismiss: "Ẩn",
    collapse: "Thu gọn",
    expand: "Mở rộng",
    status: {
      queued: "Đang chờ",
      running: "Đang chạy",
      succeeded: "Hoàn tất",
      failed: "Thất bại",
    },
    steps: {
      queued: "Đang chờ",
      cloning: "Đang tải mã nguồn",
      building: "Đang build",
      pushing: "Đang đẩy image",
      starting: "Đang khởi động",
      running: "Đang chạy",
      failed: "Thất bại",
    },
  },
};

export default vi;
