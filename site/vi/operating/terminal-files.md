---
title: Terminal và tệp
---

# Terminal và tệp

Có hai công cụ cấp máy chủ nằm trên registry node: **shell trên máy chủ** mở một
PTY trên node, và **trình quản lý tệp** duyệt node qua dịch vụ tệp của agent. Cả
hai đều là bề mặt của quản trị viên nền tảng. Chúng không cùng quyền với terminal
của container ứng dụng — loại này chỉ cần quyền ghi vào nhóm của ứng dụng, xem
[Ứng dụng](/vi/using/applications#cac-tab-chi-tiet).

## Shell trên máy chủ

Shell trên máy chủ chạy dưới tài khoản dịch vụ của agent với quyền của tài khoản
đó, nên thực tế là quyền quản trị toàn bộ máy chủ. Bạn có thể mở từ:

- trang **Terminal** (`/terminal`), nơi giữ nhiều phiên dưới dạng tab trên trang
  và không bao giờ khởi động shell khi tải;
- tab **Terminal** trên trang chi tiết node, khởi động một phiên cho node đó;
- cửa sổ terminal nổi, mở từ thanh terminal hoặc từ một tab trên trang.

Bảng điều khiển chỉ khởi động phiên trên node **Sẵn sàng**. Bộ chọn đích sau tab
`+` liệt kê những gì quản trị viên đang đăng nhập có thể mở: shell trên máy chủ
cho các node sẵn sàng, và container đang chạy của những ứng dụng mà quản trị viên
có quyền ghi. Các danh sách đều có giới hạn (50 node, 50 ứng dụng, 50 container
mỗi ứng dụng); một nguồn bị lỗi được báo thành lỗi có giới hạn thay vì làm rỗng
bộ chọn, và danh sách bị cắt sẽ nói rõ.

### Quy tắc phiên

- Phiên gắn với **người tạo và node**. Người dùng khác không thể gắn vào; một
  phiên không tồn tại và phiên của người khác trông giống hệt nhau.
- Phiên đã tạo phải được gắn trong vòng **60 giây**, nếu không sẽ hết hạn mà
  không có PTY.
- Phiên chỉ dùng một lần. Kết nối lại luôn có nghĩa là bắt đầu shell mới; lần gắn
  thứ hai vào cùng phiên bị từ chối.
- Một quản trị viên có thể giữ **4** phiên; control plane giữ **64** phiên tổng
  cộng.
- Một shell đã gắn sống tối đa **8 giờ**.
- Quyền được kiểm tra lại mỗi **30 giây**. Đăng xuất, thu hồi phiên, đổi tài khoản
  hoặc mất vai trò quản trị viên sẽ đóng shell trong khoảng thời gian đó. Một lần
  tra cứu lỗi tạm thời giữ shell và thử lại; lỗi lặp lại sẽ đóng shell.
- Đóng tab sẽ kết thúc shell và tiến trình đang chạy của nó.

Node chạy `$SHELL` khi đó là bash hoặc zsh, sau đó ưu tiên bash, rồi zsh, rồi
`/bin/sh`. `GOTHAM_AGENT_TERMINAL_SHELL` ghim một shell tuyệt đối cho một node.

## Quản lý tệp

Mở **Quản lý tệp** (`/files`) và chọn một node, hoặc dùng tab **Tệp** trên một
node. Khi chỉ có một node được quản lý, bộ chọn chuyển thẳng tới node đó. Bộ chuyển
node ở cuối trình duyệt liệt kê mọi node được quản lý; chỉ node **Sẵn sàng** chọn
được.

Trình quản lý tệp chỉ dành cho quản trị viên nền tảng ở cả hai đầu: tuyến của
bảng điều khiển được bảo vệ và mọi lệnh gọi API đều cần quản trị viên nền tảng,
bên cạnh việc thuộc nhóm của node và có vai trò nhóm `owner` hoặc `admin` để xem
nội dung và ghi. Chủ sở hữu nhóm không có quyền quản trị viên nền tảng sẽ nhận
`403`.

### Thư mục gốc

Mọi đường dẫn đều tương đối so với một thư mục gốc mà node mở ra; đường dẫn tuyệt
đối bị từ chối. Trình duyệt gắn nhãn mỗi thư mục gốc là **ghi được** hoặc
**chỉ đọc**, và hiển thị dung lượng trống của nó. Các thư mục gốc đến từ cấu hình
của agent:

| Thư mục gốc | Ghi được | Khi nào |
|---|---|---|
| `system` (`/`) | có | Chế độ toàn bộ hệ thống tệp, mặc định của agent. Trình cài đặt hỏi có bật trình quản lý tệp không và nếu có thì chạy agent dưới quyền root. |
| `volumes` | có | Chế độ giới hạn (`GOTHAM_AGENT_FILE_FULL_ACCESS=false`): thư mục volume được quản lý chứa các bind mount của ứng dụng. |
| `compose` | không | Chế độ giới hạn: cây dịch vụ compose. Chỉnh sửa sẽ bị lần compose up tiếp theo ghi đè. |
| Các thư mục gốc của quản trị viên từ `GOTHAM_AGENT_FILE_ROOTS` | không | Chế độ giới hạn: các thư mục bổ sung, phân tách bằng dấu hai chấm. |

Ở chế độ toàn bộ hệ thống tệp, thư mục cấp cao nhất của một ứng dụng trong thư mục
gốc volume được quản lý vẫn được bảo vệ khỏi đổi tên và xóa khi có container đang
mount nó. Giải thích chi tiết hơn về các chế độ, quyền sở hữu và quyền truy cập
nằm trong [Cài đặt](/vi/guide/install#dich-vu-tep-tu-xa).

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
  Tên đã tồn tại là `409` trừ khi chọn ghi đè.
- **Xóa vĩnh viễn** xóa một mục; **Làm trống thùng rác** xóa mọi mục trong thùng
  rác của thư mục gốc đó. Không thể hoàn tác cả hai.
- Các mục nằm trong thùng rác cho tới khi được khôi phục hoặc xóa. Không có hết
  hạn tự động. Thùng rác có hơn 5.000 mục chỉ hiện các mục mới nhất và nói rõ các
  mục cũ nhất bị lược bỏ.

### Tải xuống

**Tải xuống** tạo một ticket dùng một lần, **60 giây**, qua API đã xác thực, rồi
đưa URL của ticket cho trình duyệt như một lượt tải gốc. Tuyến tải xuống cố ý
không yêu cầu xác thực — một lượt tải gốc của trình duyệt không thể gửi bearer
token — nên ticket chính là thông tin xác thực, được gắn với người dùng, nhóm, máy
chủ, thư mục gốc và đường dẫn đã cấp, và bị dùng hết ở lần đầu. Phản hồi là
`no-store`; ticket nằm trong query string và không bao giờ được ghi vào access
log. Ticket hết hạn hoặc dùng lại trả `404`.

### Mở terminal tại đây

Với một thư mục, **Mở terminal tại đây** mở shell trên máy chủ với thư mục tương
đối so với gốc đó làm thư mục làm việc. Shell này là một cửa sổ terminal nổi,
không thuộc trình duyệt tệp, và chịu cùng quy tắc quản trị viên nền tảng như mọi
shell trên máy chủ. Một liên kết tượng trưng có đích ngoài thư mục gốc duyệt được
không dùng làm thư mục được. Hành động này không bao giờ mở shell container.

## Cờ tính năng

Bảng điều khiển gọi đây là các công tắc sản phẩm; còn biến môi trường mới là thứ
quản trị viên thực sự đặt.

| Cờ | Phạm vi | Mặc định | Khi tắt |
|---|---|---|---|
| `FEATURE_FILES` | Control plane | Bật | Các tuyến tệp vẫn được gắn nhưng mọi tuyến trả lời **503** với `{"message":"files are disabled","code":"disabled"}`, và bảng điều khiển hiện trạng thái “Quản lý tệp đang tắt”. Terminal trên máy chủ không bị ảnh hưởng. |
| `GOTHAM_AGENT_FILES` | Agent của một node | Bật trong agent; trình cài đặt ghi `false` trừ khi được bật | Không có dịch vụ tệp nào được đăng ký. Mọi lệnh gọi tệp cho node đó trả **501**, mà bảng điều khiển báo là “Hãy cập nhật agent trên node” dù công tắc mới là nguyên nhân thật. |
| `FEATURE_METRICS` | Control plane | Bật | Tuyến khoảng chỉ số không được gắn, nên bảng điều khiển nhận **404** và ẩn biểu đồ; mẫu từ nhịp tim không còn được lưu. Xem [Node và container](/vi/operating/nodes-containers#chi-so). |

`GOTHAM_AGENT_FILES` nhận `false`, `0`, `no` hoặc `off` là tắt; mọi giá trị khác
giữ dịch vụ bật. Câu trả lời của trình cài đặt là riêng: nó hỏi trước khi bật
trình quản lý tệp, mặc định là **không**, ghi `GOTHAM_AGENT_FILES=false` nếu
không, và nếu có thì ghi `true` kèm một drop-in dịch vụ chạy agent dưới quyền
root. Vì vậy một node chưa từng được bật sẽ không phục vụ tệp, đây là mặc định an
toàn cho cả cụm. Đặt biến trên node và khởi động lại agent là đủ để đổi câu trả
lời về sau.

Trình quản lý tệp và shell trên máy chủ là hai công tắc riêng:
`FEATURE_FILES=false` vẫn để terminal hoạt động, và `GOTHAM_AGENT_FILES=false`
không chặn shell trên máy chủ.
