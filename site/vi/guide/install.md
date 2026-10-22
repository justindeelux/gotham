---
title: Cài đặt
---

# Cài đặt Gotham

Hướng dẫn này trình bày việc cài control plane và một node agent, lần đăng nhập đầu
tiên, cập nhật và quay lui, cùng luồng phát hành/ký.

## Yêu cầu

- **Máy chủ control plane:** Ubuntu 22.04 (hoặc bất kỳ Linux có systemd) với `curl`,
  `openssl` 3, `sudo` (kèm `visudo`) và `coreutils`. Trình cài đặt cấp phát
  PostgreSQL và Redis từ gói của bản phân phối trừ khi bạn trỏ nó tới dịch vụ được
  quản lý bằng `GOTHAM_DATABASE_DSN` / `GOTHAM_REDIS_ADDR` (hoặc đặt
  `GOTHAM_SKIP_DEPS=1`).
- **Máy chủ node:** Linux (`amd64` hoặc `arm64`) với `systemd`, `curl`, `openssl` 3,
  `sudo` (kèm `visudo`) và Docker Engine (hoặc truyền `--full` trên Ubuntu/Debian để
  trình cài đặt thiết lập Docker từ kho chính thức).
- HTTPS ra ngoài tới `github.com` (hoặc mirror của bạn qua `GOTHAM_RELEASES_URL` /
  `GOTHAM_BASE_URL`).

## Control plane

Chạy trình cài đặt từ một bản checkout, để nó dùng được các tệp đi kèm (wrapper cập
nhật thuộc sở hữu root, cấu hình của nó, quy tắc sudoers và unit systemd):

```sh
git clone https://github.com/justindeelux/gotham.git
sudo gotham/deploy/install.sh
```

Trình cài đặt:

1. phát hiện kiến trúc (`amd64`/`arm64`);
2. phân giải phiên bản — dùng `GOTHAM_VERSION` nếu có, nếu không lấy thẻ mới nhất từ
   lần chuyển hướng đầu của `<releases>/latest` — và tải `gotham-linux-<arch>`,
   `gotham-manifest-<arch>.txt` và `gotham-manifest-<arch>.txt.sig` từ đúng thẻ đã
   ghim đó;
3. xác minh chữ ký Ed25519 của manifest với khóa công khai phát hành **nhúng trong
   tệp nhị phân**, ràng buộc manifest với phiên bản/kiến trúc/tên tệp, và xác minh
   SHA-256 của artifact với manifest đã ký — bất kỳ sai lệch nào đều hủy bản cài;
4. cài tệp nhị phân vào `/var/lib/gotham/bin/gotham` (thuộc sở hữu của người dùng
   dịch vụ `gotham`), wrapper vào `/usr/libexec/gotham/gotham-update`, cấu hình vào
   `/etc/gotham/updater.conf`, thư mục trạng thái và quy tắc sudoers, và unit vào
   `/etc/systemd/system/gotham.service`;
5. cấp phát certificate authority mTLS cho gRPC tại `/var/lib/gotham/ca`
   (`gotham ca init`, lũy đẳng — CA đã có được giữ nguyên);
6. áp dụng các migration cơ sở dữ liệu và khởi động `gotham.service`;
7. cài và khởi động node agent trên chính máy đó như node đầu tiên
   (`install-agent.sh --full --ca` với CA vừa cấp phát, ghim cùng thẻ phát hành; node
   id cố định `localhost`, địa chỉ quay số tới control plane `127.0.0.1:9442`).
   Không có tùy chọn từ chối. Các nền tảng không được hỗ trợ và một `agent.env` từ xa
   đã tồn tại sẽ bị từ chối trước khi trình cài đặt thay đổi tệp; hãy di chuyển agent
   từ xa đó hoặc trỏ lại nó một cách tường minh trước khi thử lại;
8. in URL giao diện web và các bước đăng nhập đầu tiên.

Nếu bước 7 thất bại trên một nền tảng được hỗ trợ, control plane vẫn được cài và
đang chạy (không có gì bị quay lui) và trình cài đặt thoát với mã lỗi cùng đúng lệnh
thử lại chỉ cho agent.

Gateway gRPC mà các node agent kết nối tới chạy **TLS** ngay khi CA đó tồn tại:
`gotham serve` nạp `/var/lib/gotham/ca/ca.crt` + `ca.key` và xuất trình một chứng chỉ
máy chủ do nó ký. Để thêm một node, hãy sao chép chứng chỉ công khai sang node đó
(không bao giờ `ca.key`):

```sh
scp root@<cp-host>:/var/lib/gotham/ca/ca.crt .
```

Các SAN của chứng chỉ listener luôn bao gồm các tên loopback (`localhost`,
`127.0.0.1`, `::1`) và hostname của máy. Một agent từ xa quay số tới
`GOTHAM_AGENT_CP_ADDR`, nên tên hoặc IP mà nó dùng cũng phải là một SAN — nếu không
việc bắt tay của agent thất bại với `certificate is valid for …, not <name>`. Hãy
thêm địa chỉ của control plane khi cài đặt:

```sh
sudo deploy/install.sh --cp-host cp.example.com --cp-host 203.0.113.10
```

`--cp-host` lặp lại được (và `GOTHAM_GRPC_HOSTS=<a,b>` là dạng biến môi trường); danh
sách được lưu cạnh CA. Chạy lại trình cài đặt mà không có nó sẽ giữ danh sách đã lưu.
`GOTHAM_GRPC_HOSTS` trong môi trường của dịch vụ đè lên danh sách đã lưu lúc chạy, và
`gotham ca init --host <name-or-ip>` có thể đặt trực tiếp. Một node quay số tới địa
chỉ không phải SAN sẽ thất bại an toàn (không bao giờ dùng plaintext).

Người vận hành chạy `gotham serve` trực tiếp mà không có CA sẽ nhận phương án dự
phòng cho phát triển (gRPC plaintext, có ghi cảnh báo); trình cài đặt không bao giờ để
một máy chủ production ở trạng thái đó.

### HTTPS cho bảng điều khiển

Đặt `control_plane_url` của instance thành `https://panel.example.com` trong cài đặt
Chung. Node `localhost` đã đăng ký phải chạy Traefik, với cổng vào 80 và 443 mở. Việc
lưu sẽ bắt đầu một phiên xác nhận và tạo một bản ghi ý định chứng chỉ thuộc bảng điều
khiển; không cần ứng dụng nào. Hãy xác nhận qua host được đề xuất trước khi phiên thử
hết hạn. Trong phiên thử, cả host được đề xuất lẫn host trước đó đều được định tuyến,
và hoàn tác sẽ khôi phục host trước đó nếu việc cấp chứng chỉ thất bại.

HTTP-01 là mặc định: bản ghi A/AAAA của tên miền phải trỏ trực tiếp vào máy này, và
các bản ghi Cloudflare phải là **DNS only** (tắt proxy). Xóa các bản ghi AAAA cũ khi
máy không phục vụ được IPv6. Để giữ proxy của Cloudflare bật, hãy cấu hình một nhà
cung cấp DNS và chọn DNS-01 cho ý định chứng chỉ của bảng điều khiển qua API
`PATCH /api/v1/proxy/panel-certificates/{id}` dành cho quản trị viên nền tảng, dùng
các trường `challenge` và `dns_provider_id` sẵn có. DNS-01 cần một token được phép
cập nhật vùng đã cấu hình; dùng Full (strict) khi chứng chỉ origin đã được cấp.

Router HTTPS bắt đầu việc cấp ACME trong khi HTTP vẫn chuyển tiếp tới bảng điều
khiển. HTTP chỉ chuyển hướng sang HTTPS sau khi agent cục bộ báo một chứng chỉ chưa
hết hạn; trạng thái sẵn sàng được làm mới mỗi 30 giây. Việc cấp đang chờ hoặc thất
bại vẫn giữ HTTP khả dụng. API routers báo host được đề xuất trong một phiên thử và
thêm `certificate.status` (`issued`, `pending` hoặc `failed`) cùng
`certificate.not_after` cho các dòng bảng điều khiển. API chỉ dành cho nền tảng
`GET /api/v1/proxy/panel-certificates` liệt kê các ý định chứng chỉ của bảng điều
khiển với `status` sẵn có của kho lưu trữ cùng các trường bổ sung `panel_state` và
`owner_kind`. `pending` nghĩa là chưa quan sát được chứng chỉ dùng được nào; `failed`
bao gồm cấu hình không dùng được hoặc kho lưu trữ không khả dụng, vì agent không báo
lỗi đơn ACME. Các API chứng chỉ ứng dụng vẫn giữ phạm vi nhóm của chúng.

### Nguồn Git và chính sách host SSRF

URL clone của ứng dụng do người vận hành cung cấp, và control plane quay số tới chúng
bằng `git` lúc triển khai (phân giải nhánh mặc định) và ở nút Kiểm tra kết nối. Mọi
lần quay số như vậy đều đi qua chính sách host SSRF
(`internal/deploy/githostpolicy.go`): loopback, link-local (kể cả địa chỉ metadata đám
mây `169.254.169.254`), unspecified, multicast, carrier-grade NAT, benchmarking, NAT64
và 6to4 luôn bị từ chối, cũng như các mẹo dùng IP dạng số (`2130706433`,
`0x7f.0.0.1`), userinfo và zone id IPv6. RFC1918, ULA và loopback cũng bị từ chối,
trừ khi người vận hành cho phép host riêng cho một máy chủ git tự host:

```yaml
# gotham.yaml
deploy:
  git_allow_private_hosts: true
```

hoặc `GOTHAM_GIT_ALLOW_PRIVATE_HOSTS=true` trong môi trường của dịch vụ (`1` và
`true` đều được tính, giống bộ đọc tệp cấu hình). Link-local và các dải luôn bị từ
chối khác vẫn bị từ chối. Lưu ý cách phân biệt tên/IP: TÊN `localhost` vẫn bị từ chối
kể cả khi bật thiết lập này (tên có thể bị gán lại), trong khi `127.0.0.1` tường minh
được phép. Host được phân giải hai lần và câu trả lời thay đổi (DNS rebinding) sẽ từ
chối việc quay số; các remote `https` được ghim vào địa chỉ đã phân giải
(`http.curloptResolve`), và mọi lần gọi git `http(s)` đều từ chối chuyển hướng
(`http.followRedirects=false`), nên một URL công khai không thể dội sang một URL nội
bộ (kho bị đổi tên sẽ thất bại an toàn — đó là đánh đổi).
Lỗi trong log triển khai chỉ mang gợi ý đã phân loại — không bao giờ chứa địa chỉ nội
bộ đã phân giải. Đường dẫn cục bộ và URL `file://` vẫn nằm sau
`GOTHAM_DEV_CLONE_LOCAL` (chỉ dùng cho fixture phát triển).

Các điểm tồn dư mà người vận hành nên biết:

- Remote `ssh` và `git://` không được ghim (git không có tùy chọn tương đương): hai
  lần phân giải được so sánh của chúng là toàn bộ biện pháp giảm thiểu, nên một câu
  trả lời fast-flux chạy đua đúng khoảng đó vẫn tới được lần quay số.
- Một proxy `http(s)` (`HTTP(S)_PROXY` trong môi trường của dịch vụ) bỏ qua việc
  ghim: curl phân giải và kết nối qua proxy, nên môi trường có proxy phải ghép chính
  sách này với các quy tắc proxy từ chối đích nội bộ.
- Việc ghim cần git >= 2.37 (git cũ hơn lặng lẽ bỏ qua `http.curloptResolve`). Control
  plane ghi một cảnh báo lúc khởi động nêu phiên bản đã cài khi nó cũ hơn 2.37; hãy
  nâng cấp git để khôi phục việc ghim.

### Nâng cấp một control plane đã có

Chạy lại `deploy/install.sh` trên một máy được cài trước thay đổi này giờ sẽ cấp một
CA, nên gateway gRPC chuyển từ plaintext sang TLS. Các agent đã cài trước đó kết nối
bằng plaintext và không có CA, nên chúng **chuyển sang ngoại tuyến** (Register và
Heartbeat thất bại an toàn ở bước bắt tay) cho đến khi từng agent được cài lại kèm CA:

```sh
scp root@<cp-host>:/var/lib/gotham/ca/ca.crt .           # on each node
sudo gotham/deploy/install-agent.sh --ca ./ca.crt
```

Tệp nhị phân của agent không cần cập nhật; chỉ `/etc/gotham/agent.env` có thêm
`GOTHAM_AGENT_CA=/etc/gotham/ca.crt`. Nếu bạn chưa với tới CP bằng một tên nằm trong
SAN của listener, hãy thêm nó trước (`--cp-host`).

Chạy lại trình cài đặt sẽ giữ `/etc/gotham/gotham.env`: các khóa được quản lý
(`GOTHAM_DATABASE_DSN`, `GOTHAM_REDIS_ADDR`, `GOTHAM_CA_DIR`, `GOTHAM_SECRET_KEY`,
các đường dẫn khóa JWT) được làm mới — DSN đưa trên dòng lệnh thắng, nếu không thì giữ
giá trị hiện có — và mọi khóa người vận hành thêm (`AUTO_UPDATE`, `PLATFORM_ADMINS`,
...) được để nguyên. Một DSN được quản lý không bao giờ bị thay bằng mặc định cục bộ,
và PostgreSQL/Redis cục bộ chỉ được cấp phát khi DSN phân giải được là mặc định cục bộ
dựng sẵn. Trình cài đặt khởi động lại `gotham.service` sau khi cài lại, nên tệp nhị
phân vừa cài chính là tệp đang chạy (không khởi động lại thì một máy đã nâng cấp vẫn
phục vụ phiên bản trước). Các bản phát hành không có `admin exists` (`v0.2.0`) bỏ qua
việc tạo tự động và in lệnh `admin create` thủ công thay vì thất bại với cảnh báo cơ
sở dữ liệu; các bản phát hành không có lệnh admin nào (`v0.1.0` và sớm hơn) in hướng
dẫn nâng cấp thay vào đó, vì tệp nhị phân đó cũng không tạo được tài khoản. Khi nâng
cấp từ một trình cài đặt cũ: việc từ chối dấu nháy cũng áp dụng cho các giá trị được
giữ nguyên từ `agent.env` hiện có, nên một node id chứa dấu nháy do trình cài đặt cũ
ghi sẽ chặn việc nâng cấp với lỗi rõ ràng nêu đúng khóa — hãy gỡ dấu nháy khỏi
`agent.env` và chạy lại. Điều tương tự cũng đúng với `gotham.env`: một DSN hoặc giá
trị Redis được giữ nguyên có dấu nháy hoặc dấu gạch chéo ngược ở cuối sẽ chặn việc
nâng cấp và nêu đúng khóa — sửa giá trị trong `gotham.env` (DSN dạng keyword có giá
trị đặt trong dấu nháy phải dùng dạng URL) rồi chạy lại.

Các ghi đè hữu ích:

| Biến | Mục đích |
|---|---|
| `GOTHAM_VERSION` | Cài một thẻ cụ thể (mặc định: phân giải thẻ mới nhất). |
| `GOTHAM_RELEASES_URL` | Gốc phát hành kiểu GitHub cho một mirror (mặc định `https://github.com/<repo>/releases`). |
| `GOTHAM_BASE_URL` | URL gốc đầy đủ của asset tĩnh; cần `GOTHAM_VERSION`. |
| `GOTHAM_REPO` | `owner/name` (mặc định `justindeelux/gotham`). |
| `GOTHAM_DATABASE_DSN` | DSN PostgreSQL được quản lý; bỏ qua việc cấp phát cục bộ. |
| `GOTHAM_REDIS_ADDR` | Redis `host:port` (mặc định `localhost:6379`). |
| `GOTHAM_SKIP_DEPS=1` | Không cài hay cấu hình PostgreSQL/Redis. |
| `GOTHAM_ADMIN_EMAIL` | Email của tài khoản quản trị đầu tiên (cài phi tương tác; không có nó thì bỏ qua việc tạo). Trên terminal, đây là giá trị mặc định cho câu hỏi email. |
| `GOTHAM_ADMIN_PASSWORD_FILE` | Tệp một dòng chứa mật khẩu quản trị đầu tiên (dùng cùng `GOTHAM_ADMIN_EMAIL`, hoặc làm mặc định cho tty mà không hỏi); không có nó thì một mật khẩu ngẫu nhiên được tạo và in một lần. Một tệp đọc được bởi nhóm/mọi người sẽ in cảnh báo. |
| `GOTHAM_AUTH_ALLOW_REGISTRATION` | Chỉ cho test/dev: mở lại tự đăng ký sau tài khoản đầu tiên (mặc định là đóng — thành viên tham gia qua lời mời). |
| `--cp-host <name-or-ip>` | Thêm một tên DNS hoặc IP vào SAN của chứng chỉ listener gRPC (lặp lại được). |
| `GOTHAM_AGENT_NODE_ID` | Mặc định là `localhost`; cố định với `install.sh`. Đặt một id riêng cho mỗi lần chạy `install-agent.sh` từ xa. |
| `GOTHAM_AGENT_CP_ADDR` | Cố định ở `127.0.0.1:9442` với `install.sh`; agent từ xa phải đặt địa chỉ control plane của chúng. |
| `GOTHAM_GRPC_HOSTS` | Danh sách host SAN phân tách bằng dấu phẩy (giống `--cp-host`); cũng đè lên danh sách đã lưu lúc chạy. |
| `GOTHAM_MANAGED_VOLUME_ROOT` | Thư mục cha của mọi bind mount ứng dụng (mặc định `/var/lib/gotham/volumes`). Một bind của ứng dụng phải nằm trong `<root>/<app id>`; volume có tên được đặt tên theo ứng dụng. Phải khớp với `GOTHAM_AGENT_MANAGED_VOLUME_ROOT` của node. |
| `GOTHAM_INSTALL_ROOT` | Cài dưới một tiền tố thay vì `/` (chỉ để test; không phải root; bật chế độ test). |
| `GOTHAM_INSTALL_TEST_PUBLIC_KEY` | Chỉ để test: thay khóa tin cậy đã ghim (PEM hoặc base64); chỉ được dùng **khi có** `GOTHAM_INSTALL_ROOT`, nếu không sẽ cảnh báo và bị bỏ qua. |

`deploy/install.sh --dry-run` in ra những gì nó sẽ làm mà không thay đổi máy chủ.

## Lần đăng nhập đầu tiên

Việc đăng ký bị đóng: đúng một tài khoản khởi tạo instance và các thành viên tham gia
qua lời mời do quản trị viên tạo.

Trình cài đặt tạo tài khoản đầu tiên đó thay bạn. Thông tin đăng nhập được thu thập
trước khi bất cứ thứ gì được cài, và tài khoản được tạo ngay sau các migration cơ sở
dữ liệu và trước khi dịch vụ khởi động — nên không ai đăng ký trước được qua biểu mẫu
web đang mở trong lúc bạn còn đang cân nhắc câu hỏi:

- **Cài tương tác** (có terminal, kể cả `curl | sh`): trình cài đặt hỏi email quản
  trị, rồi mật khẩu với đầu vào ẩn và xác nhận (không khớp và vi phạm chính sách sẽ
  hỏi lại, tối đa 3 lần thử tạo). Để trống mật khẩu sẽ tạo một mật khẩu ngẫu nhiên
  mạnh, in một lần trong phần tóm tắt cuối cùng cùng email và URL đăng nhập — hãy lưu
  ngay, nó không hiện lại.
- **Cài phi tương tác** (không có terminal): trình cài đặt không bao giờ hỏi. Đặt
  `GOTHAM_ADMIN_EMAIL` — và tùy chọn `GOTHAM_ADMIN_PASSWORD_FILE` (đường dẫn tới một
  tệp chỉ root đọc được chứa mật khẩu trên một dòng) — để tạo tài khoản; không có tệp
  mật khẩu thì một mật khẩu ngẫu nhiên được tạo và in một lần trong phần tóm tắt cuối
  cùng. Không có email thì trình cài đặt bỏ qua việc tạo và in lệnh thủ công bên dưới.
- **Đặt cả hai cách**: trên terminal, `GOTHAM_ADMIN_EMAIL` là giá trị mặc định cho
  câu hỏi email (nhập rỗng thì giữ nguyên, `-` thì bỏ qua) và
  `GOTHAM_ADMIN_PASSWORD_FILE` được dùng mà không hỏi. Một tệp mật khẩu đọc được bởi
  nhóm/mọi người sẽ in cảnh báo nhưng vẫn được dùng.
- **Chạy lại / nâng cấp**: nếu instance đã có tài khoản, trình cài đặt báo
  `admin already exists, skipping` và không thay đổi gì (nó kiểm tra trước khi hỏi).
  Nếu việc tạo thua một cuộc đua với một lần đăng ký khác, nó báo
  `an account already exists (created by someone else?)` thay vì lỗi chung.
- **`--dry-run`** không hỏi gì và không thay đổi gì.

Mật khẩu không bao giờ được truyền làm đối số dòng lệnh, không bao giờ được đặt trong
biến môi trường, và không bao giờ được ghi ra đĩa, tệp môi trường, log hay lịch sử
shell: nó chỉ đi tới tệp nhị phân qua stdin (việc trace `sh -x` bị tắt quanh phần xử
lý bí mật).

Muốn tự làm bằng tay (hoặc đã bỏ qua câu hỏi với email rỗng)?

```sh
sudo -u gotham -- /var/lib/gotham/bin/gotham admin create --email ops@example.com
```

Câu hỏi mật khẩu được ẩn. Các cờ hữu ích: `--password-stdin` đọc mật khẩu từ stdin
(một dòng, dùng cho script — hãy pipe vào, đừng bao giờ dùng `--password`, vốn hiện
trong `ps`), và `--generate-password` tạo một mật khẩu ngẫu nhiên và in một lần dưới
dạng `generated-password: <value>`. Chỉ chạy lại với `--force` khi bạn cố ý muốn thêm
tài khoản thứ hai; CLI mặc định từ chối khi đã có một tài khoản. `gotham admin exists`
in `admin-exists: true|false` để dùng trong script (đó là cách trình cài đặt phát hiện
một lần chạy lại).

Sau đó:

1. Mở `http://<host>:8000` và đăng nhập.
2. Để thêm thành viên, tạo một lời mời trong **Nhóm** và gửi liên kết được hiển thị:
   thành viên mở `/register?invite=<token>` và chọn thông tin đăng nhập của họ. Liên
   kết chỉ hiện một lần và sẽ hết hạn.
3. Từ **Máy chủ**, cài một agent trên một node (bên dưới); node sau đó báo nhịp tim và
   hiện trong giao diện.

Mất mật khẩu quản trị?
`sudo -u gotham -- /var/lib/gotham/bin/gotham admin reset-password
--email ops@example.com` thay mật khẩu và thu hồi các phiên refresh của tài khoản.
Bearer access token là JWT không trạng thái, nên một token đã cấp vẫn hợp lệ cho đến
khi hết hạn (15 phút); việc đặt lại không phải một lần đăng xuất tức thời trên toàn
hệ thống.

Tài khoản đầu tiên tự động là quản trị viên nền tảng: trên một instance mới, tài khoản
được tạo khi bảng người dùng còn trống (lần đăng ký đầu tiên, lần đăng nhập OAuth đầu
tiên, hoặc `gotham admin create` không có `--force`) nhận cờ quản trị viên nền tảng và
các phiên của nó mang claim vai trò `admin`, nên các thao tác toàn nền tảng (đồng bộ
proxy toàn node, nhà cung cấp DNS) hoạt động mà không cần cấu hình thêm. Các instance
đã nâng cấp không thay đổi — migration để mọi dòng hiện có ở trạng thái không phải
quản trị, nên hãy tiếp tục liệt kê email tài khoản vận hành trong `PLATFORM_ADMINS` ở
`/etc/gotham/gotham.env` (hoặc cấp một API token có phạm vi admin).

Rủi ro bị chiếm quyền: trên một instance mới, việc đăng ký mở cho đến khi tài khoản
đầu tiên tồn tại, nên ai đăng ký trước sẽ trở thành quản trị viên. Trên một instance
công khai chưa cấu hình, hãy tạo tài khoản đó (hoặc đặt `PLATFORM_ADMINS`) trước khi
phơi control plane ra. Xóa trống bảng người dùng bằng tay sẽ mở lại việc đăng ký lần
đầu, và người đăng ký tiếp theo lại trở thành quản trị viên nền tảng.

Một API token có phạm vi admin được cấp bởi một phiên quản trị vẫn sống sót sau khi
tài khoản đó bị hạ cấp về sau: không có việc thu hồi phiên hay token nào gắn với thay
đổi vai trò (việc sửa điều này nằm ngoài phạm vi ở đây), nên hãy xoay vòng token nếu
quyền đó phải kết thúc.

## Node agent

`install.sh` đã cài agent trên chính máy chủ control plane (node đầu tiên,
`localhost`); các bước dưới đây thêm node *khác*. Hãy sao chép chứng chỉ CA công khai
của control plane sang node trước (xem [Control plane](#control-plane)), rồi chạy
trình cài đặt:

```sh
scp root@<cp-host>:/var/lib/gotham/ca/ca.crt .
git clone https://github.com/justindeelux/gotham.git
sudo GOTHAM_AGENT_CP_ADDR=<cp-host>:9442 GOTHAM_AGENT_NODE_ID=<node> \
    gotham/deploy/install-agent.sh --ca ./ca.crt
```

Trình cài đặt **thất bại an toàn** khi không có CA: nó từ chối chạy trừ khi có
`--ca <path>` (hoặc `GOTHAM_AGENT_CA_FILE=<path>`), nên một node không bao giờ quay số
tới control plane bằng plaintext. Nó cài chứng chỉ thành `/etc/gotham/ca.crt` (thuộc
sở hữu root, `0644`) và ghi `GOTHAM_AGENT_CA=/etc/gotham/ca.crt` vào
`/etc/gotham/agent.env`, việc này khiến agent xác minh control plane qua TLS.
`--insecure` là ghi đè **chỉ dành cho phát triển cục bộ** đã được ghi nhận: nó bỏ qua
kiểm tra CA, ghi `GOTHAM_AGENT_INSECURE=true`, và để `GOTHAM_AGENT_CA` chưa đặt, nên
agent kết nối không có TLS. Bản thân agent giờ cũng thất bại an toàn: không có CA thì
nó từ chối khởi động trừ khi `GOTHAM_AGENT_INSECURE=true`, và một listener plaintext
bị giới hạn ở loopback (trình cài đặt mặc định địa chỉ là `127.0.0.1:9443`). Không bao
giờ dùng nó trên một node thật.

Qua TLS, kênh là song hướng: agent tạo một khóa, gửi CSR trong `Register`, nhận một
chứng chỉ node, và xuất trình nó trong mọi kết nối về sau; control plane ràng buộc
định danh chứng chỉ và khóa với node, và mỗi agent chỉ chấp nhận control plane (một
chứng chỉ mang định danh `spiffe://gotham/control-plane`), nên một node không thể đóng
vai control plane với node khác. Lần đăng ký đầu tiên của một node chưa có chứng chỉ và
cần token đăng ký: control plane tạo một token tại `<GOTHAM_CA_DIR>/enrollment-token`
(hoặc bạn đặt `GOTHAM_GRPC_ENROLLMENT_TOKEN`), và bạn đưa nó cho trình cài đặt bằng
`--enrollment-token-file /root/token` (một tệp thuộc sở hữu root, chế độ 0600, không
phải symlink) hoặc, không cần echo, biến môi trường `GOTHAM_AGENT_ENROLLMENT_TOKEN` cho
`sudo -E`. Không bao giờ viết nó dưới dạng `sudo VAR=<token> …`: cách đó phơi bí mật
trong `ps`/`/proc` và lịch sử shell. Không có token (và không có tùy chọn rõ ràng
`GOTHAM_GRPC_ALLOW_OPEN_ENROLLMENT=true`), lần đăng ký đầu tiên bị từ chối. Chỉ lần
đăng ký đầu tiên cần nó. Các node đã đăng ký trước thay đổi này vẫn hoạt động (control
plane chấp nhận chứng chỉ hiện có của chúng và ràng buộc khóa của nó), và một lần tự
cập nhật không bao giờ đụng tới khóa hay chứng chỉ của node.

Trình cài agent dùng cùng việc xác minh manifest đã ký (`deploy/release-verify.sh`)
cho `gotham-agent-linux-<arch>` và `gotham-agent-manifest-<arch>.txt`, với khóa công
khai phát hành đã ghim và không có ghi đè lúc chạy. Nó cài agent vào
`/var/lib/gotham-agent/bin/gotham-agent`, wrapper dùng chung thành
`gotham-agent-update`, cấu hình `/etc/gotham/agent-updater.conf`, quy tắc sudoers và
unit. Hãy đặt `GOTHAM_AGENT_CP_ADDR` (địa chỉ gRPC của control plane) và
`GOTHAM_AGENT_NODE_ID` trong môi trường trước khi chạy nó; chúng được ghi vào
`/etc/gotham/agent.env`.

Các cờ và biến hữu ích của trình cài agent:

| Cờ / biến | Mục đích |
|---|---|
| `--ca <path>` | Cài chứng chỉ CA này của control plane (bắt buộc). |
| `GOTHAM_AGENT_CA_FILE` | Giống `--ca`, qua biến môi trường. |
| `--insecure` | Chỉ để phát triển: kết nối không có TLS. |
| `--full` / `GOTHAM_AGENT_FULL=1` | Cài thêm Docker Engine và plugin compose từ kho apt chính thức của Docker (chỉ Ubuntu/Debian; không làm gì khi đã cài). Mặc định bật cho agent `localhost` do `install.sh` thiết lập; mặc định tắt khi chạy trực tiếp. |
| `--dry-run` | In ra những gì sẽ làm; không thay đổi gì. |
| `GOTHAM_AGENT_CA` | Đường dẫn phía agent mà trình cài đặt ghi (`/etc/gotham/ca.crt`). Nó **không** được đọc từ môi trường xung quanh; khi cài lại, trình cài đặt giữ giá trị đã có trong `agent.env`. Dùng `--ca`/`GOTHAM_AGENT_CA_FILE` để đổi nó. |
| `GOTHAM_AGENT_UPDATE_CHANNEL` | Kênh phát hành node này chấp nhận, `stable` (mặc định) hoặc `beta`. Node `beta` nhận cả đề nghị stable và beta; node `stable` từ chối đề nghị beta; một kênh rỗng hoặc không rõ bị từ chối. |
| `GOTHAM_AGENT_MANAGED_VOLUME_ROOT` | Thư mục cha của mọi bind mount ứng dụng mà node chấp nhận (mặc định `/var/lib/gotham/volumes`); quay về `GOTHAM_MANAGED_VOLUME_ROOT` dùng chung nếu không có. Phải khớp với control plane. |
| `--file-manager` / `--no-file-manager` / `GOTHAM_AGENT_FILE_MANAGER=yes\|no` | Trả lời câu hỏi của trình cài đặt về trình quản lý tệp mà không cần hỏi. Trình cài đặt có hỏi (mặc định **no**; không có terminal cũng nghĩa là no). Yes ghi `GOTHAM_AGENT_FILES=true` và một drop-in `gotham-agent.service.d/file-manager.conf` chạy agent **dưới root** mà không có sandbox của systemd; no ghi `GOTHAM_AGENT_FILES=false` và gỡ drop-in. Chạy lại sẽ giữ câu trả lời trước đó (một `GOTHAM_AGENT_FILES=true` do bạn đặt tay được giữ nguyên, không kèm drop-in root); một `GOTHAM_AGENT_FILES` từ môi trường bị bỏ qua để dùng câu trả lời đó. |
| `GOTHAM_AGENT_FILES` | Dịch vụ tệp từ xa trên node. Bật trừ khi đặt là `false` (trình cài đặt ghi câu trả lời ở trên). |
| `GOTHAM_AGENT_FILE_FULL_ACCESS` | Bật trừ khi đặt là `false`: dịch vụ phơi **toàn bộ hệ thống tệp** thành một gốc ghi được tên `system` (`/`), không có đường dẫn được bảo vệ — người vận hành đã chấp nhận rủi ro đó. `false` khôi phục các gốc bị giới hạn mô tả bên dưới. |
| `GOTHAM_AGENT_FILE_ROOTS` | Các gốc duyệt thêm, là đường dẫn tuyệt đối phân tách bằng dấu hai chấm (mặc định không có). Các gốc của người vận hành là **chỉ đọc** trong giai đoạn này; một gốc bị thiếu, không tuyệt đối, nằm trong cây nguy hiểm hoặc chồng lấn gốc khác sẽ bị từ chối lúc khởi động. |
| `GOTHAM_AGENT_ARCHIVE_MAX_BYTES` | Tổng số byte chưa nén mà một lần **nén hoặc giải nén** được đọc/ghi trước khi bị từ chối với `ResourceExhausted` (413) — giới hạn kích thước và một nửa biện pháp chống zip-bomb. Mặc định `4294967296` (4 GiB). |
| `GOTHAM_AGENT_ARCHIVE_MAX_ENTRIES` | Số mục tối đa mà một thao tác archive được duyệt hoặc giải nén (mặc định `100000`); vượt quá thì thao tác bị từ chối với `ResourceExhausted` (413). |
| `GOTHAM_AGENT_ARCHIVE_MAX_INPUT_BYTES` | Kích thước trên đĩa tối đa của một tệp archive mà **giải nén** sẽ đọc (mặc định `1073741824`, 1 GiB); archive lớn hơn bị từ chối với `ResourceExhausted` (413) trước khi bất kỳ bộ phân tích nào chạy. Áp dụng cho mọi định dạng. |
| `GOTHAM_AGENT_COMPOSE_ROOT` | Thư mục chứa một thư mục con cho mỗi dự án compose (mặc định `/var/lib/gotham-agent/compose`); duyệt được ở chế độ chỉ đọc khi dịch vụ tệp được bật. |

`GOTHAM_AGENT_CP_ADDR` phải dùng một tên hoặc IP nằm trong các SAN của listener
control plane (xem [Control plane](#control-plane)); nếu không, việc bắt tay TLS thất
bại và node không bao giờ đăng ký được. Nó phải ở dạng `host:port` không có khoảng
trắng, dấu nháy hay `=` (địa chỉ lắng nghe theo cùng quy tắc); mọi giá trị khác trong
`agent.env` cũng được kiểm tra trước tương tự — ký tự điều khiển và dấu gạch chéo
ngược ở cuối bị từ chối ở mọi nơi, node id bị giới hạn theo những gì agent chấp nhận
(tối đa 253 byte; không có khoảng trắng, tab, `*`, `/` hay dấu gạch chéo ngược) và từ
chối thêm dấu nháy (bộ phân tích EnvironmentFile của systemd sẽ bỏ hoặc nhóm lại
chúng, nên unit sẽ thấy một giá trị khác với giá trị trình cài đặt đã ghi), đường dẫn
phải tuyệt đối, endpoint docker là một trong `unix:///abs/path`, `/abs/path` hoặc
`tcp://host:port`, tự cập nhật đúng `true`/`false`, khoảng cách là một Go duration
(giá trị có khoảng trắng thừa được cắt như agent cắt; đơn vị ngày/tuần và giá trị tràn
bị từ chối) — tất cả trước khi trình cài đặt thay đổi bất cứ thứ gì, nên một giá trị
sai thất bại mà không tạo ra gì. `install.sh` chạy các kiểm tra này trước khi thay
đổi và luôn cài agent cục bộ của nó dưới tên `localhost`, kết nối tới `127.0.0.1:9442`.

Control plane tạo hoặc sửa node `localhost` dùng chung lúc khởi động; trạng thái của
nó chỉ thành sẵn sàng sau một nhịp tim từ agent. Xóa nó trả về HTTP 409; PATCH cũng bị
từ chối. Migration nâng cấp 49 đổi tên định danh agent cục bộ đã lưu tại chỗ, giữ
nguyên UUID máy chủ, ứng dụng, bản triển khai và router. Trình cài đặt chuyển id của
`agent.env` trước đó cho migration, nên đổi hostname hoặc chạy migration trong
container là an toàn. Với một lần migration thủ công, đặt `GOTHAM_LEGACY_LOCAL_NODE_ID`
thành node ID đã lưu đó. Migration tiến 51 cũng phát hiện một node duy nhất có địa chỉ
loopback với id tùy chỉnh (ví dụ `test-node-1` tại `127.0.0.1`), kể cả các địa chỉ
agent cục bộ như `localhost:9443` và `[::1]:9443`. Lúc khởi động, bước kiểm tra này
được lặp lại trước khi tạo `localhost`, giữ nguyên UUID, ứng dụng, cơ sở dữ liệu, dịch
vụ và router. Migration 51 chỉ gỡ placeholder `localhost` rỗng, đang chờ, chưa từng
đăng ký của một lần khởi động trước sau khi kiểm tra không có tham chiếu khóa ngoại
nào, rồi đổi tên dòng cũ. Một `localhost` đã đăng ký hoặc đang được tham chiếu, hoặc
nhiều ứng viên cục bộ, sẽ dừng migration với lỗi có thể xử lý được. Hãy kiểm tra
`agent.env` và các bản ghi máy chủ đã lưu, đối chiếu khối lượng công việc của chúng và
gỡ các mục agent cục bộ trùng lặp trước khi thử lại; không có khối lượng công việc nào
bị gộp hay xóa. Lúc khởi động, việc quét chỉ chạy khi `localhost` vắng mặt; các lần
đăng ký về sau không chặn được việc khởi động lại. Việc phát hiện dùng IP/địa chỉ agent
đã ghi, không bao giờ chỉ dùng node ID; các IP được sao chép trực tiếp từ node ID dạng
IP do agent chọn bị loại trừ. Ứng viên phải có dữ liệu đăng ký agent và không có người
dùng, khóa hay mật khẩu SSH; các node loopback/tunnel do SSH quản lý giữ nguyên định
danh và nhóm của chúng. Với một node cũ có IP công khai hoặc thiếu IP, hãy cung cấp
định danh đã lưu cho migration 49 như mô tả ở trên; migration 51 không diễn giải lại
gợi ý môi trường đó.

Sau khi nâng cấp thủ công, hãy đổi `GOTHAM_AGENT_NODE_ID` trong môi trường đã lưu của
agent cục bộ (thường là `/etc/gotham/agent.env`) thành `localhost` và khởi động lại
agent. Trình cài đặt làm việc này tự động; giữ id cũ có thể đăng ký lại node cũ và để
lại hai bản ghi cùng trỏ tới một agent.

Việc chính sách host của URL Git từ chối tên `localhost` chỉ áp dụng cho remote Git;
nó không kiểm tra node ID hay đường dẫn quay số loopback của agent. Node ID `localhost`
chính xác chỉ được chấp nhận từ một peer loopback. Chứng chỉ của nó dùng SAN riêng
`localhost.agent.gotham.invalid`, và control plane xác minh tên đó khi quay số tới
`127.0.0.1:9443`. Nó không thể được dùng làm chứng chỉ control plane cho `localhost`
hoặc một IP loopback. Đừng đưa SAN của agent vào danh sách host của listener control
plane; việc đăng ký sẽ từ chối sự trùng lặp đó. Các định danh và bí danh khác của
listener control plane vẫn được dành riêng. Agent từ xa phải đặt node ID riêng và địa
chỉ control plane riêng:

```sh
sudo GOTHAM_AGENT_CP_ADDR=<cp-host>:9442 GOTHAM_AGENT_NODE_ID=worker-1 \
    gotham/deploy/install-agent.sh --ca ./ca.crt
```

### Công cụ máy chủ của node (cài / gỡ)

Agent phơi `ToolService` (ListTools/InstallTool/UninstallTool) qua mTLS; control plane
điều khiển nó từ trình quản lý công cụ của node. Mọi bước đặc quyền đều chạy qua một
helper cố định, thuộc sở hữu root — cùng mô hình với `gotham-hostctl` — không bao giờ
dùng `sudo` tràn lan:

```sh
sudo install -D -m 0755 -o root -g root deploy/gotham-toolctl.sh /usr/libexec/gotham/gotham-toolctl
sudo deploy/install-toolctl-sudoers.sh gotham-agent
```

`install-agent.sh` cài cả hai, nên một lần cài agent bình thường không cần bước thêm.
Drop-in sudoers (`/etc/sudoers.d/gotham-toolctl`) cấp đúng một quy tắc cho mỗi động từ
**và** mỗi tool id:

- `install-tool <id>` — `railpack`, `buildkit`, `pack`, `docker`, `git`;
- `uninstall-tool <id>` — `railpack`, `buildkit`, `pack`, `git`
  (`docker` không bao giờ bị gỡ: nó sẽ phá hủy mọi khối lượng công việc trên node).

Agent chỉ truyền một id trong danh mục; helper sở hữu toàn bộ argv thực thi và kiểm
tra lại id với cùng danh sách cho phép, nên một agent bị xâm nhập không thể nêu tên
một gói, một URL hay một cờ thêm. Các unit máy chủ chỉ để phát hiện
(`systemd-networkd`, `systemd-resolved`, `networkctl`, `systemctl`, …) bị từ chối với
`FAILED_PRECONDITION` và không có trình cài nào cả.

Hành vi:

- **Phụ thuộc được phân giải** bởi agent, phụ thuộc trước:
  `railpack → buildkit → docker`. Mỗi công cụ được cài bằng một lần gọi helper.
- **Được ghim và xác minh.** Railpack và pack được cài từ tarball phát hành có phiên
  bản mà sha256 được viết cứng trong helper (không bao giờ dùng `install.sh` không
  ghim piped vào shell); BuildKit là image `moby/buildkit:v0.34.0` được ghim digest,
  chạy thành container đặc quyền được quản lý tên `buildkit`. Checksum không khớp sẽ
  hủy bỏ.
- **Lũy đẳng.** Công cụ đã phát hiện trên node sẽ bị bỏ qua. Việc cài BuildKit xóa
  một container cũ trước khi tạo lại và chỉ gắn tham chiếu phát hiện sau khi container
  chạy, nên một lần chạy thất bại không để công cụ trông như đã cài.
- **Mỗi lần một việc.** Một khóa toàn node cho phép một lần cài hoặc gỡ tại một thời
  điểm; lần gọi thứ hai bị từ chối với `RESOURCE_EXHAUSTED`. Yêu cầu bị hủy khi
  control plane ngắt luồng, và bị giới hạn bởi timeout 15 phút. Helper giữ `flock`
  riêng cho toàn bộ công việc, nên ngay cả một công việc mà yêu cầu từ agent đã bị hủy
  (unit tạm thời vẫn chạy cho đến khi xong hoặc đến khi chạm giới hạn
  `RuntimeMaxSec=840`, tùy cái nào tới trước) cũng chặn lần thử lại bằng một lời từ
  chối rõ ràng "already running" thay vì chạy chồng lên; một lần hủy nhẹ nhàng
  (`SIGTERM`) có thể hoãn trap dừng của helper cho đến khi lệnh `systemd-run` phía
  trước trả về, nên nó không bảo đảm dừng ngay.
- **Nâng cấp một node.** Helper và quy tắc sudoers của nó được `install-agent.sh` cài,
  không phải bởi việc hoán đổi tệp nhị phân tự cập nhật: một node chỉ cập nhật qua tự
  cập nhật `gotham-agent` vẫn giữ bản cài cũ và các lời gọi cài/gỡ công cụ của nó thất
  bại với thông báo "not allowed" của sudo. Chạy lại `deploy/install-agent.sh` trên
  node (lũy đẳng) để cài chúng.
- **Gỡ bị từ chối khi một công cụ đang được dùng** (`FAILED_PRECONDITION`): khi một
  bản build đang chạy, và khi một công cụ đã cài khác phụ thuộc vào nó (ví dụ BuildKit
  không thể đi khi Railpack đã cài). Chỉ các *bản build* đang diễn ra được theo dõi là
  "đang được dùng"; một dịch vụ compose hoặc một container thường dùng Docker thì
  không (Docker không gỡ được, nên chỉ các lần gỡ `buildkit`/`git` cần rào chắn này).
- **Sandbox.** Unit của agent chạy với `ProtectSystem=full`, nên `/usr`, `/etc` và
  `/boot` là chỉ đọc trong mount namespace của nó và một tiến trình `sudo` con thừa
  hưởng điều đó. Vì vậy helper tự thực thi lại chính nó qua
  `systemd-run --pipe --wait --collect`, việc này khởi động một unit tạm thời mới bên
  ngoài namespace của agent; node cần systemd (unit của agent vốn đã yêu cầu). Đầu ra
  được truyền là stdout/stderr của chính helper, rồi một `ToolResult{status, exit_code}`
  ở cuối.

Helper cũng chạy độc lập để gỡ lỗi (dưới root, không có `systemd-run`):

```sh
sudo GOTHAM_TOOLCTL_DRYRUN=1 GOTHAM_TOOLCTL_ARCH=amd64 GOTHAM_TOOLCTL_DISTRO=apt \
    /usr/libexec/gotham/gotham-toolctl install-tool railpack
```

## Dịch vụ tệp từ xa

**Chế độ toàn bộ hệ thống tệp (mặc định).** Dịch vụ tệp bật trừ khi
`GOTHAM_AGENT_FILES=false`, và mặc định nó phơi một gốc ghi được tên `system` chính là
`/`: mọi tệp mà tiến trình agent với tới được, không loại trừ đường dẫn nguy hiểm hay
được bảo vệ nào (kể cả khóa và chứng chỉ của chính agent). Chủ sở hữu đã chấp nhận rủi
ro này; chỉ người vận hành nền tảng mới dùng được nó. Những gì agent thực sự đọc hay
thay đổi được bị giới hạn bởi người dùng Unix của nó, nên trình cài đặt hỏi bạn có muốn
trình quản lý tệp không (mặc định không) và, nếu có, chạy agent dưới root qua drop-in
`file-manager.conf`. Khi chạy dưới người dùng `gotham-agent` không đặc quyền, cùng gốc
đó gần như chỉ đọc. Các node hiện có tự cập nhật lên phiên bản này sẽ phơi `/` cho người
vận hành với quyền của người dùng hiện tại cho đến khi `GOTHAM_AGENT_FILES=false` được
đặt.

Trong chế độ này, rào chắn thư mục ứng dụng vẫn bảo vệ các thư mục ứng dụng cấp cao
nhất của gốc volume được quản lý (không đổi tên/xóa khi một container đang gắn chúng).
`os.Root` từ chối đi theo một symlink *tuyệt đối* ngay cả bên trong `/`, nên một đường
dẫn qua nó (ví dụ `/var/run`) không thể đi vào qua liên kết; hãy dùng đường dẫn thật.
Trả lời không sau khi đã trả lời có sẽ chạy `chown -R gotham-agent` trên
`/var/lib/gotham-agent` để chứng chỉ và khóa mà agent chạy dưới root đã ghi lại thuộc
về người dùng dịch vụ. `GOTHAM_AGENT_FILES` và `GOTHAM_AGENT_FILE_FULL_ACCESS` bị tắt
bởi `false`, `0`, `no` hoặc `off`; mọi giá trị khác đều để chúng bật.

**Chế độ bị giới hạn** (`GOTHAM_AGENT_FILE_FULL_ACCESS=false`). Các gốc là một danh
sách cho phép cục bộ của agent:

- `GOTHAM_AGENT_MANAGED_VOLUME_ROOT` — gốc ghi được, chứa các bind mount của ứng dụng.
- `GOTHAM_AGENT_COMPOSE_ROOT` — cây compose, duyệt được ở chế độ chỉ đọc (lần
  `ComposeUp` tiếp theo sẽ ghi đè các chỉnh sửa).
- `GOTHAM_AGENT_FILE_ROOTS` — các gốc của người vận hành phân tách bằng dấu hai chấm,
  chỉ đọc trong giai đoạn này.

Mọi đường dẫn được phân giải và kiểm tra lúc khởi động. Một gốc không tuyệt đối, phân
giải vào `/etc /proc /sys /dev /run /boot /usr /home /root`, vào gốc dữ liệu của Docker
(`data-root` từ `/etc/docker/daemon.json`, nếu không thì `/var/lib/docker`) hoặc vào
trạng thái riêng của agent sẽ bị từ chối khởi động; một gốc chồng lấn gốc khác cũng
vậy. Một gốc dựng sẵn chưa tồn tại (Docker và dịch vụ compose tạo chúng một cách lười)
sẽ bị bỏ qua kèm cảnh báo thay vì hủy agent. Bản thân việc giới hạn là `os.OpenRoot`:
mọi truy cập được phân giải tương đối với một descriptor thư mục đang giữ, nên một
thành phần `..`, một đường dẫn tuyệt đối hoặc một symlink rời khỏi cây đều bị từ chối
bởi bước duyệt descriptor. Dịch vụ không được phơi ra khi chưa cấu hình CA của agent,
vì việc giới hạn không phải xác thực.

`List` trả về tối đa 5.000 mục mỗi trang, sắp theo tên. Một trang còn nhiều mục hơn sẽ
báo `truncated=true` và một `cursor` base64 không trong suốt; hãy truyền cursor trở lại
để tiếp tục, và tiếp tục phân trang cho đến khi `truncated=false` — một trang bị cắt
luôn mang cursor khác rỗng và tiến về phía trước, nên client không bao giờ lặp vô hạn.
Cursor là base64 vì một tên ở ranh giới có thể chứa byte không phải UTF-8 hợp lệ (hợp
lệ trên Linux), thứ mà một chuỗi proto3 thô không mang được; `name` hiển thị của một mục
như vậy được làm sạch thành U+FFFD và do đó không thể đưa lại vào `Stat`/`Read`, nhưng
cursor vẫn trỏ tới các byte thật. Một cursor sai định dạng bị từ chối với
`InvalidArgument`. Một lần `List` quét tối đa 100.000 mục thư mục và chỉ giữ trang
trong bộ nhớ; một thư mục lớn hơn thế thất bại với `ResourceExhausted` ("directory too
large") thay vì trả về một danh sách thiếu mà không báo.

### Archive (nén / giải nén)

`Compress` và `Extract` tạo và giải nén `zip`, `tar` và `tar.gz` **không dùng shell**
(chỉ `archive/zip`, `archive/tar`, `compress/gzip`; không có `os/exec` ở bất kỳ đâu).
Compress bỏ qua symlink và tệp đặc biệt (không bao giờ đi theo liên kết) và đếm chúng
trong thông điệp tiến trình cuối (thứ mà control plane không hiển thị lên trình duyệt
— xem bên dưới); archive được dàn dựng trong một tệp tạm và đổi tên vào vị trí, nên
một lần nén thất bại hoặc bị hủy không bao giờ để lại archive dở dang. Extract từ chối
các mục symlink, hard-link, device và FIFO, gỡ các bit setuid/setgid/sticky, và ghi mọi
mục qua `os.Root`, nên một tên tuyệt đối, một đoạn `..` hay một thư mục cha là symlink
đều không thể thoát khỏi gốc đích. Một tệp đã tồn tại bị từ chối trừ khi bên gọi đặt
`overwrite`; một thư mục đã tồn tại được dùng lại, không bao giờ bị gỡ. Mỗi tệp được
ghi vào một tệp tạm cùng thư mục rồi đổi tên đè lên đích, nên một lần ghi đè thất bại
không bao giờ cắt cụt tệp hiện có. Cả hai đều bảo vệ thư mục ứng dụng đang chạy (một
archive không thể thay một thư mục ứng dụng cấp cao nhất của gốc volume được quản lý),
từ chối gốc chỉ đọc, và tuân theo phần dự trữ dung lượng trống cùng ngân sách truyền
của đường ghi.

**Extract không có tính giao dịch.** Một lỗi giữa chừng để các mục đã giải nén nằm
nguyên tại chỗ (chỉ tệp đang ghi được dàn dựng và gỡ), nên bên gọi phải coi một lần
giải nén thất bại là kết quả một phần và dọn dẹp nếu cần.

Bốn giới hạn bảo vệ một archive: **kích thước trên đĩa của tệp archive** mà extract sẽ
đọc (`GOTHAM_AGENT_ARCHIVE_MAX_INPUT_BYTES`, mặc định 1 GiB, được kiểm tra trước khi
bất kỳ bộ phân tích nào chạy và cho mọi định dạng); tổng số byte chưa nén
(`GOTHAM_AGENT_ARCHIVE_MAX_BYTES`, mặc định 4 GiB); số mục
(`GOTHAM_AGENT_ARCHIVE_MAX_ENTRIES`, mặc định 100000); và một tỉ lệ nén **cố định**
**200:1** so với kích thước của chính archive (không cấu hình được qua biến môi
trường). Các mục thư mục cũng tính vào giới hạn số mục. Với zip, bản ghi
end-of-central-directory (và locator/record zip64 khi có) được đọc từ phần đuôi tệp
trước khi `zip.NewReader` chạy, và archive bị từ chối khi số mục khai báo vượt
`GOTHAM_AGENT_ARCHIVE_MAX_ENTRIES` hoặc kích thước central-directory khai báo vượt 64
MiB — nên một archive được dàn dựng khai báo hàng triệu mục bị từ chối trước khi bất kỳ
header nào được cấp phát. Một tên mục sâu hơn 64 đoạn bị từ chối. Một archive vượt giới
hạn bị từ chối trước khi nó làm đầy đĩa. Mọi giới hạn được đọc dưới dạng số nguyên
dương; một giá trị không phân tích được hoặc không dương lặng lẽ quay về mặc định
(giống các giới hạn `GOTHAM_AGENT_FILE_*` khác), nên một lỗi gõ không tắt được giới
hạn.

Số mục bị bỏ qua mà một lần nén phát ra **không được hiển thị lên trình duyệt**: agent
báo nó trong `Progress.skipped` cuối của luồng, nhưng control plane rút cạn luồng và
chỉ trả về archive đã tạo, nên giao diện không hiển thị được.

Một giới hạn archive được trả lời bằng **413** với mã body riêng `archive_limit`, một
mục archive bị từ chối (liên kết, device, FIFO hoặc loại không được hỗ trợ) bằng
**409** với mã `archive_entry`, và một body archive bị cắt hoặc hỏng bằng **400** với
mã `invalid_archive`, nên giao diện hiển thị thông báo riêng cho archive thay vì "file
is too large to edit/upload", "conflict" hay một lỗi nội bộ.

Một yêu cầu archive **mở suốt toàn bộ quá trình truyền**: control plane giữ kết nối
đến hết ngân sách truyền của nó, và trình duyệt tự tắt timeout phía client, nhưng một
reverse proxy đặt trước control plane phải cho phép một phản hồi sống lâu, nếu không
yêu cầu sẽ bị cắt trong khi agent vẫn đang làm việc. Traefik ở biên của node không đứng
trước API tệp, nên các timeout của nó không áp dụng.

### Phía control plane

Các tuyến `/api/v1/servers/{id}/files` được gắn mặc định; đặt `FEATURE_FILES=false` để
tắt chúng (khi đó chúng trả **503**). Toàn bộ bề mặt bị giới hạn cho **người vận hành
nền tảng** (một API token có phạm vi admin, một phiên mang claim vai trò `admin`, hoặc
một email trong `PLATFORM_ADMINS`): mọi người khác nhận **403**, vì API phơi hệ thống
tệp của node. Người vận hành cũng cần thuộc nhóm của node, và các API token có phạm vi
read/deploy không còn với tới được API tệp (cần một token có phạm vi admin). Các tuyến
cũng có phạm vi nhóm: một máy chủ duyệt được phải thuộc một nhóm (một node cũ không có
nhóm trả 404), `roots`/`list`/`stat` cần bất kỳ thành viên nào, còn `content`,
`download`, các lần tải lên và mọi thao tác thay đổi cần vai trò chủ sở hữu/quản trị.
Body của tệp có thể chứa bí mật (một `.env` compose `0600`), nên `GET /content` và
`POST /download-ticket` vẫn chỉ dành cho chủ sở hữu/quản trị. Với API token:
`roots`/`list`/`stat` đọc được bằng token phạm vi **read**; mọi thao tác ghi,
`GET /content` và `POST /download-ticket` cần phạm vi **deploy** (cổng chặn phương thức
không phải GET cộng với hậu tố đọc mang bí mật); và `GET /files/download` có cổng vé
nằm ngoài nhóm xác thực, nên vé dùng một lần chính là thông tin đăng nhập của nó.

| Biến | Mặc định | Mục đích |
|---|---|---|
| `FEATURE_FILES` | bật | Đặt `false` để tắt API tệp (503 khi tắt). |
| `FILES_MAX_UPLOAD_BYTES` | `1073741824` (1 GiB) | Mức trần cho một body ghi/tải lên (413 khi vượt); nên khớp với `GOTHAM_AGENT_FILE_MAX_UPLOAD` của agent. |
| `FILES_SPOOL_DIR` | `<tmpdir>/gotham-spool` (0700) | Thư mục dàn dựng một body tải lên dạng luồng trước khi nó tới agent. |
| `FILES_SPOOL_CONCURRENCY` | `4` | Số body tải lên được dàn dựng cùng lúc tối đa; vượt quá thì một lần tải lên trả 429. |

Một body dạng luồng được dàn dựng trên control plane (header ghi của agent phải khai
báo kích thước chính xác, thứ mà một phần multipart không mang). Spool được quét lúc
khởi động và mỗi giờ, và dung lượng trống của nó được kiểm tra với mức trần trước mọi
lần ghi. API tệp do control plane phục vụ trực tiếp, nên nó **không** đi qua Traefik
của node; xem `deploy/README.md` → "File-manager uploads (staging)" để biết các nút
điều chỉnh việc dàn dựng.

### Quyền (chmod)

`POST /files/chmod` (chủ sở hữu/quản trị, phạm vi deploy với API token) đổi bit quyền
của một mục. Body là `{root, path, mode}` và **`mode` là bắt buộc**: trường bị thiếu bị
từ chối **400** thay vì lặng lẽ áp dụng `000`. Chỉ các bit thấp `0o777` được chấp nhận:
một yêu cầu mang setuid (`0o4000`), setgid (`0o2000`), sticky (`0o1000`) hoặc bất kỳ
bit cao hơn bị từ chối **400** trước khi agent được gọi. Một symlink bị từ chối **400**
(nó không bao giờ đi theo liên kết để đổi đích của nó), một tệp đặc biệt
(FIFO/socket/device) **409**, và một gốc chỉ đọc **403**. Agent cũng từ chối một thư
mục sẽ mất bit execute của chủ sở hữu (nó phải còn đi qua được) và một tệp thường có
hard-link trong một gốc bị giới hạn (inode của nó có thể với tới được từ ngoài gốc); gốc
toàn bộ hệ thống tệp không có "bên ngoài", nên việc kiểm tra hard-link ở đó không cần
thiết. Các bit setuid/setgid/sticky hiện có được **giữ nguyên, không sửa được**: chỉ
các bit quyền thường thay đổi và một bit đặc biệt không bao giờ được tạo. Một agent cũ
trước RPC này trả lời **501**. chmod không bao giờ đổi loại của một mục, nên rào chắn
thư mục ứng dụng (chỉ cho đổi tên/xóa) không áp dụng; đệ quy không được hỗ trợ.

Việc giới hạn mở gốc và mọi thư mục cha bằng `O_PATH` (cần quyền tìm, không cần đọc),
`openat` mục với `O_NOFOLLOW`, `fstat` descriptor để phân loại, và chmod đúng inode đó —
qua `fchmodat2(AT_EMPTY_PATH)` ở nơi kernel hỗ trợ (Linux 6.6+) và qua liên kết ma thuật
`/proc/self/fd` ở nơi khác. Không gì bên ngoài gốc có thể thay đổi, một liên kết bị
hoán đổi vào không thể bị đi theo, và một mục mà agent vừa làm thành không đọc được
(`000` hoặc `0o111`) vẫn cứu được qua giao diện, kể cả bởi một agent không chạy root.

### Mô hình quyền

Agent chạy dưới người dùng không đặc quyền `gotham-agent` và không có
`CAP_DAC_OVERRIDE`, nên **ai sở hữu một tệp quyết định trình quản lý tệp làm được gì**:

- Gốc `volumes` (mặc định `/var/lib/gotham/volumes`) là gốc ghi được, nhưng Docker tạo
  mỗi thư mục bind của ứng dụng với `root:root 0755`. Agent liệt kê được nó và đọc được
  các tệp ai cũng đọc được, nhưng **không tạo, đổi tên, xóa hay sửa** được bất cứ thứ gì
  một container đã ghi; một tệp `0600` là không đọc được. Đây là trải nghiệm chủ yếu
  đọc có chủ đích, với trạng thái "permission denied" theo từng mục.
- Gốc `compose` và `GOTHAM_AGENT_FILE_ROOTS` là **chỉ đọc** trong dịch vụ dù hệ thống
  tệp có thể cho phép ghi (một chỉnh sửa compose sẽ bị lần `ComposeUp` tiếp theo ghi
  đè).

Để container sửa được, việc cấp phát phải làm cho `<root>/<appID>` ghi được bởi agent,
bằng **hoặc** control plane chown nó cho `gotham-agent`, **hoặc** một nhóm dùng chung
cộng thư mục ứng dụng có setgid (`2775`) (agent cũng phải thuộc nhóm đó). Đây là một
**quyết định của chủ sở hữu** cần có trước khi bật một trong hai cờ cho các node như
vậy; chỉ setgid là không đủ cho đến khi agent tham gia nhóm.

## Cập nhật và quay lui

Các bản phát hành được xác minh bằng khóa công khai nhúng trong tệp nhị phân đang chạy,
nên không cần xác minh thủ công lúc cập nhật. Tệp nhị phân `gotham` nằm trong thư mục
thuộc sở hữu của dịch vụ `/var/lib/gotham/bin`, nên hãy chạy CLI vận hành dưới người
dùng dịch vụ:

```sh
sudo -u gotham /var/lib/gotham/bin/gotham update check
sudo -u gotham /var/lib/gotham/bin/gotham update apply
sudo -u gotham /var/lib/gotham/bin/gotham update rollback
```

Chạy dưới root sẽ cài một tệp nhị phân `root:root` đè lên tệp thuộc sở hữu của dịch vụ
và lần áp dụng tiếp theo do dịch vụ chạy sẽ thất bại ở bước sao lưu hardlink
(`fs.protected_hardlinks`); `gotham update apply`/`rollback` từ chối chủ sở hữu không
khớp với thông báo rõ ràng.

- **Control plane:** các lệnh trên, hoặc đặt `AUTO_UPDATE=true` trong
  `/etc/gotham/gotham.env` để cập nhật không cần giám sát. Wrapper kiểm tra sức khỏe tệp
  nhị phân mới và quay lui về `gotham.old` khi thất bại.
- **Node agent:** agent cập nhật từ control plane qua kênh TLS đã xác minh bằng CA; bật
  áp dụng không cần giám sát bằng `GOTHAM_AGENT_AUTO_UPDATE=true`, hoặc kích hoạt một
  đợt triển khai toàn hệ thống bằng API chỉ dành cho người vận hành
  `POST /api/v1/servers/agents/update-all` — chưa có điều khiển nào trên giao diện cho
  việc này. Một bản phát hành không kích hoạt được sẽ quay lui và phiên bản của nó bị
  giãn thời gian chờ (xem `deploy/README.md`).
- **Quay lui:** `gotham update rollback` khôi phục tệp nhị phân trước đó trên đĩa. Tiến
  trình đang chạy vẫn giữ tệp nhị phân hiện tại cho đến khi được khởi động lại, nên hãy
  chạy `systemctl restart gotham` sau đó để thực sự chạy tệp nhị phân đã khôi phục.

Cài lại một phiên bản cụ thể là một lần cài bình thường với `GOTHAM_VERSION=vX.Y.Z`.

## Luồng phát hành và ký

Các bản phát hành được build và công bố bởi `.github/workflows/release.yml` trên một
thẻ `v*`, trên một runner do GitHub host. Công việc phát hành nhắm tới **environment
`release`** của GitHub (với một người xem lại bắt buộc), nên nó đọc các secret của
environment và chờ chủ sở hữu phê duyệt trước khi được dùng chúng:

1. Công việc thất bại an toàn trừ khi **cả hai** secret của environment được đặt và
   khác rỗng: `GOTHAM_UPDATE_PUBLIC_KEY` (khóa công khai Ed25519 thô dạng base64, được
   nhúng vào cả hai tệp nhị phân bằng `-ldflags`) và `GOTHAM_UPDATE_SIGNING_KEY` (khóa
   riêng Ed25519 PEM PKCS#8, chỉ dùng trên runner).
2. Nó cũng kiểm tra rằng khóa riêng tương ứng với `GOTHAM_UPDATE_PUBLIC_KEY` và với
   `deploy/gotham-signing-key.pub` đã commit (và do đó với khóa nhúng trong
   `deploy/install.sh`).
3. GoReleaser build `gotham` và `gotham-agent` cho `linux/amd64` và `linux/arm64` dưới
   dạng tệp nhị phân thô tên `gotham-linux-<arch>` / `gotham-agent-linux-<arch>` (những
   tên mà các bộ kiểm tra cập nhật phân giải), nhúng phiên bản và khóa công khai, và tạo
   một GitHub Release **nháp** với các tệp nhị phân, `checksums.txt` và khóa công khai
   đã công bố.
4. Workflow ký các manifest theo kiến trúc (`gotham-manifest-<arch>.txt` và
   `gotham-agent-manifest-<arch>.txt`) trên đúng các byte đã build. Nó định vị bản
   **nháp** qua danh sách phát hành (bản nháp không được `/releases/tags/<tag>` trả về),
   kiểm tra 6 asset gốc mà GoReleaser đã tải lên, tải lên 8 tệp manifest/chữ ký, rồi
   khẳng định đủ bộ 14 asset (4 tệp nhị phân, `checksums.txt`, khóa công khai và cả 8
   tệp manifest/chữ ký) trước khi công bố bản nháp và xác minh nó không còn là bản nháp.
   Bản phát hành nháp bị bộ kiểm tra bỏ qua, nên một bản phát hành chưa đầy đủ không bao
   giờ được đề nghị.

Cách đặt tên asset là một hợp đồng với mã cập nhật — xem `internal/updates/checker.go`
(control plane), `internal/updates/agents.go` (agent) và `updatecore.ManifestName` /
`ManifestNameWithPrefix`.

### Runner phát hành

Công việc phát hành và mọi workflow của PR (`ci.yml`, `e2e.yml`, `ui-e2e.yml`) chạy trên
runner do GitHub host, nên mã `pull_request` không đáng tin không bao giờ thực thi trên
máy build và ký bản phát hành. Khóa ký nằm trong environment `release`, và một bản build
từ thẻ `v*` chỉ đọc nó sau khi người xem lại bắt buộc phê duyệt lần chạy. Cổng người xem
lại vì vậy điều khiển *ai có thể tạo một bản phát hành đã ký*: hãy giữ bộ quy tắc thẻ
`v*` và những người xem lại bắt buộc trong phạm vi chủ sở hữu dự án.

### Cặp khóa

- **Khóa công khai** (`deploy/gotham-signing-key.pub`): an toàn để công bố. Nó được
  nhúng trong cả hai tệp nhị phân, trong `deploy/install.sh`, và được tải lên như một
  asset phát hành để người dùng so sánh được.
- **Khóa riêng** (secret của environment `GOTHAM_UPDATE_SIGNING_KEY`): bí mật duy nhất.
  Nó không bao giờ được commit hay in ra. **Hãy sao lưu khóa riêng ngoại tuyến** — nếu
  mất, không bản cài đặt hiện có nào xác minh được một bản phát hành mới và phải xoay
  vòng một cặp khóa mới vào (việc này đòi hỏi cài lại hoặc nhúng khóa mới trong một bản
  phát hành thủ công). Tạo nó bằng:

  ```sh
  go run ./cmd/signer keygen -out signing.key
  # put signing.key (PKCS#8 PEM) in the GOTHAM_UPDATE_SIGNING_KEY release environment secret
  # put signing.key.pub at deploy/gotham-signing-key.pub
  # put the base64 value printed above in GOTHAM_UPDATE_PUBLIC_KEY and in
  # deploy/install.sh (GOTHAM_RELEASE_PUBLIC_KEY_B64) and install-agent.sh
  ```

## Thử chuỗi cài đặt mà không cần GitHub

`deploy/test-release-install.sh` build một bộ ký và các tệp nhị phân snapshot, ký các
manifest, phục vụ chúng từ một máy chủ phát hành giả trên loopback (kể cả một lần
chuyển hướng `/releases/latest`), và chạy `deploy/install.sh` với nó dưới một
`GOTHAM_INSTALL_ROOT` tạm. Nó bao phủ đường thẻ đã ghim, việc phân giải thẻ mới nhất mặc
định, họ agent, cài lại mà không có thêm gì từ người vận hành (bản hồi quy B1), việc giữ
nguyên khi cài lại (khóa của người vận hành và DSN được quản lý), và các trường hợp thất
bại an toàn (artifact bị sửa, manifest bị sửa, khóa ghim không khớp):

```sh
sh deploy/test-release-install.sh
```
