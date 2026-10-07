import type { DashboardMessages } from "./en";

/**
 * Dashboard Vietnamese catalog. Keys mirror `en.ts` exactly; only values
 * are translated. Counts, wire values and technical source notes stay
 * untranslated by design.
 */
const vi: DashboardMessages = {
  header: {
    eyebrow: "Tổng quan",
    title: "Bảng điều khiển",
    description: "Một control plane cho mọi nút, ứng dụng và cơ sở dữ liệu.",
    viewServers: "Xem máy chủ",
    addServer: "Thêm máy chủ",
  },
  kpi: {
    serversReady: "Máy chủ sẵn sàng",
    noServers: "Chưa có máy chủ nào — hãy thêm một máy để bắt đầu.",
    unreachable: "{names} không thể kết nối",
    applications: "Ứng dụng đang chạy",
    viewProjects: "Xem dự án",
    deploys: "Triển khai trong 24 giờ",
    noDeploys: "Chưa có dữ liệu triển khai",
    ssl: "Chứng chỉ SSL",
    noSsl: "Chưa có dữ liệu chứng chỉ",
    loadFailed: "Không thể tải ứng dụng",
    retry: "Thử lại",
    noApplications: "Chưa có ứng dụng nào",
  },
  tiles: {
    incompleteHint: "Không đọc được một số trạng thái",
  },
  health: {
    title: "Tình trạng máy chủ",
    moreOne: "+{count} nút nữa",
    moreOther: "+{count} nút nữa",
    viewAll: "xem tất cả máy chủ",
    empty: "Chưa có máy chủ nào",
    emptyHint: "Thêm nút đầu tiên để xem tình trạng CPU, RAM và đĩa tại đây.",
    heartbeatMeta: "heartbeat mỗi 10s qua gRPC server-authenticated TLS",
    addServer: "Thêm máy chủ",
    containersOne: "{count} container",
    containersOther: "{count} container",
  },
  aside: {
    heartbeat: "Nhịp tim",
    live: "trực tiếp",
    noHeartbeats: "Chưa có nhịp tim nào — hãy thêm máy chủ",
    moreOne: "+{count} nút nữa",
    moreOther: "+{count} nút nữa",
    components: "Thành phần control-plane",
    noTelemetry: "Chưa có dữ liệu đo thành phần",
    telemetryNote: "Tình trạng cơ sở dữ liệu, bộ nhớ đệm và cổng chưa được báo cáo.",
    teamActivity: "Hoạt động nhóm",
    noActivity: "Chưa có hoạt động nhóm nào",
    alerts: "Cảnh báo",
    unreachableTitle: "{name} không thể kết nối",
    heartbeatLost: "Mất nhịp tim agent. Thấy lần cuối {time}.",
    noAlerts: "Không có cảnh báo — mọi nút đều ổn",
  },
  deploys: {
    title: "Lượt triển khai gần đây",
    empty: "Chưa có lượt triển khai nào",
    emptyHint: "Đẩy một ứng dụng để xem lịch sử build, thời lượng và trạng thái tại đây.",
    queue: "Hàng đợi: chưa có dữ liệu",
    noHistory: "Chưa có lịch sử build để hiện",
  },
};

export default vi;
