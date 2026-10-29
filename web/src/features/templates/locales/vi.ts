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
    invalidValuesWithDetail: "Giá trị không hợp lệ: {detail}",
    notFound: "Không tìm thấy mẫu. Danh mục có thể đã đổi — tải lại trang.",
    notFoundWithDetail: "Không tìm thấy: {detail}",
    disabled: "Dịch vụ đang tắt trên control plane này. Hãy đề nghị chủ sở hữu bật.",
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
  secret: {
    generate: "Tạo ngẫu nhiên",
    generateLabel: "Tạo {label}",
    copy: "Sao chép",
    copyLabel: "Sao chép {label}",
    copied: "Đã sao chép {label} vào bộ nhớ tạm.",
    copyFailed: "Không thể sao chép {label}; hãy chọn giá trị và sao chép thủ công.",
    generated: "Đã tạo {label}.",
    copiedAnnouncement: "Đã sao chép.",
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
  },
  created: {
    title: "Đã tạo {name}",
    open: "Mở chi tiết dịch vụ",
    deployed: "đã triển khai",
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
          help: "Mã hóa thông tin đăng nhập đã lưu; dùng Tạo ngẫu nhiên để có giá trị mạnh. Mất khóa sẽ khóa luôn thông tin.",
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
          help: "Mật khẩu quản trị Nextcloud ban đầu; dùng Tạo ngẫu nhiên để có giá trị mạnh.",
        },
        redis_password: {
          help: "Nextcloud luôn kèm Redis; dùng Tạo ngẫu nhiên để bảo vệ instance bộ nhớ đệm.",
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
          help: "WordPress và MySQL cùng dùng; dùng Tạo ngẫu nhiên để có giá trị mạnh.",
        },
      },
    },
  },
};

export default vi;
