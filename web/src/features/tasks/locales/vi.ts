import type { TasksMessages } from "./en";

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
      unknown: "Đã xong",
    },
    steps: {
      cloning: "Đang tải mã nguồn",
      building: "Đang build",
      pushing: "Đang đẩy image",
      starting: "Đang khởi động",
    },
  },
};

export default vi;
