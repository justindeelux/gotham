---
title: Kho ứng dụng
---

# Kho ứng dụng

**Kho ứng dụng** (`/toolchain`) quản lý các công cụ máy chủ mà một node cần. Đây
là góc nhìn theo từng node trên danh mục công cụ cố định của nền tảng, không phải
cửa hàng ứng dụng: không có gì cài ở đây là một khối lượng công việc. Node nào
cũng cần Docker; các công cụ build là tùy chọn.

Việc cài đặt và gỡ bỏ chạy trên node qua một helper máy chủ cố định, thuộc sở hữu
root. Control plane và agent chỉ truyền một id trong danh mục; helper sở hữu lệnh
nó chạy và tự kiểm tra lại id với danh sách cho phép của chính nó. Xem
[Thêm node](/vi/guide/nodes) để biết cách helper được đặt lên một node.

## Chọn node

Chọn một node ở đầu trang. Danh mục, trạng thái cài đặt và hàng đợi tác vụ đều
gắn với node đó, nên đổi node là đổi toàn bộ khung nhìn. Khi chưa có node nào được
đăng ký, trang yêu cầu bạn thêm máy chủ trước; xem
[Node và container](/vi/operating/nodes-containers).

Công cụ được nhóm theo các danh mục `build`, `runtime` và `network` của danh mục
công cụ, và các tab cũng dùng làm bộ lọc. Tìm kiếm khớp tên, id hoặc danh mục của
công cụ. Lựa chọn được giữ khi đổi tab, nên một lần cài hàng loạt có thể gồm công
cụ từ nhiều danh mục.

## Danh mục

Mười bốn mục:

| Công cụ | Nhóm | Nền tảng dùng để làm gì | Hành động |
|---|---|---|---|
| Railpack | Build | Build image từ một kế hoạch ngôn ngữ được sinh ra (build pack “auto”). | Cài đặt / Gỡ bỏ |
| BuildKit | Build | Image container mà Railpack dùng để build; được khởi động qua Docker, không bao giờ cài như một tệp nhị phân trên máy chủ. | Cài đặt / Gỡ bỏ |
| Cloud Native Buildpacks (`pack`) | Build | Build image bằng vòng đời Cloud Native Buildpacks. | Cài đặt / Gỡ bỏ |
| Git | Build | Sao chép mã nguồn ứng dụng và commit; cần 2.37 trở lên để ghim DNS. | Cài đặt / Cập nhật |
| Docker | Runtime | Chạy container ứng dụng, cơ sở dữ liệu và proxy; node nào cũng cần. | Cài đặt |
| systemd-networkd | Network | Cấu hình mạng của máy chủ; Gotham ghi một drop-in và nạp lại nó. | Chỉ phát hiện |
| systemd-resolved | Network | Phân giải DNS của máy chủ; Gotham ghi một drop-in và nạp lại nó. | Chỉ phát hiện |
| `networkctl` | Network | Xem và nạp lại các liên kết của systemd-networkd. | Chỉ phát hiện |
| OpenSSH client | Runtime | Sao chép một kho riêng tư qua SSH. | Cài đặt |
| `sudo` | Runtime | Chạy các helper máy chủ đặc quyền qua `sudo -n`. | Cài đặt |
| `systemctl` | Runtime | Quản lý các unit systemd của agent, proxy và cơ sở dữ liệu. | Chỉ phát hiện |
| `curl` | Runtime | Tải bản phát hành và tài liệu cài đặt trên máy chủ. | Cài đặt |
| `resolvectl` | Network | Đọc và đặt DNS theo từng liên kết qua systemd-resolved. | Chỉ phát hiện |
| `ip` | Network | Đọc địa chỉ đang dùng và tuyến mặc định của máy chủ. | Chỉ phát hiện |

Việc phát hiện tìm từng công cụ trên `PATH` của máy chủ (BuildKit và các unit
systemd được dò qua Docker và `systemctl` thay thế). Một công cụ có mặt nhưng
không trả lời được đợt dò phiên bản vẫn được hiển thị là đã cài, không kèm phiên
bản. Chỉ Git khai báo phiên bản tối thiểu, 2.37, nên đây là dòng duy nhất có thể
báo **Có bản mới** khi phiên bản phát hiện được cũ hơn.

Phụ thuộc là một phần của danh mục: Railpack cần BuildKit và BuildKit cần Docker,
và agent cài các phụ thuộc trước. Các công cụ mạng systemd khai báo những unit mà
chúng điều khiển — ví dụ `networkctl` cần systemd-networkd và `resolvectl` cần
systemd-resolved — và việc phát hiện kiểm tra điều đó. Một lệnh gỡ bỏ bị từ chối
khi vẫn còn công cụ đã cài khác cần công cụ đang bị gỡ.

## Cài đặt, cập nhật, gỡ bỏ

- **Cài đặt** xếp công cụ vào hàng đợi. Một lệnh cài đã có trong hàng đợi hoặc
  đang chạy cho cùng công cụ và cùng hành động sẽ bị bỏ qua và được nêu trong
  thông báo xác nhận, nên nhấp đúp không bao giờ tạo tác vụ thứ hai.
- **Cài đặt / Cập nhật** xuất hiện cho Git khi phiên bản phát hiện được thấp hơn
  mức tối thiểu 2.37 của danh mục; hiện không mục nào khác khai báo phiên bản tối
  thiểu. Bảng điều khiển không so phiên bản với bản phát hành thượng nguồn và
  không có bộ chọn phiên bản: nó cài đúng bản phát hành mà helper máy chủ ghim.
- **Gỡ bỏ** chỉ có cho Railpack, BuildKit và Buildpacks. Docker không bao giờ bị
  gỡ vì việc đó sẽ phá hủy mọi khối lượng công việc trên node, còn Git, SSH
  client, `sudo`, `curl` và các unit hệ thống là nền tảng cần đến, nên việc gỡ
  chúng bị từ chối ở phía máy chủ kể cả khi có yêu cầu.
- Các dòng **Chỉ phát hiện** không có thao tác cài hay gỡ nào. Bảng điều khiển nói
  rõ điều đó thay vì đưa ra một nút sẽ thất bại; đây là các gói máy chủ mà nền
  tảng cấu hình nhưng không quản lý.

Chỉ những công cụ còn thiếu và cài được mới có thể chọn để cài hàng loạt. Các dòng
đã cài, dòng chỉ phát hiện và công cụ có bản mới được xử lý từng cái một ngay trên
dòng của nó.

## Hàng đợi và kết quả tác vụ

**Hàng đợi** mở hàng đợi cài đặt cho node đang chọn. Mỗi thao tác công cụ là một
tác vụ, và danh sách tác vụ hiển thị trạng thái của nó:

| Trạng thái | Ý nghĩa |
|---|---|
| Đang đợi | Chờ tác vụ hiện tại của node chạy xong. |
| Đang chạy | Helper đang cài hoặc gỡ công cụ. |
| Thành công | Công cụ đạt trạng thái được yêu cầu. |
| Thất bại | Thao tác thất bại; phần kết quả nói lý do. |
| Đã hủy | Đã hủy trước khi bắt đầu. |

Trạng thái cập nhật trực tiếp khi hàng đợi đang mở. Node chạy một tác vụ mỗi lần,
nên nhiều công cụ trong hàng đợi chạy lần lượt; các node khác nhau chạy song song.

Chọn một tác vụ để xem kết quả cài đặt. Các dòng được truyền trong lúc tác vụ chạy,
và kết quả của tác vụ đã xong vẫn xem được. Kết quả lưu của một tác vụ bị giới hạn
ở 256 KiB; quá mức đó, nhật ký kết thúc bằng dấu hiệu bị cắt. Đóng cửa sổ hàng đợi
không dừng gì cả — các tác vụ vẫn chạy nền.

- **Hủy** dùng được khi tác vụ còn trong hàng đợi. Tác vụ đang chạy không thể hủy
  từ bảng điều khiển.
- **Thử lại** có cho tác vụ thất bại. Nó xếp một tác vụ mới cho cùng công cụ và
  cùng hành động; tác vụ cũ vẫn nằm trong danh sách như lịch sử.

Một node mà control plane không kết nối được sẽ hiện lỗi không kết nối được agent
thay vì một danh mục rỗng. Hãy kiểm tra trạng thái node rồi thử lại.

## Công cụ máy chủ cung cấp cho các engine build

Railpack và Buildpacks là các engine build có nối dây, và công cụ máy chủ của chúng
đến từ danh mục này. Railpack build một dự án ngôn ngữ mà không cần Dockerfile,
nhưng chỉ cài Railpack là chưa đủ — nó còn cần BuildKit và Docker. Buildpacks cần
`pack`. Công cụ còn thiếu được báo trên đợt triển khai. Xem
[Ứng dụng](/vi/using/applications).

## Khi cài đặt thất bại

- **Không kết nối được agent trên node** — chưa có gì được xếp hàng. Hãy kiểm tra
  node đang trực tuyến.
- **Thiếu helper máy chủ hoặc helper từ chối lệnh** — một node chỉ được cập nhật
  qua tính năng tự cập nhật của agent có thể còn bản cài cũ trên máy chủ. Hãy chạy
  lại trình cài đặt node, vốn có tính lũy đẳng, rồi thử lại; xem
  [Thêm node](/vi/guide/nodes).
- **Công cụ đang được dùng** — lệnh gỡ bỏ bị từ chối khi đang có bản build chạy,
  hoặc khi một công cụ đã cài khác phụ thuộc vào nó. Hãy gỡ công cụ phụ thuộc
  trước (Railpack trước BuildKit).
- **Kết quả hiện lỗi checksum hoặc lỗi trình quản lý gói** — tác vụ thất bại trên
  máy chủ và không thay đổi gì. Hãy đọc kết quả, sửa tình trạng của máy chủ, rồi
  thử lại.
