---
title: Nhóm và thông báo
---

# Nhóm và thông báo

Mọi tài nguyên thuộc về đúng một nhóm, và vai trò của bạn trong nhóm đó quyết định
bạn đổi được gì. Nhóm cũng sở hữu các kênh thông báo. Cài đặt tài khoản — tên hiển
thị, mật khẩu, xác thực hai lớp và các phiên — nằm trong **Cài đặt**.

## Vai trò

Có ba vai trò: `owner`, `admin` và `read_only`. Không có vai trò `member`.

| Vai trò | Có thể làm |
|---|---|
| `owner` | Mọi thứ, kể cả đổi chủ sở hữu và xóa nhóm. |
| `admin` | Quản lý tài nguyên và thành viên, trừ chủ sở hữu. |
| `read_only` | Đọc tài nguyên và thành viên, và không bao giờ thay đổi. |

Vai trò chỉ có hiệu lực trong một nhóm. Các bề mặt của người vận hành — Tên miền &
SSL, Cập nhật và Cài đặt instance — cần cờ quản trị viên nền tảng ở cấp instance,
tách biệt với vai trò trong nhóm.

Vai trò trong nhóm được API thực thi, không chỉ bảng điều khiển. Thành viên
`read_only` đọc được tài nguyên và thành viên của nhóm, nhưng mọi thao tác ghi — tạo
hay sửa tài nguyên, đổi thành viên, quản lý lời mời, gửi thử, xóa kênh — đều bị từ
chối phía máy chủ. Một vài điều khiển vẫn hiện và báo rằng vai trò của bạn không cho
phép thao tác đó; đừng coi một nút hiện ra là quyền.

Chọn nhóm đang hoạt động trên **Nhóm** (bấm vào tên nhóm); trang Thông báo có cùng
bộ chọn đó. Danh sách, thông báo và tài nguyên đều thuộc phạm vi nhóm đó.

## Quản lý nhóm

- **Tạo** một nhóm từ **Nhóm**. Bạn trở thành chủ sở hữu của nó; nhóm mới bắt đầu
  trống.
- **Đổi tên** một nhóm từ danh sách nhóm (chủ sở hữu hoặc quản trị viên).
- **Xóa** một nhóm từ danh sách nhóm (chỉ chủ sở hữu). Nhóm còn sở hữu tài nguyên
  hoặc node sẽ bị từ chối, nên hãy di chuyển hoặc xóa những thứ đó trước. Nhóm cá
  nhân không bao giờ xóa được: nó là phạm vi mặc định của tài khoản.

## Thành viên

Tab **Thành viên** liệt kê email, vai trò và ngày tham gia của từng thành viên. Chủ
sở hữu hoặc quản trị viên có thể đổi vai trò của một thành viên hoặc gỡ họ, với các
bảo vệ sau:

- Một nhóm luôn giữ ít nhất một chủ sở hữu. Hạ cấp hoặc gỡ chủ sở hữu cuối cùng sẽ
  bị từ chối.
- Chỉ chủ sở hữu mới được cấp vai trò chủ sở hữu, hoặc đổi hay gỡ một chủ sở hữu
  khác. Quản trị viên không đụng được vào chủ sở hữu.
- Tư cách chủ sở hữu của nhóm cá nhân là bất biến: không thể bị hạ cấp hay gỡ.

Thay đổi vai trò có hiệu lực ngay; các tài nguyên mà thành viên đó với tới sẽ theo
vai trò mới ở yêu cầu tiếp theo. Bảng điều khiển có thể vẫn hiển thị một điều khiển
mà vai trò của bạn không dùng được — API sẽ trả lời bằng việc từ chối.

## Lời mời

Chủ sở hữu và quản trị viên tạo lời mời từ tab **Lời mời**. Một lời mời nêu một địa
chỉ email và một vai trò, và chỉ có thể cấp `admin` hoặc `read_only`; quyền sở hữu
được chuyển từ bảng thành viên, không bao giờ qua lời mời.

Lời mời là **dùng một lần** và gắn với địa chỉ email được mời. Chúng hết hạn sau bảy
ngày kể từ khi tạo. Token chỉ được lưu dưới dạng hash, nên liên kết chỉ hiện đúng
một lần, khi lời mời được tạo; việc gửi email chưa được nối, nên hãy sao chép liên
kết và tự gửi cho người được mời. Một lời mời đang chờ có thể bị thu hồi trước khi
được chấp nhận.

Hộp thoại đưa ra hai liên kết:

- **Liên kết thành viên mới** — `/register?invite=…`. Việc đăng ký bị đóng khi
  instance đã có tài khoản, nên đây là cách một người mới có tài khoản.
- **Liên kết chấp nhận** — `/invite/accept?token=…`. Dành cho người đã có tài khoản.

Liên kết nào cũng chấp nhận được lời mời. Liên kết đăng ký tạo tài khoản và dùng
token trong một bước; liên kết chấp nhận dành cho người đã có tài khoản. Token được
gửi kèm phiên mà họ đang đăng nhập, nên họ phải đăng nhập bằng địa chỉ được mời;
email của tài khoản và email của lời mời phải khớp nhau. Một lời mời đã dùng, đã hết
hạn, đã bị thu hồi hoặc sai địa chỉ không bao giờ cấp quyền thành viên, và sau khi
chấp nhận, token được gỡ khỏi thanh địa chỉ để việc tải lại không phát lại được.

Thành viên `read_only` không thấy danh sách lời mời: liệt kê và quản lý lời mời là
thao tác của chủ sở hữu/quản trị viên.

## Kênh thông báo

Mở **Cài đặt → Kênh thông báo** (`/settings/notifications`). Các kênh thuộc về nhóm
đang hoạt động, và trang đi theo bộ chọn nhóm.

| Loại | Cấu hình |
|---|---|
| Discord | URL webhook đến. |
| Slack | URL webhook đến. |
| Telegram | Token bot và chat ID mà bot đăng tới. |
| Email (SMTP) | Host SMTP, cổng, tên người dùng và mật khẩu tùy chọn, địa chỉ gửi và ít nhất một người nhận. |

Bí mật truyền tải — URL webhook, token bot, mật khẩu SMTP — được niêm phong phía máy
chủ trước khi lưu, không bao giờ ghi log và không bao giờ được API trả về. Khi đọc,
giá trị được che, tối đa để lộ bốn ký tự cuối, và một kết nối đã lưu cho biết bí mật
đã được cấu hình hay chưa. Sửa một kênh và để nguyên giá trị đã che sẽ giữ bí mật đã
lưu; loại kênh không thể đổi sau khi tạo.

### Phạm vi và sự kiện

Một kênh hoặc là **toàn nhóm** hoặc thuộc phạm vi một tài nguyên:

- Kênh toàn nhóm nhận mọi sự kiện được chọn cho bất kỳ tài nguyên nào trong nhóm.
- Kênh theo phạm vi ứng dụng chỉ nhận sự kiện triển khai của ứng dụng đó.
- Kênh theo phạm vi cơ sở dữ liệu chỉ nhận sự kiện sao lưu của cơ sở dữ liệu đó.

Các sự kiện đăng ký được là `deploy_success`, `deploy_failure`, `backup_success` và
`backup_failure`. Kênh mới đăng ký tất cả trừ khi bạn chọn một tập con. Kênh bị tắt
không nhận gì.

### Gửi thử và lịch sử gửi

**Gửi thử** gửi một tin nhắn thử tổng hợp tới đúng kênh đó và báo kết quả ngay. Nó
được ghi vào lịch sử như một lần thử thật.

**Lịch sử gửi** hiển thị 20 lần thử mới nhất của kênh, mới nhất trước: sự kiện (hoặc
**Tin nhắn thử**), đã gửi hay thất bại, thời điểm, và một tóm tắt lỗi có giới hạn cho
các lần thất bại. Mỗi kênh giữ 50 lần thử cuối; các dòng cũ hơn bị cắt tự động. Việc
gửi không bao giờ chặn một lần triển khai hay sao lưu — nó chạy trên một hàng đợi nền
có giới hạn, và một kênh lỗi không làm dừng các kênh khác.

Tạo, sửa, xóa, bật và thử một kênh đều cần chủ sở hữu hoặc quản trị viên. Thành viên
`read_only` đọc được các kênh và lịch sử của chúng, nhưng API từ chối mọi thao tác
thay đổi, kể cả gửi thử.

## Cài đặt tài khoản

Mở **Cài đặt → Hồ sơ** (`/settings/profile`).

- **Tài khoản** hiển thị email, vai trò nền tảng và ngày tham gia của bạn. Địa chỉ
  email không thể đổi ở đây.
- **Tên hiển thị** thay cho email trong sidebar. Nó dài 1–64 ký tự; xóa trống để
  quay lại dùng email. Ảnh đại diện lấy từ GitHub khi bạn đăng nhập bằng GitHub, nếu
  không sẽ hiện chữ cái đầu của bạn.
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
[API token](/vi/reference/api-tokens).
