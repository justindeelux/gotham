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
- Email chủ sở hữu và mật khẩu trong phần tóm tắt đó. Nếu bạn để trình cài đặt tạo
  mật khẩu, nó đã được in một lần ở cuối.
- Một terminal trên máy chủ control plane cho các lệnh `admin` bên dưới.

## Đăng nhập

Mở URL bảng điều khiển và đăng nhập bằng email chủ sở hữu và mật khẩu. Việc đăng ký
bị đóng: đúng một tài khoản khởi tạo instance, và mọi người khác tham gia qua lời
mời của chủ sở hữu.

Trình cài đặt thu thập thông tin đăng nhập trước khi cài bất cứ thứ gì và tạo tài
khoản ngay sau các migration cơ sở dữ liệu, trước khi dịch vụ khởi động. Không ai
đăng ký được qua biểu mẫu web đang mở trong lúc trình cài đặt chờ ở câu hỏi.

- **Cài tương tác** (có terminal, kể cả `curl | sh`): trình cài đặt hỏi email chủ
  sở hữu, rồi mật khẩu với đầu vào ẩn và xác nhận. Không khớp hoặc mật khẩu ngoài
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

Việc tự đăng ký vẫn đóng khi tài khoản đó đã tồn tại: chỉ lời mời của chủ sở hữu
mới tạo được thành viên. Một lời mời gắn với một địa chỉ email và với các dự án mà
thành viên mới được mở, có hiệu lực bảy ngày và chỉ dùng được một lần. Chủ sở hữu
quản lý thành viên, quyền truy cập dự án của họ, và việc đặt lại phiên đăng nhập
cùng xác thực hai lớp của một thành viên ngay trong bảng điều khiển.

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
- `--force` thêm một tài khoản dù instance đã có chủ sở hữu. Tài khoản mới là
  thành viên, không phải chủ sở hữu thứ hai.
- `gotham admin exists` in `admin-exists: true|false` để dùng trong script (đó là
  cách trình cài đặt phát hiện một lần chạy lại).

## Mời người khác

1. Mở **Người dùng**, chọn **Mời người dùng**, chọn các dự án người đó được dùng
   và tạo lời mời, rồi gửi liên kết được hiển thị.
2. Thành viên mở `/register?invite=<token>` và chọn thông tin đăng nhập bằng email
   được mời. Liên kết chỉ hiện một lần, dùng được một lần và hết hạn sau bảy ngày.
3. Đổi quyền dự án của một thành viên, hoặc đặt lại phiên đăng nhập và xác thực
   hai lớp của họ, ngay trên trang đó. Xoá một thành viên sẽ kết thúc ngay các
   phiên và API token của họ.

Lời mời chỉ tạo tài khoản mới. Để cấp thêm dự án cho một thành viên đã có tài
khoản, hãy sửa **Quyền dự án** của tài khoản đó.

Thêm máy chủ từ **Máy chủ**; xem [Thêm node](/vi/guide/nodes). Mỗi node báo nhịp
tim và hiện trong bảng điều khiển khi sẵn sàng.

## Mất mật khẩu chủ sở hữu

Đặt lại trên máy chủ control plane:

```sh
sudo -u gotham -- /var/lib/gotham/bin/gotham admin reset-password --email ops@example.com
```

Việc này thu hồi mọi phiên của tài khoản. Bearer access token được đối chiếu với
phiên còn sống ở mỗi yêu cầu, nên token đã cấp trước lần đặt lại mất hiệu lực
ngay; việc đặt lại là một lần đăng xuất tức thời trên mọi thiết bị.

## Tài khoản đầu tiên là chủ sở hữu

Trên một instance mới, tài khoản được tạo khi bảng người dùng còn trống (lần đăng
ký đầu tiên, lần đăng nhập OAuth đầu tiên, hoặc `gotham admin create`) trở thành
chủ sở hữu. Đúng một tài khoản giữ vai trò đó.

Chỉ chủ sở hữu mới quản lý được người dùng và lời mời, đổi quyền dự án, thêm máy
chủ và khoá, kết nối nhà cung cấp Git, đổi cài đặt instance và mạng, quản lý bề
mặt Tên miền & SSL toàn cục, áp dụng cập nhật, và dùng terminal cùng trình quản lý
tệp trên máy chủ.

Các instance đã nâng cấp theo cùng quy tắc: tài khoản hiện có sớm nhất trở thành
chủ sở hữu và mọi tài khoản khác trở thành thành viên. Thành viên khởi đầu không có
quyền dự án, nên chủ sở hữu phải cấp từng dự án họ cần từ **Người dùng → Quyền dự
án**.

**Rủi ro bị chiếm quyền.** Trên một instance mới, việc đăng ký mở cho đến khi tài
khoản đầu tiên tồn tại, nên ai đăng ký trước sẽ trở thành chủ sở hữu. Trên một
instance công khai chưa cấu hình, hãy tạo tài khoản đó trước khi phơi control plane
ra. Xóa trống bảng người dùng bằng tay sẽ mở lại việc đăng ký lần đầu, và người
đăng ký tiếp theo lại trở thành chủ sở hữu.

## Tiếp theo

- [Tên miền và HTTPS](/vi/guide/domain-and-https) cho tên miền thật và chứng chỉ.
- [Thêm node](/vi/guide/nodes) để mở rộng ngoài máy chủ cục bộ.
- [Cập nhật và quay lui](/vi/guide/updates) để giữ bản cài luôn mới.
