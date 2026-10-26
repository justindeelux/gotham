---
title: Thông báo
---

# Thông báo

Gotham ghi nhận kết quả triển khai, sao lưu, thay đổi kết nối máy chủ, cảnh báo
chứng chỉ sắp hết hạn, bản cập nhật Gotham mới và ứng dụng cần triển khai lại
trong hộp thông báo ngay trên bảng điều khiển, kể cả khi chưa cấu hình kênh nào.
Bạn có thể thêm kênh để gửi cùng các sự kiện đó ra ngoài qua Discord, Slack,
Telegram hoặc email.

## Hộp thông báo

Mọi người đã đăng nhập đều có biểu tượng chuông trên thanh trên cùng, kèm số thông
báo chưa xem. Nhấn vào chuông, hoặc đưa tiêu điểm đến chuông rồi nhấn Enter hay
Space, để mở hộp thông báo. Hộp tải 30 thông báo mới nhất mà bạn được xem, sắp xếp
từ mới đến cũ. Mỗi mục hiển thị sự kiện, tài nguyên, tên miền hoặc phiên bản, địa
chỉ máy chủ hoặc lỗi nếu có, và thời gian tương đối.

Hộp ghi nhận triển khai thành công, triển khai thất bại, sao lưu thành công, sao
lưu thất bại, máy chủ mất kết nối, máy chủ kết nối lại, chứng chỉ sắp hết hạn,
bản cập nhật Gotham mới và ứng dụng cần triển khai lại. Mục triển khai lại nêu
thiết lập đang chờ (tên miền hoặc cổng, biến môi trường, secret hoặc lưu trữ) và
mở ứng dụng. Sự kiện máy chủ, chứng chỉ và cập nhật chỉ hiện với chủ sở hữu. **Gửi thử** chỉ kiểm tra kênh, không tạo mục trong hộp
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

Mở **Cài đặt → Thông báo** (`/settings/notifications`). Kênh có phạm vi **Mọi tài
nguyên**, một ứng dụng hoặc một cơ sở dữ liệu. Chỉ chủ sở hữu thấy và quản lý kênh
Mọi tài nguyên; người truy cập được tài nguyên sẽ thấy kênh thuộc tài nguyên đó.

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

Chọn phạm vi khi tạo hoặc sửa kênh:

- Kênh **Mọi tài nguyên** nhận mọi sự kiện đã chọn, kể cả sự kiện máy chủ, chứng chỉ và cập nhật trên
  toàn instance.
- Kênh ứng dụng nhận sự kiện triển khai của ứng dụng đó và sự kiện cần triển
  khai lại của ứng dụng đó.
- Kênh cơ sở dữ liệu nhận sự kiện sao lưu của cơ sở dữ liệu đó.

Các sự kiện đăng ký được là `deploy_success`, `deploy_failure`, `backup_success`,
`backup_failure`, `server_offline`, `server_online`, `certificate_expiring`,
`update_available` và `redeploy_needed`. Kênh mới chỉ chọn sẵn bốn sự
kiện triển khai/sao lưu. Với cả kênh cũ và mới, hãy sửa kênh **Mọi tài nguyên** và
chọn thêm sự kiện máy chủ, chứng chỉ hoặc cập nhật nếu muốn nhận. Kênh ứng dụng và cơ sở dữ
liệu không nhận các sự kiện trên toàn instance này. `redeploy_needed` cũng phải
chọn thêm, và chỉ kênh **Mọi tài nguyên** hoặc kênh gắn với chính ứng dụng đó mới
nhận được. Kênh bị tắt không nhận gì.

Ứng dụng cần triển khai lại khi một thiết lập chỉ áp dụng cho container mới thay
đổi: tên miền hoặc cổng, biến môi trường, secret hoặc lưu trữ. Đúng một sự kiện
`redeploy_needed` được ghi ở thay đổi đầu tiên sau một lần triển khai thành công;
các thay đổi tiếp theo khi vẫn đang chờ sẽ không báo, và lần triển khai thành công
kế tiếp xóa cờ để thay đổi sau đó báo lại.

Máy chủ được coi là mất kết nối sau ít nhất 30 giây không gửi nhịp tim. Gotham
kiểm tra mỗi 10 giây ngay cả khi không ai mở trang máy chủ, và chờ 30 giây sau khi
khởi động trước khi kiểm tra máy chủ mất kết nối. Mỗi lần chuyển trạng thái tạo
một thông báo mất kết nối; nhịp tim thành công đầu tiên sau đó tạo một thông báo
kết nối lại. Chỉ đăng ký lại hoặc đăng ký lần đầu không tạo thông báo kết nối lại.
Gián đoạn ngắn hơn không tạo cảnh báo; mất kết nối lặp lại lâu hơn khoảng chờ có
thể tạo nhiều cặp thông báo mất kết nối/kết nối lại.

Gotham kiểm tra chứng chỉ đang bật của bảng điều khiển và ứng dụng khi khởi động,
sau đó mỗi giờ một lần. Khi chứng chỉ còn tối đa bảy ngày, hai lần quan sát thành
công trong khoảng này mới tạo cảnh báo. Chứng chỉ đã hết hạn cũng được tính.
Chứng chỉ không rõ trạng thái, không có hoặc bị tắt và các lần đọc thất bại được
bỏ qua, không tính là một lần quan sát. Đọc thất bại không xóa một lần quan sát
đang chờ. Mỗi chứng chỉ và ngày hết hạn chỉ tạo một cảnh báo; chứng chỉ được gia
hạn với ngày hết hạn mới có thể tạo cảnh báo khi gần đến ngày đó.
Khởi động lại Gotham xóa bộ nhớ này, nên chứng chỉ vẫn gần hết hạn có thể được thông báo lại.

Thông báo có bản cập nhật được tạo khi lần kiểm tra cập nhật Gotham thủ công hoặc
theo lịch thành công tìm thấy phiên bản mới hơn cho kênh cập nhật đã chọn. Mỗi cặp
kênh/phiên bản chỉ thông báo một lần; lần kiểm tra thất bại hoặc kết quả đang ở
bản mới nhất không xóa bộ nhớ này. Thông báo cần dịch vụ cập nhật đang chạy:
`FEATURE_UPDATES=false` tắt dịch vụ đó. Khi tắt cập nhật tự động, kiểm tra theo lịch
mặc định cũng tắt, nên chỉ kiểm tra thủ công trên bảng điều khiển tạo thông báo
nếu chưa bật lịch kiểm tra. Thông báo không bật kiểm tra hay cập nhật tự động.
Khởi động lại Gotham xóa bộ nhớ này, nên cùng bản cập nhật có thể được thông báo lại.

## Gửi thử và lịch sử gửi

**Gửi thử** gửi một tin nhắn thử tổng hợp tới đúng kênh đó và báo kết quả ngay. Nó
được ghi vào lịch sử như một lần thử thật.

**Lịch sử gửi** hiển thị 20 lần thử mới nhất của kênh, mới nhất trước: sự kiện (hoặc
**Tin nhắn thử**), đã gửi hay thất bại, thời điểm, và một tóm tắt lỗi có giới hạn cho
các lần thất bại. Mỗi kênh giữ 50 lần thử cuối; các dòng cũ hơn bị cắt tự động. Việc
gửi không bao giờ chặn một lần triển khai hay sao lưu — nó chạy trên một hàng đợi nền
có giới hạn, và một kênh lỗi không làm dừng các kênh khác.

Với kênh thuộc một tài nguyên, tạo, sửa, xóa, bật và thử cần quyền ghi vào dự án
chứa tài nguyên. Thành
viên ngoài dự án đó không thấy kênh.
