---
title: Bảo mật
---

# Bảo mật

Gotham tách ba thứ dễ bị nhầm lẫn: vai trò của một người trong nhóm, quyền quản trị
viên nền tảng với chính instance, và các phạm vi của một API token. Agent trên node
xác thực qua một kênh riêng, và thông tin xác thực đã lưu được mã hóa khi lưu trữ.

## Vai trò trong nhóm

Mọi tài khoản đều hoạt động trong một nhóm đang hoạt động. Thành viên nhóm có đúng
một trong ba vai trò; không có vai trò `member`.

| Vai trò | Có thể làm | Không thể làm |
|---|---|---|
| `owner` | Mọi thứ trong nhóm, kể cả xóa nhóm và quản lý các chủ sở hữu. | — |
| `admin` | Quản lý tài nguyên và thành viên. | Gỡ hoặc giáng cấp một chủ sở hữu. |
| `read_only` | Đọc tài nguyên và thành viên. | Tạo, cập nhật hoặc xóa bất cứ thứ gì. |

`read_only` được máy chủ áp đặt trên mọi thao tác thay đổi, không chỉ bằng cách ẩn
nút. Một số nút trong bảng điều khiển vẫn hiện với thành viên chỉ đọc và bị từ chối
kèm lỗi quyền khi dùng; đó là dự kiến, và API mới là nơi có thẩm quyền.

Vai trò tính theo từng nhóm. Chủ sở hữu nhóm không phải quản trị viên nền tảng, và
là quản trị viên nền tảng cũng không cấp quyền vào một nhóm mà tài khoản không
thuộc về. Các tài nguyên gắn với node (container, tệp, hàng đợi của Kho ứng dụng)
cũng kiểm tra nhóm của node và vai trò của người gọi.

## Quản trị viên nền tảng

Quyền quản trị viên nền tảng gác các bề mặt toàn instance: cài đặt instance, đồng
bộ proxy toàn cục và nhà cung cấp DNS, áp dụng bản cập nhật, trình quản lý tệp trên
máy chủ, terminal trên máy chủ, và đợt triển khai agent ra toàn cụm. Một yêu cầu
được chấp nhận khi nó là:

- một API token mang phạm vi `admin`,
- một phiên có claim vai trò là `admin`, hoặc
- một phiên có email tài khoản nằm trong `PLATFORM_ADMINS`.

Điều này an toàn theo mặc định: không liệt kê ai thì không ai có quyền quản trị
viên nhờ địa chỉ email, và hai đường còn lại vẫn áp dụng. Tài khoản đầu tiên trên
một instance mới là quản trị viên nền tảng; các tài khoản sau không có quyền đó trừ
khi claim vai trò của họ là `admin`, họ giữ một token phạm vi `admin`, hoặc email
của họ được liệt kê. Việc tạo một token phạm vi `admin` cũng chỉ quản trị viên nền
tảng làm được.

## API token

API token là thông tin xác thực không đọc được với một tiền tố cố định, quản lý qua
API (xem [API token](/vi/reference/api-tokens)). Chúng mang một hoặc nhiều phạm vi:

| Phạm vi | Cấp quyền |
|---|---|
| `read` | Các yêu cầu đọc (`GET`, `HEAD`) trên API tài nguyên. Mặc định khi không yêu cầu phạm vi nào. |
| `deploy` | Mọi thao tác ghi, cộng các lần đọc chứa bí mật như thông tin xác thực cơ sở dữ liệu và nội dung tệp. |
| `admin` | Các thao tác toàn nền tảng, gồm đồng bộ proxy, nhà cung cấp DNS và cài đặt của quản trị viên. |

Các phạm vi có thứ bậc: `admin` bao `deploy`, và `deploy` bao `read`. Một token chỉ
tạo hoặc xoay vòng được token có phạm vi ngang hoặc thấp hơn phạm vi của chính nó,
nên token `deploy` không tạo được token `admin`. Token không bao giờ mở rộng vai
trò nhóm của người sở hữu — việc kiểm tra nhóm vẫn chạy.

Bí mật chỉ hiện một lần, khi tạo hoặc xoay vòng. Chỉ một hash được lưu; việc liệt
kê token trả về siêu dữ liệu (phạm vi, lần dùng cuối, thu hồi) và không bao giờ trả
bí mật. Xoay vòng thay bí mật và vô hiệu hóa bản cũ ngay lập tức; thu hồi là vĩnh
viễn. Hãy cấp cho token phạm vi hẹp nhất cần thiết, ưu tiên `read` cho bảng điều
khiển và `deploy` cho một tác vụ CI.

## Kênh node

Control plane chạy một tổ chức phát hành chứng chỉ. Nó cấp chứng chỉ cho listener và
một chứng chỉ máy khách cho mỗi node đã đăng ký, và agent trên node nói chuyện với
control plane qua mutual TLS. Một instance production bắt buộc phải có CA; không có
kênh agent dạng văn bản thường nếu không có lựa chọn phát triển rõ ràng.

Ngoài TLS, mỗi kết nối được gắn với một định danh node:

- **Đăng ký ghim khóa.** Dấu vân tay khóa công khai của node được ghi lại khi node
  đăng ký. Mọi RPC và luồng sau đó đều kiểm tra rằng khóa của chứng chỉ được trình
  khớp với dấu vân tay đã ghi của node đó và node vẫn còn tồn tại.
- **CSR phải dùng cùng khóa.** Lần đăng ký đầu tiên kèm một yêu cầu ký chứng chỉ bị
  từ chối khi khóa trong CSR khác với chứng chỉ đã xác thực.
- **Việc từ chối không làm lộ registry.** Một node không tồn tại, một khóa không
  khớp và một node đã bị gỡ đều nhận cùng một lời từ chối; lý do cụ thể chỉ được ghi
  log trên control plane.
- **Xóa node là gỡ bỏ quyền của nó.** Chỉ chứng chỉ thì không còn xác thực được sau
  khi dòng registry của node đã biến mất.

### Đăng ký lần đầu

Lần đăng ký đầu tiên được gác bởi một enrollment token, thông tin xác thực khởi tạo
mà quản trị viên truyền cho trình cài đặt trên node. Nó được so sánh trong thời
gian hằng, và một token thiếu với một token sai cho cùng một lỗi. Khi chưa cấu hình
token nào, control plane sinh một token ở lần khởi động đầu và từ chối đăng ký nếu
không có nó — việc đăng ký thất bại theo hướng an toàn. Đăng ký kiểu
trust-on-first-use mở chỉ tồn tại như một lựa chọn rõ ràng của quản trị viên, được
ghi log rất rõ, và không phù hợp cho một instance thật.

Hãy giữ token ngoài lịch sử shell, image và ticket. Một token bị lộ cho phép một
node mới đăng ký; hãy gỡ node giả đó và xoay vòng token.

### Ngừng chấp nhận chứng chỉ cũ

Các node đăng ký trước khi có mutual TLS giữ một chứng chỉ chỉ hợp lệ cho việc xác
thực máy chủ. Gateway chấp nhận chứng chỉ lá như vậy tới một ngày kết thúc để cụm
hiện có tiếp tục hoạt động trong lúc nâng cấp, và ghi log ngày đó cùng số node vẫn
còn trình chứng chỉ ấy. Ngày kết thúc tích hợp là 2027-01-15 và có thể dời hoặc tắt;
một ngày kết thúc bị tắt khiến chứng chỉ lá yếu hơn được chấp nhận vô thời hạn. Hãy
đăng ký lại các node còn lại trước ngày đó — không còn chứng chỉ lá cũ là trạng thái
khỏe mạnh.

## Bí mật khi lưu trữ

Thông tin xác thực đã lưu được mã hóa bằng AES-256-GCM trước khi vào cơ sở dữ liệu:
một nonce mới cho mỗi giá trị, có xác thực để một giá trị bị hỏng hoặc sai khóa
không mở được thay vì giải mã ra rác. Giá trị thông tin xác thực không được trả về
trong các phản hồi liệt kê; một tính năng chỉ mở nó khi cần dùng hoặc cần hiển thị.

Điều này bao phủ dữ liệu chứa thông tin xác thực mà nền tảng lưu: khóa triển khai
Git và token HTTPS, bí mật của nhà cung cấp Git đã kết nối, thông tin xác thực cơ sở
dữ liệu, thông tin xác thực sao lưu S3, bí mật của kênh thông báo, token API DNS, và
biến của ứng dụng cùng dự án.

Khóa mã hóa đến từ `GOTHAM_SECRET_KEY`. Khi biến này chưa đặt, control plane dùng
một khóa bền được sinh ở lần khởi động đầu để các giá trị đã niêm phong sống qua lần
khởi động lại; chỉ khi khóa đó không lưu được thì nó mới dùng một khóa tạm, ghi cảnh
báo, và để thông tin xác thực đã lưu không đọc được sau khi khởi động lại. Hãy đặt
khóa rõ ràng ở môi trường production, giữ nó ngoài kho mã, và sao lưu nó cùng cơ sở
dữ liệu: mất một trong hai thì các giá trị đã niêm phong không thể khôi phục, và đổi
khóa mà không di trú dữ liệu cũng vậy.

Token phiên được ký bằng khóa Ed25519. Không có cặp khóa được cấu hình thì một cặp
mới được sinh lúc khởi động, nên phiên không sống qua lần khởi động lại; điều đó
tiện cho phát triển và không phù hợp cho production.

## Cờ tính năng

Mười công tắc môi trường tắt các bề mặt tùy chọn. Tất cả mặc định **bật**: giá trị
chưa đặt hoặc rỗng giữ module, và chỉ giá trị `false` rõ ràng (không phân biệt hoa
thường) mới tắt nó. Chúng được đọc khi control plane khởi động, nên đổi giá trị cần
khởi động lại, và mỗi module bị tắt ghi một dòng cảnh báo vào log khởi động kèm tên
cờ của nó.

| Cờ | Mặc định | Tắt thì mất gì |
|---|---|---|
| `FEATURE_APPLICATIONS` | bật | API ứng dụng và triển khai: tạo, triển khai, hoàn tác, nhật ký, môi trường, tên miền. Dự án và môi trường vẫn còn. |
| `FEATURE_PREVIEWS` | bật | Các đợt triển khai xem trước cho pull request, danh sách của chúng, và tiến trình dọn bản mồ côi. Webhook push vẫn hoạt động. |
| `FEATURE_DATABASES` | bật | Vòng đời cơ sở dữ liệu được quản lý, thông tin xác thực, các tuyến sao lưu và khôi phục, và bộ lập lịch sao lưu. |
| `FEATURE_SERVICES` | bật | Dịch vụ compose và danh mục template một chạm. |
| `FEATURE_TEAMS` | bật | Các tuyến nhóm và lời mời. Các tuyến tài nguyên thay vào đó phân giải qua nhóm cá nhân của từng tài khoản. |
| `FEATURE_FILES` | bật | API quản lý tệp từ xa, trả lời 503 khi tắt. |
| `FEATURE_NOTIFICATIONS` | bật | Kênh thông báo, gửi thử, log gửi đi, và các móc thông báo triển khai/sao lưu. |
| `FEATURE_PROXY` | bật | Đồng bộ proxy, danh sách bộ định tuyến, cấu hình chứng chỉ, chuyển hướng, nhà cung cấp DNS, đồng bộ lại chứng chỉ bảng điều khiển, và móc triển khai. Các node giữ cấu hình cuối cùng đã được đẩy. |
| `FEATURE_METRICS` | bật | Tuyến chỉ số của node, việc lưu chỉ số theo nhịp tim, và tiến trình thanh lọc giữ 30 ngày. Biểu đồ chỉ số để trống. |
| `FEATURE_UPDATES` | bật | Bề mặt cập nhật trong bảng điều khiển: kiểm tra, lập lịch, áp dụng, và vòng lặp cập nhật tự động. CLI `gotham update` vẫn dùng được. |

Tắt một module sẽ gỡ các tuyến của nó thay vì để lại một bề mặt hoạt động nửa vời;
ngoại lệ là API tệp, với các tuyến vẫn được gắn nhưng trả lời 503.

## Trách nhiệm của quản trị viên

- Cấu hình `GOTHAM_SECRET_KEY` và cặp khóa ký phiên, và giữ bản sao lưu của chúng
  cùng bản sao lưu cơ sở dữ liệu.
- Giữ tài liệu của tổ chức phát hành chứng chỉ ở chế độ riêng tư và có sao lưu;
  không có nó thì không thể đăng ký lại agent hay khởi động lại listener một cách an
  toàn.
- Giới hạn `PLATFORM_ADMINS` cho những tài khoản thực sự vận hành instance, và cấp
  token có phạm vi thay vì token admin.
- Loại bỏ chứng chỉ lá cũ của node trước ngày kết thúc.
- Giữ control plane sau TLS và phục vụ bảng điều khiển qua HTTPS, để chứng chỉ bảng
  điều khiển và TLS của ứng dụng cùng hoạt động.

Các tệp phát hành được ký và xác minh tự động; xem
[Cập nhật](/vi/guide/updates) để biết mô hình ký bản phát hành.
