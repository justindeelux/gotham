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
    any: "Tất cả",
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
      any: "Tất cả",
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
  access: {
    ssh: "SSH",
    agent: "agent của nút",
    control_plane: "control plane",
  },
  errors: {
    details: "Chi tiết: {message}",
    load: "Không tải được tường lửa.",
    toggle: "Không thể đổi trạng thái tường lửa.",
    save: "Không thể lưu quy tắc.",
    delete: "Không thể xóa quy tắc.",
    no_firewall: "Nút này không cài tường lửa nào được hỗ trợ.",
    unsupported_backend:
      "Nút này dùng {backend}. Gotham nhận biết được nhưng chưa sửa được quy tắc; hãy quản lý chúng trên nút.",
    helper_missing:
      "Trợ lý tường lửa trên nút không phản hồi. Hãy cập nhật nút này (chạy cập nhật Gotham) rồi thử lại.",
    helper_failed:
      "Trợ lý tường lửa trên nút bị lỗi; thay đổi chưa được áp dụng.",
    backend_changed: "Backend tường lửa trên nút đã thay đổi. Hãy tải lại trang.",
    state_read_failed:
      "Nút không đọc được các quy tắc tường lửa đã lưu.",
    state_write_failed:
      "Thay đổi đã được áp dụng nhưng nút không lưu được trạng thái quy tắc.",
    rule_not_found: "Quy tắc này không còn trên nút. Hãy tải lại trang.",
    server_not_found: "Không tìm thấy máy chủ này. Có thể nó đã bị xóa.",
    read_only_rule:
      "Quy tắc này được quản lý trên nút và không thể sửa từ Gotham.",
    would_lock_out:
      "Thay đổi này sẽ chặn cổng {port}, cổng cần cho truy cập {access}; hãy giữ một quy tắc cho phép cho cổng đó.",
    invalid_action: "Hành động phải là cho phép hoặc chặn.",
    invalid_direction: "Chiều phải là vào hoặc ra.",
    invalid_protocol: "Giao thức phải là TCP, UDP hoặc tất cả.",
    invalid_port:
      "Cổng phải từ 1 đến 65535, hoặc dải như 8000-8100.",
    invalid_source: "Địa chỉ phải là địa chỉ IP hoặc dải CIDR.",
    invalid_outbound_source: "Quy tắc ra không được có địa chỉ nguồn.",
    invalid_comment:
      "Ghi chú chỉ dùng chữ cái, chữ số và . _ ( ) , : / + {'@'} - , tối đa 200 ký tự.",
    invalid_rule: "Không đọc được quy tắc. Hãy tải lại trang và thử lại.",
    invalid_request: "Yêu cầu không hợp lệ.",
    agent_unavailable:
      "Agent của nút không phản hồi. Hãy kiểm tra nút đang online rồi thử lại.",
    agent_outdated:
      "Agent của nút này quá cũ để quản lý tường lửa. Hãy cập nhật agent rồi thử lại.",
  },
};

export default vi;
