---
title: Sức khỏe runtime
---

# Sức khỏe runtime

Gotham theo dõi ứng dụng và cơ sở dữ liệu được quản lý sau khi triển khai mà
không cần mở bảng điều khiển. Một lần triển khai thành công vẫn là kết quả của
pipeline; việc tài nguyên có khỏe **ngay bây giờ** hay không là một đánh giá
runtime riêng. Node agent đẩy bằng chứng container khoảng 15 giây một lần và
control plane đánh giá bản kiểm kê mới nhất được chấp nhận. Không cần mở trình
duyệt và control plane không thăm dò Docker theo lịch.

Mở một ứng dụng hoặc cơ sở dữ liệu để xem thẻ runtime, hoặc xem nhãn runtime
cạnh trạng thái trong danh sách tài nguyên của môi trường. Thẻ hiển thị trạng
thái, thời điểm đo, độ phủ, dịch vụ/replica bị ảnh hưởng, lý do sự cố và bằng
chứng khởi động lại/OOM gần nhất. Thẻ của ứng dụng liên kết tới các tab nhật ký
và triển khai hiện có.

## Trạng thái và độ phủ

| Trạng thái | Ý nghĩa |
| --- | --- |
| khỏe mạnh (healthy) | Mọi replica dự kiến đang chạy và mọi replica có healthcheck gốc đều báo khỏe mạnh. |
| đang khởi động (starting) | Container đang được tạo/khởi động, hoặc tài nguyên đang trong thời gian chờ khởi động sau khi start/deploy. |
| suy giảm (degraded) | Replica đang chạy nhưng healthcheck lỗi, kẹt khởi động lại, hoặc vừa bị OOM kill. |
| không khả dụng (unavailable) | Replica dự kiến bị thiếu hoặc dừng ngoài dự kiến, hoặc container bị tạm dừng. |
| dừng có chủ đích | Chủ sở hữu đã dừng tài nguyên, hoặc trạng thái cơ sở dữ liệu là stopped. Đây không phải lỗi. |
| không rõ (unknown) | Chưa có bằng chứng hiện tại: chưa quan sát, bằng chứng cũ, kiểm kê chưa đầy đủ, agent cũ, hoặc container chạy không có healthcheck. |
| đã tắt (disabled) | Giám sát liên tục đang tắt cho tài nguyên này. |

Container đang chạy **không có healthcheck là không rõ, không phải khỏe mạnh**.
Gotham chỉ kết luận khỏe mạnh khi healthcheck đã khai báo xác nhận. Độ phủ không
đầy đủ được hiển thị rõ ràng thay vì cho rằng mọi container đã biến mất hoặc đã
phục hồi.

## Kiểm kê đầy đủ mới chứng minh được thiếu container

Sự cố thiếu container chỉ mở từ một kiểm kê **đầy đủ**. Khi agent không thể
kiểm tra mọi container được quản lý, kiểm kê là một phần; Gotham đánh dấu bằng
chứng hiện có là không rõ và tạm dừng tính thời gian. Agent cũ không đẩy kiểm
kê cũng là độ phủ không rõ. Khi node offline, bằng chứng runtime của node trở
thành không rõ thay vì báo mọi container đã mất hoặc đã phục hồi. Khi node trở
lại trong lúc tài nguyên vẫn lỗi, Gotham đánh giá lại bằng chứng còn sống.

## Dừng dự kiến và thời gian chờ khởi động

Các chuyển đổi sau không tạo sự cố runtime:

- Người dùng **dừng** ứng dụng hoặc cơ sở dữ liệu. Tài nguyên hiển thị là dừng
  có chủ đích cho tới khi được start hoặc deploy lại.
- **Triển khai, khởi động lại hoặc thay thế** trong khi deployment hiện tại còn
  đang chạy, và trong thời gian chờ khởi động sau khi hoàn tất.
- Cơ sở dữ liệu đang **creating, deleting hoặc stopped**.
- **Tác vụ một lần** được hỗ trợ: container thoát với mã 0 dưới chính sách
  Docker `restart: "no"`. Dùng `restart: "no"` cho tác vụ; dịch vụ chạy lâu nên
  dùng `always` hoặc `unless-stopped`. Vì `no` cũng là mặc định của Compose,
  một dịch vụ chạy lâu bị dừng gọn gàng ngoài Gotham có thể trông như tác vụ
  một lần đã xong; chính sách khởi động lại ở trên tránh âm tính giả này.

Thời gian chờ khởi động mặc định là **120 giây** và có thể chỉnh từ **30 đến
600 giây** trong thẻ runtime bởi chủ sở hữu hoặc người viết dự án. Một thao tác
bị kẹt không che giấu lỗi mãi mãi: deployment đang chạy chỉ được coi là dự kiến
trong khoảng thời gian thao tác có giới hạn.

## Replica Compose

Với ứng dụng Compose, replica được nhóm theo ứng dụng và dịch vụ Compose, với số
container Compose là danh tính replica ổn định và ID container, ID triển khai là
bằng chứng.
Nếu một replica lỗi, thẻ nêu rõ dịch vụ và replica bị ảnh hưởng; một replica
cùng dịch vụ đang khỏe không thể che giấu nó. Ứng dụng không phải Compose được
đánh giá như một container dự kiến. Mỗi cơ sở dữ liệu được đánh giá như một
container được quản lý.

## Vòng lặp khởi động lại và OOM

Gotham lấy mốc bộ đếm khởi động lại ở lần quan sát đầu tiên. Sự cố vòng lặp mở
khi bộ đếm tăng ít nhất **3 lần trong cửa sổ trượt năm phút**. Bộ đếm bị đặt
lại, container được tạo lại, redeploy và khoảng trống bằng chứng cũ đều đặt lại
mốc, nên không tạo ra delta âm hoặc quá lớn. Sự cố đóng khi năm phút trôi qua
không có lần khởi động lại mới và có bằng chứng đang chạy, chấp nhận được.

Phát hiện OOM là tường minh: cần cờ `OOMKilled` của Docker trên một lần container
kết thúc mới. Chỉ mã thoát 137 không bao giờ được coi là OOM, và cùng một lần cũ
không bị báo lại ở mọi mẫu. Một lần khởi động lại hoặc container mới sẽ đóng sự
cố OOM khi dịch vụ chạy lại.

## Quyền và thông báo

Chỉ người đã có quyền đọc ứng dụng hoặc cơ sở dữ liệu mới thấy thẻ runtime và
nhãn trong danh sách; nhật ký giữ nguyên phân quyền và che giấu hiện có. Chủ sở
hữu và người viết dự án có thể bật giám sát và chỉnh thời gian chờ khởi động.

Thông báo runtime dùng lại các kênh thông báo hiện có. Hai sự kiện tường minh
mới, **Lỗi runtime** và **Runtime đã phục hồi**, có sẵn cho kênh ứng dụng và cơ
sở dữ liệu. Chúng không được tự động thêm vào đăng ký kênh hiện có, nên bật giám
sát không mở rộng kênh bên ngoài mà bạn không chọn.

## Giới hạn

Gotham không tự động khởi động lại hay sửa tài nguyên lỗi, không vẽ biểu đồ số
liệu từng container, không phân tích truy vấn cơ sở dữ liệu và không thêm lịch
thăm dò Docker trên control plane. Việc phát hiện phụ thuộc vào bằng chứng do
agent hiện hành đẩy lên.
