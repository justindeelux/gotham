import type { DashboardMessages } from "./en";

const vi: DashboardMessages = {
  header: {
    eyebrow: "Tổng quan",
    title: "Bảng điều khiển",
    description: "Một control plane cho mọi nút, ứng dụng và cơ sở dữ liệu.",
    addServer: "Thêm máy chủ",
  },
  status: {
    allClear: "Mọi thứ đang chạy",
    attentionOne: "{count} vấn đề cần chú ý",
    attentionOther: "{count} vấn đề cần chú ý",
    unavailable: "Không có trạng thái",
    noServers: "Chưa có máy chủ nào",
    starting: "Máy chủ đang khởi động",
    serversReady: "máy chủ sẵn sàng",
    appsRunning: "ứng dụng đang chạy",
    appsLoadFailed: "Không thể tải ứng dụng",
    retry: "Thử lại",
  },
  tiles: {
    incompleteHint: "Không đọc được một số trạng thái",
  },
  health: {
    title: "Tình trạng máy chủ",
    moreOne: "+{count} nút nữa",
    moreOther: "+{count} nút nữa",
    viewAll: "xem tất cả máy chủ",
    heartbeatMeta: "Cập nhật trực tiếp mỗi 10 giây",
    containersOne: "{count} container",
    containersOther: "{count} container",
  },
  empty: {
    title: "Bảng điều khiển đã sẵn sàng",
    body: "Thêm máy chủ đầu tiên để bắt đầu triển khai ứng dụng.",
    action: "Thêm máy chủ đầu tiên",
  },
  deploys: {
    title: "Lượt triển khai gần đây",
    empty: "Chưa có lượt triển khai nào",
    emptyHint: "Đẩy một ứng dụng để xem lịch sử build, thời lượng và trạng thái tại đây.",
    loadFailed: "Không thể tải các lượt triển khai gần đây",
    retry: "Thử lại",
  },
};

export default vi;
