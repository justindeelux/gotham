---
title: Terminal và tệp
---

# Terminal và tệp

Có hai công cụ cấp máy chủ nằm trên registry node: **shell trên máy chủ** mở một
PTY trên node, và **trình quản lý tệp** duyệt node qua dịch vụ tệp của agent. Cả
hai đều chỉ dành cho chủ sở hữu, giống terminal trong container ứng dụng — xem
[Ứng dụng](/vi/using/applications#cac-tab-chi-tiet).

## Shell trên máy chủ

Shell trên máy chủ chạy dưới tài khoản dịch vụ của agent với quyền của tài khoản
đó, nên thực tế là quyền quản trị toàn bộ máy chủ. Bạn có thể mở từ:

- trang **Terminal** (`/terminal`), nơi giữ nhiều phiên dưới dạng tab trên trang
  và không bao giờ khởi động shell khi tải;
- tab **Terminal** trên trang chi tiết node, khởi động một phiên cho node đó;
- cửa sổ terminal nổi, mở từ thanh terminal hoặc từ một tab trên trang.

Bảng điều khiển chỉ khởi động phiên trên node **Sẵn sàng**. Bộ chọn đích sau tab
`+` liệt kê những gì chủ sở hữu đang đăng nhập có thể mở: shell trên máy chủ
cho các node sẵn sàng, và container đang chạy của mọi ứng dụng. Các danh sách đều
có giới hạn (50 node, 50 ứng dụng, 50 container mỗi ứng dụng); một nguồn bị lỗi
được báo thành lỗi có giới hạn thay vì làm rỗng bộ chọn, và danh sách bị cắt sẽ
nói rõ.

### Quy tắc phiên

- Phiên gắn với **người tạo và node**. Người dùng khác không thể gắn vào; một
  phiên không tồn tại và phiên của người khác trông giống hệt nhau.
- Phiên đã tạo phải được gắn trong vòng **60 giây**, nếu không sẽ hết hạn mà
  không có PTY.
- Phiên chỉ dùng một lần. Kết nối lại luôn có nghĩa là bắt đầu shell mới; lần gắn
  thứ hai vào cùng phiên bị từ chối.
- Một chủ sở hữu có thể giữ **4** phiên; control plane giữ **64** phiên tổng
  cộng.
- Một shell đã gắn sống tối đa **8 giờ**.
- Quyền được kiểm tra lại mỗi **30 giây**. Đăng xuất, thu hồi phiên, đổi tài khoản
  hoặc mất vai trò chủ sở hữu sẽ đóng shell trong khoảng thời gian đó. Một lần
  tra cứu lỗi tạm thời giữ shell và thử lại; lỗi lặp lại sẽ đóng shell.
- Đóng tab sẽ kết thúc shell và tiến trình đang chạy của nó.

Node chạy `$SHELL` khi đó là bash hoặc zsh, sau đó ưu tiên bash, rồi zsh, rồi
`/bin/sh`. `GOTHAM_AGENT_TERMINAL_SHELL` ghim một shell tuyệt đối cho một node.

## Quản lý tệp

Mở **Quản lý tệp** (`/files`) và chọn một node, hoặc dùng tab **Tệp** trên một
node. Khi chỉ có một node được quản lý, bộ chọn chuyển thẳng tới node đó. Bộ chuyển
node ở cuối trình duyệt liệt kê mọi node được quản lý; chỉ node **Sẵn sàng** chọn
được.

Trình quản lý tệp chỉ dành cho chủ sở hữu ở cả hai đầu: tuyến của
bảng điều khiển được bảo vệ và mọi lệnh gọi API đều cần tài khoản chủ sở hữu.

### Thư mục gốc

Mọi đường dẫn đều tương đối so với một thư mục gốc mà node mở ra; đường dẫn tuyệt
đối bị từ chối. Trình duyệt gắn nhãn mỗi thư mục gốc là **ghi được** hoặc
**chỉ đọc**, và hiển thị dung lượng trống của nó. Các thư mục gốc đến từ cấu hình
của agent:

| Thư mục gốc | Ghi được | Khi nào |
|---|---|---|
| `system` (`/`) | có | Chế độ toàn bộ hệ thống tệp, mặc định của agent. Trình cài đặt control plane bật trình quản lý tệp trên `localhost` mặc định; một nút được thêm chọn tham gia bằng hộp kiểm trong wizard, và cả hai đều chạy agent dưới quyền root khi bật. |
| `volumes` | có | Chế độ giới hạn (`GOTHAM_AGENT_FILE_FULL_ACCESS=false`): thư mục volume được quản lý chứa các bind mount của ứng dụng. |
| `compose` | không | Chế độ giới hạn: cây dịch vụ compose. Chỉnh sửa sẽ bị lần compose up tiếp theo ghi đè. |
| Các thư mục gốc của quản trị viên từ `GOTHAM_AGENT_FILE_ROOTS` | không | Chế độ giới hạn: các thư mục bổ sung, phân tách bằng dấu hai chấm. |

Ở chế độ toàn bộ hệ thống tệp, thư mục cấp cao nhất của một ứng dụng trong thư mục
gốc volume được quản lý vẫn được bảo vệ khỏi đổi tên và xóa khi có container đang
mount nó. Giải thích chi tiết hơn về các chế độ, quyền sở hữu và quyền truy cập
nằm trong [Dịch vụ tệp từ xa](/vi/guide/file-service).

### Chia đôi khung nhìn

**Chia đôi khung nhìn** mở thêm một trình duyệt tệp bên cạnh trình duyệt đầu tiên.
Mỗi khung giữ riêng node, thư mục gốc, thư mục hiện tại, tab, lựa chọn, kiểu xem và
vị trí cuộn, nên một khung có thể đứng trên một server trong khi khung kia duyệt
một server hoặc thư mục gốc khác.

- Khung **đang hoạt động** được đánh dấu; nhấp hoặc nhấp đúp vào một khung sẽ biến
  nó thành khung hoạt động. Thao tác trên dòng, bàn phím và menu thư mục chỉ áp
  dụng cho khung đang hoạt động.
- Trên điện thoại hoặc cửa sổ hẹp, hai khung hiện lần lượt, kèm nút **Hiện khung
  trái** / **Hiện khung phải** để giữ cả hai ngữ cảnh.
- Có thể kéo vách ngăn hoặc di chuyển nó bằng bàn phím. Tỉ lệ này không được lưu:
  sau khi tải lại, hai khung mở ở tỉ lệ mặc định.
- Cửa sổ trình soạn thảo nổi đi theo khung đang hoạt động; đóng khung chia đôi vẫn
  giữ trình soạn thảo và bản nháp của nó.

### Duyệt và tìm kiếm

Trình duyệt có dạng danh sách và dạng lưới, sắp xếp, bộ lọc tên theo từng thư mục,
đường dẫn, các tab có thể đứng ở những thư mục gốc khác nhau, và tính kích thước
thư mục. Một lần liệt kê trả tối đa 5.000 mục mỗi trang; thư mục lớn hơn hiện
**Tải thêm** cho tới khi hết. Dung lượng trống được báo cho thư mục gốc đang chọn.

Tìm kiếm chỉ khớp **tên**, không phân biệt hoa thường, trong thư mục hiện tại, tùy
chọn gồm cả thư mục con. Một lần tìm bị giới hạn sẽ báo rằng đang hiển thị tập kết
quả đầu tiên thay vì nói danh sách đã đầy đủ.

Liên kết sâu dùng `?root=<name>&path=<relative/path>`. Chúng được kiểm tra với
danh sách thư mục gốc trực tiếp của node trước khi trình duyệt mở, và tab
**Lưu trữ** của ứng dụng dùng một liên kết như vậy để mở thư mục của một volume tại
đây. Đường dẫn tuyệt đối, một đoạn `..` hoặc thư mục gốc không tồn tại bị từ chối
và trình duyệt giữ nguyên ở thư mục hiện tại.

### Tải lên

**Tải lên** nhận tệp, không nhận thư mục, và gửi từng tệp một. Giới hạn là
**1 GiB mỗi tệp**, áp ở cả hai đầu: `FILES_MAX_UPLOAD_BYTES` trên control plane
(mặc định 1 GiB) và `GOTHAM_AGENT_FILE_MAX_UPLOAD` trên node (mặc định 1 GiB). Nội
dung vượt giới hạn của control plane trả `413` trước khi agent được gọi.

Mỗi tệp báo kết quả riêng, nên một lỗi không hủy cả lô. Tên đã tồn tại là `409` và
hộp thoại đưa ra **Thay thế** hoặc **Bỏ qua**; không có gì bị ghi đè khi bạn chưa
chọn. Một lượt tải lên bị ngắn hoặc bị hủy không bao giờ được ghi vào. Các tệp
tải lên vào thư mục đã có mục trùng tên theo cùng quy tắc xung đột. Khi ngân sách
truyền của node đã đầy (mặc định 8 lượt truyền đồng thời) hoặc vùng đệm của
control plane đầy, yêu cầu trả `429`; khi hệ thống tệp đích sắp hết chỗ, trả `507`.

### Tạo, đổi tên, di chuyển và sao chép

**Tạo mới** tạo một tệp hoặc thư mục; **Đổi tên** đổi một tên; **Sao chép** và
**Cắt** đổ vào một bộ nhớ tạm mà **Dán** áp dụng trong thư mục hiện tại. Mọi lượt
chuyển đều ở trong một thư mục gốc — bộ nhớ tạm lấy từ thư mục gốc khác bị từ
chối kèm thông báo, không được tự động chuyển đổi. Tên không được rỗng, là `.`,
`..`, chứa `/`, `\` hay một byte NUL, hoặc dài quá 255 byte; tên đã tồn tại là
`409`. Không thể di chuyển hay sao chép thư mục vào chính nó, và việc dán vào
thư mục mà các mục đang nằm trong đó bị từ chối.

### Sao chép sang node khác

**Sao chép sang node…** gửi các mục đã chọn tới một thư mục trên node được quản lý
khác; **Sao chép sang khung kia** dùng node, thư mục gốc và thư mục của khung kia
làm đích. Đích được chốt khi hộp thoại sao chép mở ra: di chuyển hoặc đổi khung
đích trong lúc đang sao chép không làm đổi đích. Đích phải là node **Sẵn sàng** có
thư mục gốc ghi được; thư mục gốc chỉ đọc bị từ chối.

- Một tệp thường duy nhất được truyền trực tiếp. Nhiều mục được chọn, hoặc một thư
  mục, được đóng thành một `tar.gz` tạm trên node nguồn; trình duyệt chỉ nhận sự
  kiện tiến trình, không bao giờ nhận tệp nén.
- Cùng node và cùng thư mục gốc dùng bản sao chép cục bộ thông thường. Sao chép
  giữa hai thư mục gốc trên cùng một node không được hỗ trợ, và không có cắt hay
  di chuyển giữa các node — thao tác này luôn là sao chép.
- Giới hạn được thương lượng giữa hai node: gói nén **1 GiB** (hoặc mức trần một
  tệp thấp hơn của control plane), **4 GiB** sau khi giải nén, **100.000** mục
  trong kho, **1.024** tên cấp cao nhất được chọn và **8** lượt truyền đồng thời
  mỗi node. Một tệp vượt mức trần của control plane làm bước kiểm tra đích thất
  bại với lý do "quá lớn" rõ ràng thay vì trả về giới hạn. Một lượt truyền chạy
  tối đa **30 phút**.
- Tên cấp cao nhất đã tồn tại, dù là tệp hay cả thư mục, là **xung đột** và bị bỏ
  qua. Không có gì bị ghi đè và thư mục không bao giờ được gộp đệ quy. Liên kết
  tượng trưng và tệp đặc biệt bị loại khỏi kho; báo cáo cho biết có bao nhiêu mục
  lồng nhau bị bỏ. Một lựa chọn không có gì truyền được bị từ chối ngay khi chuẩn
  bị, mục đó được đánh dấu thất bại và không tạo mục nào ở đích.
- Tiến trình đi qua **chuẩn bị**, **đang truyền**, **đang giải nén**, **đang xuất
  bản** và **đang dọn dẹp**. Hộp thoại liệt kê từng mục cấp cao nhất là đã sao
  chép, xung đột, bỏ qua, thất bại, bị hủy hoặc chưa chắc chắn.
- **Hủy** dừng lượt chạy và giữ các mục đích đã xác nhận. Một lượt bị hủy không
  gửi tổng kết cuối: chỉ các kết quả từng mục đã nhận là đáng tin, phần còn lại
  chưa được xác nhận. Hãy làm mới đích trước khi thử lại. Nếu mất xác nhận cuối,
  mục bị ảnh hưởng được báo là **chưa chắc chắn**, không bao giờ là đã sao chép,
  và không bao giờ được tự động thử lại; tệp tạm ở đích có thể còn tới khi chu kỳ
  dọn dẹp hết hạn xóa nó.
- Lượt sao chép chỉ đọc nguồn, không đổi hay xóa nguồn. Có thể sao chép từ thư mục
  gốc nguồn chỉ đọc. Làm mới danh sách đích sau khi chạy để thấy các mục đã xuất
  bản.
- Rời workspace hoặc đăng xuất khi đang sao chép sẽ hỏi trước và hủy lượt sao chép
  nếu bạn rời đi. Một lượt đã xong không được tiếp tục chạy nền, và lựa chọn không
  phải một giao dịch nguyên tử: một số mục cấp cao nhất có thể đã được xuất bản
  trong khi mục khác thất bại.

Đây là sao chép một chiều, không phải đồng bộ, lưu trữ chia sẻ hay bản sao lưu cơ
sở dữ liệu đang chạy. Không có tự động thử lại, tiếp tục, hoàn tất chạy nền hay
tính nguyên tử cho cả lựa chọn.

Thư mục được đóng gói giữ mode của từng mục. Một tệp sao chép trực tiếp nhận mode
tệp mặc định của node (`0644`).

### Quyền (chmod)

**Quyền…** sửa mode của một mục, hoặc theo dạng **bát phân** (ví dụ `644`), hoặc
theo các ô chọn đọc/ghi/thực thi cho chủ sở hữu, nhóm và khác.

- Chỉ các bit thấp `0o777` là sửa được. setuid, setgid và sticky được giữ nguyên
  nhưng không bao giờ đặt được ở đây, và một yêu cầu mang chúng bị từ chối.
- Thư mục phải giữ quyền thực thi của chủ sở hữu để còn truy cập được.
- Liên kết tượng trưng bị từ chối — không bao giờ đi theo liên kết để đổi mục
  tiêu của nó.
- Tệp đặc biệt (socket, FIFO, thiết bị) và thư mục gốc chỉ đọc bị từ chối.
- chmod không đệ quy và không bao giờ đổi loại mục.

### Xem và sửa

Tệp văn bản mở trong trình soạn thảo có tab, tìm và thay thế, đi tới dòng, ngắt
dòng, bản nháp giữ trong trình duyệt, và tô sáng cú pháp. Ảnh mở trong trình xem
có thu/phóng và di chuyển; các tệp nhị phân khác tải về được nhưng không sửa
được.

- Trình soạn thảo tải đầy đủ tối đa **2 MiB**. Tệp lớn hơn mở dưới dạng phần đuôi
  chỉ đọc (1.000 dòng cuối trong phạm vi 2 MiB) và không lưu được.
- Nội dung nhị phân hoặc UTF-8 không hợp lệ trả `415`; việc lưu bị từ chối thay vì
  làm hỏng tệp.
- Khi lưu, etag của tệp được kiểm tra. Nếu tệp đã thay đổi trên node sau khi mở,
  việc lưu bị từ chối với lỗi xung đột, và trình soạn thảo đưa ra lựa chọn tải
  lại bản trên máy chủ, ghi đè, hoặc lưu bộ đệm thành một bản sao.
- Bản lưu được ghi theo UTF-8, ASCII, GBK, GB2312 hoặc Big5; ký tự mà bảng mã
  đích không biểu diễn được sẽ bị từ chối.
- Mỗi lần lưu thành công lưu nội dung bị thay thế vào lịch sử tệp của control
  plane: tối đa **100** phiên bản và **32 MiB** nội dung cho mỗi tệp. Lịch sử có
  thể được so sánh, khôi phục hoặc xóa, và việc khôi phục lưu nội dung hiện tại
  trước.

### Nén và giải nén

**Nén…** tạo `zip`, `tar` hoặc `tar.gz` trên node; **Giải nén tại đây…** giải nén
một tệp. Cả hai chạy trong agent bằng các gói archive của Go, không bao giờ qua
shell.

- Nén bỏ qua liên kết tượng trưng và tệp đặc biệt.
- Giải nén từ chối các mục liên kết tượng trưng, liên kết cứng, thiết bị và FIFO,
  loại bỏ các bit mode đặc biệt, và không bao giờ ghi ra ngoài thư mục gốc đích.
- Tệp đã tồn tại được giữ nguyên trừ khi chọn **Thay thế tệp đã tồn tại**; thư mục
  đã tồn tại được dùng lại. Giải nén không có tính giao dịch: một lỗi giữa chừng
  để lại các mục đã giải nén trong thư mục đích.
- Giới hạn: tệp nén đầu vào 1 GiB, tổng 4 GiB sau giải nén, 100.000 mục, và tỷ lệ
  nén cố định 200:1. Tệp nén vượt giới hạn trả `413`; một thành viên bị từ chối
  trả `409`; tệp nén hỏng hoặc bị cắt trả `400`.

### Thùng rác và khôi phục

Xóa một mục không làm nó biến mất. Agent chuyển nó vào thùng rác của thư mục gốc
(`.gotham-trash`), nơi bị ẩn khỏi danh sách, tìm kiếm và kích thước thư mục, và
không thể truy cập bằng các thao tác tệp thông thường. **Thùng rác** liệt kê các
mục đã xóa, mới nhất trước, kèm tên, thời điểm xóa, kích thước và loại.

- **Khôi phục** đưa một mục về đường dẫn gốc đã ghi, hoặc tới một đích bạn chọn.
  Các thư mục cha bị thiếu được tạo lại, dùng mode đã ghi lúc mục vào thùng rác.
  Tên đã tồn tại là `409` trừ khi chọn ghi đè.
- **Xóa vĩnh viễn** xóa một mục; **Làm trống thùng rác** xóa mọi mục trong thùng
  rác của thư mục gốc đó. Không thể hoàn tác cả hai.
- Các mục nằm trong thùng rác cho tới khi được khôi phục, xóa, hoặc bị lần quét
  hết hạn của agent dọn đi: các mục cũ hơn
  `GOTHAM_AGENT_FILE_TRASH_MAX_AGE_DAYS` (mặc định 30 ngày; `0` giữ lại), và khi
  đặt `GOTHAM_AGENT_FILE_TRASH_MAX_BYTES`, các mục cũ nhất vượt mức trần đó.
  Thùng rác có hơn 5.000 mục chỉ hiện các mục mới nhất và nói rõ các mục cũ nhất
  bị lược bỏ. Xem [Dịch vụ tệp từ xa](/vi/guide/file-service).

### Tải xuống

**Tải xuống** tạo một ticket dùng một lần, **60 giây**, qua API đã xác thực, rồi
đưa URL của ticket cho trình duyệt như một lượt tải gốc. Tuyến tải xuống cố ý
không yêu cầu xác thực — một lượt tải gốc của trình duyệt không thể gửi bearer
token — nên ticket chính là thông tin xác thực, được gắn với máy chủ, thư mục gốc
và đường dẫn đã cấp, và bị dùng hết ở lần đầu. Phản hồi là
`no-store`; ticket nằm trong query string và không bao giờ được ghi vào access
log. Ticket hết hạn hoặc dùng lại trả `404`.

### Mở terminal tại đây

Với một thư mục, **Mở terminal tại đây** mở shell trên máy chủ với thư mục tương
đối so với gốc đó làm thư mục làm việc. Shell này là một cửa sổ terminal nổi,
không thuộc trình duyệt tệp, và chịu cùng quy tắc chủ sở hữu như mọi
shell trên máy chủ. Một liên kết tượng trưng có đích ngoài thư mục gốc duyệt được
không dùng làm thư mục được. Hành động này không bao giờ mở shell container.

## Cờ tính năng

Bảng điều khiển gọi đây là các công tắc sản phẩm; còn biến môi trường mới là thứ
chủ sở hữu thực sự đặt.

| Cờ | Phạm vi | Mặc định | Khi tắt |
|---|---|---|---|
| `FEATURE_FILES` | Control plane | Bật | Các tuyến tệp vẫn được gắn nhưng mọi tuyến trả lời **503** với `{"message":"files are disabled","code":"disabled"}`, và bảng điều khiển hiện trạng thái “Quản lý tệp đang tắt”. Terminal trên máy chủ không bị ảnh hưởng. |
| `GOTHAM_AGENT_FILES` | Agent của một node | Bật trong agent; trình cài đặt control plane bật nó trên `localhost`, wizard Thêm máy chủ mặc định tắt cho nút được thêm | Dịch vụ vẫn được đăng ký và mọi lệnh gọi tệp trả **409** `file_manager_disabled`, mà bảng điều khiển báo là “đang tắt trên node này”. Một agent cũ hơn dịch vụ tệp trả **501** `agent_outdated`. |
| `FEATURE_METRICS` | Control plane | Bật | Tuyến khoảng chỉ số không được gắn, nên bảng điều khiển nhận **404** và ẩn biểu đồ; mẫu từ nhịp tim không còn được lưu. Xem [Node và container](/vi/operating/nodes-containers#chi-so). |

`GOTHAM_AGENT_FILES` nhận `false`, `0`, `no` hoặc `off` là tắt; mọi giá trị khác
giữ dịch vụ bật. Trình cài đặt control plane bật trình quản lý tệp trên node
`localhost` của chính nó mặc định và ghi `GOTHAM_AGENT_FILES=true` kèm một
drop-in dịch vụ chạy agent dưới quyền root. Với một nút được thêm sau, hộp kiểm
của wizard **Thêm máy chủ** tắt mặc định và lệnh được tạo mang
`--no-file-manager` hoặc `--file-manager`. Đặt biến trong `/etc/gotham/agent.env`
trên node và khởi động lại agent là đủ để đổi câu trả lời về sau; bảng điều khiển
không có điều khiển cho việc này.

Trình quản lý tệp và shell trên máy chủ là hai công tắc riêng:
`FEATURE_FILES=false` vẫn để terminal hoạt động, và `GOTHAM_AGENT_FILES=false`
không chặn shell trên máy chủ.
