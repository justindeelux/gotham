import type { MonitoringMessages } from "./en";

const vi: MonitoringMessages = {
  page: {
    eyebrow: "Vận hành · Áp lực máy chủ",
    title: "Quy tắc giám sát",
    description:
      "Cảnh báo CPU, bộ nhớ và dung lượng tệp được đánh giá từ các quan sát nút vẫn gửi lên. Mặc định của instance áp dụng cho mọi nút; thiết lập riêng của nút sẽ được ưu tiên.",
  },
  scope: {
    label: "Phạm vi quy tắc",
    instance: "Mặc định của instance",
    hint: "Thay đổi quy tắc áp dụng cho các quan sát tiếp theo. Lịch sử không được đánh giá lại và không tạo ra phục hồi giả.",
  },
  source: {
    default: "Mặc định có sẵn",
    instance: "Mặc định của instance",
    node: "Thiết lập riêng của nút",
  },
  rules: {
    cpuTitle: "CPU",
    cpuHint: "Cảnh báo khi bằng hoặc vượt ngưỡng, xóa khi bằng hoặc thấp hơn giá trị xóa.",
    memoryTitle: "Bộ nhớ",
    memoryHint:
      "Bộ nhớ đã dùng theo phần trăm tổng bộ nhớ. Gotham không suy ra OOM từ giá trị này.",
    spaceTitle: "Dung lượng tệp",
    spaceHint:
      "Bao gồm gốc, dữ liệu Docker, lưu trữ được quản lý và bản sao lưu. Đĩa dữ liệu đầy vẫn cảnh báo dù phân vùng gốc khỏe.",
    inodeTitle: "Inode hệ thống tệp",
    inodeHint:
      "Áp lực inode có thể chặn ghi dù dung lượng còn trống. Bộ đếm inode không hỗ trợ vẫn là chưa rõ.",
    enabled: "Bật",
    warning: "Cảnh báo",
    critical: "Nghiêm trọng",
    downgrade: "Hạ mức nghiêm trọng",
    clear: "Xóa",
    confirm: "Cảnh báo trong",
    criticalFor: "Nghiêm trọng trong",
    downgradeFor: "Hạ mức trong",
    clearFor: "Xóa trong",
    defaultsNote:
      "Mặc định: CPU và bộ nhớ cảnh báo ở 90% trong 5 phút và xóa ở 85% trong 1 phút. Hệ thống tệp cảnh báo ở 90% trong 1 phút, nghiêm trọng ở 95% trong 1 phút, hạ mức ở 90% trong 1 phút và xóa ở 85% trong 1 phút.",
  },
  actions: {
    save: "Lưu quy tắc",
    reset: "Khôi phục mặc định",
    resetNode: "Dùng mặc định của instance",
    saved: "Đã lưu quy tắc giám sát",
    resetDone: "Đã khôi phục quy tắc giám sát",
  },
  validation: {
    range: "Nhập phần trăm lớn hơn 0 và tối đa 100.",
    duration: "Nhập thời lượng từ 10 đến 3600 giây.",
    order: "Các giá trị phải theo thứ tự: xóa < hạ mức ≤ cảnh báo < nghiêm trọng.",
  },
  status: {
    title: "Áp lực máy chủ",
    refresh: "Làm mới",
    fresh: "Quan sát mới",
    stale: "Quan sát cũ",
    unknown: "Chưa có quan sát",
    lastObservation: "Quan sát gần nhất:",
    unknownHint:
      "Nút chưa có quan sát mới. Mẫu thiếu hoặc chưa rõ không phải là phục hồi; sự cố vẫn mở cho đến khi có bằng chứng mới xóa nó.",
    noActive: "Không có áp lực CPU, bộ nhớ hay hệ thống tệp đang hoạt động.",
    suggestion:
      "Kiểm tra lưu trữ và nhật ký, hoặc giảm tải. Gotham không bao giờ tự xóa dữ liệu hay khởi động lại khối lượng công việc.",
    openMetrics: "Mở chỉ số",
  },
  detectors: {
    cpu: "Áp lực CPU",
    memory: "Áp lực bộ nhớ",
    space: "Dung lượng hệ thống tệp",
    inode: "Inode hệ thống tệp",
  },
  severity: {
    warning: "Cảnh báo",
    critical: "Nghiêm trọng",
  },
  filesystems: {
    mount: "Điểm gắn",
    purpose: "Mục đích",
    space: "Dung lượng",
    inodes: "Inode",
    available: "còn {available} trên {total}",
    unsupported: "Chưa rõ",
  },
  resourceHistory: {
    tab: "Số liệu",
    rangeLabel: "Khoảng thời gian",
    serviceLabel: "Dịch vụ",
    allServices: "Tất cả dịch vụ",
    refresh: "Làm mới",
    retry: "Thử lại",
    partial:
      "Chỉ có một phần bản sao: ít nhất một bản sao không được lấy mẫu trong khoảng này. Biểu đồ không lấp chỗ trống.",
    seriesCount: "{count} chuỗi",
    unavailableTitle: "Chưa có lịch sử tài nguyên",
    unavailableBody:
      "Agent của nút này chưa gửi mẫu tài nguyên container. Agent cũ báo không có lịch sử thay vì một biểu đồ trống trông khỏe mạnh.",
    emptyRange:
      "Không có mẫu trong khoảng này. Mẫu thiếu là chỗ trống, không phải giá trị 0.",
    memoryLimit: "Giới hạn bộ nhớ: {limit}",
    units:
      "CPU tính theo lõi (1.0 = một lõi đầy đủ). Bộ nhớ là working set: mức dùng trừ bộ nhớ đệm trang có thể thu hồi. Tốc độ tính từ chênh lệch bộ đếm theo thời gian thực tế; mẫu đầu tiên, bộ đếm đặt lại và khoảng thời gian không dương là chỗ trống. Mẫu thô giữ 24 giờ, tổng hợp 1 phút giữ 7 ngày và 1 giờ giữ 30 ngày. Mốc triển khai phân tách các thế hệ container.",
    charts: {
      cpu: "CPU",
      memory: "Bộ nhớ",
      network: "Mạng",
      disk: "I/O đĩa",
    },
  },
};

export default vi;
