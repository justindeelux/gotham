---
title: Cập nhật
---

# Cập nhật

Gotham tự cập nhật từ các bản phát hành GitHub có ký. Control plane và các node
agent dùng chung một luồng phát hành: mỗi tệp nhị phân được công bố kèm manifest
đã ký, và mọi artifact đều được xác minh chữ ký tự động trước khi cài. Không có
gì phải kiểm tra bằng tay và không có khóa nào phải quản lý trên máy chủ.

## Dòng lệnh

Tệp nhị phân nằm trong thư mục thuộc sở hữu của dịch vụ, nên hãy chạy CLI vận
hành dưới người dùng dịch vụ. `apply` và `rollback` từ chối chạy dưới root: việc
hoán đổi sẽ cài một tệp nhị phân `root:root` đè lên tệp thuộc sở hữu của dịch vụ.

```sh
sudo -u gotham /var/lib/gotham/bin/gotham update check
sudo -u gotham /var/lib/gotham/bin/gotham update apply
sudo -u gotham /var/lib/gotham/bin/gotham update rollback
sudo -u gotham /var/lib/gotham/bin/gotham update reset
```

| Lệnh | Tác dụng |
|---|---|
| `check` | Báo phiên bản mới nhất và kênh của nó, hoặc báo phiên bản đang chạy đã là mới nhất. |
| `apply` | Tải về, xác minh và dàn dựng bản phát hành mới nhất. `--channel stable\|beta` chọn kênh cho lần chạy này. |
| `rollback` | Khôi phục tệp nhị phân trước đó trên đĩa. Tiến trình đang chạy vẫn giữ tệp nhị phân hiện tại cho đến khi bạn chạy `systemctl restart gotham`. |
| `reset` | Xóa dấu hiệu cập nhật đang chờ bị kẹt và thời gian chờ của bản phát hành lỗi, để người vận hành buộc thử lại. |

Một lần `apply` dàn dựng thành công sẽ in `staged <version>`: sau đó wrapper khởi
động lại có quyền sẽ kiểm tra sức khỏe tệp nhị phân mới và ghi lại kết quả. Nếu
kiểm tra thất bại, wrapper khôi phục tệp nhị phân trước đó và ghi `rolled_back`
(hoặc `rollback_failed` nếu việc đó cũng thất bại). Bảng điều khiển hiển thị kết
quả gần nhất, nên một bản cập nhật lỗi vẫn nhìn thấy được thay vì trông như thành
công.

Một bản phát hành đã bị quay lui sẽ bị vòng lặp tự động giãn thời gian chờ — 5
phút, 10, 20, 40, rồi tối đa 1 giờ — để bộ lập lịch không cài lại cùng phiên bản
hỏng đó trong vòng lặp. Bản phát hành mới hơn vẫn được đề nghị ngay;
`gotham update reset` xóa thời gian chờ khi bạn muốn thử lại chính phiên bản đó.

Cả hai lệnh đều kiểm tra chủ sở hữu tệp nhị phân trước, nên nếu không khớp sẽ
thất bại với thông báo rõ ràng thay vì làm hỏng lần cập nhật tiếp theo.

## Lịch trên bảng điều khiển

**Hệ thống → Cập nhật** chỉ dành cho chủ sở hữu. Trang này hiển thị
phiên bản hiện tại, phiên bản mới nhất, lần kiểm tra gần nhất, nhật ký thay đổi và
kết quả của lần cập nhật gần nhất, đồng thời có **Kiểm tra cập nhật** và
**Cập nhật ngay**.

Thẻ **Lịch cập nhật** điều khiển vòng lặp tự động:

- **Tự động kiểm tra cập nhật** — tìm bản phát hành mới theo lịch.
- **Tự động áp dụng cập nhật** — cài bản phát hành tìm thấy mà không cần xác
  nhận. Mặc định tắt, và cần bật kiểm tra tự động.
- **Kênh** — `stable` hoặc `beta`.
- **Tần suất** — mỗi N giờ (1 đến 720), hằng ngày hoặc hằng tuần, với giờ HH:MM
  theo múi giờ của instance và, với hằng tuần, một ngày trong tuần.

Lưu là lịch có hiệu lực ngay; không cần khởi động lại. Cho đến khi lịch được lưu,
các mặc định từ biến môi trường được áp dụng: `AUTO_UPDATE=true` trong môi trường
của dịch vụ bật cả kiểm tra lẫn áp dụng, và `AUTO_UPDATE_INTERVAL` đặt khoảng cách
(mặc định 6 giờ).

Nếu trang báo tự cập nhật đang tắt, bản cài đặt đang chạy với
`FEATURE_UPDATES=false`.

## Kênh phát hành

`stable` bỏ qua bản phát hành trước (prerelease). `beta` bao gồm chúng. Kênh được
chọn theo từng bản cài đặt trong lịch (hoặc bằng `GOTHAM_UPDATE_CHANNEL`), và theo
từng node cho agent bằng `GOTHAM_AGENT_UPDATE_CHANNEL`:

- Node `stable` từ chối đề nghị `beta`.
- Node `beta` nhận cả bản `stable` lẫn `beta`.

Đề nghị mang kênh của chính bản phát hành, không phải tùy chọn của người nhận, nên
một bản cài đặt `beta` nhận bản `stable` mới hơn vẫn xác minh manifest `stable`.

## Triển khai agent toàn hệ thống

Control plane coi bản phát hành của chính nó là mục tiêu của agent trên node. Khi
nó chạy một bản phát hành, mọi agent hỏi nó sẽ được đề nghị cùng bản phát hành đã
ký đó dưới dạng một đợt triển khai, nên cập nhật control plane đến được các node
mà không cần bước riêng. Một node ngoại tuyến trong lúc cập nhật sẽ áp dụng khi
kết nối lại, vì điều kiện là phiên bản control plane đang chạy chứ không phải một
bộ đếm thời gian.

Agent không chuyển sang bản phát hành mà control plane chưa nhận: một đề nghị
thường (bản mới hơn control plane chưa cài) chỉ là thông tin trừ khi node đặt
`GOTHAM_AGENT_AUTO_UPDATE=true` trong môi trường agent của nó.

Tự cập nhật thay toàn bộ bản cài trên node, không chỉ tệp nhị phân. Bản phát hành
đã ký còn mang theo host helper, các quy tắc sudoers và unit systemd, và wrapper
đặc quyền áp dụng chúng trước khi khởi động lại agent. Một node kênh `stable` vẫn
từ chối đề nghị `beta`, và một bản phát hành không ký, không khớp hoặc không xác
minh được sẽ bị từ chối trước khi áp dụng bất cứ thứ gì.

Một node được cài trước bản phát hành này vẫn chạy wrapper đặc quyền cũ. Tệp nhị
phân agent được cập nhật, nhưng wrapper đó không bao giờ cài bản mới, nên việc làm
mới helper, unit và sudoers chỉ bắt đầu sau một lần chạy
`deploy/install-agent.sh` trên node đó. Trình cài đặt có tính lũy đẳng và giữ
`/etc/gotham/agent.env`; sau lần chạy đó, tự cập nhật giữ toàn bộ bản cài hiện
hành.

Trang Servers hiển thị phiên bản agent của từng node cạnh phiên bản control plane
và đánh dấu khi không khớp. **Cập nhật agent** trên một node lỗi thời xếp hàng bản
phát hành cho node đó và xóa trạng thái thất bại trước đó, nên cùng phiên bản được
thử lại mà không cần lệnh shell. Node cập nhật ở lần hỏi tiếp theo; thẻ node sau
đó hiển thị đang chờ, đang cập nhật, đã mới nhất hoặc lý do thất bại.

Chủ sở hữu vẫn có thể kích hoạt một đợt triển khai toàn hệ thống qua API:

```sh
curl -X POST -H "Authorization: Bearer <admin-token>" \
  https://<panel>/api/v1/servers/agents/update-all
```

Lệnh gọi này ghi lại mục tiêu triển khai và trả về ngay, không chờ: mỗi agent nhận
mục tiêu ở lần hỏi tiếp theo. `GET /api/v1/servers/agents` trả về phiên bản và
trạng thái từng node báo cáo, mục tiêu triển khai đang hoạt động và bản phát hành
mới nhất mà control plane thấy được.

Một lần kích hoạt thất bại sẽ quay lui và giãn thời gian chờ giống control plane.
Nút **Cập nhật agent** xóa thời gian chờ đó cho node được chọn; đặt lại trên node
vẫn dùng được khi agent không kết nối được tới control plane:

```sh
sudo gotham-agent update reset
```

Tự cập nhật cũng sửa môi trường CLI docker trong sandbox systemd: khi unit ẩn
`~/.docker` (`ProtectHome=true`), agent trỏ `DOCKER_CONFIG` vào
`/var/lib/gotham-agent/.docker` lúc khởi động, nên `docker compose` tìm thấy
plugin mà không cần cài lại. Cần cài lại một lần trên node được cài trước bản
phát hành này để wrapper của nó có thể làm mới helper và unit; sau đó chỉ cần cài
lại để đổi lựa chọn lúc cài (file manager hoặc CA) hay sửa một bản cài hỏng.

Cài lại một phiên bản cụ thể là một lần cài bình thường với
`GOTHAM_VERSION=vX.Y.Z`; xem [Cài đặt](/vi/guide/install).

## Cài lại trên một máy đã có

Chạy lại trình cài đặt trên một máy đã cài để nâng cấp tại chỗ. Trước khi thay
đổi bất kỳ tệp nào, nó từ chối một nền tảng không được hỗ trợ và một máy đã có
`agent.env` từ xa (một node được cài từ control plane khác): hãy di chuyển agent
từ xa đó, hoặc trỏ lại nó một cách tường minh, trước khi thử lại. Nó giữ
`/etc/gotham/gotham.env`: các khóa được quản lý (`GOTHAM_DATABASE_DSN`,
`GOTHAM_REDIS_ADDR`, `GOTHAM_SERVER_PORT`, `GOTHAM_CA_DIR`, `GOTHAM_SECRET_KEY`,
các đường dẫn khóa JWT)
được làm mới — DSN đưa trên dòng lệnh thắng, nếu không thì giữ giá trị hiện có — và
mọi khóa người vận hành thêm (`AUTO_UPDATE`, `AUTO_UPDATE_INTERVAL`, ...) được để
nguyên. Một DSN được quản lý không bao giờ bị thay bằng mặc định cục bộ, và
PostgreSQL/Redis cục bộ chỉ được cấp phát khi DSN phân giải được là mặc định cục bộ
dựng sẵn. Trình cài đặt khởi động lại `gotham.service` sau khi cài lại, nên tệp
nhị phân vừa cài chính là tệp đang chạy (không khởi động lại thì một máy đã nâng
cấp vẫn phục vụ phiên bản trước).

## Ghi chú phát hành: cổng mặc định của bảng điều khiển

Một bản cài mới phục vụ bảng điều khiển trên cổng `9074` thay vì `8000`. Một bản
cài đã có giữ cổng nó đang dùng: trình cài đặt và trình cập nhật ghi cổng đó vào
`GOTHAM_SERVER_PORT` trong `/etc/gotham/gotham.env` trước khi tệp nhị phân mới
chạy, nên bookmark, webhook và URL callback OAuth đã đăng ký với cổng cũ vẫn hoạt
động. Một bản cài cấu hình qua `gotham.yaml` giữ `server.port` của nó: không có
cổng nào bị ghi đè lên. Bước kiểm tra sức khỏe của trình cập nhật đi theo cổng
đang được cấu hình. Muốn đổi cổng,
sửa `GOTHAM_SERVER_PORT` trong `gotham.env` rồi khởi động lại; trình cập nhật sẽ
dò cổng mới.

Một máy được cài trước thay đổi mTLS giờ sẽ nhận một CA, nên gateway gRPC chuyển
từ plaintext sang TLS. Các agent đã cài trước đó kết nối bằng plaintext và không
có CA, nên chúng **chuyển sang ngoại tuyến** (Register và Heartbeat thất bại an
toàn ở bước bắt tay) cho đến khi từng agent được cài lại kèm CA:

```sh
scp root@<cp-host>:/var/lib/gotham/ca/ca.crt .           # on each node
sudo gotham/deploy/install-agent.sh --ca ./ca.crt
```

Tệp nhị phân của agent không cần cập nhật; chỉ `/etc/gotham/agent.env` có thêm
`GOTHAM_AGENT_CA=/etc/gotham/ca.crt`. Nếu bạn chưa với tới control plane bằng một
tên nằm trong SAN của listener, hãy thêm nó trước (`--cp-host`; xem
[Thêm node](/vi/guide/nodes)).

Các bản phát hành không có `admin exists` (`v0.2.0`) bỏ qua việc tạo tài khoản tự
động và in lệnh `admin create` thủ công thay vì thất bại với cảnh báo cơ sở dữ
liệu; các bản phát hành không có lệnh admin nào (`v0.1.0` và sớm hơn) in hướng dẫn
nâng cấp thay vào đó, vì tệp nhị phân đó cũng không tạo được tài khoản.

Khi nâng cấp từ một trình cài đặt cũ, việc từ chối dấu nháy cũng áp dụng cho các
giá trị được giữ nguyên từ `agent.env` hiện có, nên một node id chứa dấu nháy do
trình cài đặt cũ ghi sẽ chặn việc nâng cấp với lỗi rõ ràng nêu đúng khóa — hãy gỡ
dấu nháy khỏi `agent.env` và chạy lại. Điều tương tự cũng đúng với `gotham.env`:
một DSN hoặc giá trị Redis được giữ nguyên có dấu nháy hoặc dấu gạch chéo ngược ở
cuối sẽ chặn việc nâng cấp và nêu đúng khóa — sửa giá trị trong `gotham.env` (DSN
dạng keyword có giá trị đặt trong dấu nháy phải dùng dạng URL) rồi chạy lại.

## Ghi chú phát hành: tài khoản chủ sở hữu và thành viên

Bản phát hành này thay mô hình nhóm bằng một chủ sở hữu và nhiều thành viên.

- Tài khoản hiện có sớm nhất trở thành **chủ sở hữu**; mọi tài khoản khác trở
  thành **thành viên**. Có đúng một chủ sở hữu.
- Nhóm và các vai trò của chúng không còn. Dự án giờ ở cấp instance; tên dự án
  phải duy nhất trên toàn instance.
- Thành viên khởi đầu **không có quyền dự án**. Sau khi nâng cấp, hãy đăng nhập
  bằng chủ sở hữu, mở **Người dùng → Quyền dự án**, và cấp cho từng thành viên
  những dự án họ cần. Trước đó họ thấy bảng điều khiển trống.
- Thành viên chỉ truy cập được dự án đã cấp và tài nguyên của nó. Máy chủ, khoá
  SSH, kết nối Git, cài đặt instance, Tên miền & SSL toàn cục, cập nhật, terminal/
  trình quản lý tệp trên máy chủ và Kho ứng dụng chỉ dành cho chủ sở hữu.
- Cài đặt danh sách email được phép cũ không còn được đọc. Hãy gỡ dòng đó khỏi
  `/etc/gotham/gotham.env`; quyền chủ sở hữu giờ đến từ vai trò tài khoản, và API
  token của chủ sở hữu cần phạm vi `admin`.

Xem [Thành viên và quyền truy cập](/vi/using/members-access) để biết mô hình mới.

## Ghi chú phát hành: đăng nhập OAuth chuyển vào Cài đặt

Đăng nhập GitHub giờ được cấu hình trong phần cài đặt instance của giao diện
Gotham, không phải trong `gotham.yaml` hay biến môi trường.
`GOTHAM_OAUTH_GITHUB_CLIENT_ID`, `GOTHAM_OAUTH_GITHUB_CLIENT_SECRET`,
`GOTHAM_OAUTH_GITHUB_REDIRECT_URL` và khối `oauth:` bị bỏ qua; không có giá trị
nào được nhập từ chúng.

Instance từng dùng chúng vẫn đăng nhập bằng email và mật khẩu cho đến khi client ID
và secret của ứng dụng GitHub OAuth được nhập trong cài đặt instance. URL callback
ủy quyền của ứng dụng là `/api/v1/auth/oauth/github/callback` trên origin control
plane của instance. Lưu thông tin đăng nhập là bật nhà cung cấp ngay, không cần
khởi động lại.

## Phạm vi xác minh

Với cả hai tệp nhị phân, manifest được ký bằng khóa phát hành nhúng trong tệp nhị
phân đang chạy, và nó ràng buộc phiên bản, kiến trúc, tên tệp và kênh. SHA-256 của
artifact tải về được so với manifest đã ký; bất kỳ sai lệch nào đều hủy bản cập
nhật. Bản phát hành nháp bị bỏ qua, và bản phát hành thiếu asset hoặc manifest
không bao giờ được đề nghị. Không có tài liệu khóa ký nào được lưu trên máy chủ
hay cần đến trên máy chủ.

Nếu một lần cập nhật thất bại, xem [Xử lý sự cố](/vi/guide/troubleshooting).
