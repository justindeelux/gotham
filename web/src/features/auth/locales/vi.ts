import type { AuthMessages } from "./en";

/**
 * Vietnamese auth catalog. Keys mirror `en.ts` exactly; only values are
 * translated. Technical tokens (JWT, Ed25519, gRPC, TLS, protobuf, mono
 * product names, ports) stay untranslated by design.
 */
const vi: AuthMessages = {
  shell: {
    eyebrow: "PaaS tự host",
    headline: "Control plane cho hạ tầng của riêng bạn",
    lede: "Một file nhị phân Go chạy control plane, một agent nhỏ trên mỗi node. Không dịch vụ bên ngoài, không lớp nào mà bạn không đọc được mã nguồn.",
    footnote: "Control plane tự host · hạ tầng của bạn, dữ liệu của bạn.",
    selfHostedTitle: "Tự host trên VPS chỉ với 1 GB RAM",
    selfHostedBody:
      "Control plane là một file nhị phân Go duy nhất cùng PostgreSQL và Redis; 1 vCPU và 1 GB RAM là đủ để bắt đầu.",
    tlsTitle: "Kênh điều khiển qua gRPC TLS xác thực máy chủ",
    tlsBody:
      "Agent điều khiển Docker Engine trên mỗi node; control plane và agent trao đổi protobuf phiên bản qua :9442, với chứng chỉ từ CA nội bộ.",
    updatesTitle: "Cập nhật ký Ed25519 có rollback",
    updatesBody:
      "Control plane và agent tự cập nhật từ GitHub Releases, xác minh chữ ký trước khi thay file nhị phân và giữ bản cũ để rollback.",
    enginesTitle: "4 engine build cùng template một chạm",
    enginesBody:
      "Dockerfile, Railpack, Buildpacks và static; thư viện template dựng WordPress, Nextcloud, n8n hoặc Uptime Kuma chỉ trong một chạm.",
  },
  login: {
    switchLabel: "Đăng nhập hoặc tạo tài khoản",
    title: "Đăng nhập",
    subtitle:
      "Dùng tài khoản nhóm Gotham của bạn. Phiên dùng JWT ngắn hạn với refresh token xoay vòng.",
    emailLabel: "Email",
    emailPlaceholder: "you{'@'}gotham.dev",
    passwordLabel: "Mật khẩu",
    passwordPlaceholder: "Mật khẩu của bạn",
    submit: "Đăng nhập",
    createAccount: "Tạo tài khoản",
    forgot: "Quên mật khẩu?",
    resetHint: "Chưa hỗ trợ đặt lại mật khẩu.",
    divider: "hoặc",
    oauthFailed: "Đăng nhập GitHub thất bại. Vui lòng thử lại.",
  },
  register: {
    title: "Tạo tài khoản",
    firstAccount: "Tài khoản đầu tiên trên một instance mới sẽ trở thành",
    ownerWord: "owner",
    ownerSuffix: "của nhóm mặc định.",
    invitedPrefix: "Bạn được mời tham gia",
    invitedFallback: "nhóm này",
    invitedSuffix: "Chọn thông tin đăng nhập để chấp nhận.",
    emailLabel: "Email",
    emailPlaceholder: "you{'@'}gotham.dev",
    passwordLabel: "Mật khẩu",
    passwordPlaceholder: "Ít nhất 10 ký tự",
    passwordHint:
      "Ít nhất 10 ký tự với 2 nhóm ký tự: chữ thường, chữ hoa, chữ số, ký hiệu.",
    confirmLabel: "Xác nhận mật khẩu",
    confirmPlaceholder: "Nhập lại mật khẩu",
    terms: "Tôi đồng ý với Điều khoản sử dụng và cách instance này lưu dữ liệu.",
    submit: "Tạo tài khoản",
    divider: "hoặc",
  },
  oauth: {
    githubSignin: "Đăng nhập bằng GitHub",
    githubSignup: "Đăng ký bằng GitHub",
    signingIn: "Đang đăng nhập",
    failed: "Đăng nhập thất bại",
    inProgress: "Đang hoàn tất đăng nhập GitHub…",
    noSession: "GitHub không trả về phiên dùng được.",
    back: "Quay lại đăng nhập",
  },
  invite: {
    title: "Lời mời vào nhóm",
    joined: "Bạn đã tham gia {name}.",
    openTeams: "Mở nhóm",
    backToTeams: "Quay lại nhóm",
    noToken:
      "Liên kết này không mang token mời. Hãy mở đúng liên kết trong thư mời như đã chia sẻ.",
    hint: "Việc chấp nhận dùng phiên bạn đang đăng nhập — hãy đăng nhập bằng địa chỉ được mời. Mỗi lời mời chỉ dùng một lần.",
  },
  footnote: {
    hashPrefix: "Mật khẩu được băm bằng",
    tokenMid:
      "· JWT truy cập 15 phút với refresh token xoay vòng 30 ngày · GitHub OAuth qua",
    interfaceSuffix: "giao diện.",
  },
  strength: {
    level0: "Chưa nhập",
    level1: "Rất yếu",
    level2: "Yếu",
    level3: "Trung bình",
    level4: "Mạnh",
  },
  validation: {
    emailRequired: "Email là bắt buộc",
    emailInvalid: "Nhập địa chỉ email hợp lệ",
    passwordRequired: "Mật khẩu là bắt buộc",
    passwordPolicy:
      "Dùng ít nhất 10 ký tự với 2 nhóm ký tự (chữ thường, chữ hoa, chữ số, ký hiệu)",
    confirmRequired: "Vui lòng xác nhận mật khẩu",
    confirmMismatch: "Mật khẩu không khớp",
    termsRequired: "Bạn phải chấp nhận điều khoản để tạo tài khoản",
  },
};

export default vi;
