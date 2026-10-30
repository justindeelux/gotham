---
title: Proxy và DNS
---

# Proxy và DNS

Mỗi node chạy một container Traefik được quản lý. Control plane sinh toàn bộ cấu
hình của nó từ trạng thái của chính mình và đẩy tới node, nơi file provider nạp
cấu hình đó. Hai tài liệu được sinh ra: cấu hình tĩnh (các entrypoint và resolver
ACME) và cấu hình động (bộ định tuyến, dịch vụ, middleware). Việc sinh cấu hình là
một hàm thuần của trạng thái cơ sở dữ liệu, nên cùng một trạng thái luôn tạo ra
cùng những tệp.

Bề mặt trong bảng điều khiển là **Tên miền & SSL**. Danh sách bộ định tuyến, nhà
cung cấp DNS, chuyển hướng và lần đồng bộ thủ công cần tài khoản chủ sở hữu; cấu
hình chứng chỉ gắn với một ứng dụng theo quyền dự án của ứng dụng đó.

## Các bộ định tuyến được sinh

Tab **Bộ định tuyến** là khung nhìn chỉ đọc về những gì đang được phục vụ. Mỗi dòng
gộp cả bộ định tuyến HTTP và HTTPS của một máy chủ, và liệt kê:

- máy chủ và quy tắc khớp,
- nguồn: control plane, ứng dụng, dịch vụ compose hoặc chuyển hướng,
- node,
- các cổng vào (`web`, và thêm `websecure` khi TLS đang bật),
- resolver TLS, để trống với HTTP thường,
- trạng thái đồng bộ: `synced`, `pending` hoặc `unknown`.

Bộ định tuyến được sinh từ đợt triển khai **đang chạy** mới nhất của mỗi ứng dụng,
từ các dịch vụ compose có khai báo máy chủ, từ quy tắc chuyển hướng, và từ URL
control plane. Ứng dụng đã dừng hoặc chưa từng triển khai chưa có bộ định tuyến.
Một dòng không định tuyến được (không có đợt triển khai đang chạy, máy chủ trùng
lặp, thiếu cổng đã công bố) trở thành chẩn đoán cho ứng dụng đó thay vì làm hỏng
toàn bộ cấu hình của node.

Ở đây không có gì để sửa. Hãy đổi tên miền của ứng dụng, dịch vụ compose, quy tắc
chuyển hướng hoặc URL control plane, và lần đồng bộ tiếp theo sẽ sinh lại cấu hình.
`pending` nghĩa là trạng thái vừa sinh khác với bản đã đẩy lần cuối tới node đó
(hoặc chưa từng được đẩy).

## Khi nào đồng bộ thủ công

Hầu hết thay đổi tự đồng bộ:

- tạo, cập nhật hoặc xóa một ứng dụng, và mọi đợt triển khai đạt trạng thái đang
  chạy, đều kích hoạt đồng bộ lại node của nó;
- lưu một cấu hình chứng chỉ, một quy tắc chuyển hướng hoặc một nhà cung cấp DNS
  kích hoạt một lần đồng bộ lại theo kiểu nỗ lực tối đa;
- lưu một URL control plane mới sẽ đẩy tuyến của bảng điều khiển.

Đồng bộ thủ công là đường sửa chữa, không phải đường thường lệ. Dùng nó khi:

- tab Bộ định tuyến hiện `pending` sau một lần đồng bộ lại thất bại hoặc bị gián
  đoạn;
- một node ngoại tuyến hoặc không kết nối được trong lúc thay đổi được lưu, và sau
  đó đã trở lại;
- một node được thêm hoặc đăng ký lại và cấu hình của nó chưa từng được đẩy.

Bảng điều khiển không có nút đồng bộ. Đồng bộ là thao tác API của chủ sở hữu:
`POST /api/v1/proxy/sync`, với `server_id` tùy chọn để đồng bộ một node. Không có
body thì nó đồng bộ mọi node đã đăng ký và mọi node đang chạy một ứng dụng được
proxy. Phản hồi liệt kê kết quả của từng node và các chẩn đoán nếu có. Một yêu cầu
`revert` đẩy lại phiên bản cấu hình đã lưu trước đó của node thay vì sinh từ trạng
thái hiện tại; một phiên bản bị thay thế vẫn hoàn tác được trong một ngày kể từ lúc
bị thay thế.

## Chứng chỉ

Chứng chỉ được ghi lại dưới dạng ý định, mỗi tên miền một bản; node thực hiện đơn
ACME và giữ chứng chỉ trong bộ nhớ của chính nó. Một tuyến có chứng chỉ đang hoạt
động sẽ trả lời HTTPS và nhận chuyển hướng HTTP sang HTTPS dùng chung. Tuyến không
có chứng chỉ vẫn là HTTP thường.

| Thử thách | Dùng khi | Yêu cầu |
|---|---|---|
| `http-01` | Mặc định. | Máy chủ phân giải về node và cổng 80 truy cập được. Một bản ghi Cloudflare được proxy phải để DNS only. |
| `dns-01` | Cổng 80 không dùng được, hoặc bạn cần wildcard. | Một nhà cung cấp DNS có vùng đã cấu hình bao phủ tên miền. Wildcard chỉ có được theo cách này. |

Một cấu hình `dns-01` phải nêu một nhà cung cấp, và nhà cung cấp đó phải giữ một
vùng bao phủ tên miền (và gốc của wildcard). Một nhà cung cấp đã cấu hình nhưng
đang tắt, hoặc một ý định mà tên miền ứng dụng không còn khớp, được báo thành chẩn
đoán cho từng ứng dụng và tuyến quay về HTTP.

Trạng thái được quan sát trực tiếp từ node và control plane không bao giờ lưu nó:
`present` nghĩa là bộ nhớ ACME của node có chứng chỉ cho tên miền và ngày hết hạn
được hiển thị, `absent` nghĩa là đã đọc bộ nhớ và không có, còn `unknown` nghĩa là
không đọc được node. `unknown` không phải một đơn hàng thất bại. Tên miền được ghi
là tên miền chính của ứng dụng tại thời điểm lưu, nên sau khi đổi tên miền chính,
hãy lưu lại cấu hình chứng chỉ.

### Chứng chỉ của bảng điều khiển

Khi URL control plane là HTTPS, hostname của nó tự động có một ý định chứng chỉ
riêng. Nó thuộc về instance, không thuộc về ứng dụng: nó không được tạo hay sửa từ
biểu mẫu chứng chỉ theo ứng dụng, và được đồng bộ lại từ URL thay vì bị xóa khỏi
danh sách chứng chỉ ứng dụng. Đổi hoặc xóa URL sẽ thêm hoặc gỡ bỏ nó. Trạng thái
của nó hiện trong [Cài đặt và cài đặt máy chủ của nút](/vi/operating/instance-network) ở phần trạng
thái tên miền control plane. Với URL HTTP thường thì không có chứng chỉ bảng điều
khiển và trạng thái ghi là “not applicable”.

## Nhà cung cấp DNS

DNS-01 cần một thông tin xác thực API cho nhà cung cấp giữ vùng. Hai loại nhà cung
cấp được hỗ trợ: **Cloudflare** và **DigitalOcean**. Mỗi mục có tên, một hoặc nhiều
vùng, một thông tin xác thực và một công tắc bật. Mỗi loại chỉ cho phép một nhà
cung cấp đang bật.

Thông tin xác thực chỉ ghi được kể từ lúc lưu:

- nó được gửi một lần qua API và niêm phong phía máy chủ;
- API không bao giờ trả về nó, và bảng điều khiển có thể đặt hoặc xoay vòng nhưng
  không thể hiển thị hay sao chép nó;
- để trống thông tin xác thực khi sửa sẽ giữ bản đã lưu; nhập giá trị mới sẽ xoay
  vòng nó;
- việc xóa một nhà cung cấp bị từ chối khi có cấu hình chứng chỉ đang tham chiếu
  tới nó.

Các vùng được ghi lại, không được xác minh. Máy chủ tên của vùng phải thực sự được
ủy quyền cho nhà cung cấp, nếu không đơn ACME sẽ thất bại — control plane không
kiểm tra ủy quyền giúp bạn.

Thông tin xác thực DNS chỉ đến node dưới dạng biến môi trường cho container proxy,
và chỉ cho những nhà cung cấp được một chứng chỉ đang hoạt động trên node đó tham
chiếu. Chúng không bao giờ đi vào các tài liệu được sinh hay lịch sử cấu hình, và
các giá trị được lọc khỏi lỗi và nhật ký trên đường đẩy đi.

## Chuyển hướng

Một quy tắc chuyển hướng gửi một máy chủ nguồn chính xác tới một máy chủ đích chính
xác, với ý định vĩnh viễn (301) hoặc tạm thời (302) và tùy chọn giữ đường dẫn.
`GET` trả lời mã đã lưu; `HEAD` và các phương thức khác trả lời 308/307 để giữ
nguyên phương thức. Quy tắc do một ứng dụng sở hữu và không thể di chuyển sau khi
tạo.

Máy chủ nguồn là duy nhất trên toàn hệ thống và không bao giờ che khuất tên miền
của một ứng dụng. Một đích không thể là nguồn của quy tắc đang bật khác và một
nguồn không thể là đích của quy tắc đang bật khác. Các lần ghi xung đột bị từ chối,
và bộ sinh giữ lại một quy tắc sẽ tạo thành chuỗi, nên cấu hình node phục vụ luôn
không có chuỗi. Đích phải tự phục vụ chứng chỉ của nó; một chuyển hướng kết thúc
yêu cầu và không bao giờ proxy tới backend.

Lưu một quy tắc sẽ áp dụng nó ở lần đồng bộ cấu hình động tiếp theo — không cần
triển khai lại. Nếu lần đồng bộ không tới được node, quy tắc vẫn được lưu và được
áp dụng ở lần node hội tụ tiếp theo; xem
[Khi nào đồng bộ thủ công](/vi/operating/proxy-dns#khi-nao-đong-bo-thu-cong).

Để xem góc nhìn phía ứng dụng về máy chủ, tên miền và bản xem trước, xem
[Tên miền và bản xem trước](/vi/using/domains-previews).
