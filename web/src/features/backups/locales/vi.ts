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
    cleanupPending: "Đang chờ dọn dẹp",
    retryCleanup: "Thử dọn lại",
    discardCleanup: "Bỏ qua",
    discardCleanupConfirm:
      "Bỏ dòng này mà không xóa tệp sao lưu? Tệp vẫn nằm ở nơi lưu trữ và không thể quản lý từ đây.",
    cleanupDone: "Đã dọn xong bản sao lưu.",
    cleanupDiscarded: "Đã bỏ dòng chờ dọn dẹp.",
    inProgress: "đang chạy",
    verified: "Đã xác thực",
    notVerified: "Chưa xác thực",
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
    keepLastLabel: "Giữ số bản sao lưu theo lịch gần nhất",
    keepLastHint:
      "Các bản sao lưu theo lịch cũ hơn sẽ tự động bị xóa. Bản sao lưu thủ công không bao giờ bị xóa tự động.",
    keepAll:
      "Giữ mọi bản sao lưu theo lịch — sửa lịch để đặt giới hạn",
    keepLast: "Giữ {count} bản sao lưu theo lịch gần nhất",
    keepLastInvalid: "Số bản giữ lại phải từ 1 đến 365.",
    pauseLabel: "Dừng tài nguyên khi sao lưu",
  },
  errors: {
    noSpace: "Không đủ dung lượng trống trên đĩa sao lưu",
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
    providerRemoved: "Nơi lưu trữ đã bị xóa",
    unknownProvider: "Lưu trữ",
    usage: "{count} bản sao lưu · {size}",
    freeSpace: "Còn {free} trống trên {total}",
    deleteConfirm:
      "Xóa {name}? Các bản sao lưu ở đó sẽ không đọc được và không thể khôi phục. Không thể xóa nơi lưu trữ đang được một lịch sử dụng.",
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
    everyMinute: "Mỗi phút",
    everyMinutes: "Mỗi {count} phút",
    hourlyAt: "Mỗi giờ lúc :{minute}",
    everyNHours: "Mỗi {count} giờ lúc :{minute}",
    dailyAt: "Mỗi ngày lúc {time}",
    weekdaysAt: "Mỗi ngày trong tuần lúc {time}",
    weeklyAt: "Mỗi {day} lúc {time}",
    monthlyAt: "Ngày {day} mỗi tháng lúc {time}",
    custom: "Lịch tùy chỉnh",
    serverTime: "Giờ máy chủ",
    invalid: "Nhập biểu thức cron gồm 5 trường: phút giờ ngày tháng thứ.",
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
    verifiedCheck:
      "Mã kiểm tra của bản sao lưu được xác thực trước khi chạm vào tài nguyên. Nếu bản sao lưu bị hỏng, quá trình khôi phục dừng lại và tài nguyên tiếp tục chạy.",
    noChecksum:
      "Bản sao lưu này được tạo trước khi có mã kiểm tra. Không thể xác thực và sẽ được khôi phục như trước.",
  },
  resourceTab: {
    storageHint: "Nơi lưu trữ được quản lý tại một chỗ.",
    openStorage: "Mở Bản sao lưu → Lưu trữ",
    contentsTitle: "Nội dung được sao lưu",
    contentsEmptyApplication:
      "Ứng dụng này không có vùng lưu trữ lâu dài để sao lưu. Hãy thêm ánh xạ lưu trữ trước.",
    contentsEmptyService:
      "Dịch vụ này không có volume có tên hoặc ổ gắn kết để sao lưu.",
    contentsKind: {
      volume: "Volume",
      bind: "Ổ gắn kết",
      dump: "Bản kết xuất cơ sở dữ liệu",
    },
    pauseDuringBackup: "Dừng tài nguyên trong khi tạo bản lưu",
    pauseHelper:
      "Bản lưu nhất quán, nhưng tài nguyên không khả dụng cho đến khi hoàn tất.",
    noPause: "Không dừng tài nguyên",
    noPauseHelper:
      "Không gián đoạn, nhưng tệp được ghi trong lúc sao lưu có thể không nhất quán.",
    noPauseTag: "Không dừng",
  },
};

export default vi;
