---
title: Node và container
---

# Node và container

Node là một máy chủ Linux chạy `gotham-agent`. Control plane không bao giờ gọi
Docker trực tiếp: mọi lệnh container, lần đọc chỉ số và luồng nhật ký đều đi qua
agent trên node đó. Đăng ký node tại **Máy chủ** (`/servers`); mở một node để
xem tổng quan, chỉ số và cài đặt, và dùng tab **Container** cho danh mục Docker.

Quản lý node theo phạm vi nhóm. Thành viên của nhóm sở hữu node có thể đọc, còn
`owner` hoặc `admin` có thể thay đổi. Xem
[Bảo mật](/vi/operating/security) để biết ranh giới quản trị viên nền tảng mà
trình quản lý tệp và shell trên máy chủ dùng.

## Thêm node

**Thêm máy chủ** mở một wizard bốn bước: **Kết nối**, **Kiểm tra**, **Cài đặt**,
**Hoàn tất**.

1. **Kết nối** hỏi tên node, địa chỉ (IPv4 hoặc hostname), cổng SSH (thường là
   22), người dùng SSH, và một thông tin xác thực: khóa riêng dán ở định dạng
   PEM, hoặc mật khẩu của node. Cụm mật khẩu cho khóa được bảo vệ chỉ dùng cho
   lần chạy đó. Bí mật đã lưu được mã hóa và API không bao giờ trả về. Tiếp tục
   sẽ đăng ký dòng máy chủ.
2. **Kiểm tra** tự động chạy các đợt dò SSH: Docker Engine có mặt và đang chạy,
   kiến trúc và số nhân CPU, RAM khả dụng, dung lượng đĩa trống. Các đợt dò dừng
   sau 15 giây. Đợt dò Docker thất bại sẽ đưa ra **Cài Docker**, cài Docker Engine
   qua chính phiên SSH đó bằng trình quản lý gói của node và truyền trực tiếp kết
   quả.
3. **Cài đặt** hiển thị một lệnh cài đặt dùng một lần để chạy trên node qua SSH.
   Lệnh này sao chép CA của control plane, lấy các tệp phụ của trình cài đặt và
   đăng ký agent. Trang hỏi lại mỗi năm giây; không có tiến trình mô phỏng.
4. **Hoàn tất** xác nhận agent đã báo cáo. Node chỉ **Sẵn sàng** sau nhịp tim đầu
   tiên — chỉ kiểm tra SSH thành công thì chưa đủ.

Khóa máy chủ SSH của node được ghim ở lần kiểm tra thành công đầu tiên
(trust-on-first-use). Các lần kiểm tra sau sẽ từ chối nếu khóa đã thay đổi. Node
xác thực bằng mật khẩu cần tích **Tin cậy khóa máy chủ này ở lần kiểm tra đầu**
cho lần chạy đầu tiên, vì nếu không, một kẻ chen giữa ở lần dùng đầu cũng sẽ lấy
được mật khẩu.

### Trạng thái node

| Trạng thái | Ý nghĩa |
|---|---|
| `Pending` | Đã đăng ký, nhưng agent chưa gửi nhịp tim. Cũng là trạng thái sau khi đổi địa chỉ, người dùng hoặc thông tin xác thực. |
| `Validating` | Các đợt dò SSH đang chạy. |
| `Ready` | Đã nhận nhịp tim trong 30 giây gần nhất. |
| `Offline` | Không có nhịp tim trong 30 giây. Việc chuyển trạng thái xảy ra khi danh sách node được đọc. |
| `Error` | Lần kiểm tra SSH gần nhất thất bại. |

## Sửa, kiểm tra lại, đặt lại, xóa

**Sửa** thay đổi tên, địa chỉ, cổng SSH, người dùng SSH hoặc thông tin xác thực.
Khóa và mật khẩu là hai lựa chọn thay thế, không bao giờ dùng cả hai. Đổi địa
chỉ, người dùng hoặc thông tin xác thực sẽ xóa khóa máy chủ đã ghim và đưa node
về `Pending`, nên điểm cuối mới phải được kiểm tra lại.

**Kiểm tra lại SSH** trên thẻ node, hoặc **Kiểm tra SSH** trên trang, chạy lại
các đợt dò. **Kiểm tra SSH** kiểm tra mọi node đã đăng ký và báo số node đạt. Một
lần chạy thành công cập nhật hệ điều hành, kiến trúc, phiên bản Docker và tài
nguyên đã ghi; nó chỉ ghim lại khóa máy chủ đã thay đổi nếu việc ghim đã được đặt
lại trước đó. Trạng thái sau khi kiểm tra được suy ra từ nhịp tim cuối, nên node
có agent đã biến mất sẽ thành `Offline`, không phải `Ready`.

**Đặt lại khóa máy chủ đã ghim.** Sau khi xoay vòng khóa hợp lệ trên máy chủ,
lần kiểm tra tiếp theo sẽ thất bại cho tới khi bản ghim cũ bị quên đi. Không có
nút điều khiển nào trong bảng điều khiển cho việc này; quản trị viên gọi:

```http
DELETE /api/v1/servers/{id}/host-key
```

Hành động này chỉ xóa dấu vân tay đã lưu. Lần kiểm tra tiếp theo sẽ ghim khóa
mới. Nó không đổi địa chỉ hay thông tin xác thực của node.

**Xóa** chỉ gỡ node khỏi control plane. Yêu cầu bị từ chối khi vẫn còn ứng dụng,
dịch vụ hoặc cơ sở dữ liệu gắn với node; hãy di chuyển hoặc xóa chúng trước. Node
`localhost` (agent cục bộ tích hợp) không thể sửa hay xóa. Container, volume và
chứng chỉ trên máy vẫn được giữ nguyên.

## Tổng quan node

Mở một node sẽ thấy các tab:

| Tab | Hiển thị |
|---|---|
| **Tổng quan** | Tên, địa chỉ, người dùng SSH, ID node, hệ điều hành, kiến trúc, phiên bản Docker, loại thông tin xác thực, mức dùng CPU/bộ nhớ/đĩa, số container, thấy lần cuối, thời điểm đăng ký, và vùng nguy hiểm với **Xóa nút**. |
| **Container** | Lối tắt tới trang container của node. |
| **Tệp** | Chỉ quản trị viên nền tảng: lối tắt tới trình quản lý tệp cho node này. |
| **Terminal** | Chỉ quản trị viên nền tảng: tab shell trên máy chủ cho node này. |
| **Chỉ số** | Biểu đồ chuỗi thời gian của node. |
| **Proxy và Traefik** | Các bộ định tuyến Traefik trên node. |
| **Cài đặt nút** | Bản tóm tắt chỉ đọc về định danh của node. |

## Chỉ số

Tab **Chỉ số** vẽ bốn chuỗi: **CPU**, **RAM**, **Ổ đĩa I/O** và **Mạng**. Giá
trị đến từ nhịp tim của agent; control plane gom chúng thành các bucket và không
bao giờ tự tạo điểm cho bucket mà node không báo cáo, nên một khoảng trống trong
chuỗi là khoảng trống trên biểu đồ, không phải số 0.

| Khoảng | Cửa sổ |
|---|---|
| Bước `1m` | Một giờ qua |
| Bước `1h` | 24 giờ qua |
| Bước `1d` | 7 ngày qua |

**Tự động làm mới** mặc định tắt và có thể đặt 15 hoặc 60 giây. **Tải lại** thủ
công tải lại cửa sổ đang chọn.

Mẫu được giữ **30 ngày**, bị dọn bởi tiến trình thanh lọc mỗi giờ. Đường ghi nhịp
tim lưu tối đa một mẫu mỗi năm giây; ảnh chụp trực tiếp trên dòng node cập nhật
theo mọi nhịp tim.

Bề mặt này do `FEATURE_METRICS` điều khiển, mặc định bật. Đặt thành `false` sẽ gỡ
tuyến khoảng chỉ số — yêu cầu từ bảng điều khiển nhận `404` và biểu đồ bị ẩn —
đồng thời ngừng lưu mẫu từ nhịp tim. Phần còn lại của registry node vẫn hoạt
động.

## Container

`/servers/{id}/containers` liệt kê mọi container trên node, kể cả container đã
dừng. Lọc theo **Tất cả**, **Đang chạy** hoặc **Đã thoát**, hoặc tìm theo tên
container hay image. Bảng hiển thị tên, image, trạng thái, cổng, CPU, RAM, thời
gian chạy và hành động; hợp đồng container của agent không báo CPU hay RAM theo
từng container, nên hai cột đó hiển thị dấu gạch thay vì một con số đoán.

- Container đang chạy có **Khởi động lại** và **Dừng**; container đã dừng có
  **Chạy**.
- Danh sách được phục vụ từ cache ngắn (10 giây). **Tải lại** lấy danh sách hiện
  tại từ node.
- Đọc danh sách mở cho nhóm của node; chạy, dừng và khởi động lại cần vai trò
  nhóm `owner` hoặc `admin`.

### Kéo image và chạy container

Hai thao tác này có trên API nhưng không có nút nào trong bảng điều khiển. Bảng
điều khiển là nơi vận hành các khối lượng công việc do Gotham quản lý; container
thô tạo theo cách này không phải ứng dụng đã triển khai, không có tuyến, không có
biến và không có hoàn tác.

```http
POST /api/v1/servers/{id}/images/pull
POST /api/v1/servers/{id}/containers/run
```

**Pull** nhận `{"image": "registry/repo:tag"}` và trả lời `202` sau khi node đã
kéo image. **Run** nhận `image` cùng các trường tùy chọn `name`, `env`,
`command`, `entrypoint`, `labels`, `ports`, `volumes`, `networks`,
`restart_policy` và `healthcheck`, và trả lời `201` kèm id container mới.

Một lần chạy thô không thể giả mạo tài nguyên thuộc nền tảng: nhãn bắt đầu bằng
`gotham.` bị từ chối, tương tự việc mount một volume có tên bắt đầu bằng `gotham-`.
Hãy dùng luồng ứng dụng, cơ sở dữ liệu hoặc dịch vụ để tạo khối lượng công việc
được quản lý.

### Nhật ký trực tiếp

**Nhật ký** trên dòng container, hoặc nhấp vào dòng, mở ngăn nhật ký. Ngăn này
truyền đầu ra chuẩn và lỗi chuẩn của container từ agent.

| Điều khiển | Tác dụng |
|---|---|
| **Theo dõi** / **Đang theo dõi** | Giữ khung nhìn ở dòng mới nhất. Cuộn ra khỏi phần đuôi sẽ tắt chế độ này; nút bật lại sẽ nhảy về phần đuôi. |
| **Tạm dừng** / **Tiếp tục** | Đóng băng khung nhìn. Các dòng đến trong lúc tạm dừng được xếp hàng và nối theo thứ tự khi tiếp tục. |
| **Xóa** | Làm rỗng bộ đệm đã hiển thị. Không ảnh hưởng tới container hay node. |
| **Tải xuống** | Lưu các dòng hiện có trong khung nhìn thành tệp `.log` đặt tên theo id container, kèm mốc thời gian đang hiển thị. |

Ngăn này giữ **2.000** dòng mới nhất; các dòng cũ hơn bị bỏ. Huy hiệu trạng thái
báo `Streaming`, `Paused`, `Connecting`, `Reconnecting`, `Offline`, `Closed` hoặc
`Idle`, và một đăng ký bị từ chối hay luồng đang kết nối lại được hiện thành dòng
thông báo trong đầu ra. Luồng kết thúc khi container dừng.
