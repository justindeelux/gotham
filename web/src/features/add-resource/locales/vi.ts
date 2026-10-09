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
    service: "Service",
    database: "Cơ sở dữ liệu",
  },
  sources: {
    git_public: "Triển khai kho công khai bằng URL, không cần kết nối.",
    git_private: "Triển khai kho riêng bằng deploy key hoặc token.",
    github_app: "Triển khai từ tài khoản GitHub đã kết nối.",
    gitlab_app: "Triển khai từ tài khoản GitLab đã kết nối.",
    dockerfile: "Build từ nội dung Dockerfile dán sẵn, không cần kho.",
    image: "Chạy container image dựng sẵn từ registry bất kỳ.",
    compose: "Chạy service từ file Docker Compose.",
  },
  engines: {
    postgres: "Cơ sở dữ liệu quan hệ tin cậy cho dữ liệu ứng dụng.",
    mysql: "Cơ sở dữ liệu quan hệ phổ biến cho tải web.",
    mariadb: "Bản fork cộng đồng của MySQL, tương thích giao thức.",
    mongodb: "Cơ sở dữ liệu tài liệu cho dữ liệu linh hoạt kiểu JSON.",
    redis: "Cache và hàng đợi trong bộ nhớ, có thể lưu bền vững.",
  },
};

export default vi;
