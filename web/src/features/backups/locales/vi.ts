const vi = {
  page: {
    eyebrow: "Vận hành · Bảo vệ dữ liệu",
    title: "Bản sao lưu",
    description:
      "Mọi lần sao lưu và lịch chạy trên các dự án của bạn, cùng các nơi " +
      "lưu trữ mà bảng điều khiển ghi vào.",
  },
  tabs: {
    backups: "Bản sao lưu",
    storage: "Lưu trữ",
  },
  filters: {
    allTypes: "Mọi loại tài nguyên",
    allStatuses: "Mọi trạng thái",
    typeAria: "Lọc theo loại tài nguyên",
    statusAria: "Lọc theo trạng thái sao lưu",
    refresh: "Làm mới",
  },
  runs: {
    title: "Bản sao lưu gần đây",
    empty: "Chưa có bản sao lưu nào",
    emptyFiltered: "Không có bản sao lưu nào khớp bộ lọc",
    emptyHint:
      "Hãy sao lưu ngay, hoặc mở một tài nguyên và thêm lịch để dữ liệu " +
      "được sao lưu tự động.",
    restore: "Khôi phục",
    inProgress: "đang chạy",
    seconds: "{count} giây",
    minutes: "{count} phút",
    minutesSeconds: "{minutes} phút {seconds} giây",
    columns: {
      resource: "Tài nguyên",
      status: "Trạng thái",
      size: "Dung lượng",
      provider: "Lưu trữ",
      started: "Bắt đầu",
      duration: "Thời lượng",
      actions: "Thao tác",
    },
  },
  schedules: {
    title: "Lịch chạy",
    add: "Lịch mới",
    empty: "Chưa có lịch nào — sao lưu tự động đang tắt",
  },
  backupNow: {
    action: "Sao lưu ngay",
    title: "Sao lưu một tài nguyên",
    resource: "Tài nguyên",
    provider: "Lưu trữ",
    note: "Tài nguyên tạm thời không khả dụng trong khi sao lưu chạy.",
    submit: "Bắt đầu sao lưu",
    chooseResource: "Chọn tài nguyên cần sao lưu.",
  },
  schedule: {
    chooseResource: "Chọn tài nguyên",
    preset: "Tần suất",
  },
  storage: {
    title: "Nơi lưu trữ",
    add: "Thêm nơi lưu trữ",
    addTitle: "Thêm nơi lưu trữ",
    editTitle: "Sửa nơi lưu trữ",
    name: "Tên",
    kind: "Loại",
    endpoint: "Endpoint",
    region: "Vùng",
    bucket: "Bucket",
    prefix: "Tiền tố",
    accessKey: "Access key",
    secretKey: "Secret key",
    localNote:
      "Đĩa cục bộ giữ bản sao trên máy điều khiển. Không cần cấu hình và không thể xóa.",
    builtin: "Có sẵn",
    empty: "Chưa có nơi lưu trữ nào",
    onlyBuiltin:
      "Bản sao lưu đang nằm trên đĩa của máy điều khiển. Thêm một nơi lưu trữ " +
      "tương thích S3 để giữ bản sao ở nơi khác.",
    localDisk: "Đĩa cục bộ",
    deletedProvider: "Nơi lưu trữ đã xóa",
    unknownProvider: "Lưu trữ",
  },
  resourceTypes: {
    application: "Ứng dụng",
    database: "Cơ sở dữ liệu",
    service: "Dịch vụ",
  },
  resources: {
    unknown: "Tài nguyên không rõ ({id})",
    shortId: "Tài nguyên {id}",
  },
  cron: {
    dailyAtTwo: "Mỗi ngày lúc 02:00",
    everySixHours: "Mỗi 6 giờ",
    weeklySunday: "Mỗi Chủ nhật lúc 03:00",
    custom: "Lịch tùy chỉnh ({cron})",
  },
  restore: {
    title: "Khôi phục bản sao lưu",
    prompt: "Khôi phục {name} từ bản sao lưu {backup} ({when}, {size})?",
    confirm: "Khôi phục · ghi đè dữ liệu",
    warningDatabase:
      "Khôi phục sẽ ghi đè dữ liệu hiện tại của cơ sở dữ liệu này. Không thể hoàn tác — hãy sao lưu trước nếu dữ liệu đang chạy vẫn còn quan trọng.",
    warningApplication:
      "Khôi phục sẽ ghi đè dữ liệu ứng dụng đã lưu. Không thể hoàn tác — hãy sao lưu trước nếu dữ liệu đang chạy vẫn còn quan trọng.",
    warningService:
      "Khôi phục sẽ ghi đè các volume đã lưu của dịch vụ. Không thể hoàn tác — hãy sao lưu trước nếu dữ liệu đang chạy vẫn còn quan trọng.",
  },
  resourceTab: {
    storageHint: "Nơi lưu trữ được quản lý tại một chỗ.",
    openStorage: "Mở Bản sao lưu → Lưu trữ",
  },
};

export default vi;
