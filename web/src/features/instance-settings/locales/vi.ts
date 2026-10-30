import type en from "./en";

const vi: typeof en = {
  page: {
    eyebrow: "Hệ thống · Chủ sở hữu",
    title: "Cài đặt",
    description:
      "Cấu hình chính instance Gotham: địa chỉ control plane công khai và tên của nó. Chỉ dành cho chủ sở hữu.",
  },
  forbidden: "Chỉ chủ sở hữu mới xem hoặc thay đổi được các cài đặt này.",
  locked: "Bị khóa bởi biến môi trường {name}.",
  general: {
    title: "Instance",
    url: "URL control plane",
    urlHint: "URL công khai dùng cho đăng ký agent, OAuth redirect và webhook.",
    name: "Tên instance",
    submit: "Lưu cài đặt",
    saved: "Đã lưu cài đặt.",
  },
  domainStatus: {
    title: "Trạng thái tên miền control plane",
    empty: "Đặt URL control plane để xem trạng thái router.",
    invalid: "Nhập URL control plane hợp lệ để kiểm tra trạng thái.",
    dnsUnknown: "DNS: chưa báo cáo",
    certUnknown: "Chứng chỉ: chưa báo cáo",
    certState: {
      issued: "Chứng chỉ: đã cấp",
      pending: "Chứng chỉ: đang chờ cấp",
      failed: "Chứng chỉ: cấp thất bại",
    },
    certExpiry: "Hết hạn {date}",
    certNotApplicable: "Chứng chỉ: không áp dụng (HTTP)",
    observationHint:
      "API router không báo cáo kết quả phân giải DNS.",
    loading: "Router: đang kiểm tra…",
    synced: "Router: đã đồng bộ",
    pending: "Router: chờ đồng bộ",
    unknown: "Router: không rõ",
    missingLocalhost:
      "Node localhost chưa được đăng ký. " +
      "Hãy đăng ký node trước khi đồng bộ router control plane.",
    readFailed: "Không đọc được trạng thái router localhost. Hãy kiểm tra node và thử lại.",
    unsaved: "Lưu URL trước khi tạo và đồng bộ router tương ứng.",
    refresh: "Làm mới trạng thái",
  },
  pendingConfirm: {
    title: "Thay đổi URL control plane đang chờ xác nhận",
    body: "URL control plane giờ là {url}. Hãy mở Gotham tại địa chỉ đó để giữ thay đổi.",
    hint:
      "Chỉ có thể giữ thay đổi từ địa chỉ mới. Nếu không truy cập được Gotham tại đó, hãy để nguyên: địa chỉ trước được khôi phục khi hết thời gian đếm ngược.",
    countdownStart: "Thay đổi URL control plane sẽ tự hoàn tác sau {time}.",
    keep: "Giữ thay đổi",
    revert: "Hoàn tác ngay",
  },
  validation: {
    url: "Nhập URL http(s) có tên máy chủ, không kèm thông tin đăng nhập, query hay fragment.",
    name: "Tên phải dài 1-64 ký tự.",
  },
};

export default vi;
