import type { FirewallMessages } from "./en";

const vi: FirewallMessages = {
  tab: "Tường lửa",
  title: "Tường lửa",
  description:
    "Mở hoặc đóng cổng trên nút này. Quy tắc được áp dụng ngay trên máy chủ; mỗi nút khác giữ tường lửa riêng.",
  status: {
    on: "Bật",
    off: "Tắt",
    backend: "Backend",
    enabled: "Tường lửa đang bật",
    disabled: "Tường lửa đang tắt",
    accessHint:
      "SSH ({ssh}), agent của nút ({agent}) và control plane ({controlPlane}) luôn được phép.",
    accessHintRemote:
      "SSH ({ssh}) và agent của nút ({agent}) luôn được phép. Control plane chạy trên nút khác.",
    turnOnHint:
      "Bật tường lửa vẫn giữ các quy tắc truy cập Gotham cần, nên bạn không thể tự khóa mình ra ngoài.",
    unsupportedTitle: "Không thể quản lý tường lửa của nút này tại đây",
    unsupportedBody:
      "Gotham quản lý ufw và firewalld. Nút không có cả hai sẽ hiện trạng thái bên dưới.",
    reload: "Tải lại",
  },
  rules: {
    title: "Quy tắc",
    add: "Thêm quy tắc",
    empty: "Chưa có quy tắc tường lửa nào",
    emptyHint: "Thêm quy tắc để mở hoặc đóng một cổng.",
    any: "Mọi",
    enabled: "Đang bật",
    disabled: "Đã tắt",
    required: "Bắt buộc",
    protectedHint:
      "Quy tắc này giữ quyền truy cập SSH, agent của nút hoặc control plane.",
    readOnly: "Quản lý trên nút",
    readOnlyHint:
      "Quy tắc này được quản lý trên nút và không thể sửa từ Gotham.",
    edit: "Sửa",
    delete: "Xóa",
    deleteConfirm: "Xóa quy tắc cho {target}?",
    targetPort: "cổng {port}",
    targetAll: "tất cả cổng",
    columns: {
      action: "Hành động",
      direction: "Chiều",
      protocol: "Giao thức",
      port: "Cổng",
      source: "Địa chỉ",
      comment: "Ghi chú",
      status: "Trạng thái",
      actions: "Thao tác",
    },
    action: {
      allow: "Cho phép",
      deny: "Chặn",
    },
    direction: {
      in: "Vào",
      out: "Ra",
    },
    protocol: {
      tcp: "TCP",
      udp: "UDP",
      any: "Mọi",
    },
  },
  form: {
    addTitle: "Thêm quy tắc tường lửa",
    editTitle: "Sửa quy tắc tường lửa",
    action: "Hành động",
    direction: "Chiều",
    protocol: "Giao thức",
    port: "Cổng hoặc dải cổng",
    portPlaceholder: "22 hoặc 8000-8100",
    portHint: "Để trống cho mọi cổng.",
    source: "Địa chỉ",
    sourcePlaceholder: "10.0.0.0/8",
    sourceHint:
      "Nguồn cho quy tắc vào, đích cho quy tắc ra. Để trống cho mọi địa chỉ.",
    comment: "Ghi chú",
    commentPlaceholder: "Ghi chú tùy chọn",
    enabled: "Áp dụng quy tắc này",
    enabledHint: "Quy tắc bị tắt vẫn được lưu trên nút nhưng không được áp dụng.",
    save: "Lưu quy tắc",
    cancel: "Hủy",
  },
  validation: {
    port: "Dùng cổng từ 1 đến 65535, hoặc dải như 8000-8100.",
    source: "Dùng địa chỉ IP hoặc dải CIDR.",
    comment:
      "Chỉ dùng chữ cái, chữ số và . _ ( ) , : / + {'@'} - , tối đa 200 ký tự.",
    commentDash: "Ghi chú không được bắt đầu bằng dấu gạch ngang.",
    outboundSource: "Quy tắc ra không được có địa chỉ nguồn.",
  },
  toasts: {
    enabled: "Đã bật tường lửa",
    disabled: "Đã tắt tường lửa",
    saved: "Đã lưu quy tắc",
    deleted: "Đã xóa quy tắc",
  },
  errors: {
    load: "Không tải được tường lửa.",
    toggle: "Không thể đổi trạng thái tường lửa.",
    save: "Không thể lưu quy tắc.",
    delete: "Không thể xóa quy tắc.",
  },
};

export default vi;
