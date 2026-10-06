/**
 * Vietnamese catalog for the templates surface (I18N-7). Same shape as the
 * English catalog; slugs, field keys, rendered compose and secret values
 * stay raw parameters, and unknown template metadata falls back to the
 * provider copy.
 */
import type en from "./en";

const vi: typeof en = {
  validation: {
    required: "Trường này là bắt buộc.",
    pattern: "Không đúng định dạng yêu cầu.",
    wholeNumber: "Phải là số nguyên.",
    bool: "Phải là true hoặc false.",
    maxLength: "Tối đa {max} ký tự.",
    minNumber: "Tối thiểu {min}.",
    maxNumber: "Tối đa {max}.",
    selectOptions: "Phải là một trong: {options}.",
  },
  errors: {
    sessionExpired: "Phiên đăng nhập đã hết hạn. Vui lòng đăng nhập lại.",
    invalidValues: "Giá trị mẫu không hợp lệ. Kiểm tra các trường được đánh dấu.",
    notFound: "Không tìm thấy mẫu. Danh mục có thể đã đổi — tải lại trang.",
    disabled: "Dịch vụ bị tắt trên control plane (FEATURE_SERVICES=false).",
  },
  gallery: {
    empty: "Danh mục chưa có mẫu nào.",
  },
  page: {
    eyebrow: "Vận hành · mẫu một chạm",
    title: "Thư viện mẫu",
    description:
      "Chọn một mẫu, điền biểu mẫu, control plane sẽ dựng tài liệu compose rồi triển khai qua node agent. Biểu mẫu sinh từ schema của mẫu nên thêm mẫu mới không cần đổi giao diện.",
    calloutTitle: "Cấu trúc của một mẫu",
    calloutYaml:
      "{file} khai báo tên, biểu tượng, mô tả và các trường biểu mẫu (kiểu, mặc định, bắt buộc, kiểm tra).",
    calloutComposeUses: "{file} dùng chỗ giữ {placeholder}.",
    calloutComposeNote:
      "Engine chỉ thay thế chuỗi, không bao giờ chạy mã, nên mẫu không thể chạy lệnh trên node. Sửa mẫu không ảnh hưởng dịch vụ đã triển khai.",
  },
  wizard: {
    title: "Triển khai mẫu {name}",
    steps: {
      configure: "Cấu hình",
      configureDesc: "Điền các trường của mẫu",
      preview: "Xem trước compose",
      previewDesc: "Được dựng từ schema",
      create: "Tạo",
      createDesc: "Đặt tên dịch vụ và chọn node",
    },
    counter: "Bước {step} / 3",
    back: "Quay lại",
    next: "Tiếp tục",
    close: "Đóng",
    create: "Tạo dịch vụ",
    deploy: "Triển khai ngay",
  },
  configure: {
    description:
      "Các trường lấy từ schema của mẫu ({endpoint}); thêm mẫu không cần đổi giao diện. Giá trị bí mật giữ trong biểu mẫu này cho tới khi gửi dưới dạng môi trường dịch vụ.",
  },
  preview: {
    servicesOne: "{count} dịch vụ",
    servicesOther: "{count} dịch vụ",
    volumesOne: "{count} volume có tên",
    volumesOther: "{count} volume có tên",
    domainsOne: "{count} tuyến tên miền",
    domainsOther: "{count} tuyến tên miền",
    secretNote:
      "Engine chỉ thay thế chuỗi. Trường bí mật giữ nguyên tham chiếu {ref} trong tài liệu này; giá trị đi riêng trong môi trường dịch vụ và bị biên tập khỏi lỗi và lịch sử triển khai.",
    heldSeparately: "Giữ riêng: {keys}.",
  },
  target: {
    nameLabel: "Tên dịch vụ",
    scopeError: "Chọn dự án và môi trường.",
    description:
      "Tạo mới sẽ lưu tài liệu đã dựng và môi trường của nó ({endpoint}); một dịch vụ chỉ chạy trên đúng một node. Triển khai gửi dự án tới agent của node đó và hiển thị kết quả agent báo về.",
  },
  created: {
    title: "Đã tạo {name}",
    description:
      "Dòng dịch vụ đã tồn tại; chưa có gì chạy. Triển khai sẽ dựng lại tài liệu trên node và ghi một dòng triển khai.",
    open: "Mở chi tiết dịch vụ",
    deployed: "đã triển khai",
    stubTitle: "Dòng thời gian bước triển khai — chờ backend",
    stubBody:
      "API ghi một dòng cho mỗi lần triển khai (trạng thái, lỗi, thời gian) và không có tiến trình từng bước nên không hiển thị dòng thời gian tại đây. Trang chi tiết liệt kê lịch sử.",
  },
  toast: {
    created: "Đã tạo dịch vụ {name}.",
    deployed: "Triển khai xong.",
  },
  overlay: {
    n8n: {
      description:
        "Tự động hóa quy trình n8n với volume có tên bền vững và tên miền được định tuyến.",
      fields: {
        domain: {
          help: "Host công khai do Traefik proxy của node phục vụ.",
        },
        encryption_key: {
          help: "Mã hóa thông tin đăng nhập đã lưu; giữ cẩn thận, mất khóa sẽ khóa luôn thông tin.",
        },
        timezone: {
          help: "Tên múi giờ IANA dùng cho lịch và đồng hồ container.",
        },
        diagnostics: {
          help: "Cho phép n8n gửi chẩn đoán sử dụng ẩn danh.",
        },
        executions_retention_hours: {
          help: "Bản ghi thực thi cũ hơn mức này sẽ bị dọn; 336 giờ là 14 ngày.",
        },
      },
    },
    nextcloud: {
      description:
        "Nextcloud với PostgreSQL và Redis, các volume có tên bền vững và tên miền được định tuyến.",
      fields: {
        domain: {
          help: "Host công khai do Traefik proxy của node phục vụ.",
        },
        admin_password: {
          help: "Mật khẩu quản trị Nextcloud ban đầu.",
        },
        redis_password: {
          help: "Nextcloud luôn kèm Redis; mật khẩu này bảo vệ instance bộ nhớ đệm.",
        },
      },
    },
    "uptime-kuma": {
      description:
        "Giám sát Uptime Kuma với volume có tên bền vững và tên miền được định tuyến.",
      fields: {
        domain: {
          help: "Host công khai do Traefik proxy của node phục vụ.",
        },
        tz: {
          help: "Tên múi giờ IANA cho đồng hồ container và thông báo.",
        },
      },
    },
    wordpress: {
      description:
        "WordPress với cơ sở dữ liệu MySQL, các volume có tên bền vững và tên miền được định tuyến.",
      fields: {
        domain: {
          help: "Host công khai do Traefik proxy của node phục vụ.",
        },
        db_password: {
          help: "WordPress và MySQL cùng dùng; hãy chọn giá trị mạnh.",
        },
      },
    },
  },
};

export default vi;
