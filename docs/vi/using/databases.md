---
title: Cơ sở dữ liệu
---

# Cơ sở dữ liệu

Một cơ sở dữ liệu là một container chạy một engine duy nhất trên một node, với
volume có tên riêng. Gotham tạo thông tin đăng nhập lúc tạo, lưu chúng ở dạng mã
hóa, và giữ chúng sẵn trên trang cơ sở dữ liệu cho thành viên của dự án.

Cơ sở dữ liệu nằm trong một môi trường của dự án, bên cạnh ứng dụng và dịch vụ. Xem
[Dự án và môi trường](/vi/using/projects-environments) để biết cách tài nguyên được
nhóm lại, và [Dịch vụ và template](/vi/using/services-templates#chon-loai-can-tao)
để biết khi nào nên dùng cơ sở dữ liệu được quản lý thay vì cơ sở dữ liệu đi kèm
trong một stack compose.

## Engine

Có năm engine được hỗ trợ. Biểu mẫu tạo đưa ra các thẻ image đã được kiểm chứng cho
từng engine và quay về mặc định bên dưới khi không chọn thẻ nào.

| Engine | Image mặc định | Cổng container |
| --- | --- | --- |
| PostgreSQL | `postgres:16-alpine` | 5432 |
| MySQL | `mysql:8.4` | 3306 |
| MariaDB | `mariadb:11.4` | 3306 |
| MongoDB | `mongo:7.0` | 27017 |
| Redis | `redis:7.2-alpine` | 6379 |

Mọi engine đều dùng image chính thức của nó. Gotham thêm biến môi trường khởi tạo
của engine, volume dữ liệu và một healthcheck kiểm tra sẵn sàng.

## Tạo cơ sở dữ liệu

Dùng **Thêm tài nguyên → Cơ sở dữ liệu** và làm theo ba bước: engine, cấu hình, xem
lại.

- **Engine và phiên bản** — chọn một trong năm engine, rồi chọn thẻ image.
- **Tên** — 1–63 ký tự: chữ cái, chữ số, `.`, `_` hoặc `-`. Tên này được dùng cho
  container và thông tin đăng nhập được tạo.
- **Node** — node mà cơ sở dữ liệu chạy trên đó. Nó được ghim lúc tạo: container và
  volume nằm ở đó, và cơ sở dữ liệu không thể chuyển sang node khác sau đó.
- **Cổng public** — tùy chọn và mặc định tắt. Cổng public công bố cổng của engine
  trên node để các client bên ngoài kết nối được. Ràng buộc được cố định lúc tạo và
  không thể đổi về sau, nên hãy để tắt trừ khi một client bên ngoài cần đến.

Thông tin đăng nhập (người dùng, mật khẩu, tên cơ sở dữ liệu) được tạo phía máy
chủ; bạn không chọn chúng. MySQL và MariaDB còn có thêm một mật khẩu root riêng.
Chúng hiển thị dưới mục **Thông tin đăng nhập** trên trang cơ sở dữ liệu.

Quá trình tạo trả về ngay khi dòng cơ sở dữ liệu và thông tin đăng nhập đã có;
wizard đóng lại và mở trang cơ sở dữ liệu, nơi hiển thị trạng thái **Đang tạo**
trong lúc image được tải về và container trở nên khỏe mạnh. Trên một node chưa có
image, việc này có thể mất vài phút. Trang tự polling API và chuyển sang **Đang
chạy** khi healthcheck đạt, hoặc sang **Lỗi** kèm lý do ngắn nếu cấp phát thất
bại. Hợp đồng API là `POST /api/v1/databases`, trả `202 Accepted` với cơ sở dữ
liệu ở trạng thái `creating` kèm thông tin đăng nhập; hãy polling
`GET /api/v1/databases/{id}` cho đến khi `status` là `running` hoặc `error`.

Mỗi cơ sở dữ liệu nhận một volume có tên (`gotham-db-<id>`) được gắn tại thư mục dữ
liệu của engine. Volume độc lập với container: gỡ container không gỡ dữ liệu.

## Vòng đời

Khởi động, dừng và khởi động lại có sẵn trên trang cơ sở dữ liệu.

- **Dừng** dừng container; volume và dữ liệu của nó không bị đụng tới.
- **Khởi động** khởi động lại container đang có.
- **Khởi động lại** khởi động lại nó tại chỗ.
- **Xóa** gỡ container khỏi node nhưng giữ volume trong bảy ngày trước khi tiến
  trình dọn theo thời gian lưu giữ xóa nó vĩnh viễn. Vì vậy xóa không phá hủy ngay
  lập tức, nhưng cũng không phải một bản sao lưu: hãy sao lưu trước nếu dữ liệu
  quan trọng.

## Thông tin đăng nhập và chuỗi kết nối

Thẻ **Thông tin đăng nhập** hiển thị người dùng, mật khẩu và tên cơ sở dữ liệu được
tạo, với các thao tác hiện/sao chép, cùng một chuỗi kết nối dùng scheme URL của
engine (`postgresql://`, `mysql://`, `mariadb://`, `mongodb://`, `redis://`). Các
giá trị được lưu mã hóa và đọc được bởi thành viên có quyền truy cập dự án;
chúng không nằm trong phản hồi danh sách cơ sở dữ liệu.

Chuỗi kết nối phụ thuộc vào cách cơ sở dữ liệu được phơi ra:

- Có **cổng public** thì nó trỏ tới địa chỉ node và cổng được công bố.
- Không có thì nó trỏ tới tên container của cơ sở dữ liệu và cổng nội bộ của engine,
  chỉ có ý nghĩa trên mạng container của node.

## Sao lưu

Mở tab **Sao lưu** của một cơ sở dữ liệu để quản lý các lần chạy và lịch. Nhà
cung cấp lưu trữ được quản lý một lần cho toàn sản phẩm trong
**Bản sao lưu → Lưu trữ**; xem [Bản sao lưu](/vi/using/backups). Một bản sao lưu là một lần kết xuất dữ liệu (dump) của engine do một
container tạm thời thực hiện trên cùng node; artifact được nén và lưu vào đích đã
chọn. Các lần chạy được liệt kê kèm loại (`manual` hoặc `scheduled`), trạng thái,
kích thước, đích đến và dấu thời gian. Một lần chạy thất bại vẫn giữ dòng và văn
bản lỗi của nó, nên bạn thấy được chỗ sai trước khi chạy lại.

Sao lưu là một tính năng riêng: cùng một lõi phục vụ cơ sở dữ liệu hôm nay và sẽ
phục vụ lưu trữ của ứng dụng cùng volume của service, và trang **Sao lưu** liệt kê
mọi lần chạy cùng lịch ở một chỗ. Bên cạnh đó, `GET /v1/backups`, `POST /v1/backups`,
`GET /v1/backups/{id}`, `POST /v1/backups/{id}/restore`,
`GET/POST /v1/backups/schedules`, `PATCH/DELETE /v1/backups/schedules/{id}`,
`GET /v1/backups/restores` và `GET/POST /v1/backups/providers` là API độc lập với
loại tài nguyên. Các tuyến dạng bộ sưu tập nhận `limit` và `offset`, sắp xếp mới
nhất trước, và quyền truy cập được áp dụng trước khi phân trang, nên trang của
thành viên chứa đầy các lần chạy họ thực sự với tới được. Hiện chỉ `database` được chấp nhận làm loại tài nguyên; các loại
khác trả `400` cho tới khi driver của chúng có mặt.

Các tuyến gắn với cơ sở dữ liệu được nêu trên trang này
(`/v1/databases/{id}/backups`, `.../schedules`, `.../restore` và
`/v1/databases/backup-targets`) là **bí danh đã lỗi thời** của lõi đó. Chúng giữ
nguyên hình dạng yêu cầu và phản hồi, nên token và MCP client hiện có vẫn hoạt
động, và luôn mô tả cùng các dòng công việc, cùng cách bố trí artifact như các
tuyến mới.

### Sao lưu ngay

**Sao lưu ngay** xếp hàng một lần chạy thủ công. Việc kết xuất dữ liệu dừng
container của cơ sở dữ liệu trong lúc container tạm thời đọc volume, nên cơ sở dữ
liệu tạm thời không khả dụng; nó được khởi động lại sau đó nếu trước đó đang chạy.
Điều tương tự cũng đúng với các lần chạy theo lịch. Chọn một nhà cung cấp cho mỗi
lần chạy, hoặc để mặc định.

### Khôi phục

**Khôi phục** trên một bản sao lưu đã hoàn tất sẽ xếp hàng một lần khôi phục. Cơ sở
dữ liệu bị dừng trong lúc artifact được dàn dựng và áp dụng, và việc khôi phục ghi
đè dữ liệu hiện tại của cơ sở dữ liệu — không thể hoàn tác, nên hãy sao lưu trước
nếu dữ liệu đang sống vẫn còn quan trọng. **Lịch sử khôi phục** giữ kết quả bền
vững của mọi lần khôi phục đã xếp hàng, kể cả các lần thất bại. Một lần khôi phục
thất bại để cơ sở dữ liệu ở trạng thái dừng với nguyên nhân hiển thị trên trang; nó
không bao giờ khởi động lại với dữ liệu khôi phục dở dang.

### Lịch tự động

Một lịch là biểu thức cron năm trường trên control plane: phút, giờ, ngày trong
tháng, tháng, ngày trong tuần. Các trường chấp nhận `*`, giá trị đơn, khoảng,
bước (`*/5`, `1-10/2`) và danh sách phân tách bằng dấu phẩy của những dạng đó. Lần
chạy tới được tính khi bạn lưu và hiển thị trong danh sách. Mỗi lịch có thể bật
hoặc tạm dừng và mang nhà cung cấp riêng; không có thì các lần chạy rơi vào nhà
cung cấp **Đĩa cục bộ** có sẵn. Xóa một lịch không đụng tới các bản sao lưu trước
đó.

### Nhà cung cấp lưu trữ

Nhà cung cấp là một danh mục ở cấp chủ sở hữu: cấu hình một lần và dùng cho mọi cơ
sở dữ liệu. Mỗi bản cài có sẵn nhà cung cấp **Đĩa cục bộ** ghi vào thư mục sao lưu
của control plane. Nó luôn được liệt kê, không thể đổi sang loại khác và không thể
xóa, nên bản cài mới sao lưu được mà không cần thiết lập gì và artifact không bao
giờ rơi vào một đích không tên.

Lưu trữ tương thích S3 được hỗ trợ với AWS S3, Cloudflare R2, MinIO và các dịch vụ
tương tự: cấu hình endpoint, region, bucket, tiền tố khóa tùy chọn, access key và
secret key. Các nhà cung cấp được cấu hình trong **Sao lưu → Nhà cung cấp lưu trữ**,
một lần cho tài khoản chủ sở hữu chứ không phải cho mỗi cơ sở dữ liệu. Chỉ chủ sở
hữu cấu hình chúng; thành viên có thể chạy, liệt kê và khôi phục bản sao lưu của
cơ sở dữ liệu thuộc dự án mình tham gia, nhưng không thể chọn hay đọc nhà cung cấp.
Các khóa được niêm phong trên máy chủ và không bao giờ hiển thị lại, nên biểu mẫu
chỉnh sửa giữ nguyên các khóa đã lưu khi các trường khóa để trống. **Kiểm tra**
kiểm tra endpoint và thông tin đăng nhập với bucket, hoặc khả năng ghi của thư mục
cục bộ, rồi báo kết quả.

Xóa một nhà cung cấp không xóa các bản sao lưu trước đó, nhưng chúng không còn đọc
lại được từ nhà cung cấp đó, nên việc khôi phục từ chúng sẽ thất bại. Thư mục sao
lưu cục bộ là một cài đặt control plane trên máy chủ.

> [!TIP]
> Sao lưu sẽ dừng cơ sở dữ liệu. Với một cơ sở dữ liệu đang bận rộn, hãy lên lịch
> kết xuất dữ liệu vào khung giờ ít tải và thông báo cho người dùng về khoảng gián
> đoạn ngắn đó.

## Lịch sử tài nguyên

Tab **Số liệu** vẽ CPU, bộ nhớ, mạng và I/O đĩa cho container cơ sở dữ liệu trong
các khoảng 1 giờ, 24 giờ, 7 ngày và 30 ngày. Nó dùng cùng mẫu agent, đơn vị và thời
gian lưu giữ như lịch sử ứng dụng: CPU theo lõi (`1.0` = một lõi đầy đủ), bộ nhớ là
working set sau khi trừ bộ nhớ đệm trang có thể thu hồi, và tốc độ byte mỗi giây từ
chênh lệch bộ đếm theo thời gian thực tế. Mẫu đầu tiên, bộ đếm đặt lại hoặc khoảng
thời gian không dương là chỗ trống, không phải số 0. Khi không có giới hạn bộ nhớ,
biểu đồ hiển thị byte và **không giới hạn**, không bao giờ là phần trăm tự đặt ra.
Nhãn **một phần** cho thấy thiếu bao phủ bản sao, và mốc triển khai phân tách các thế
hệ container để một lần khởi động lại hoặc thay thế không trộn với container trước.
Mẫu thô giữ 24 giờ, tổng hợp 1 phút giữ 7 ngày và tổng hợp 1 giờ giữ 30 ngày.

API là `GET /api/v1/databases/{id}/metrics?range=1h|24h|7d|30d&service=`. Agent cũ
báo chưa có lịch sử thay vì một biểu đồ trống trông khỏe mạnh. Thành viên dự án chỉ
thấy cơ sở dữ liệu trong dự án được cấp.
