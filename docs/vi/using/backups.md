---
title: Bản sao lưu
---

# Bản sao lưu

Trang **Bản sao lưu** là nơi quản lý bảo vệ dữ liệu cho toàn sản phẩm. Trang
liệt kê mọi lần sao lưu và lịch chạy trên các tài nguyên bạn có quyền truy cập,
và đây là nơi duy nhất chủ sở hữu quản lý các nơi lưu trữ. Trang của cơ sở dữ
liệu vẫn có tab **Bản sao lưu** riêng cho cơ sở dữ liệu đó; mọi thứ áp dụng cho
tất cả tài nguyên nằm ở đây.

## Tab Bản sao lưu

Danh sách hiển thị một dòng cho mỗi lần chạy:

- **Tài nguyên** — ứng dụng, cơ sở dữ liệu hoặc dịch vụ, kèm liên kết tới trang.
- **Trạng thái** — đang chạy, hoàn tất hoặc thất bại, kèm **Đã xác thực** khi
  lần chạy đã ghi mã kiểm tra cho tệp (lần chạy cũ hiện **Chưa xác thực**).
- **Dung lượng** — kích thước bản sao, hoặc `đang tính` khi còn chạy.
- **Lưu trữ** — nơi lưu trữ đã nhận bản sao.
- **Bắt đầu** và **Thời lượng** — mốc thời gian của lần chạy.

Dùng bộ lọc loại và trạng thái để thu hẹp danh sách. **Sao lưu ngay** hỏi tài
nguyên và nơi lưu trữ, rồi xếp một lần chạy thủ công. **Khôi phục** trên lần
chạy đã hoàn tất sẽ xếp một lần khôi phục và hỏi xác nhận trước; **Xóa** gỡ bản
sao đã lưu cùng dòng của nó.

Bên dưới, **Lịch chạy** liệt kê mọi lần sao lưu tự động bằng ngôn ngữ thường,
tài nguyên và nơi lưu trữ, lần chạy trước và kế tiếp, cùng nút bật/tắt. **Lịch
mới** chọn tài nguyên, nơi lưu trữ và một trong các mẫu có sẵn.

Ứng dụng, dịch vụ và cơ sở dữ liệu dùng chung trang và danh sách lần chạy này.
Mỗi lần chạy thuộc về tài nguyên mà nó đã ghi và liên kết tới trang của tài
nguyên đó.

### Thời gian gián đoạn

Sao lưu cơ sở dữ liệu dừng cơ sở dữ liệu trong lúc dump, rồi khởi động lại nếu
trước đó đang chạy. Lưu trữ volume cũng làm vậy với ứng dụng hoặc dịch vụ. Lịch
trình có thể chọn **Không dừng tài nguyên**: không có thời gian chết, nhưng tệp
được ghi trong lúc lưu có thể không nhất quán và lịch sử sẽ ghi rõ. Với tài
nguyên bận rộn, hãy chọn khung giờ yên tĩnh.

### Khôi phục

Khôi phục ghi đè dữ liệu hiện tại của tài nguyên. Không thể hoàn tác, nên hãy
sao lưu mới trước nếu dữ liệu đang chạy vẫn còn quan trọng. Lịch sử khôi phục
nằm ở tab tài nguyên để một lần khôi phục thất bại vẫn hiện rõ nguyên nhân.

Trước khi khôi phục chạm vào tài nguyên, bảng điều khiển đọc lại tệp sao lưu từ
nơi lưu trữ và so mã SHA-256 với giá trị đã ghi khi tạo bản sao. Chỉ tệp khớp
mới được áp dụng. Sai lệch nghĩa là bản sao bị hỏng — tải lên bị cắt, quy tắc
vòng đời bucket hoặc đĩa hỏng — và quá trình khôi phục dừng với thông báo **bản
sao lưu này bị hỏng** trong khi tài nguyên vẫn chạy. Bản sao dùng để xác thực nằm
trong thư mục sao lưu cục bộ, được xóa trên mọi nhánh thoát, và vẫn áp dụng kiểm
tra dung lượng trống.

Bản sao lưu tạo trước khi có mã kiểm tra không có giá trị đã lưu. Chúng khôi
phục như trước và hộp thoại khôi phục ghi rõ rằng sẽ không xác thực; danh sách
lần chạy gắn nhãn **Chưa xác thực**. Hãy tạo bản sao mới nếu dữ liệu còn quan
trọng.

## Tab Lưu trữ

Tab **Lưu trữ** chỉ chủ sở hữu thấy. Đây là danh mục cấp chủ sở hữu: cấu hình
một lần và dùng cho mọi lịch chạy cùng lần chạy thủ công.

Mỗi bản cài có sẵn nơi lưu trữ **Đĩa cục bộ**, ghi vào thư mục sao lưu của máy
điều khiển. Không thể xóa, nên bản cài mới có thể sao lưu mà không cần cấu
hình.

Lưu trữ tương thích S3 hoạt động với AWS S3, Cloudflare R2, MinIO và các dịch
vụ tương tự. Một nơi lưu trữ gồm tên, endpoint, vùng, bucket, tiền tố tùy chọn,
access key và secret key. Khóa được niêm phong trên máy chủ và không bao giờ
hiện lại; để trống khi sửa sẽ giữ khóa đã lưu. **Kiểm tra** ghi, đọc lại và xóa
một tệp thử nhỏ, nên nơi lưu trữ chỉ đọc hoặc chặn xóa được phát hiện trước lần
sao lưu thật đầu tiên; với thư mục cục bộ cũng chứng minh đủ vòng ghi-đọc-xóa.
Kết quả hiện ngay tại chỗ.

Xóa nơi lưu trữ không xóa các bản sao cũ, nhưng không thể đọc lại từ đó, nên
khôi phục từ các lần chạy đó sẽ thất bại và các lần chạy vẫn hiện là **Nơi lưu
trữ đã bị xóa**. Không thể xóa nơi lưu trữ đang được một lịch sử dụng: hãy
chuyển hoặc xóa các lịch đó trước, để không lịch nào âm thầm quay về đĩa cục
bộ.

## Giữ lại và dọn dẹp

Mỗi lịch có mục **Giữ số bản sao lưu theo lịch gần nhất** (mặc định 7 với lịch
mới). Sau khi một lần chạy theo lịch thành công, các lần chạy đã hoàn tất cũ
hơn của lịch đó vượt quá giới hạn sẽ bị xóa, cũ nhất trước, kể cả tệp lưu.

Những gì không bao giờ bị xóa tự động:

- bản sao lưu thủ công — chỉ bạn mới xóa;
- lần chạy theo lịch hoàn tất mới nhất;
- lần chạy đang thực hiện;
- lần chạy thất bại chưa quá 30 ngày.

Lịch được tạo trước phiên bản này giữ mọi bản sao cho đến khi bạn đặt giới hạn;
khi đó danh sách lịch hiện **Giữ mọi bản sao lưu theo lịch**.

Nếu nơi lưu trữ không truy cập được trong lúc xóa một lần chạy, lần chạy đó ở
trạng thái **Đang chờ dọn dẹp** kèm lỗi gần nhất. Sao lưu mới vẫn chạy bình
thường. Tiến trình dọn dẹp hàng giờ sẽ thử lại; **Thử dọn lại** thử ngay còn
**Bỏ qua** quên dòng đó mà không xóa tệp.

Khi một ứng dụng, dịch vụ hoặc cơ sở dữ liệu bị xóa vĩnh viễn, các tệp sao lưu
của nó được xếp vào hàng đợi xóa trước khi các dòng biến mất. Cơ sở dữ liệu vẫn
giữ thời gian ân hạn xóa mềm trước, nên không có gì bị xóa sớm. Hàng đợi sống
sót qua sự cố nơi lưu trữ và được dọn bởi cùng tiến trình.

Tab Lưu trữ hiện, theo từng nơi lưu trữ, số bản sao và tổng dung lượng, và với
Đĩa cục bộ là dung lượng trống. Trước khi chạy vào Đĩa cục bộ, bảng điều khiển
kiểm tra dung lượng trống đủ cho lần chạy trước của tài nguyên đó cộng 20% (hoặc
64 MiB khi chưa có lần chạy trước) và từ chối chạy với thông báo dễ hiểu thay vì
làm đầy đĩa.

## Thành viên

Thành viên chỉ thấy các lần chạy và lịch của tài nguyên trong dự án họ tham gia.
Tên nơi lưu trữ hiện trong danh sách, nhưng không thể chọn, đọc hay sửa. Quản
lý lưu trữ chỉ dành cho chủ sở hữu.
