---
title: Ứng dụng
---

# Ứng dụng

Một ứng dụng là một khối lượng công việc có thể triển khai: một nguồn, một bản
build, một container và việc định tuyến của nó. Ứng dụng nằm trong một môi trường,
nên địa chỉ của chúng giữ toàn bộ đường dẫn:
`/projects/{project}/environments/{environment}/applications/{id}`.

Không có danh sách Ứng dụng ở cấp cao nhất. Mở dự án, mở môi trường, rồi dùng tab
**Ứng dụng** hoặc **Thêm tài nguyên**.

## Tạo ứng dụng

Chọn **Thêm tài nguyên → Ứng dụng**, rồi chọn một nguồn. Wizard đi qua **Nguồn**,
**Gói build**, **Runtime**, **Môi trường và lưu trữ** và **Triển khai**;
bước xem lại tạo ứng dụng và xếp hàng lần triển khai đầu tiên.

- Tên ứng dụng dài 3–31 ký tự: một chữ thường, rồi chữ thường, chữ số hoặc dấu
  gạch ngang. Tên này được dùng cho container và thẻ image.
- **Runtime** chọn node. Mọi tài nguyên đều gắn với một node.
- Nguồn và gói build cố định sau khi tạo. Node có thể đổi về sau trong **Cài đặt**,
  và nội dung nguồn (văn bản Dockerfile, tài liệu compose, tham chiếu image) có thể
  sửa từ **Tổng quan**.

Lần triển khai đầu tiên bắt đầu mà không có tuyến. Thêm hostname sau đó ở tab
**Tên miền**; xem [Tên miền và bản xem trước](/vi/using/domains-previews).

## Nguồn

| Nguồn | Nó dùng gì |
|---|---|
| Kho git công khai | URL clone `http(s)` hoặc `git://` không lưu thông tin đăng nhập. |
| Kho git riêng | URL clone cùng khóa triển khai SSH hoặc token HTTPS. |
| GitHub (đã kết nối) | Một tài khoản GitHub đã kết nối; wizard liệt kê các kho và nhánh của nó. |
| GitLab (đã kết nối) | Một tài khoản GitLab đã kết nối, với cùng bộ chọn kho và nhánh. |
| Dockerfile | Văn bản Dockerfile được dán vào. Không clone kho nào. |
| Docker Compose | Một tài liệu compose được dán, hoặc đường dẫn tệp compose trong một kho. |
| Container image | Một tham chiếu dựng sẵn từ registry bất kỳ, kèm thông tin đăng nhập tùy chọn. |

**Git riêng.** Ứng dụng nhận khóa triển khai ed25519 riêng, hoặc một token HTTPS
được lưu niêm phong. Mở **Tổng quan → Nguồn git riêng tư**, sao chép khóa công khai
sang kho dưới dạng khóa triển khai chỉ đọc (hoặc lưu token), và dùng
**Kiểm tra kết nối** trước khi triển khai. Cả hai loại thông tin đăng nhập đều có
thể thay hoặc gỡ ở đó.

**Nhà cung cấp đã kết nối.** Kết nối một GitHub App hoặc một kết nối GitLab một
lần trong **Cài đặt → Nguồn Git**, rồi chọn tài khoản và kho trong wizard. Các kết
nối thuộc về tài khoản của bạn, và kho riêng vẫn được triển khai bằng khóa triển
khai SSH.

**Dockerfile.** Văn bản được dán chính là toàn bộ ngữ cảnh build; không clone gì
cả, và một lần triển khai lại sẽ build văn bản mới nhất đã lưu. Văn bản phải chứa
một chỉ thị `FROM` và bị giới hạn ở 64 KiB. Build arg là tùy chọn và được lưu ở
dạng rõ, kể cả trong lịch sử image — hãy để thông tin đăng nhập trong biến môi
trường hoặc bí mật niêm phong thay vào đó.

**Docker Compose.** Dán tài liệu (tối đa 256 KiB) hoặc trỏ tới một đường dẫn tệp
trong kho. Chọn dịch vụ web mà tên miền và cổng sẽ định tuyến tới. Triển khai sẽ
pull các image mà tài liệu khai báo và chạy dự án với `up -d`; tài liệu được lưu
và áp dụng lại khi quay lui. Dùng biến môi trường cho các giá trị, đừng bao giờ dán
bí mật vào tài liệu.

Compose cũng xuất hiện ở tab **Dịch vụ** của bộ chọn thêm tài nguyên, nơi dự án
compose được quản lý như một dịch vụ thay vì một ứng dụng. Xem
[Dịch vụ và template](/vi/using/services-templates).

**Container image.** Nhập `registry/repo:tag`, có thể ghim bằng digest
(`@sha256:…`). Một registry riêng có thể lưu tên người dùng và token; token được
niêm phong và không bao giờ hiện lại. Triển khai lại sẽ pull lại thẻ, còn quay lui
thì dùng digest đã ghi cho bản phát hành đó.

## Build

Có bốn gói build được hỗ trợ; chọn một ở bước **Gói build** hoặc để chế độ tự phát
hiện:

- **Dockerfile** build Dockerfile trong kho.
- **Railpack** build một dự án ngôn ngữ mà không cần Dockerfile; nó cần các công cụ
  máy chủ Railpack, BuildKit và Docker.
- **Buildpacks** build bằng Cloud Native Buildpacks; nó cần công cụ máy chủ `pack`.
- **Static** build một thư mục tĩnh và phục vụ nó qua nginx; không tiến trình nào
  chạy.

Khi không chọn gói nào, việc phát hiện thử Dockerfile, rồi Railpack, rồi
Buildpacks, rồi static. Công cụ máy chủ bắt buộc bị thiếu sẽ được báo lúc triển
khai; cài các công cụ từ [Kho ứng dụng](/vi/operating/app-store).

Nguồn Dockerfile được dán luôn build từ văn bản đã lưu. Nguồn compose và container
image không có gói build: compose chạy các dịch vụ trong tài liệu, và nguồn image
pull tham chiếu.

## Pipeline triển khai

Mỗi lần triển khai được ghi lại và đi qua các giai đoạn sau (một số nguồn bỏ qua
hai giai đoạn đầu):

| Giai đoạn | Chuyện gì xảy ra |
|---|---|
| `queued` | Lần triển khai được chấp nhận và chờ worker. |
| `cloning` | Nguồn được tải về, hoặc Dockerfile hay tài liệu compose đã lưu được phân giải. |
| `building` | Image được build, hoặc tài liệu compose được dựng. |
| `pushing` | Image đã build được đẩy lên registry của node, hoặc các image nguồn được pull. |
| `starting` | Container hoặc dự án compose khởi động và vượt qua kiểm tra sức khỏe. |
| `running` | Bản phát hành đang chạy. |
| `failed` | Một giai đoạn thất bại; lỗi được ghi trên lần triển khai. |

Một lần triển khai container image bỏ qua cloning và building vì không có nguồn để
clone và không có image để build. Quay lui một bản phát hành dựa trên image cũng bỏ
qua cả hai giai đoạn: nó dùng lại image đã đẩy (nguồn image pull digest đã ghi).
Quay lui compose thì dựng lại và áp dụng lại tài liệu đã lưu.

Tab **Tổng quan** hiển thị pipeline này cho lần triển khai mới nhất, với mỗi giai
đoạn được đánh dấu đã xong, đang chạy hoặc đang chờ. Một lần triển khai thất bại
không biết giai đoạn nào đã hỏng, nên nó hiển thị mọi giai đoạn là đang chờ và thêm
một nút `failed` ở cuối thay vì khẳng định các giai đoạn sau đã chạy.

## Triển khai và vận hành

Trên một ứng dụng chưa có lần triển khai nào, **Triển khai ngay** ở **Tổng quan**
xếp hàng lần đầu tiên. **Triển khai lại** ở phần đầu trang xếp hàng một lần triển
khai mới của nguồn hiện tại bất cứ lúc nào. Một lần triển khai lại sẽ pull lại
nhánh, build lại Dockerfile đã lưu, pull lại các image compose, hoặc pull lại thẻ
image, tùy theo nguồn.

**Quay lui** ở phần đầu trang, hoặc trên một dòng triển khai, khôi phục một bản
phát hành trước đó. Hộp thoại liệt kê các lần triển khai vẫn đang chạy và mặc định
chọn lần trước đó. Với ứng dụng dựa trên image, control plane chuyển thẻ image và
khởi động lại container; biến môi trường và volume giữ nguyên, image cũ vẫn nằm
trong registry nội bộ, và container bị thay thế được giữ lại để bạn có thể tiến lên
lại. Quay lui compose áp dụng lại tệp compose đã lưu với môi trường và bí mật hiện
tại.

**Dừng** và **Khởi động** điều khiển container của lần triển khai mới nhất (với
ứng dụng compose là container web được định tuyến). Dừng vẫn giữ nguyên lịch sử
triển khai — bản phát hành vẫn đọc là đang chạy và phần đầu trang hiển thị
`stopped` — và khởi động sẽ đưa đúng container đó trở lại. Các nút này bị vô hiệu
khi một lần triển khai đang diễn ra hoặc khi chưa có container; hãy triển khai ứng
dụng trước. Ứng dụng đã dừng không có container đang chạy để mở phiên terminal, nên
tab **Terminal** sẽ yêu cầu bạn khởi động nó.

## Lịch sử triển khai

Tab **Đợt triển khai** liệt kê mọi lần thử của ứng dụng. Mỗi dòng hiển thị id triển
khai, loại (`deploy` hoặc `rollback`), commit, image, thời lượng, trạng thái, lỗi,
thời điểm tạo và các thao tác.

**Nhật ký** mở log build của lần triển khai đó. **Quay lui** chỉ được cung cấp trên
một lần triển khai vẫn đang chạy; nó mở cùng hộp thoại quay lui với bản phát hành
đó được chọn sẵn.

## Nhật ký

Tab **Nhật ký** có hai khung:

- **Nhật ký triển khai** hiển thị đầu ra build của lần triển khai được chọn. Khi
  lần triển khai đang chạy, log được truyền trực tiếp; khi xong, log đã lưu được
  hiển thị, và một lần triển khai không tạo log lưu nào sẽ báo rõ điều đó.
- **Nhật ký runtime** truyền đầu ra của container đang chạy. Nó cần một container
  đang chạy, và truyền từ chính node của ứng dụng.

## Các tab chi tiết

| Tab | Nó hiển thị gì |
|---|---|
| **Tổng quan** | Nguồn, nhánh, gói build, tên miền và node; pipeline mới nhất và các lần triển khai gần đây; trình soạn thảo cho thông tin đăng nhập git riêng, Dockerfile, tài liệu compose hoặc tham chiếu image. |
| **Đợt triển khai** | Toàn bộ lịch sử triển khai cùng các thao tác xem log và quay lui. |
| **Nhật ký** | Log build của một lần triển khai và log runtime trực tiếp. |
| **Biến môi trường** | Biến riêng của ứng dụng, với biến dùng chung được kế thừa hiển thị chỉ đọc. |
| **Terminal** | Một shell bên trong container dịch vụ đang chạy. Chỉ chủ sở hữu; chọn container dịch vụ khi ứng dụng chạy nhiều hơn một. |
| **Lưu trữ** | Các volume và điểm gắn của chúng trong container. Dữ liệu nằm trên node và tồn tại qua các lần triển khai lại và quay lui. |
| **Bản sao lưu** | Các lần chạy và lịch trình cho dữ liệu lâu dài của ứng dụng. |
| **Tên miền** | Hostname, cổng đích và cấu hình chứng chỉ. Xem [Tên miền và bản xem trước](/vi/using/domains-previews). |
| **Bản xem trước** | Các bản triển khai xem trước theo pull request. Xem [Tên miền và bản xem trước](/vi/using/domains-previews). |
| **Cài đặt** | Chuyển ứng dụng sang môi trường hoặc node khác, và vùng nguy hiểm để xóa. |

Các tab Đợt triển khai và Bản xem trước hiển thị số lượng của chúng. Lưu trữ xuất
hiện khi ứng dụng đã tải xong, Bản xem trước khi bản xem trước được bật trên
instance. Tab Terminal chỉ dành cho chủ sở hữu.

## Bản sao lưu

Tab **Bản sao lưu** ghi lại dữ liệu lâu dài của ứng dụng: các ánh xạ lưu trữ,
hoặc volume có tên của ứng dụng compose. Mã và image của ứng dụng được dựng lại
từ nguồn nên không nằm trong bản sao lưu, và biến môi trường cùng cấu hình cũng
không được xuất.

Tab liệt kê nội dung được sao lưu để một lần chạy không trông như thể ghi nhiều
hơn thực tế. Ứng dụng không có ánh xạ lưu trữ thì không có gì để ghi; hãy thêm
ánh xạ trong **Lưu trữ** trước, nếu không API sẽ từ chối với lỗi xác thực rõ
ràng.

Bản sao lưu dừng ứng dụng trong khi tạo kho lưu, rồi khởi động lại nếu ứng dụng
đang chạy. Lịch trình có thể chọn **Không dừng tài nguyên**: không có thời gian
chết, nhưng tệp được ghi trong lúc lưu có thể không nhất quán. Bản sao lưu bị
từ chối khi ứng dụng đang triển khai, rollback, khởi động lại hoặc khôi phục, và
yêu cầu triển khai trong lúc sao lưu cũng bị từ chối tương tự.

Khôi phục sẽ dừng ứng dụng, thay dữ liệu đã lưu bằng kho lưu rồi khởi động lại.
Hộp xác nhận nêu rõ thời gian chết và việc dữ liệu hiện tại bị thay thế. Nếu
việc nhập thất bại, ứng dụng vẫn dừng và lịch sử khôi phục hiển thị lỗi, để
không ai phục vụ dữ liệu khôi phục dở dang.

## Biến môi trường

Tab **Biến môi trường** sửa biến cho ứng dụng này. Biến dùng chung từ môi trường và
dự án được liệt kê chỉ đọc phía trên trình soạn thảo.

Giá trị cụ thể nhất thắng: ứng dụng đè lên môi trường, và môi trường đè lên dự án.
Thêm cùng khóa ở đây sẽ đè lên giá trị được kế thừa tại thời điểm triển khai.

- Dùng quy ước `^[A-Z][A-Z0-9_]*$`. API chỉ từ chối các tên không thể dùng về mặt
  cấu trúc (rỗng, quá 128 byte, có khoảng trắng, `=` hoặc NUL); một tên không theo
  chuẩn vẫn được chấp nhận nhưng có thể không được đưa vào như mong đợi.
- Thêm tiền tố `secret:` cho một giá trị để lưu nó ở dạng niêm phong. API trả về
  một tham chiếu `secret:<id>`, không bao giờ trả về văn bản rõ; lưu một bí mật đã
  lưu mà không đổi sẽ giữ nguyên giá trị niêm phong.
- **Lưu** thay thế toàn bộ tập hợp. Giá trị mới có hiệu lực từ lần triển khai tiếp
  theo.

## Di chuyển hoặc xóa

Mở **Cài đặt** để đổi nơi ứng dụng chạy. **Máy chủ và môi trường → Đổi phạm vi**
chọn một dự án và môi trường khác cùng một node; **Lưu vị trí** áp dụng thay đổi,
và trang chuyển sang tuyến lồng mới.

Việc di chuyển bị từ chối khi một lần triển khai đang diễn ra, khi môi trường đích
đã có ứng dụng cùng tên, và khi một ứng dụng gốc vẫn còn bản xem trước đang sống.

**Vùng nguy hiểm → Xóa ứng dụng** gỡ ứng dụng và yêu cầu bạn nhập tên nó để xác
nhận. Nó gỡ các container, các volume dữ liệu có tên và cấu hình đã lưu rồi dừng
các tuyến của ứng dụng; thư mục bind vẫn nằm trên node, nên hãy gỡ chúng riêng nếu
dữ liệu không còn cần nữa. Volume còn đang được dùng sẽ được báo là cảnh báo và
lần xóa vẫn hoàn tất. Việc xóa bị từ chối khi một lần triển khai đang diễn ra.

## Quyền truy cập

Bạn thấy ứng dụng này vì chủ sở hữu đã cấp dự án của nó cho bạn. Bên trong dự án
được cấp, bạn có thể tạo, triển khai, quay lui, khởi động, dừng, đổi cài đặt và xóa.
Dự án không được cấp sẽ bị ẩn.

Chủ sở hữu quản lý node và các bề mặt máy chủ quanh ứng dụng. Xem
[Thành viên và quyền truy cập](/vi/using/members-access).

## Lịch sử tài nguyên

Tab **Số liệu** vẽ CPU, bộ nhớ, mạng và I/O đĩa cho các container được quản lý của
ứng dụng trong các khoảng 1 giờ, 24 giờ, 7 ngày và 30 ngày. Agent trên node lấy mẫu
các container đang chạy khoảng mỗi 30 giây; control plane không bao giờ tự tạo một
điểm cho khoảng thời gian không nhận được mẫu.

- CPU tính theo lõi: `1.0` là một lõi đầy đủ, `0.5` là nửa lõi.
- Bộ nhớ là working set: mức dùng của container trừ bộ nhớ đệm trang có thể thu hồi.
  Khi không có giới hạn bộ nhớ, biểu đồ hiển thị byte và **không giới hạn**, không
  bao giờ hiển thị phần trăm của một giới hạn tự đặt ra. Khi có giới hạn, giới hạn
  được hiển thị riêng.
- Mạng RX/TX và đọc/ghi khối là byte mỗi giây, tính từ chênh lệch bộ đếm đơn điệu
  theo thời gian thực tế. Mẫu đầu tiên, bộ đếm đặt lại, container khởi động lại hoặc
  khoảng thời gian không dương là chỗ trống, không phải tốc độ âm hay tăng vọt.
- Nhãn **một phần** nghĩa là ít nhất một bản sao của dịch vụ không được lấy mẫu
  trong khoảng đó. Biểu đồ để lộ khoảng trống bao phủ này.
- Mốc triển khai phân tách các thế hệ container. Một lần triển khai lại giữ lịch sử
  của ứng dụng và bắt đầu một đoạn mới thay vì trộn container đã thay thế vào dịch
  vụ hiện tại.
- Mẫu thô giữ 24 giờ, tổng hợp 1 phút giữ 7 ngày và tổng hợp 1 giờ giữ 30 ngày.
  Mỗi tầng hết hạn độc lập.
- Mở bộ chọn **Dịch vụ** để tách một dịch vụ Compose khi ứng dụng có nhiều dịch vụ.

API là `GET /api/v1/applications/{id}/metrics?range=1h|24h|7d|30d&service=`. Nếu
agent trên node quá cũ để gửi mẫu tài nguyên container, tab báo chưa có lịch sử thay
vì vẽ một biểu đồ trống trông khỏe mạnh. Thành viên dự án chỉ thấy lịch sử của ứng
dụng trong dự án được cấp; container không liên quan, đường dẫn máy chủ thô và chi
tiết máy chủ không bao giờ được đưa vào.
