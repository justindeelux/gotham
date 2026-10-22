---
title: Mạng của instance
---

# Mạng của instance

**Cài đặt → Instance** cấu hình chính máy chủ control plane: địa chỉ công khai và
định danh, ngăn xếp mạng, và vài tùy chọn hệ thống Linux. Mục này chỉ quản trị viên
nền tảng thấy được và áp dụng cho máy chủ control plane, không áp dụng cho các node.

Trang có ba tab: **Chung**, **Mạng** và **Hệ thống**.

## Chung

| Cài đặt | Mục đích |
|---|---|
| URL control plane | URL gốc công khai dùng cho đăng ký agent, OAuth redirect và webhook. Nó cũng quyết định tuyến của bảng điều khiển và chứng chỉ của tuyến đó. |
| Tên instance | Tên hiển thị của instance. Mặc định: `gotham`. |
| Múi giờ | Múi IANA áp dụng cho lịch chạy, hiển thị log và sao lưu. Mặc định: `UTC`. |

URL phải là `http` hoặc `https` có tên máy chủ, không kèm thông tin đăng nhập, query
hay fragment. Mỗi trường hiển thị nguồn của giá trị — biến môi trường, cơ sở dữ
liệu hay mặc định tích hợp. Một trường được đặt bởi biến môi trường sẽ bị khóa và
không sửa được ở đây; trang nêu tên biến đó.

URL control plane là địa chỉ mà agent đăng ký tới và các dịch vụ bên ngoài gọi về,
nên việc đổi nó được bảo vệ:

1. Lưu URL mới sẽ ghi nhận một thay đổi đang chờ và đẩy tuyến bảng điều khiển mới.
2. Cấu hình mới chỉ được coi là đã áp dụng khi bạn xác nhận **qua địa chỉ mới**,
   trong vòng hai phút. Hãy mở Gotham ở URL mới và giữ thay đổi tại đó.
3. Nếu tuyến mới chưa đồng bộ, việc xác nhận bị từ chối. Nếu bạn không truy cập
   được Gotham ở địa chỉ mới, hãy không làm gì: tuyến trước được khôi phục khi hết
   thời gian đếm ngược, hoặc bạn có thể hoàn tác ngay từ trang này.

Mỗi lần chỉ có một thay đổi URL control plane đang chờ.

### Trạng thái tên miền và chứng chỉ

Tab Chung báo trạng thái trực tiếp của hostname control plane:

- **Router** — `synced` khi node phục vụ cấu hình được sinh hiện tại, `pending` khi
  cần đồng bộ, `unknown` khi chưa có lịch sử đồng bộ. Máy chủ control plane phải là
  một node đã đăng ký thì router của nó mới đồng bộ được.
- **Chứng chỉ** — `issued` kèm ngày hết hạn, `pending`, `failed`, hoặc
  `not applicable` khi URL là HTTP thường. Trạng thái được đọc từ bộ nhớ ACME của
  node, không bao giờ lưu như một khai báo.
- **DNS** — không được báo cáo. API không kiểm tra hostname có phân giải về instance
  này hay không.

Chứng chỉ của bảng điều khiển do proxy quản lý; xem
[Proxy và DNS](/vi/operating/proxy-dns). Việc đổi URL sẽ tự động đồng bộ lại nó.

## Mạng

Tab Mạng sửa địa chỉ của máy chủ control plane:

- **Máy chủ DNS** — bắt buộc có DNS chính, DNS phụ tùy chọn, tối đa ba địa chỉ IPv4
  hoặc IPv6 khác nhau.
- **IPv4** — tự động (DHCP), hoặc tĩnh với địa chỉ dạng CIDR và một gateway.
- **IPv6** — bật với địa chỉ tự động hoặc tĩnh và một gateway, hoặc tắt.

Khi không có thay đổi nào đang chờ, biểu mẫu hiển thị cấu hình trực tiếp của máy
chủ thay vì các giá trị lưu gần nhất.

### Áp dụng, giữ, hoàn tác

Áp dụng một thay đổi mạng sẽ đưa nó vào hoạt động ngay nhưng vẫn ở trạng thái
**chưa xác nhận**. Một dải đếm ngược đưa ra **Giữ thay đổi** và **Hoàn tác ngay**:

- **Giữ thay đổi** xác nhận cấu hình và xóa trạng thái đang chờ.
- **Hoàn tác ngay** khôi phục cấu hình trước đó ngay lập tức.
- **Không làm gì** — máy chủ khôi phục cấu hình trước đó khi hết hai phút đếm ngược.
  Đây là đường an toàn khi thay đổi làm mất kết nối của bạn: bạn không cần truy cập
  Gotham để hoàn tác.

Mỗi lần chỉ có một thay đổi mạng đang chờ. Hãy xử lý xong trước khi thay đổi tiếp;
lần áp dụng thứ hai bị từ chối trong lúc chờ.

Một **thay đổi chỉ về DNS** — khi cấu hình interface không đổi — vẫn có đếm ngược,
nhưng chỉ cài đặt resolver được ghi: tệp interface không bị ghi lại và không cần
xác nhận thay đổi rủi ro.

Một thay đổi có thể ảnh hưởng tới interface đang hoạt động — địa chỉ hoặc gateway
khác, chuyển một interface tĩnh sang DHCP, hoặc tắt một họ địa chỉ — sẽ hiện cảnh
báo và yêu cầu bạn tích xác nhận rõ ràng trước khi áp dụng. Máy chủ có thể không
truy cập được cho tới khi thay đổi được xác nhận hoặc hoàn tác.

Phần này cần một máy chủ chạy systemd-networkd, systemd-resolved và `networkctl`.
Trên máy chủ khác, các giá trị chỉ đọc và trang nói rằng ngăn xếp này không được hỗ
trợ; cài helper máy chủ không thay đổi được điều đó.

## Hệ thống

Tab Hệ thống đặt:

- **Hostname** — chữ cái, chữ số và dấu gạch ngang; dấu chấm ngăn cách các nhãn.
- **NTP** — bật đồng bộ thời gian và liệt kê tối đa bốn máy chủ. Để trống thì dùng
  mặc định của hệ thống.

Cài đặt hệ thống được áp dụng ngay; chúng không có đếm ngược xác nhận. Chúng cần
`systemctl` và `hostnamectl` trên máy chủ, và chỉ đọc trên máy chủ thiếu hai công
cụ đó.

## Helper máy chủ

Các thay đổi mạng và hệ thống được áp dụng bởi một helper máy chủ cố định, thuộc sở
hữu root, với một quy tắc `sudo` ghim theo lệnh, không phải bởi một shell đặc quyền
dùng chung. Trang báo một trong ba trạng thái của helper:

| Trạng thái | Ý nghĩa |
|---|---|
| Ready | Helper đã cài và chạy được. Cả hai phần đều sửa được nếu ngăn xếp của chúng được hỗ trợ. |
| Missing | Thiếu tệp helper. Thao tác **Cài helper máy chủ** trong sản phẩm có thể khôi phục nó khi đường sửa chữa đặc quyền có mặt. |
| Denied | Helper có mặt nhưng `sudo` từ chối nó vì thiếu quy tắc. Chỉ chạy lại trình cài đặt control plane (hoặc nâng cấp) mới sửa được. |

**Cài helper máy chủ** chỉ xuất hiện khi thiếu helper và việc sửa chữa trong sản
phẩm thực sự chạy được. Một lần cài thất bại hiển thị kết quả của trình cài đặt để
bạn đọc nguyên nhân. Máy chủ ở trạng thái bị từ chối, hoặc đã mất phần payload
thuộc sở hữu root, không có cách sửa trong sản phẩm; trang nói rõ điều đó và bạn
phải chạy lại trình cài đặt control plane.

Không có helper sẵn sàng thì các bảng **Mạng** và **Hệ thống** chỉ đọc và giải
thích lý do. **Chung** không bị ảnh hưởng: URL, tên và múi giờ do control plane lưu
và không cần helper máy chủ.
