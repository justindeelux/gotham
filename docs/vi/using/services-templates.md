---
title: Dịch vụ và template
---

# Dịch vụ và template

Một dịch vụ là một dự án Docker Compose mà Gotham chạy thay bạn trên một node. Bạn
cung cấp `compose.yaml` và một môi trường cho việc thay thế `${VAR}`; control plane
lưu cả hai, dựng tài liệu, và gửi nó tới agent của node, nơi chạy nó bằng CLI
`docker compose` của chính node.

Template là con đường không cần YAML để tới cùng kết quả: một stack dựng sẵn cộng
một biểu mẫu, được dựng thành một tài liệu compose và một bảng biến môi trường, rồi
được tạo thành một dịch vụ.

Nếu bạn muốn triển khai mã nguồn hoặc một image duy nhất với các tính năng ứng dụng
của Gotham (triển khai, quay lui, tên miền theo từng ứng dụng), hãy xem
[Ứng dụng](/vi/using/applications) thay vào đó.

## Một dịch vụ là một dự án compose

Mỗi dịch vụ đúng là một dự án compose trên đúng một node. Tên dự án của nó được
sinh từ id dịch vụ (`gotham-<id>`), nên hai dịch vụ không bao giờ dùng chung
container hay volume.

Tài liệu được lưu chính là tài liệu bạn viết, nguyên văn. Nó được kiểm tra trước
khi lưu, nên một dịch vụ đã lưu luôn dựng được. Đường triển khai chấp nhận tập con
compose thông dụng:

- Các dịch vụ phải tham chiếu một **image**; không có `build`, `extends`,
  `env_file` hay `include`.
- Một tài liệu bị giới hạn ở 1 MiB và 256 dịch vụ; môi trường của một dịch vụ ở
  256 biến.
- Volume có tên là kho lưu trữ của dự án. Chúng được đặt tên theo tên dự án, và
  dừng hay xóa dự án vẫn giữ chúng. Volume ẩn danh không được gắn lại bởi lần `up`
  sau, nên hãy đặt dữ liệu cần giữ vào một volume có tên.
- Tiền tố `gotham-db-` được dành riêng cho cơ sở dữ liệu được quản lý và không thể
  được một dịch vụ khai báo hay tham chiếu.

### Môi trường và nội suy

Môi trường của dịch vụ là đầu vào thay thế cho các tham chiếu `${VAR}` trong tài
liệu. Việc dựng diễn ra trên control plane trước khi node kiểm tra kết quả, và hỗ
trợ các dạng compose:

- `$VAR` và `${VAR}`
- `${VAR:-default}`, `${VAR-default}`
- `${VAR:?message}`, `${VAR?message}`
- `${VAR:+alternative}`, `${VAR+alternative}`
- `$$` cho một ký tự đô la theo nghĩa đen

Một biến chưa đặt nằm ngoài dạng mặc định hoặc dạng lỗi là lỗi kiểm tra, và các
tham chiếu lồng nhau như `${A:-${B}}` không được hỗ trợ. Vì các giá trị được thay
thế trước khi tài liệu rời control plane, lần chạy compose trên node không nội suy
thêm lần nữa.

Các giá trị môi trường được coi là bí mật: chúng bị che trên bảng điều khiển và bị
biên tập khỏi lỗi đã lưu và các dòng lịch sử triển khai. Bí mật bạn gõ trực tiếp vào
YAML là nội dung của bạn và không bị biên tập.

### Tên miền

Một dịch vụ compose được định tuyến bằng nhãn, không bằng một trang tên miền riêng:

```yaml
services:
  web:
    image: nginx:1.27
    labels:
      gotham.domain: app.example.com
      gotham.domain.port: "80"
```

`gotham.domain` được kiểm tra theo cùng quy tắc như tên miền của ứng dụng, và
`gotham.domain.port` mặc định là 80. Bản thân nhãn tên miền có thể là một tham chiếu
`${VAR}`. Xem [Tên miền và bản xem trước](/vi/using/domains-previews) để biết về DNS
và chứng chỉ, và [Proxy và DNS](/vi/operating/proxy-dns) cho phía người vận hành.

## Tạo dịch vụ

Dùng **Thêm tài nguyên → Dịch vụ**, rồi chọn một template hoặc
**Nhập tệp compose**. Việc nhập hỏi tên, node và tài liệu; môi trường được sửa sau
đó trên trang chi tiết.

Một dịch vụ tạo từ template nhớ slug của nó: API trả về nó dưới dạng `template`
trong JSON của dịch vụ, và `POST /api/v1/services` nhận một slug `template` tùy
chọn, slug này phải tồn tại trong danh mục (slug không rõ là lỗi `400`). Việc nhập
compose không gửi `template` và đọc về giá trị rỗng.

Việc lưu kiểm tra tài liệu với môi trường. Một thay đổi chỉ cập nhật tài liệu đã lưu
— dự án đang chạy vẫn chạy cho đến lần triển khai tiếp theo.

Tạo từ template là một thao tác: **Tạo và triển khai** lưu dịch vụ và bắt đầu
triển khai trong nền, đóng wizard rồi mở trang chi tiết dịch vụ; trang này theo dõi
lần triển khai cho tới khi `running` hoặc `failed`. Nhập tệp compose chỉ tạo dịch vụ;
hãy triển khai từ trang chi tiết.

## Triển khai, khởi động lại, dừng, xóa

- **Triển khai** dựng tài liệu đã lưu, để node kiểm tra nó bằng
  `docker compose config`, khởi động dự án và ghi một dòng triển khai. API nhận
  lệnh với `202` và chạy nền, nên phản hồi không chờ tải image: dòng triển khai đi
  từ `deploying` sang `running` hoặc `failed`, và trang chi tiết dịch vụ tự đọc lại
  cho tới trạng thái cuối.
- **Khởi động lại** khởi động lại một dự án đang chạy tại chỗ và khởi động một dự
  án đã dừng.
- **Dừng** đưa dự án xuống bằng `compose down`. Volume có tên giữ nguyên từng byte,
  nên một dịch vụ đã dừng hoàn toàn khôi phục được bằng một lần triển khai hoặc
  khởi động lại.
- **Xóa** dừng dự án và xóa mềm dịch vụ. Volume có tên vẫn ở trên node.

Một thao tác vòng đời phát ra khi lần triển khai còn chạy sẽ bị từ chối với xung
đột (`409`) thay vì chờ triển khai xong. Nếu control plane khởi động lại giữa lúc
triển khai, lần triển khai dang dở được ghi là thất bại ("interrupted") khi khởi
động và dịch vụ còn `creating` được chuyển sang `error`, nên không có gì kẹt mãi.

Hợp đồng API khớp với giao diện: `POST /api/v1/services/{id}/deploy` trả `202
Accepted` kèm dịch vụ và dòng triển khai mới; client polling
`GET /api/v1/services/{id}` và `GET /api/v1/services/{id}/deploys` cho tới khi lần
triển khai mới nhất là `running` hoặc `failed`. `POST /api/v1/services` nhận tùy
chọn `"deploy": true` (wizard template dùng nó) để tạo và bắt đầu trong một lần
gọi; API trả `201 Created` kèm dịch vụ trong khi triển khai tiếp tục ở nền.

## Container và nhật ký

Thẻ **Container** liệt kê các container của dự án theo báo cáo của agent trên node:
compose service, tên container, image, trạng thái, tình trạng và sức khỏe. Việc đọc
danh sách là một lệnh gọi theo yêu cầu, nên nó phản ánh node tại thời điểm bạn làm
mới. Nếu không với tới agent, thẻ hiển thị lỗi (và lần đọc thành công gần nhất, nếu
có), chứ không hiển thị một dự án trống.

**Nhật ký** truyền `docker compose logs -f` qua agent trên node. Chọn mọi dịch vụ
hoặc một compose service; luồng có thể tạm dừng, theo dõi hoặc xóa. Chỉ có luồng
trực tiếp — không có tệp log đã lưu để tải về — và API mặc định lấy 200 dòng cuối
khi không yêu cầu số dòng cụ thể. Nội dung log đi qua nguyên vẹn; lỗi luồng ở
terminal được biên tập giống mọi lỗi node khác.

## Lịch sử triển khai

Mỗi lần thử triển khai được ghi lại với trạng thái (`deploying`, `running`,
`failed`), lỗi đã biên tập và dấu thời gian. Lịch sử hiển thị 50 lần thử mới nhất,
mới nhất trước. Tài liệu đã dựng của mỗi lần triển khai được lưu cùng lần thử đó.

API ghi một dòng cho mỗi lần thử và không cung cấp tiến trình từng bước, nên không
có dòng thời gian theo bước: lịch sử cộng với luồng log trực tiếp là toàn bộ bức
tranh. Một lỗi khi khởi động dự án cũng đặt trạng thái dịch vụ thành `error` cho đến
lần triển khai thành công tiếp theo.

## Di chuyển dịch vụ

Thẻ **Máy chủ và môi trường** đổi môi trường và node của dịch vụ.

- Chuyển sang môi trường khác vẫn ở trong dự án. Tên phải còn trống ở đó, nếu không
  việc di chuyển bị từ chối như một xung đột.
- Node chỉ có thể đổi trước lần triển khai đầu tiên. Khi dự án đã chạy trên một
  node thì node bị ghim, và chuyển sang môi trường khác vẫn giữ nguyên node.

## Template

Danh mục dựng sẵn có bốn template. Mỗi template là một stack cố định; không có dịch
vụ tùy chọn nào để bật tắt lúc dựng.

| Template | Stack | Trường bắt buộc |
| --- | --- | --- |
| WordPress | `wordpress:6-apache` + `mysql:8.4` | tên miền, mật khẩu cơ sở dữ liệu, mật khẩu root cơ sở dữ liệu |
| Nextcloud | `nextcloud:31-apache` + `postgres:16-alpine` + `redis:7-alpine` | tên miền, mật khẩu quản trị, mật khẩu cơ sở dữ liệu, mật khẩu Redis |
| n8n | `n8nio/n8n:1` | tên miền, khóa mã hóa |
| Uptime Kuma | `louislam/uptime-kuma:1` | tên miền |

Mỗi template mang theo các volume có tên cho dữ liệu phải sống qua một lần khởi
động lại. Các trường khác (tên và người dùng cơ sở dữ liệu, người dùng quản trị,
múi giờ, mức log, chẩn đoán và thời gian lưu giữ thực thi của n8n, múi giờ của
Uptime Kuma) có giá trị mặc định và có thể để nguyên. Mọi template định tuyến một
dịch vụ trên tên miền bạn nhập.

### Tạo giá trị bí mật

Các trường bí mật được đánh dấu có thể tạo sẽ hiện nút **Tạo ngẫu nhiên** bên cạnh
ô nhập. Nút này điền một giá trị gồm 24 chữ cái và chữ số, được tạo ngay trong trình
duyệt; không có gì được gửi đi trước khi bạn tạo dịch vụ. Dùng **Sao chép** để lưu
giá trị ở nơi an toàn — bí mật chỉ ghi được một lần sau khi tạo — nhưng bạn vẫn có
thể tự nhập giá trị của mình.

Tác giả template bật tính năng này cho từng trường bằng `generate: true` trên một
trường `secret`. Trường đó không được khai báo `pattern`, và `max_length` nếu có phải
lớn hơn hoặc bằng 16. Khai báo `generate` trên bất kỳ loại trường nào khác là lỗi khi
tải.

### Việc dựng tạo ra gì

Việc dựng là thay thế chuỗi nghiêm ngặt, không thực thi — mỗi trường của template là
một placeholder Go-template như:

```text
{{ .field }}
```

placeholder. Nó tạo ra:

- một tài liệu **compose YAML** với mọi placeholder đã được phân giải, và
- một **bảng biến môi trường** chứa giá trị của các trường bí mật.

Các trường bí mật không bao giờ được ghi vào YAML. Một placeholder bí mật trở thành
tham chiếu `${field}`, và giá trị của nó đi trong bảng biến môi trường, nên pipeline
của dịch vụ thay thế nó lúc triển khai và biên tập nó khỏi lỗi cùng lịch sử triển
khai. Hãy giữ bí mật ở vị trí giá trị (biến môi trường, đối số lệnh); một bí mật
dùng làm tên dịch vụ hoặc nhãn tên miền sẽ bị từ chối.

Kết quả dựng được kiểm tra bằng cùng bộ phân tích mà đường triển khai dùng, nên một
tài liệu đã dựng là triển khai được. Bản thân việc dựng không thực thi gì phía máy
chủ: tạo một dịch vụ từ template ghi dòng dịch vụ và môi trường của nó, rồi bắt đầu
triển khai trong nền. Bước xem trước trong wizard hiển thị tài liệu, số dịch vụ và
volume của nó, cùng mọi tuyến tên miền.

## Chọn loại cần tạo

| Lựa chọn | Nó là gì | Chọn khi nào |
| --- | --- | --- |
| Ứng dụng | Khối lượng công việc từ nguồn Git, Dockerfile, image hoặc compose, với triển khai, quay lui, tên miền, terminal và lưu trữ. | Bạn triển khai mã nguồn hoặc một image và muốn các tính năng của ứng dụng. |
| Dịch vụ | Một dự án compose bạn viết, cùng môi trường của nó. | Bạn có một tệp compose hoặc cần nhiều container mà không cần bước build. |
| Template | Một stack dựng sẵn được dựng thành một dịch vụ. | Bạn muốn WordPress, Nextcloud, n8n hoặc Uptime Kuma mà không cần YAML. |
| Cơ sở dữ liệu được quản lý | Một container engine với thông tin đăng nhập được tạo và sao lưu được quản lý. | Bạn cần một cơ sở dữ liệu cho ứng dụng hoặc dịch vụ. |
| Cơ sở dữ liệu đi kèm trong stack compose | Cơ sở dữ liệu là một container bên trong dự án của bạn (WordPress, Nextcloud). | Bạn sẽ tự vận hành cơ sở dữ liệu — không có sao lưu hay thông tin đăng nhập được quản lý. |

Template không bao giờ cấp phát một cơ sở dữ liệu được quản lý. Cơ sở dữ liệu mà
template mang theo là một compose service bình thường trong cùng dự án, với mật khẩu
bạn đã nhập trong biểu mẫu.

> [!NOTE]
> Chủ sở hữu có thể tắt toàn bộ các bề mặt cơ sở dữ liệu và dịch vụ cho
> cả instance. Khi đó, các tuyến và mục tương ứng trên bảng điều khiển sẽ không còn.
