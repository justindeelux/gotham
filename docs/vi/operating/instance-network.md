---
title: Cài đặt và cài đặt máy chủ của nút
---

# Cài đặt và cài đặt máy chủ của nút

**Cài đặt** (`/settings/instance`) cấu hình chính instance Gotham: địa chỉ
control plane công khai và tên của nó. Mọi thứ mô tả một máy cụ thể — múi giờ,
mạng và các tùy chọn hệ thống Linux — nằm trên trang nút của máy đó, trong
**Máy chủ → [nút] → Cài đặt nút**. Cả hai nơi chỉ chủ sở hữu thấy được.

## Cài đặt

| Cài đặt | Mục đích |
|---|---|
| URL control plane | URL gốc công khai dùng cho đăng ký agent, OAuth redirect và webhook. Nó cũng quyết định tuyến của bảng điều khiển và chứng chỉ của tuyến đó. |
| Tên instance | Tên hiển thị của instance. Mặc định: `gotham`. |

URL phải là `http` hoặc `https` có tên máy chủ, không kèm thông tin đăng nhập, query
hay fragment. Mỗi trường hiển thị nguồn của giá trị — biến môi trường, cơ sở dữ
liệu hay mặc định có sẵn. Một trường được đặt bởi biến môi trường sẽ bị khóa và
không sửa được ở đây; trang nêu tên biến đó.

URL control plane là địa chỉ mà agent đăng ký tới và các dịch vụ bên ngoài gọi về,
nên việc đổi nó được bảo vệ:

1. Lưu URL mới sẽ ghi nhận một thay đổi đang chờ và đẩy tuyến bảng điều khiển mới.
2. Cấu hình mới chỉ được coi là đã áp dụng khi bạn xác nhận **qua địa chỉ mới**,
   trong vòng hai phút. Hãy mở Gotham ở URL mới và giữ thay đổi tại đó.
3. Nếu tuyến mới chưa đồng bộ, việc xác nhận bị từ chối. Nếu bạn không truy cập
   được Gotham ở địa chỉ mới, hãy không làm gì: tuyến trước được khôi phục khi hết
   thời gian đếm ngược.

Mỗi lần chỉ có một thay đổi URL control plane đang chờ.

### Trạng thái tên miền và chứng chỉ

Trang báo trạng thái trực tiếp của hostname control plane:

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

## Cài đặt nút

Tab **Cài đặt nút** của mỗi nút sửa máy chủ mà nút đó chạy. Dữ liệu được đọc trực
tiếp từ nút đó mỗi lần mở; control plane không lưu đệm. Đổi một nút không bao giờ
làm đổi nút khác.

| Phần | Cài đặt |
|---|---|
| Mạng | Máy chủ DNS, IPv4 (DHCP hoặc địa chỉ và gateway tĩnh), IPv6 (tắt, tự động hoặc tĩnh). |
| Hệ thống | Hostname, bật/tắt NTP với tối đa bốn máy chủ, và múi giờ của nút. |
| Helper máy chủ | Helper đặc quyền áp dụng hai phần trên. |

Múi giờ của nút **localhost** cũng là múi giờ của control plane: lịch chạy, hiển
thị log và sao lưu chạy theo nó. Múi giờ của nút từ xa chỉ đổi đồng hồ của máy đó.

### Mạng

- **Máy chủ DNS** — bắt buộc có DNS chính, DNS phụ tùy chọn, tối đa ba địa chỉ IPv4
  hoặc IPv6 khác nhau.
- **IPv4** — tự động (DHCP), hoặc tĩnh với địa chỉ dạng CIDR và một gateway.
- **IPv6** — bật với địa chỉ tự động hoặc tĩnh và một gateway, hoặc tắt.

Khi không có thay đổi nào đang chờ, biểu mẫu hiển thị cấu hình trực tiếp của nút
thay vì các giá trị lưu gần nhất.

### Áp dụng, giữ, hoàn tác

Áp dụng một thay đổi mạng sẽ đưa nó vào hoạt động ngay nhưng vẫn ở trạng thái
**chưa xác nhận**. Một dải đếm ngược đưa ra **Giữ thay đổi** và **Hoàn tác ngay**:

- **Giữ thay đổi** xác nhận cấu hình và xóa trạng thái đang chờ.
- **Hoàn tác ngay** khôi phục cấu hình trước đó ngay lập tức.
- **Không làm gì** — nút khôi phục cấu hình trước đó khi hết hai phút đếm ngược.
  Đây là đường an toàn khi thay đổi làm mất kết nối của bạn: bạn không cần truy cập
  Gotham để hoàn tác. Trên nút từ xa, bộ đếm chạy trên chính nút đó, nên nó vẫn
  hoàn tác khi mất kết nối agent.

Mỗi nút chỉ có một thay đổi mạng đang chờ. Hãy xử lý xong trước khi thay đổi tiếp;
lần áp dụng thứ hai trên cùng nút bị từ chối trong lúc chờ.

Một **thay đổi chỉ về DNS** — khi cấu hình interface không đổi — vẫn có đếm ngược,
nhưng chỉ cài đặt resolver được ghi: tệp interface không bị ghi lại và không cần
xác nhận thay đổi rủi ro.

Một thay đổi có thể ảnh hưởng tới interface đang hoạt động — địa chỉ hoặc gateway
khác, chuyển một interface tĩnh sang DHCP, hoặc tắt một họ địa chỉ — sẽ hiện cảnh
báo và yêu cầu bạn tích xác nhận rõ ràng trước khi áp dụng. Nút có thể không truy
cập được cho tới khi thay đổi được xác nhận hoặc hoàn tác.

Phần này cần một máy chủ chạy systemd-networkd, systemd-resolved và `networkctl`.

### Hệ thống

- **Hostname** — chữ cái, chữ số và dấu gạch ngang; dấu chấm ngăn cách các nhãn.
- **NTP** — bật đồng bộ thời gian và liệt kê tối đa bốn máy chủ. Để trống thì dùng
  mặc định của hệ thống.
- **Múi giờ** — múi IANA có thể tìm kiếm, kèm độ lệch UTC hiện tại.

Cài đặt hệ thống được áp dụng ngay; chúng không có đếm ngược xác nhận. Chúng cần
`systemctl` và `hostnamectl` trên nút.

## Trạng thái khả năng

Mỗi nút báo một khả năng. Chỉ **Ready** cho phép sửa; mọi trạng thái khác hiển thị
cả hai phần ở chế độ chỉ đọc kèm lý do rõ ràng, nên không bao giờ có lần lưu thất
bại:

| Trạng thái | Ý nghĩa |
|---|---|
| `ready` | Helper máy chủ chạy được và nút có đủ ngăn xếp mạng và thời gian. |
| `unsupported_stack` | Nút thiếu systemd-networkd/resolved hoặc timedatectl. Cài helper không khắc phục được ngăn xếp thiếu. |
| `helper_outdated` | Helper cũ hơn control plane này và không báo được múi giờ. Trên máy chạy control plane, **Cài helper máy chủ** sẽ cập nhật nó; trên nút từ xa, hãy cập nhật agent hoặc chạy lại trình cài agent. |
| `helper_missing` | Thiếu tệp helper. Trên máy chạy control plane, **Cài helper máy chủ** có thể khôi phục khi đường sửa chữa đặc quyền có mặt. Trên nút từ xa, hãy chạy lại trình cài agent. |
| `helper_denied` | Helper có mặt nhưng quy tắc đặc quyền từ chối nó. Hãy chạy lại trình cài tương ứng. |
| `agent_outdated` | Agent cũ hơn tính năng cài đặt máy chủ. Hãy cập nhật agent hoặc chạy lại trình cài agent trên nút đó, rồi tải lại trang. |
| `offline` | Không kết nối được agent. Cài đặt chỉ để xem cho tới khi agent kết nối lại. |

Một lần cài helper thất bại hiển thị kết quả của trình cài đặt để bạn đọc nguyên
nhân. Việc chạy lại trình cài agent trên mọi nút từ xa là thao tác thủ công: agent
hiện có không tự được nâng cấp để hỗ trợ cài đặt máy chủ.

Xem [Node và container](/vi/operating/nodes-containers) để đăng ký nút và dùng
trình cài agent.
