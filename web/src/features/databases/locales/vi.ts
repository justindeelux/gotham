import type en from "./en";

/**
 * Databases Vietnamese catalog. Keys mirror `en.ts` exactly; only values
 * are translated. Engine names, versions, DSNs, cron expressions, bucket
 * names, paths, dumps, wire enums and server diagnostics stay untranslated
 * by design (they ride as `{param}` values).
 */
const vi: typeof en = {
  validation: {
    nameRule:
      "Tên phải dài 1-63 ký tự gồm chữ cái, chữ số, ., _ hoặc -.",
    cronRequired: "Biểu thức cron là bắt buộc, ví dụ 0 2 * * *.",
    targetNameRequired: "Tên đích lưu trữ là bắt buộc.",
    s3LocationRequired: "Endpoint và bucket là bắt buộc cho đích S3.",
    s3KeysRequired:
      "Access key và secret key là bắt buộc cho đích S3 mới.",
  },
  status: {
    creating: "Đang tạo",
    running: "Đang chạy",
    stopped: "Đã dừng",
    error: "Lỗi",
    deleting: "Đang xóa",
  },
  errors: {
    withDetail: "{summary} ({detail})",
    databaseNotFound:
      "Không tìm thấy database. Có thể nó đã bị xóa hoặc thuộc tài khoản khác.",
    nameTaken: "Đã tồn tại database trùng tên.",
    databaseAgentUnreachable:
      "Không kết nối được node agent hoặc kiểm tra trạng thái thất bại. Kiểm tra trạng thái node rồi thử lại.",
    featureDisabled:
      "Databases đang tắt trên control plane (FEATURE_DATABASES=false).",
    backupInvalidRequest:
      "Yêu cầu không hợp lệ. Kiểm tra biểu thức cron và các trường đích lưu trữ.",
    sessionExpired: "Phiên đăng nhập đã hết hạn. Vui lòng đăng nhập lại.",
    backupNotFound:
      "Không tìm thấy. Có thể nó đã bị xóa hoặc thuộc tài khoản khác.",
    backupConflict:
      "Đã có một backup hoặc restore đang chạy cho database này.",
    backupAgentUnreachable:
      "Không kết nối được node agent hoặc tác vụ thất bại trên node. Kiểm tra trạng thái node rồi thử lại.",
  },
  detail: {
    tabs: {
      overview: "Tổng quan",
      backups: "Sao lưu",
      settings: "Cài đặt",
    },
    noSelection: "Chưa chọn database.",
    serverPinnedReason:
      "Database không thể đổi server sau khi tạo; chỉ có thể chuyển sang environment khác.",
    actions: {
      start: "Khởi động",
      stop: "Dừng",
      restart: "Khởi động lại",
      rename: "Đổi tên",
    },
    deleteConfirm:
      "Xóa database này? Container sẽ bị gỡ khỏi node, volume {volume} được giữ 7 ngày trước khi xóa vĩnh viễn.",
    overview: {
      details: "Chi tiết",
      engine: "Engine",
      node: "Node",
      publicPort: "Cổng public",
      publicPortOff: "tắt · chỉ dùng mạng nội bộ",
      volume: "Volume",
      container: "Container",
      created: "Ngày tạo",
    },
    credentials: {
      title: "Thông tin đăng nhập",
      storedNote: "Lưu mã hóa · chỉ chủ sở hữu",
      username: "Tên người dùng",
      password: "Mật khẩu",
      database: "Database",
      rootPassword: "Mật khẩu root",
      copy: "Sao chép",
      reveal: "Hiện",
      hide: "Ẩn",
      connectionString: "Chuỗi kết nối",
      copyConnection: "Sao chép chuỗi kết nối",
      nodeUnknown: "Chưa có endpoint public · chưa rõ địa chỉ node.",
      empty:
        "Chưa có thông tin đăng nhập trong bộ nhớ — chúng sẽ tự tải cùng trang.",
      noCached: "Chưa có {label} trong bộ nhớ",
      nodeDsnError: "Chưa rõ địa chỉ node — chưa dựng được DSN public",
      notLoaded: "Thông tin đăng nhập chưa được tải",
    },
    rename: {
      title: "Đổi tên database",
      hint: "Chỉ đổi tên hiển thị — container, volume và thông tin đăng nhập giữ nguyên.",
      placeholder: "Tên database mới",
    },
    lifecycle: {
      started: "Database đã khởi động",
      stopped: "Database đã dừng",
      restarted: "Database đã khởi động lại",
      renamed: "Database đã đổi tên thành “{name}”",
      deleted: "Database “{name}” đã xóa · volume được giữ 7 ngày",
      locationSaved: "Đã lưu vị trí",
    },
  },
  backups: {
    runs: {
      title: "Các bản sao lưu",
      destination: "Đích lưu trữ",
      destinationAria: "Đích sao lưu",
      refresh: "Tải lại",
      backupNow: "Sao lưu ngay",
      backupConfirm:
        "Sao lưu sẽ dừng database này trong lúc dump chạy nên dịch vụ tạm gián đoạn. Tiếp tục?",
      noTarget:
        "Chưa cấu hình đích S3 — các bản sao lưu nằm trên đĩa control plane. Thêm đích tương thích S3 bên dưới để lưu ngoài node.",
      sizePending: "chưa có dung lượng",
      dumpInProgress: "Đang dump…",
      scheduled: "theo lịch",
      restore: "Khôi phục",
      restoreAria: "Khôi phục bản sao lưu lúc {when}",
      deleteAria: "Xóa bản sao lưu lúc {when}",
      deleteConfirm:
        "Xóa bản sao lưu này? Artifact lưu trữ bị xóa trước, rồi đến dòng dữ liệu. Không thể hoàn tác.",
      empty: "Chưa có bản sao lưu",
      emptyHint:
        "Tạo sao lưu thủ công ở trên, hoặc thêm lịch để control plane tự dump database này.",
      queued: "Đã xếp hàng sao lưu · dump chạy trong container tạm",
      deleted: "Đã xóa bản sao lưu",
    },
    runStatus: {
      running: "Đang chạy",
      completed: "Hoàn tất",
      failed: "Thất bại",
    },
    runType: {
      manual: "Thủ công",
      scheduled: "Theo lịch",
    },
    restores: {
      title: "Lịch sử khôi phục",
      subtitle: "Kết quả bền vững của mỗi lần khôi phục đã xếp hàng",
      backupRef: "bản sao lưu {id}",
      finished: "xong lúc",
      completed: "Khôi phục hoàn tất",
      failed: "Khôi phục thất bại",
      failedWithDetail: "Khôi phục thất bại: {detail}",
      queued: "Đã xếp hàng khôi phục · database dừng trong lúc chạy",
    },
    dialog: {
      title: "Khôi phục database",
      prompt:
        "Khôi phục {name} từ bản sao lưu {backup} ({when}, {size})?",
      warning:
        "Khôi phục chạy trong container tạm và ghi đè dữ liệu hiện tại của database này. Không thể hoàn tác — hãy sao lưu trước nếu dữ liệu đang chạy còn quan trọng.",
      confirm: "Khôi phục · ghi đè dữ liệu",
    },
    schedules: {
      title: "Lịch tự động",
      subtitle: "Cron trên control plane",
      nextRun: "Lần chạy tiếp",
      lastRun: "lần trước",
      on: "Bật",
      off: "Tắt",
      enableAria: "Bật lịch {cron}",
      editAria: "Sửa lịch {cron}",
      deleteAria: "Xóa lịch {cron}",
      deleteConfirm: "Xóa lịch này? Các bản sao lưu cũ giữ nguyên.",
      empty: "Chưa có lịch — sao lưu tự động đang tắt",
      newTitle: "Lịch mới",
      editTitle: "Sửa lịch",
      cronAria: "Biểu thức cron",
      destinationAria: "Đích lưu trữ của lịch",
      enableNewAria: "Bật lịch mới",
      add: "Thêm lịch",
      save: "Lưu lịch",
      saved: "Đã lưu lịch · lần chạy tiếp tính từ {cron}",
      updated: "Đã cập nhật lịch · lần chạy tiếp tính từ {cron}",
      enabled: "Đã bật lịch",
      paused: "Đã tạm dừng lịch",
      deleted: "Đã xóa lịch",
    },
    targets: {
      title: "Đích sao lưu",
      subtitle: "Lưu trữ tương thích S3",
      kindS3: "Tương thích S3",
      kindLocal: "Đĩa cục bộ",
      localDefault: "Đĩa cục bộ (mặc định)",
      localDisk: "đĩa cục bộ",
      deletedTarget: "đích đã xóa",
      controlPlaneDisk: "đĩa control plane",
      credsOk: "Đã cấu hình credentials",
      credsMissing: "Chưa có credentials",
      test: "Kiểm tra",
      testAria: "Kiểm tra đích {name}",
      editAria: "Sửa đích {name}",
      deleteAria: "Xóa đích {name}",
      deleteConfirm:
        "Xóa đích “{name}”? Các bản sao lưu cũ giữ nguyên vị trí nhưng không còn đọc lại được từ đích này.",
      empty: "Chưa có đích sao lưu",
      emptyHint:
        "Sao lưu sẽ dùng đĩa control plane cho đến khi cấu hình đích tương thích S3.",
      newTitle: "Đích mới",
      editTitle: "Sửa đích",
      namePlaceholder: "Tên đích",
      nameAria: "Tên đích",
      kindAria: "Loại đích",
      endpointPlaceholder: "https://…endpoint",
      endpointAria: "Endpoint",
      regionPlaceholder: "Region (ví dụ auto)",
      regionAria: "Region",
      bucketPlaceholder: "Bucket",
      bucketAria: "Bucket",
      prefixPlaceholder: "Tiền tố key (tùy chọn)",
      prefixAria: "Tiền tố key",
      accessKeyPlaceholder: "Access key",
      accessKeyEditPlaceholder: "Access key · để trống giữ key đã lưu",
      accessKeyAria: "Access key",
      secretKeyPlaceholder: "Secret key",
      secretKeyEditPlaceholder: "Secret key · để trống giữ key đã lưu",
      secretKeyAria: "Secret key",
      secretsNote:
        "Secret được niêm phong trên server và không bao giờ hiện lại — để trống các ô key nhằm giữ key đã lưu.",
      add: "Thêm đích",
      save: "Lưu đích",
      saved: "Đã lưu đích “{name}” · credentials đã niêm phong",
      updated: "Đã cập nhật đích “{name}”",
      deleted: "Đã xóa đích “{name}”",
      testFailed: "Kiểm tra thất bại",
    },
  },
  wizard: {
    title: "Tạo database",
    stepOf: "Bước {current} trên {total} · {step}.",
    intro:
      "Mỗi database là một container với volume riêng trên một node; thông tin đăng nhập do server sinh và lưu mã hóa.",
    steps: {
      engine: "Engine",
      configure: "Cấu hình",
      review: "Xem lại",
    },
    engineStep: {
      engine: "Engine",
      version: "Phiên bản",
      defaultVersion: "Mặc định: {version}",
      node: "Node",
      engineMeta: "· {repo}:{version} · cổng {port}",
    },
    configure: {
      name: "Tên",
      nameFeedback:
        "Dùng cho container và thông tin đăng nhập; 1-63 ký tự: chữ cái, chữ số, ., _ hoặc -.",
      namePlaceholder: "pg-orders",
      expose: "Mở cổng public",
      exposeWarning:
        "Cổng public là bề mặt tấn công và không đổi được sau này — binding cổng Docker cố định lúc tạo. Hãy tắt trừ khi client ngoài bắt buộc.",
      publicPort: "Cổng public",
      publicPortFeedback: "Cổng host chuyển tiếp tới cổng engine.",
      publicPortPlaceholder: "ví dụ 15432",
    },
    review: {
      intro: "Xem lại database trước khi tạo:",
      engineImage: "Engine / image · {image}",
      node: "Node · {server}",
      name: "Tên · {name}",
      publicPort: "Cổng public · {mapping}",
      publicPortOff: "tắt · chỉ dùng mạng nội bộ",
      volume: "Volume · giữ 7 ngày sau khi xóa",
      successTitle: "Đã tạo database",
      successBody:
        "Đã bắt đầu cấp phát container trên node. Hãy lưu thông tin đăng nhập này — chúng vẫn có trên trang chi tiết database.",
    },
    back: "Quay lại",
    continue: "Tiếp tục",
    create: "Tạo database",
    done: "Xong",
    scopeError: "Chọn project và environment trước.",
    created: "Database “{name}” đã được tạo",
  },
};

export default vi;
