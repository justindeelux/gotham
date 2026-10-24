---
title: Đăng nhập lần đầu
---

# Đăng nhập lần đầu

Trình cài đặt tạo một tài khoản cho bạn và in địa chỉ của nó cùng URL bảng điều
khiển. Trang này trình bày cách đăng nhập, mời người khác, và xử lý khi tài khoản
không được tạo.

## Bạn cần gì

- URL bảng điều khiển mà trình cài đặt in ra, thường là
  `http://<server-ip>:8000`.
- Email quản trị và mật khẩu trong phần tóm tắt đó. Nếu bạn để trình cài đặt tạo
  mật khẩu, nó đã được in một lần ở cuối.
- Một terminal trên máy chủ control plane cho các lệnh `admin` bên dưới.

## Đăng nhập

Mở URL bảng điều khiển và đăng nhập bằng email quản trị và mật khẩu. Việc đăng ký
bị đóng: đúng một tài khoản khởi tạo instance, và mọi người khác tham gia qua lời
mời của quản trị viên.

Trình cài đặt thu thập thông tin đăng nhập trước khi cài bất cứ thứ gì và tạo tài
khoản ngay sau các migration cơ sở dữ liệu, trước khi dịch vụ khởi động. Không ai
đăng ký được qua biểu mẫu web đang mở trong lúc trình cài đặt chờ ở câu hỏi.

- **Cài tương tác** (có terminal, kể cả `curl | sh`): trình cài đặt hỏi email quản
  trị, rồi mật khẩu với đầu vào ẩn và xác nhận. Không khớp hoặc mật khẩu ngoài
  8–128 ký tự sẽ hỏi lại, tối đa 3 lần tạo. Để trống mật khẩu sẽ tạo một mật khẩu
  ngẫu nhiên mạnh, in một lần trong phần tóm tắt cuối. Hãy lưu ngay; nó không hiện
  lại.
- **Cài không tương tác** (không có terminal): trình cài đặt không bao giờ hỏi.
  Đặt `GOTHAM_ADMIN_EMAIL` — và tùy chọn `GOTHAM_ADMIN_PASSWORD_FILE`, đường dẫn
  tới một tệp chỉ root đọc được chứa mật khẩu trên một dòng — để tạo tài khoản.
  Không có tệp mật khẩu thì một mật khẩu ngẫu nhiên được tạo và in một lần. Không
  có email thì trình cài đặt bỏ qua việc tạo và in lệnh thủ công bên dưới.
- **Đặt cả hai cách**: trên terminal, `GOTHAM_ADMIN_EMAIL` là giá trị mặc định cho
  câu hỏi email (nhập rỗng thì giữ nguyên, `-` thì bỏ qua) và
  `GOTHAM_ADMIN_PASSWORD_FILE` được dùng mà không hỏi. Một tệp mật khẩu đọc được
  bởi nhóm/mọi người sẽ in cảnh báo nhưng vẫn được dùng.
- **Chạy lại hoặc nâng cấp**: nếu instance đã có tài khoản, trình cài đặt báo
  `admin already exists, skipping` và không thay đổi gì. Nếu việc tạo thua một
  cuộc đua với lần đăng ký khác, nó báo
  `an account already exists (created by someone else?)`.
- **`--dry-run`** không hỏi gì và không thay đổi gì.

Mật khẩu không bao giờ được truyền làm đối số dòng lệnh, không bao giờ được đặt
trong biến môi trường, và không bao giờ được ghi ra đĩa, tệp môi trường, log hay
lịch sử shell: nó chỉ đi tới tệp nhị phân qua stdin (việc trace `sh -x` bị tắt
quanh phần xử lý bí mật).

## Nếu chưa có tài khoản

Nếu bạn đã bỏ qua câu hỏi email, hoặc lần cài không tương tác mà không có
`GOTHAM_ADMIN_EMAIL`, hãy tạo tài khoản bằng tay trên máy chủ control plane:

```sh
sudo -u gotham -- /var/lib/gotham/bin/gotham admin create --email ops@example.com
```

Câu hỏi mật khẩu được ẩn. Các cờ hữu ích:

- `--password-stdin` đọc mật khẩu từ stdin (một dòng, dùng cho script). Hãy pipe
  vào; đừng bao giờ dùng `--password`, vốn hiện trong `ps`.
- `--generate-password` tạo một mật khẩu ngẫu nhiên và in một lần dưới dạng
  `generated-password: <value>`.
- `--force` cố ý thêm tài khoản thứ hai. Không có nó, CLI từ chối khi đã có một
  tài khoản.
- `gotham admin exists` in `admin-exists: true|false` để dùng trong script (đó là
  cách trình cài đặt phát hiện một lần chạy lại).

## Mời người khác

1. Mở **Nhóm** và tạo một lời mời, rồi gửi liên kết được hiển thị.
2. Thành viên mở `/register?invite=<token>` và chọn thông tin đăng nhập của họ.
   Liên kết chỉ hiện một lần và sẽ hết hạn.
3. Thêm máy chủ từ **Máy chủ**; xem [Thêm node](/vi/guide/nodes). Mỗi node báo
   nhịp tim và hiện trong bảng điều khiển khi sẵn sàng.

## Mất mật khẩu quản trị

Đặt lại trên máy chủ control plane:

```sh
sudo -u gotham -- /var/lib/gotham/bin/gotham admin reset-password --email ops@example.com
```

Việc này thu hồi các phiên refresh của tài khoản. Bearer access token là JWT
không trạng thái, nên một token đã cấp vẫn hợp lệ cho đến khi hết hạn (15 phút);
việc đặt lại không phải một lần đăng xuất tức thời trên toàn hệ thống.

## Tài khoản đầu tiên là quản trị viên nền tảng

Trên một instance mới, tài khoản được tạo khi bảng người dùng còn trống (lần đăng
ký đầu tiên, lần đăng nhập OAuth đầu tiên, hoặc `gotham admin create` không có
`--force`) nhận cờ quản trị viên nền tảng, và các phiên của nó mang claim vai trò
`admin`. Các thao tác toàn nền tảng (đồng bộ proxy toàn node, nhà cung cấp DNS)
sau đó hoạt động mà không cần cấu hình thêm.

Các instance đã nâng cấp không thay đổi: migration để mọi dòng hiện có ở trạng
thái không phải quản trị, nên hãy tiếp tục liệt kê email tài khoản vận hành trong
`PLATFORM_ADMINS` ở `/etc/gotham/gotham.env` (hoặc cấp một API token có phạm vi
admin).

**Rủi ro bị chiếm quyền.** Trên một instance mới, việc đăng ký mở cho đến khi tài
khoản đầu tiên tồn tại, nên ai đăng ký trước sẽ trở thành quản trị viên. Trên một
instance công khai chưa cấu hình, hãy tạo tài khoản đó (hoặc đặt
`PLATFORM_ADMINS`) trước khi phơi control plane ra. Xóa trống bảng người dùng
bằng tay sẽ mở lại việc đăng ký lần đầu, và người đăng ký tiếp theo lại trở thành
quản trị viên nền tảng.

Một API token có phạm vi admin được cấp bởi một phiên quản trị vẫn sống sót sau
khi tài khoản đó bị hạ cấp: không có việc thu hồi phiên hay token nào gắn với thay
đổi vai trò, nên hãy xoay vòng token nếu quyền đó phải kết thúc.

## Tiếp theo

- [Tên miền và HTTPS](/vi/guide/domain-and-https) cho tên miền thật và chứng chỉ.
- [Thêm node](/vi/guide/nodes) để mở rộng ngoài máy chủ cục bộ.
- [Cập nhật và quay lui](/vi/guide/updates) để giữ bản cài luôn mới.
