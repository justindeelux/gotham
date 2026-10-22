---
title: Bắt đầu
---

# Bắt đầu

Gotham là một nền tảng dịch vụ tự host: một control plane viết bằng Go, một node
agent trên mỗi máy chủ, và một bảng điều khiển web để triển khai ứng dụng, cơ sở
dữ liệu và dịch vụ. Bạn cài đặt nó trên máy chủ Linux của riêng mình; không có
dịch vụ nào được host thay bạn.

Nếu control plane chưa được cài, hãy bắt đầu với [Cài đặt](/vi/guide/install).
Trang này tiếp nối nơi trình cài đặt kết thúc: đăng nhập và thêm node từ xa đầu
tiên của bạn.

## Lần đăng nhập đầu tiên

Trên một instance mới, tài khoản đầu tiên trở thành quản trị viên nền tảng. Tài
khoản đó do trình cài đặt tạo, hoặc tạo bằng tay trên máy chủ control plane khi
lần cài đặt chạy phi tương tác mà không có email:

```sh
sudo -u gotham /var/lib/gotham/bin/gotham admin create --email ops@example.com
```

Lệnh này hỏi mật khẩu với đầu vào ẩn. `--password-stdin` đọc một dòng từ pipe
(dùng cho script), `--generate-password` in ra một mật khẩu ngẫu nhiên đúng một
lần, và `--force` cố ý thêm tài khoản thứ hai — nếu không có nó, CLI từ chối khi
đã có tài khoản. `gotham admin exists` in `admin-exists: true|false` để dùng
trong script.

Khi đã có tài khoản, việc tự đăng ký bị đóng; người khác tham gia qua liên kết
mời dùng một lần tạo trong **Nhóm** (xem
[Nhóm và thông báo](/vi/using/teams-notifications)).

Mở URL mà trình cài đặt in ra (bảng điều khiển mặc định lắng nghe trên cổng 8000)
và đăng nhập bằng email và mật khẩu. Có thêm hai cách đăng nhập:

- **GitHub OAuth.** Dùng được khi instance đã cấu hình GitHub OAuth với
  `GOTHAM_OAUTH_GITHUB_CLIENT_ID`, `GOTHAM_OAUTH_GITHUB_CLIENT_SECRET` và một
  redirect URL đã đăng ký với ứng dụng GitHub OAuth. Đây là nhà cung cấp OAuth
  duy nhất.
- **Xác thực hai lớp.** Tùy chọn theo từng tài khoản: bật TOTP trong
  **Hồ sơ → Xác thực hai lớp**, quét mã QR bằng ứng dụng xác thực, và lưu các mã
  khôi phục — chúng chỉ hiện một lần và mỗi mã dùng được một lần. Sau đó, đăng
  nhập sẽ hỏi mật khẩu rồi đến mã. Mất ứng dụng xác thực?
  `gotham admin reset-password --email ops@example.com` thay mật khẩu, thu hồi
  các phiên refresh và xóa xác thực hai lớp. Các access token đã cấp vẫn có hiệu
  lực cho đến khi hết hạn.

Tài khoản đầu tiên giữ vai trò quản trị viên nền tảng, vai trò mà các bề mặt toàn
nền tảng (cài đặt instance, cập nhật, nhà cung cấp DNS, đồng bộ proxy toàn node)
kiểm tra. Thành viên tham gia nhóm với vai trò `owner`, `admin` hoặc `read_only`;
`read_only` đọc được mọi thứ và mọi thao tác ghi đều bị từ chối phía máy chủ.

## Thêm node

Trình cài đặt đã đăng ký máy chủ control plane thành node `localhost`, nên thường
bạn bắt đầu với một node. Thêm node khác từ **Máy chủ → Thêm máy chủ**; wizard có
bốn bước.

1. **Kết nối.** Đặt cho node một tên duy nhất, địa chỉ IP hoặc hostname, cổng SSH
   và người dùng SSH. Xác thực bằng khóa riêng (mới hoặc đã có) hoặc bằng mật
   khẩu, rồi tin cậy host key. Thông tin đăng nhập được mã hóa trước khi lưu và
   API không bao giờ trả về. Đổi địa chỉ, người dùng hoặc thông tin đăng nhập về
   sau sẽ đặt lại host key đã ghim và đưa node về trạng thái chờ.
2. **Kiểm tra.** Control plane dò node qua SSH để tìm Docker, CPU, RAM và đĩa.
   Mỗi đợt dò chạy tối đa 15 giây. Nếu thiếu Docker, wizard đề nghị cài trước khi
   bạn tiếp tục; các lỗi khác phải sửa trên node (hoặc chọn node lớn hơn) rồi
   kiểm tra lại.
3. **Cài đặt.** Sao chép đoạn lệnh được tạo vào một phiên SSH trên node. Lệnh này
   sao chép chứng chỉ CA công khai của control plane, clone trình cài đặt và chạy
   trình cài agent với địa chỉ control plane, một node id và `--ca`. Agent đăng
   ký, nhận chứng chỉ node, và trang thăm dò mỗi 5 giây cho đến khi nhịp tim đầu
   tiên chuyển trạng thái sang sẵn sàng.
4. **Hoàn tất.** Node được liệt kê kèm bộ nhớ và đĩa phát hiện được; mở nó để xem
   chỉ số, container, terminal của máy chủ và trình quản lý tệp. Các thao tác node
   thường ngày được trình bày trong
   [Node và container](/vi/operating/nodes-containers).

### Token đăng ký dùng để làm gì

Lần đăng ký đầu tiên của một node chưa có chứng chỉ, nên control plane chặn nó
bằng một token đăng ký dùng một lần. Control plane tạo token này (trừ khi người
vận hành đặt `GOTHAM_GRPC_ENROLLMENT_TOKEN`), và trình cài đặt chuyển nó cho agent
qua `--enrollment-token-file` hoặc `GOTHAM_AGENT_ENROLLMENT_TOKEN`. Chỉ lần đăng
ký đầu tiên cần token; sau đó agent xác thực bằng chứng chỉ và khóa của chính nó.
Không có token, lần đăng ký đầu tiên bị từ chối trừ khi người vận hành đã bật rõ
ràng chế độ đăng ký mở — đừng bao giờ để một control plane production ở trạng thái
đó.

### Tình trạng agent

Agent chạy một endpoint kiểm tra sống nhỏ tại `/healthz`, mặc định gắn với
loopback (`GOTHAM_AGENT_HEALTH_ADDR`, cổng 8001). Endpoint này chỉ báo tiến trình
còn sống — nó cố ý không kiểm tra kết nối tới control plane, nên một bản cập nhật
không bị quay lui chỉ vì control plane tạm thời không với tới được. Node bị báo
ngoại tuyến khi không có nhịp tim nào trong 30 giây; agent gửi nhịp tim mỗi 10
giây.

Tiếp theo: triển khai thứ gì đó trong
[Dự án và môi trường](/vi/using/projects-environments), giữ bản cài đặt luôn mới
trong [Cập nhật](/vi/guide/updates), và dùng
[Xử lý sự cố](/vi/guide/troubleshooting) khi một node hoặc bảng điều khiển hoạt
động sai.
