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
vùng gốc khỏe. Hệ thống tệp vắng mặt trong một báo cáo được chấp nhận sẽ được
báo là chưa rõ cho quy tắc dung lượng và inode, nên sự cố đang hoạt động vẫn
hiển thị và không bao giờ tự phục hồi cho đến khi có bằng chứng mới.

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

Bảng **Cần chú ý** trên bảng điều khiển tổng hợp các sự cố này trên những tài
nguyên người dùng có thể thấy, kèm xác nhận, tạm ẩn, bộ lọc và trạng thái phục
hồi. Xem
[Thông báo](/vi/using/notifications#can-chu-y-tren-bang-dieu-khien) để biết cách
hoạt động và quy tắc truy cập của bảng.

## Sức khỏe runtime của ứng dụng và cơ sở dữ liệu

Áp lực máy chủ chỉ là một nửa bức tranh: một nút có thể còn CPU và đĩa trong khi
ứng dụng hoặc cơ sở dữ liệu được quản lý đã dừng, lỗi healthcheck hoặc đang khởi
động lại. Cùng các quan sát được chấp nhận cũng nuôi sức khỏe runtime cho ứng
dụng và cơ sở dữ liệu. Đánh giá này độc lập với trạng thái triển khai, tiếp tục
chạy khi không mở trình duyệt và không thêm lịch thăm dò Docker trên control
plane.

Container đang chạy không có healthcheck là **chưa rõ**, không phải khỏe mạnh.
Cần kiểm kê đầy đủ để chứng minh container vắng mặt; báo cáo một phần, agent cũ
hoặc nút offline hiển thị độ phủ chưa rõ thay vì báo mọi container đã mất.
Replica Compose được theo dõi theo dịch vụ và số container, nên một replica cùng
dịch vụ đang khỏe không thể che giấu replica lỗi. Người dùng dừng, triển khai
đang chạy và thời gian chờ khởi động 30–600 giây sẽ che các chuyển đổi dự kiến;
vòng lặp khởi động lại cần ba lần tăng trong năm phút và vẫn mở đến năm phút sau
lần khởi động lại mới nhất. OOM chỉ được báo từ cờ `OOMKilled` tường minh của
Docker trên một lần lỗi mới.

Thẻ runtime ở chi tiết ứng dụng và cơ sở dữ liệu hiển thị trạng thái, replica bị
ảnh hưởng, thời điểm quan sát, bằng chứng khởi động lại/OOM gần nhất và liên kết
tới nhật ký, triển khai. Thông báo runtime là hai sự kiện riêng
`runtime_failure` và `runtime_recovery`; đăng ký kênh hiện có không bị mở rộng.
Xem [Sức khỏe runtime](/vi/using/runtime-health) để biết đầy đủ ngữ nghĩa và
giới hạn.

## Kiểm tra khả năng truy cập website

Tên miền của ứng dụng cũng được kiểm tra từ control plane. Mỗi tên miền được
quản lý và đang bật nhận một yêu cầu HTTP(S) mỗi 60 giây (có jitter) với thời
gian chờ 5 giây, độc lập với trình duyệt đang mở. Mặc định dùng `GET /` và chấp
nhận mã trạng thái 200 đến 399. Chỉ được coi là khỏe khi phản hồi cuối cùng nằm
trong khoảng mong đợi — chỉ kết nối TCP là chưa đủ.

Kiểm tra chỉ gắn với tên miền ứng dụng được quản lý. Yêu cầu luôn dùng giao thức,
tên miền và đường dẫn đã cấu hình; Gotham không nhận host bên ngoài, cổng tùy
chọn, thông tin đăng nhập, header tùy chỉnh hay quy tắc regex trên body. Chuyển
hướng được theo tối đa ba lần và chỉ trong cùng host; chuyển hướng sang host
khác làm lần thử thất bại, còn đích phân giải về loopback, mạng riêng, link-local
hoặc metadata bị chặn. Với triển khai cục bộ, đích bị chặn được báo là độ phủ
**không hỗ trợ**, không gợi ý bỏ qua lớp bảo vệ.

### Xác nhận và phục hồi

Ba lần thử thất bại liên tiếp đã hoàn tất sẽ mở một sự cố. Hai lần thử thành
công liên tiếp sẽ phục hồi. Lỗi bộ thực thi hoặc cơ sở dữ liệu, thời gian tắt,
thay đổi cấu hình và thời gian control plane ngừng hoạt động là khoảng **chưa
rõ**: không bao giờ tính là lỗi của đích và cũng không tính là thời gian khỏe.
Ứng dụng đã dừng và ứng dụng chưa triển khai lần nào không bị thử, nên việc dừng
có chủ đích hoặc ứng dụng trống không thể mở sự cố website.
Tên miền mới, thay đổi cấu hình và bản phát hành vừa hoàn tất chờ 120 giây
trước lần thử theo lịch tiếp theo, nên việc triển khai hoặc chỉnh sửa không gây
cảnh báo lúc khởi động.

Lỗi vận chuyển, DNS và TLS, mã trạng thái phản hồi và độ trễ phản hồi thành công
được lưu thành bằng chứng riêng. Lỗi xác thực chứng chỉ trên yêu cầu thật là bằng
chứng khả năng truy cập; cảnh báo hết hạn chứng chỉ vẫn dùng quan sát chứng chỉ
hiện có và không bị nhân đôi.

### Lịch sử và độ phủ

Kết quả phân loại và khoảng quan sát hợp lệ được giữ **30 ngày**. Trang chi tiết
tên miền trả lịch sử 24 giờ và 7 ngày, độ trễ đạt được và khả dụng quan sát được
bên cạnh độ phủ. Thời gian chưa rõ không nằm trong mẫu số uptime, lần thử lỗi
không bị vẽ thành độ trễ 0, và dưới 90% độ phủ giao diện hiển thị thiếu độ phủ
thay vì một phần trăm uptime riêng lẻ. Khoảng đọc và số điểm biểu đồ đều bị giới
hạn.

### Cấu hình kiểm tra

Mở tab **Domains** của ứng dụng và dùng thẻ **Kiểm tra website**:

- **Bật/tắt** tắt việc thử theo lịch cho tên miền đó. Tắt cũng đóng sự cố website
  đang mở.
- **Đường dẫn** là đường dẫn tương đối như `/` hoặc `/health`; query và fragment
  bị từ chối.
- **Mã trạng thái mong đợi** là khoảng bao gồm hai đầu, mặc định 200–399.
- **Thời gian chờ** từ 1 đến 30 giây.
- **Thử kiểm tra** chạy một lần ngay và không ghi lịch sử theo lịch cũng không mở
  sự cố.

Site cần đăng nhập thường trả 401 hoặc 403. Hãy chỉnh khoảng trạng thái mong đợi
cho các site đó thay vì thêm thông tin đăng nhập.

Kiểm tra chạy từ điểm nhìn của control plane, nên nó chứng minh điều người dùng
truy cập control plane đó thấy. Đây không phải SLA toàn Internet và không bao gồm
nhiều vùng, đội thử bên ngoài, giám sát TCP/UDP tùy ý hay trang trạng thái công
khai.
