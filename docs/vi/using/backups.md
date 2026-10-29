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
- **Trạng thái** — đang chạy, hoàn tất hoặc thất bại.
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

## Tab Lưu trữ

Tab **Lưu trữ** chỉ chủ sở hữu thấy. Đây là danh mục cấp chủ sở hữu: cấu hình
một lần và dùng cho mọi lịch chạy cùng lần chạy thủ công.

Mỗi bản cài có sẵn nơi lưu trữ **Đĩa cục bộ**, ghi vào thư mục sao lưu của máy
điều khiển. Không thể xóa, nên bản cài mới có thể sao lưu mà không cần cấu
hình.

Lưu trữ tương thích S3 hoạt động với AWS S3, Cloudflare R2, MinIO và các dịch
vụ tương tự. Một nơi lưu trữ gồm tên, endpoint, vùng, bucket, tiền tố tùy chọn,
access key và secret key. Khóa được niêm phong trên máy chủ và không bao giờ
hiện lại; để trống khi sửa sẽ giữ khóa đã lưu. **Kiểm tra** thử bucket và thông
tin đăng nhập, hoặc quyền ghi thư mục cục bộ, rồi hiện kết quả ngay tại chỗ.

Xóa nơi lưu trữ không xóa các bản sao cũ, nhưng không thể đọc lại từ đó, nên
khôi phục từ các lần chạy đó sẽ thất bại.

## Thành viên

Thành viên chỉ thấy các lần chạy và lịch của tài nguyên trong dự án họ tham gia.
Tên nơi lưu trữ hiện trong danh sách, nhưng không thể chọn, đọc hay sửa. Quản
lý lưu trữ chỉ dành cho chủ sở hữu.
