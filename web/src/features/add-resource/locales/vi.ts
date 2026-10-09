import type en from "./en";

/**
 * Vietnamese catalog for the Add-resource picker. Keys mirror `en.ts`
 * exactly; only values are translated. Template slugs, engine values,
 * image tags and ports stay untranslated by design.
 */
const vi: typeof en = {
  search: {
    label: "Lọc service",
    placeholder: "Lọc service…",
    empty: "Không có service nào khớp với bộ lọc.",
    emptyCatalog: "Không có template nào trong danh mục.",
  },
  groups: {
    application: "Ứng dụng",
    applicationMeta: "wizard · kho git",
    service: "Service",
    serviceMeta: "một thẻ mỗi template",
    serviceHint:
      "Một thẻ cho mỗi template trong danh mục. Chọn thẻ sẽ mở wizard deploy template với template đã chọn sẵn.",
    database: "Cơ sở dữ liệu",
    databaseMeta: "một thẻ mỗi engine",
    databaseHint:
      "Một thẻ cho mỗi engine được hỗ trợ. Chọn thẻ sẽ mở wizard cơ sở dữ liệu với engine đã chọn sẵn.",
  },
  applicationCard: {
    name: "Ứng dụng",
    description: "Build và deploy từ kho Git.",
    tag: "wizard mã nguồn",
  },
  serviceCard: {
    tag: "template",
  },
  engines: {
    postgres: "Cơ sở dữ liệu quan hệ tin cậy cho dữ liệu ứng dụng.",
    mysql: "Cơ sở dữ liệu quan hệ phổ biến cho tải web.",
    mariadb: "Bản fork cộng đồng của MySQL, tương thích giao thức.",
    mongodb: "Cơ sở dữ liệu tài liệu cho dữ liệu linh hoạt kiểu JSON.",
    redis: "Cache và hàng đợi trong bộ nhớ, có thể lưu bền vững.",
  },
  keyboardHint:
    "Bàn phím: Tab di chuyển giữa các thẻ, phím mũi tên di chuyển trong nhóm, Enter mở luồng tạo.",
};

export default vi;
