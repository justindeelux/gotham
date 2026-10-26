---
title: Thông báo
---

# Thông báo

Gotham ghi nhận kết quả triển khai và sao lưu trong hộp thông báo ngay trên bảng
điều khiển, kể cả khi chưa cấu hình kênh nào. Bạn có thể thêm kênh để gửi cùng các
sự kiện đó ra ngoài qua Discord, Slack, Telegram hoặc email.

## Hộp thông báo

Mọi người đã đăng nhập đều có biểu tượng chuông trên thanh trên cùng, kèm số thông
báo chưa xem. Nhấn vào chuông, hoặc đưa tiêu điểm đến chuông rồi nhấn Enter hay
Space, để mở hộp thông báo. Hộp tải 30 thông báo mới nhất mà bạn được xem, sắp xếp
từ mới đến cũ. Mỗi mục hiển thị kết quả triển khai hoặc sao lưu, tên ứng dụng hoặc
cơ sở dữ liệu, máy chủ hoặc lỗi nếu có, và thời gian tương đối.

Hộp ghi nhận bốn sự kiện: triển khai thành công, triển khai thất bại, sao lưu thành
công và sao lưu thất bại. **Gửi thử** chỉ kiểm tra kênh, không tạo mục trong hộp
thông báo. Khi trang đang mở, thông báo mới xuất hiện mà không cần tải lại trang.
Nếu kết nối trực tiếp không khả dụng, hộp thông báo được làm mới khoảng mỗi phút
khi trang đang hiển thị.

Mở hộp thông báo sẽ đánh dấu đã xem đến mục mới nhất đã tải, kể cả các mục cũ hơn
nằm ngoài danh sách hiển thị. Số thông báo chưa xem được lưu theo tài khoản, giữ
nguyên khi tải lại trang và dùng trên thiết bị khác.

Nhấn vào một mục để mở ứng dụng hoặc cơ sở dữ liệu tương ứng. Nếu tài nguyên không
còn tồn tại, thông báo vẫn được giữ trong hộp nhưng không mở được tài nguyên đó.
Gotham giữ 500 thông báo mới nhất cho toàn bộ instance và tự động xóa các mục cũ hơn.

Chủ sở hữu thấy mọi thông báo. Thành viên chỉ thấy thông báo thuộc các dự án được
cấp quyền, được tạo từ thời điểm cấp quyền trở đi. Thu hồi quyền sẽ ẩn ngay các
thông báo đó; thông báo không thuộc dự án nào chỉ hiện với chủ sở hữu. Xem
[Thành viên và quyền truy cập](/vi/using/members-access).

## Kênh tùy chọn

Mở **Cài đặt → Thông báo** (`/settings/notifications`). Một kênh thuộc về một ứng
dụng hoặc một cơ sở dữ liệu, và chỉ người truy cập được tài nguyên đó mới thấy kênh
trong danh sách.

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

## Phạm vi và sự kiện

Một kênh theo dõi đúng một ứng dụng hoặc một cơ sở dữ liệu:

- Kênh ứng dụng nhận sự kiện triển khai của ứng dụng đó.
- Kênh cơ sở dữ liệu nhận sự kiện sao lưu của cơ sở dữ liệu đó.

Các sự kiện đăng ký được là `deploy_success`, `deploy_failure`, `backup_success` và
`backup_failure`. Kênh mới đăng ký tất cả trừ khi bạn chọn một tập con. Kênh bị tắt
không nhận gì.

## Gửi thử và lịch sử gửi

**Gửi thử** gửi một tin nhắn thử tổng hợp tới đúng kênh đó và báo kết quả ngay. Nó
được ghi vào lịch sử như một lần thử thật.

**Lịch sử gửi** hiển thị 20 lần thử mới nhất của kênh, mới nhất trước: sự kiện (hoặc
**Tin nhắn thử**), đã gửi hay thất bại, thời điểm, và một tóm tắt lỗi có giới hạn cho
các lần thất bại. Mỗi kênh giữ 50 lần thử cuối; các dòng cũ hơn bị cắt tự động. Việc
gửi không bao giờ chặn một lần triển khai hay sao lưu — nó chạy trên một hàng đợi nền
có giới hạn, và một kênh lỗi không làm dừng các kênh khác.

Tạo, sửa, xóa, bật và thử một kênh cần quyền ghi vào dự án chứa tài nguyên. Thành
viên ngoài dự án đó không thấy kênh.
