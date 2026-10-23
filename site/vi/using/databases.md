---
title: Cơ sở dữ liệu
---

# Cơ sở dữ liệu

Một cơ sở dữ liệu là một container chạy một engine duy nhất trên một node, với
volume có tên riêng. Gotham tạo thông tin đăng nhập lúc tạo, lưu chúng ở dạng mã
hóa, và giữ chúng sẵn trên trang cơ sở dữ liệu cho nhóm của dự án.

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
Màn hình thành công hiển thị chúng, và chúng vẫn có sẵn dưới mục
**Thông tin đăng nhập** trên trang cơ sở dữ liệu.

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
giá trị được lưu mã hóa và đọc được bởi thành viên nhóm có quyền ghi vào dự án;
chúng không nằm trong phản hồi danh sách cơ sở dữ liệu.

Chuỗi kết nối phụ thuộc vào cách cơ sở dữ liệu được phơi ra:

- Có **cổng public** thì nó trỏ tới địa chỉ node và cổng được công bố.
- Không có thì nó trỏ tới tên container của cơ sở dữ liệu và cổng nội bộ của engine,
  chỉ có ý nghĩa trên mạng container của node.

## Sao lưu

Mở tab **Sao lưu** của một cơ sở dữ liệu để quản lý các lần chạy, lịch và đích lưu
trữ. Một bản sao lưu là một lần kết xuất dữ liệu (dump) của engine do một container
tạm thời thực hiện trên cùng node; artifact được nén và lưu vào đích đã chọn. Các
lần chạy được liệt kê kèm loại (`manual` hoặc `scheduled`), trạng thái, kích thước,
đích đến và dấu thời gian. Một lần chạy thất bại vẫn giữ dòng và văn bản lỗi của
nó, nên bạn thấy được chỗ sai trước khi chạy lại.

### Sao lưu ngay

**Sao lưu ngay** xếp hàng một lần chạy thủ công. Việc kết xuất dữ liệu dừng
container của cơ sở dữ liệu trong lúc container tạm thời đọc volume, nên cơ sở dữ
liệu tạm thời không khả dụng; nó được khởi động lại sau đó nếu trước đó đang chạy.
Điều tương tự cũng đúng với các lần chạy theo lịch. Chọn một đích cho mỗi lần chạy,
hoặc để mặc định.

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
hoặc tạm dừng và mang đích riêng; không có đích thì các lần chạy rơi vào kho cục bộ
mặc định. Xóa một lịch không đụng tới các bản sao lưu trước đó.

### Đích lưu trữ

Khi không cấu hình đích nào, bản sao lưu đi vào đĩa của control plane. Lưu trữ
tương thích S3 được hỗ trợ với AWS S3, Cloudflare R2, MinIO và các dịch vụ tương
tự: cấu hình endpoint, region, bucket, tiền tố khóa tùy chọn, access key và secret
key.

Các đích được cấu hình trong **Sao lưu → Đích sao lưu**, một lần cho mỗi tài khoản
chứ không phải cho mỗi cơ sở dữ liệu — bất kỳ thành viên nào có quyền triển khai
đều thêm được. Các khóa được niêm phong trên máy chủ và không bao giờ hiển thị lại,
nên biểu mẫu chỉnh sửa giữ nguyên các khóa đã lưu khi các trường khóa để trống.
**Kiểm tra** kiểm tra endpoint và thông tin đăng nhập với bucket rồi báo kết quả.

Xóa một đích không xóa các bản sao lưu trước đó, nhưng chúng không còn đọc lại được
từ đích đó, nên việc khôi phục từ chúng sẽ thất bại. Thư mục đích cục bộ do người
vận hành control plane đặt.

> [!TIP]
> Sao lưu sẽ dừng cơ sở dữ liệu. Với một cơ sở dữ liệu đang bận rộn, hãy lên lịch
> kết xuất dữ liệu vào khung giờ ít tải và thông báo cho người dùng về khoảng gián
> đoạn ngắn đó.
