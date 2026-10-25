---
title: Thành viên và quyền truy cập
---

# Thành viên và quyền truy cập

Gotham có hai vai trò tài khoản: **chủ sở hữu** (`owner`) và **thành viên**
(`member`). Tài khoản được tạo khi cài đặt là chủ sở hữu. Mọi người khác là thành
viên và chỉ thấy những dự án mà chủ sở hữu cấp.

## Chủ sở hữu làm được gì

Chỉ tài khoản chủ sở hữu mới có thể:

- mời người, xoá thành viên, đặt lại phiên đăng nhập và xác thực hai lớp của thành
  viên, và đổi dự án mà thành viên được truy cập;
- thêm và sửa máy chủ cùng khoá SSH;
- kết nối nhà cung cấp Git và GitHub App;
- đổi cài đặt instance và mạng, quản lý bề mặt Tên miền & SSL toàn cục (tuyến
  proxy, chuyển hướng, nhà cung cấp DNS và chứng chỉ bảng điều khiển);
- kiểm tra và áp dụng cập nhật, triển khai cập nhật agent;
- mở terminal và trình quản lý tệp trên máy chủ, và cài công cụ máy chủ từ Kho ứng
  dụng.

Các tuyến này yêu cầu tài khoản chủ sở hữu. API token của chủ sở hữu còn cần phạm
vi `admin`.

## Thành viên làm được gì

Thành viên thấy những dự án chủ sở hữu đã cấp và làm việc bên trong: tạo và sửa
môi trường, ứng dụng, dịch vụ và cơ sở dữ liệu; triển khai, quay lui, khởi động và
dừng chúng; quản lý biến, tên miền ứng dụng, bản xem trước và kênh thông báo cho
các ứng dụng và cơ sở dữ liệu đó; di chuyển hoặc xoá những gì thuộc dự án.

Thành viên không chọn node: tài nguyên họ tạo được control plane đặt trên một node
sẵn sàng. Dự án không được cấp sẽ không hiện ra — danh sách bỏ qua nó và một yêu
cầu trực tiếp trả `404`, giống như tài nguyên không tồn tại. Quyền được kiểm tra ở
mọi yêu cầu, nên thay đổi quyền có hiệu lực ngay.

Dự án do chủ sở hữu tạo và xoá. Thành viên có quyền trong dự án có thể đổi tên dự
án và quản lý nội dung của nó.

## Quản lý thành viên

Mở **Người dùng**. Danh sách hiện mọi tài khoản cùng vai trò, quyền dự án và ngày
tham gia. Với một thành viên, chủ sở hữu có thể:

- **Quyền dự án** — chọn các dự án thành viên được truy cập.
- **Đặt lại** — kết thúc mọi phiên và xoá xác thực hai lớp, nên người đó phải đăng
  nhập và thiết lập lại.
- **Xoá** — xoá tài khoản. Phiên và API token kết thúc ngay; tài nguyên thành viên
  đã tạo vẫn còn và chuyển cho chủ sở hữu.

Không thể xoá hay đặt lại tài khoản chủ sở hữu từ trang này. Hãy dùng lệnh `gotham
admin reset-password` trên máy chủ control plane.

Đúng một tài khoản là chủ sở hữu. Vai trò không chuyển được từ bảng điều khiển.

## Mời thành viên

1. Trên **Người dùng**, chọn **Mời người dùng**.
2. Nhập email của người đó và chọn các dự án họ được truy cập. Dự án là tùy chọn,
   nhưng thành viên không có dự án nào sẽ mở bảng điều khiển trống.
3. Sao chép liên kết được hiện và gửi tới địa chỉ đó. Liên kết chỉ hiện một lần;
   việc gửi email chưa được nối.

Người đó mở `/register?invite=…` và đặt mật khẩu bằng email được mời. Tài khoản
được tạo với vai trò thành viên và quyền dự án đã chọn.

Lời mời dùng một lần, gắn với địa chỉ được mời và hết hạn sau bảy ngày. Chủ sở hữu
có thể thu hồi lời mời đang chờ trên cùng trang. Lời mời chỉ tạo tài khoản mới: một
địa chỉ đã có tài khoản không mời được, hãy cấp dự án từ **Quyền dự án**.

## Cài đặt tài khoản

Mở **Cài đặt → Hồ sơ** (`/settings/profile`).

- **Tài khoản** hiển thị email, vai trò và ngày tham gia của bạn. Địa chỉ email
  không thể đổi ở đây.
- **Tên hiển thị** thay cho email trong sidebar. Nó dài 1–64 ký tự; xóa trống để
  quay lại dùng email. Ảnh đại diện lấy từ GitHub khi bạn đăng nhập bằng GitHub,
  nếu không sẽ hiện chữ cái đầu của bạn.
- **Đổi mật khẩu** hỏi mật khẩu hiện tại (khi tài khoản đã có mật khẩu) và một mật
  khẩu mới: ít nhất 10 ký tự thuộc hai nhóm ký tự. Đổi mật khẩu sẽ đăng xuất mọi
  thiết bị khác và giữ phiên này đăng nhập.
- **Xác thực hai lớp** dùng mã theo thời gian từ một ứng dụng xác thực. Quá trình
  thiết lập hiển thị mã QR và một khóa thiết lập thủ công, rồi hỏi mật khẩu hiện tại
  và một mã 6 chữ số. Khi bật, hệ thống trả về mười mã khôi phục, chỉ hiện một lần;
  mỗi mã dùng được một lần nếu bạn mất ứng dụng xác thực. Bật, tắt 2FA hoặc tạo lại
  mã khôi phục đều đăng xuất các thiết bị khác của bạn, và hai thao tác sau còn cần
  mật khẩu và một mã hiện tại. Đăng nhập bằng mã khôi phục sẽ kết thúc mọi phiên
  khác.
- **Phiên đang hoạt động** liệt kê từng thiết bị đang đăng nhập cùng IP, thời điểm
  tạo và hoạt động gần nhất, và đánh dấu thiết bị này. **Đăng xuất** kết thúc một
  phiên; **Đăng xuất mọi thiết bị khác** kết thúc mọi phiên trừ phiên hiện tại. Kết
  thúc thiết bị này sẽ đưa bạn về trang đăng nhập.

Quản lý phiên cần một phiên trình duyệt đã đăng nhập; một API token có phạm vi không
liệt kê hay thu hồi được phiên.

## API token

API token (`gt1_` với phạm vi read, deploy hoặc admin) có thể được tạo, xoay vòng và
thu hồi qua API, nhưng chưa có trang nào trên bảng điều khiển cho chúng. Xem
[API token](/vi/reference/api-tokens). Token của thành viên không bao giờ mang phạm
vi `admin` và chỉ hoạt động trong các dự án được cấp.
