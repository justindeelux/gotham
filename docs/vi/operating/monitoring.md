---
title: Giám sát áp lực máy chủ
---

# Giám sát áp lực máy chủ

Gotham biến các quan sát agent của nút vẫn gửi lên thành sự cố CPU, bộ nhớ và hệ
thống tệp. Gotham không gọi API của nút và không lưu lịch sử mẫu để cảnh báo:
mỗi quan sát được chấp nhận được đánh giá một lần.

Sự cố hiện trong hộp thư thông báo mà không cần cấu hình kênh bên ngoài. Gửi ra
ngoài vẫn cần kênh thông báo đã đăng ký sự kiện sự cố (`incident_opened`,
`incident_severity_changed`, `incident_recovered`).

## Mặc định

| Quy tắc | Cảnh báo | Nghiêm trọng | Xóa | Thời lượng |
|---|---|---|---|---|
| CPU | ≥ 90% | — | ≤ 85% | cảnh báo 5 phút, xóa 1 phút |
| Bộ nhớ | ≥ 90% | — | ≤ 85% | cảnh báo 5 phút, xóa 1 phút |
| Dung lượng hệ thống tệp | ≥ 90% | ≥ 95% | ≤ 85% | cảnh báo 1 phút, nghiêm trọng 1 phút, hạ mức 1 phút, xóa 1 phút |
| Inode hệ thống tệp | ≥ 90% | ≥ 95% | ≤ 85% | cảnh báo 1 phút, nghiêm trọng 1 phút, hạ mức 1 phút, xóa 1 phút |

Một đợt tăng ngắn hơn thời lượng yêu cầu sẽ không gây cảnh báo. Áp lực hệ thống
tệp nghiêm trọng hạ xuống cảnh báo khi ≤ 90% trong một phút, nên sự cố vẫn mở
khi đĩa còn trên ngưỡng xóa. Bộ nhớ là bộ nhớ đã dùng theo phần trăm tổng bộ nhớ;
Gotham không suy ra OOM từ phần trăm này.

Cảnh báo và nghiêm trọng của hệ thống tệp tính từ byte đã dùng: `tổng - còn
trống`. Áp lực inode tính từ inode đã dùng. Hệ thống tệp không đọc được bộ đếm
dung lượng hoặc inode là **chưa rõ**, không phải 0, và bằng chứng chưa rõ không
bao giờ được tính là phục hồi.

## Hệ thống tệp được theo dõi

Agent báo mọi hệ thống tệp ứng viên là thiết bị riêng:

- **gốc** — `/`, để không giả định phân vùng control-plane khỏe là đủ cho dữ liệu.
- **dữ liệu Docker** — `data-root` của engine, thường là `/var/lib/docker`.
- **lưu trữ được quản lý** — thư mục gốc volume được quản lý khi có cấu hình.
- **bản sao lưu** — thư mục gốc sao lưu khi có cấu hình.

Các đường dẫn trên cùng thiết bị được gộp, ưu tiên ứng viên đầu tiên (gốc,
Docker, lưu trữ được quản lý, sao lưu), nên một bind mount qua hai đường dẫn chỉ
tạo một sự cố. Vì vậy một ổ Docker hoặc sao lưu đầy chỉ mở một sự cố dù phân
vùng gốc khỏe. Ổ gắn biến mất được đóng với lý do `filesystem_disappeared`; đó
không phải phục hồi và không gửi thông báo phục hồi.

Không bao gồm: SMART/nhiệt độ, quét kích thước thư mục, dự báo dung lượng và dọn
dẹp tự động. Gotham không bao giờ xóa dữ liệu hay khởi động lại khối lượng công
việc để giảm áp lực.

## Lịch sử

Dung lượng hệ thống tệp được lưu theo nút và danh tính hệ thống tệp ổn định trong
các nhóm theo giờ, gồm số mẫu, nhỏ nhất, lớn nhất và tỉ lệ chất lượng
mới/một phần/chưa rõ. Khi dung lượng báo cáo thay đổi (thay ổ hoặc đổi kích
thước), phân đoạn tăng lên để dự báo sau này không vượt qua điểm gián đoạn. Lịch
sử được giữ **30 ngày** và được lượt dọn giám sát xóa; các dòng sự cố đang hoạt
động thuộc vòng đời sự cố và không bị lượt dọn này xóa.

## Cấu hình quy tắc trên giao diện

Mở **Giám sát** ở thanh bên (chỉ chủ sở hữu). Chọn **Mặc định của instance** để
sửa quy tắc mọi nút kế thừa, hoặc chọn một nút để lưu thiết lập riêng. Sửa phần
trăm cảnh báo, nghiêm trọng, hạ mức, xóa và số giây yêu cầu, rồi **Lưu quy tắc**.
**Khôi phục mặc định** xóa chính sách đã lưu của phạm vi đang chọn: quy tắc
instance quay về mặc định có sẵn, còn nút quay về quy tắc instance.

Quy tắc được kiểm tra trong biểu mẫu và một lần nữa ở API: phần trăm phải hữu hạn
và đúng thứ tự (`xóa < hạ mức ≤ cảnh báo < nghiêm trọng`), thời lượng từ 10 đến
3600 giây, và chính sách theo nút cần nút đã tồn tại. Thay đổi áp dụng cho quan
sát tiếp theo; lịch sử không được đánh giá lại và không tạo ra phục hồi giả.

## Trang chi tiết nút hiển thị gì

Thẻ **Tổng quan** của nút hiển thị sự cố áp lực đang hoạt động, quan sát gần nhất
và độ mới của dữ liệu (mới, cũ hoặc chưa rõ). Thẻ liệt kê điểm gắn, mục đích,
dung lượng trống/tổng và inode trống/tổng của từng hệ thống tệp, kèm liên kết tới
tab **Chỉ số**. Gợi ý xử lý là kiểm tra lưu trữ hoặc nhật ký, hay giảm tải.
