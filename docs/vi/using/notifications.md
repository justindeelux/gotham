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

Khi gửi, Gotham phân giải host của kênh tại thời điểm kết nối. Kênh do thành viên
tạo chỉ đến được địa chỉ công khai; loopback, mạng riêng, NAT nhà mạng, link-local
và các dải dành riêng đều bị từ chối, nên thành viên không thể trỏ webhook hay
SMTP vào control plane, cơ sở dữ liệu hay dịch vụ metadata của cloud. Kênh do chủ
sở hữu tạo vẫn có thể giữ đích riêng như một dịch vụ chat tự lưu trữ trong mạng
LAN; địa chỉ metadata và các dải luôn bị chặn vẫn bị từ chối với mọi người. Kênh
được lưu trước quy tắc này không ghi lại quyết định: khi biết người tạo, Gotham
dùng vai trò hiện tại của tài khoản đó; kênh không xác định được người tạo sẽ bị
từ chối cho tới khi chủ sở hữu sửa lại đích của kênh.

## Phạm vi và sự kiện

Chọn phạm vi khi tạo hoặc sửa kênh:

- Kênh **Mọi tài nguyên** nhận mọi sự kiện đã chọn, kể cả sự kiện máy chủ, chứng chỉ và cập nhật trên
  toàn instance.
- Kênh ứng dụng nhận sự kiện triển khai của ứng dụng đó và sự kiện cần triển
  khai lại của ứng dụng đó.
- Kênh cơ sở dữ liệu nhận sự kiện sao lưu của cơ sở dữ liệu đó.

Các sự kiện đăng ký được là `deploy_success`, `deploy_failure`, `backup_success`,
`backup_failure`, `server_offline`, `server_online`, `certificate_expiring`,
`update_available`, `redeploy_needed`, `incident_opened`,
`incident_severity_changed` và `incident_recovered`. Kênh mới chọn sẵn các sự
kiện triển khai/sao lưu và ba sự kiện sự cố. Với cả kênh cũ và mới, hãy sửa kênh **Mọi tài nguyên** và
chọn thêm sự kiện máy chủ, chứng chỉ hoặc cập nhật nếu muốn nhận. Kênh ứng dụng và cơ sở dữ
liệu không nhận các sự kiện trên toàn instance này. `redeploy_needed` cũng phải
chọn thêm, và chỉ kênh **Mọi tài nguyên** hoặc kênh gắn với chính ứng dụng đó mới
nhận được. Kênh bị tắt không nhận gì.

Sự cố giám sát là tình trạng vẫn cần xử lý, không phải một sự kiện đã gửi. Gotham
giữ sự cố ở trạng thái chờ đến khi tình trạng kéo dài, rồi chuyển sang đang hoạt
động, và chỉ đánh dấu đã khắc phục sau khi có bằng chứng khỏe mạnh ổn định; khởi
động lại bảng điều khiển không xóa sự cố đang hoạt động. Mỗi lần xuất hiện chỉ mở
một lần, báo thay đổi mức độ một lần và khắc phục một lần. Xác nhận hoặc tạm hoãn
sự cố sẽ tắt việc gửi thông báo mà không đổi mức độ hay vòng đời, và bảng điều
khiển cho biết lý do đang giữ thông báo. Sự cố đã khắc phục được giữ trong lịch sử
30 ngày; sự cố đang hoạt động không bao giờ bị xóa. Kết nối máy chủ vẫn dùng sự
kiện `server_offline`/`server_online` hiện có: giao diện sự cố nhóm các vấn đề phụ
thuộc bên dưới máy chủ với độ phủ chưa xác định, không gửi thêm thông báo kết nối
và không suy đoán nguyên nhân.

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

## Cần chú ý trên bảng điều khiển

Bảng điều khiển mở đầu bằng **Cần chú ý**, trạng thái hiện tại của mọi tài nguyên
bạn có thể thấy. Khu vực này tách khỏi hộp thông báo: số chưa đọc là lịch sử sự
kiện, còn bảng này cho biết tình trạng nào vẫn đang hoạt động.

Sự cố đang hoạt động được sắp xếp nghiêm trọng trước, sau đó theo thời gian đã
hoạt động, nên tình trạng nghiêm trọng lâu nhất đứng đầu. Mỗi dòng nêu lý do dễ
hiểu, tài nguyên, thời điểm bắt đầu, quan sát hợp lệ gần nhất và độ phủ của bằng
chứng. Bộ lọc **Mức độ** và **Tài nguyên** cùng **Tải thêm** giữ kết quả trong
giới hạn. Thành viên chỉ thấy sự cố của dự án họ còn quyền truy cập; thu hồi
quyền sẽ xóa các dòng và số đếm tương ứng ngay lập tức.

Khi một máy chủ không phản hồi, các sự cố ứng dụng và cơ sở dữ liệu phụ thuộc
được nhóm bên dưới máy chủ với độ phủ chưa xác định. Việc nhóm cho biết tài
nguyên nào bị ảnh hưởng, không phải nguyên nhân đã được chứng minh: Gotham không
khẳng định máy chủ mất kết nối là nguyên nhân của các triệu chứng đó.

**Xác nhận** ghi lại người đang xử lý và tắt việc gửi thông báo; **Tạm ẩn** tắt
thông báo trong 1 giờ hoặc 24 giờ, và dòng sự cố hiển thị chính xác thời điểm hết
hạn. Cả hai thao tác không đổi mức độ hay vòng đời đã đo, và việc mở hộp thông
báo hoặc xem sự cố không bao giờ tự xác nhận. Sự cố đã xác nhận hoặc tạm ẩn vẫn
hiển thị kèm trạng thái, và dòng sự cố cho biết lý do đang giữ thông báo. **Mới
được khắc phục** là chế độ xem riêng, có giới hạn, cho các lần phục hồi.

Bảng theo kênh thời gian thực đã xác thực: sự cố mới hoặc đã khắc phục làm mới
bảng đang mở, kết nối lại sẽ tải lại, và tab đang hiển thị có dự phòng làm mới
mỗi 60 giây. Các thông điệp riêng phân biệt API gặp sự cố, chưa có máy chủ nào
báo cáo, giám sát không khả dụng trên instance, và hệ thống thực sự khỏe mạnh.
Liên kết mở chi tiết ứng dụng hoặc cơ sở dữ liệu (và nhật ký với lỗi runtime),
số liệu máy chủ, hoặc cài đặt thông báo. Bộ phát hiện và loại tài nguyên chưa
biết nhận nhãn trung tính; bằng chứng thô, lỗi, secret và đường dẫn máy chủ
không bao giờ được hiển thị cho chúng.

## Gửi thử, thử lại và lịch sử gửi

**Gửi thử** gửi một tin nhắn thử tổng hợp tới đúng kênh đó và báo kết quả ngay. Nó
được ghi vào lịch sử như một lần thử thật và cập nhật tình trạng kênh, nhưng không
tạo mục trong hộp thông báo.

**Lịch sử gửi** hiển thị 20 lần thử mới nhất của kênh, mới nhất trước: sự kiện (hoặc
**Tin nhắn thử**), trạng thái, thời điểm, số lần thử, thời điểm thử lại tiếp theo và
một tóm tắt lỗi có giới hạn. Các trạng thái là **Đang chờ**, **Đang gửi**, **Đang thử
lại**, **Đã gửi**, **Thất bại** và **Đã hủy**. Đã hủy nghĩa là công việc không còn cần
thiết (kênh bị tắt, xóa hoặc hủy đăng ký, hoặc một lần mở sự cố chưa từng gửi được);
trạng thái này không bao giờ hiện là đã gửi. Mỗi kênh giữ 50 lần thử chưa kết thúc
cuối. Các lần thất bại và đã hủy được giữ 30 ngày để còn bằng chứng, sau đó tự động
xóa.

Công việc gửi được ghi vào cơ sở dữ liệu trước khi đánh thức worker, nên khi control
plane khởi động lại hoặc gặp sự cố giữa lúc lưu và lúc gửi, công việc hợp lệ được
tiếp tục thay vì mất. Worker nhận một lô có giới hạn, và lease gửi bị bỏ dở được thu
hồi sau khi khởi động lại. Lỗi được thử lại với backoff lũy thừa và jitter: lỗi kết
nối, timeout, HTTP 429 và 5xx. `Retry-After` có giới hạn được tôn trọng. Mỗi lần gửi
dừng sau tối đa 8 lần thử hoặc 24 giờ. Cấu hình sai, lỗi xác thực và bị nhà cung cấp
từ chối là lỗi vĩnh viễn cho đến khi người vận hành xử lý. Một kênh lỗi không chặn
các kênh khác hay một lần triển khai/sao lưu.

Trước khi gửi, Gotham đọc lại trạng thái bật, đăng ký sự kiện, phạm vi và cấu hình đã
mã hóa của kênh. Công việc của kênh bị tắt, xóa hoặc hủy đăng ký sẽ bị hủy. Thứ tự
chuyển trạng thái của từng sự cố/kênh được giữ, nên một lần mở không bị gửi sau khi
đã phục hồi; nếu lần mở hết hạn hoặc bị hủy trước khi gửi thành công, thông báo phục
hồi cho kênh đó bị chặn và lý do được giữ trong lịch sử.

Cài đặt hiển thị tình trạng hiện tại của từng kênh: **Đang gửi được**, **Đang thử
lại**, **Không gửi được** hoặc **Chưa gửi lần nào**. Khi chủ sở hữu có lần gửi thất
bại vĩnh viễn, **Cần chú ý** liệt kê chúng kèm kênh, sự kiện và lỗi có giới hạn, và
có nút **Thử lại**. Một lần gửi thử thành công hoặc một lần gửi hợp lệ sau đó xóa tình
trạng lỗi hiện tại; lỗi lịch sử vẫn còn trong lịch sử. Nếu kênh đích duy nhất bị lỗi,
tình trạng trong bảng và mục Cần chú ý là phương án dự phòng; Gotham không bao giờ gửi
thông báo lỗi qua chính kênh đang lỗi. Thử lại dùng lại định danh cục bộ và cấu hình
hiện tại, nên không tạo mục hộp thông báo trùng.

Việc gửi không chặn gì và chạy trên control plane. Việc thử lại và xếp hàng cục bộ
được chống trùng cục bộ, nhưng không phải gửi đúng một lần ra bên ngoài: nhà cung cấp
có thể đã nhận tin nhắn dù phản hồi bị mất, và lần thử lại có thể gửi nó lần thứ hai.

Với kênh thuộc một tài nguyên, tạo, sửa, xóa, bật và thử cần quyền ghi vào dự án
chứa tài nguyên. Thành viên ngoài dự án đó không thấy kênh. Tình trạng kênh và lỗi
gửi vĩnh viễn chỉ dành cho chủ sở hữu.
