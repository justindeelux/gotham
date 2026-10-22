---
title: Cấu hình
---

# Tham chiếu cấu hình

Control plane đọc `gotham.yaml` từ thư mục làm việc của nó. Tệp này là tùy chọn:
mọi khóa đều tùy chọn, thiếu tệp không phải lỗi, và các bảng dưới đây nêu giá trị
mặc định hoặc trạng thái rỗng cho từng khóa. Giá trị được xếp lớp — mặc định tích
hợp, rồi tới tệp, rồi tới các biến môi trường `GOTHAM_*`, vốn thắng.

Agent không đọc `gotham.yaml`; nó được cấu hình từ biến môi trường, chủ yếu là
`GOTHAM_AGENT_*`. Cài đặt tự cập nhật và các cờ tính năng được đọc trực tiếp từ môi
trường.

Một control plane đang chạy theo dõi tệp và nạp lại mà không cần khởi động lại,
nhưng chỉ `log.level` được áp dụng ngay. Các giá trị kết nối, listener và chứng chỉ
được dùng lúc khởi động, nên đổi chúng cần khởi động lại; `log.format` cũng cần
khởi động lại. Một chỉnh sửa không hợp lệ được ghi log và các giá trị trước đó vẫn
giữ hiệu lực.

## Bộ lắng nghe HTTP

| Khóa | Biến môi trường | Mặc định | Bắt buộc |
|---|---|---|---|
| `server.addr` | `GOTHAM_SERVER_ADDR` | `0.0.0.0` | không |
| `server.port` | `GOTHAM_SERVER_PORT` | `8000` | không |
| `server.trusted_proxies` | `GOTHAM_TRUSTED_PROXIES` | rỗng — không tin cậy peer nào | không |

- `server.port` phải nằm trong khoảng 1 đến 65535.
- `server.trusted_proxies` là một danh sách YAML hoặc một giá trị môi trường gồm các
  IP và CIDR cách nhau bằng dấu phẩy. Chỉ các header `X-Forwarded-For` và
  `X-Forwarded-Proto` của những peer này được tôn trọng; từ mọi peer khác chúng bị
  bỏ qua. Hãy đặt nó khi có một proxy kết thúc TLS đứng trước control plane, nếu
  không các máy khách sẽ dùng chung một giỏ giới hạn tần suất và HTTPS không được
  nhận ra. Giữ danh sách đúng bằng địa chỉ của proxy: mọi máy chủ trong một dải được
  tin cậy đều có thể giả mạo địa chỉ và giao thức của máy khách. Đổi giá trị này cần
  khởi động lại.

## Cơ sở dữ liệu và Redis

| Khóa | Biến môi trường | Mặc định | Bắt buộc |
|---|---|---|---|
| `database.dsn` | `GOTHAM_DATABASE_DSN` | `postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable` | có — cơ sở dữ liệu kết nối được |
| `redis.addr` | `GOTHAM_REDIS_ADDR` | `localhost:6379` | có — instance kết nối được |

- `database.dsn` là chuỗi kết nối libpq/pgx mà store, phần kiểm tra sức khỏe và
  `gotham migrate` dùng. `gotham serve` thoát khi không kết nối được.
- `redis.addr` là một `host:port`. Redis chống lưng cho cập nhật thời gian thực,
  truyền nhật ký, sao lưu và hàng đợi nền; nó không được kiểm tra lúc khởi động,
  nhưng `/healthz` trả lời 503 khi không kết nối được.

## Gateway gRPC và tổ chức phát hành chứng chỉ

| Khóa | Biến môi trường | Mặc định | Bắt buộc |
|---|---|---|---|
| `grpc.addr` | `GOTHAM_GRPC_ADDR` | `:9442` | không |
| `grpc.hosts` | `GOTHAM_GRPC_HOSTS` | rỗng | không |
| `grpc.insecure` | `GOTHAM_GRPC_INSECURE` | `false` | không |
| `grpc.enrollment_token` | `GOTHAM_GRPC_ENROLLMENT_TOKEN` | được sinh cạnh CA | không |
| `grpc.allow_open_enrollment` | `GOTHAM_GRPC_ALLOW_OPEN_ENROLLMENT` | `false` | không |
| `grpc.legacy_leaf_until` | `GOTHAM_GRPC_LEGACY_LEAF_UNTIL` | ngày kết thúc tích hợp | không |
| `ca.dir` | `GOTHAM_CA_DIR` | `./data/ca/` | có, ở môi trường production |

- `grpc.addr` là nơi agent trên node gọi tới. `grpc.hosts` thêm tên DNS và IP vào
  SAN của chứng chỉ listener để một agent từ xa gọi control plane theo tên vẫn xác
  minh được. Các tên loopback và hostname của máy luôn được bao gồm. Khi
  `grpc.hosts` rỗng, danh sách đã lưu bởi `gotham ca init --host` được dùng.
- `grpc.insecure: true` là lựa chọn phát triển cho phép gateway khởi động ở dạng văn
  bản thường khi không có CA. Không có CA và không có cờ này, `serve` từ chối khởi
  động kênh agent.
- `grpc.enrollment_token` là thông tin xác thực khởi tạo mà một node trình ra ở lần
  đăng ký đầu. Khi rỗng, `serve` sinh một token và ghi vào
  `<ca.dir>/enrollment-token`; hãy truyền giá trị đó cho node qua
  `GOTHAM_AGENT_ENROLLMENT_TOKEN` (hoặc bằng
  `install-agent.sh --enrollment-token-file`). Lần đăng ký đầu thất bại theo hướng
  an toàn khi thiếu nó, trừ khi đăng ký mở được bật rõ ràng.
- `grpc.allow_open_enrollment: true` chấp nhận lần đăng ký đầu không kèm token (tin
  cậy ở lần dùng đầu). Hãy để `false` trên một instance thật.
- `grpc.legacy_leaf_until` là một mốc thời gian RFC 3339, hoặc `none` để tắt ngày
  kết thúc cho các chứng chỉ node được cấp trước khi có mutual TLS. Để trống thì
  chọn mặc định tích hợp; một giá trị sai định dạng làm khởi động thất bại.
- `ca.dir` chứa `ca.crt`, `ca.key` (mode 0600) và danh sách máy chủ SAN tùy chọn.
  Hãy tạo nó bằng `gotham ca init` (xem [CLI](/vi/reference/cli)).

## Bí mật và ghi log

| Khóa | Biến môi trường | Mặc định | Bắt buộc |
|---|---|---|---|
| `secret_key` | `GOTHAM_SECRET_KEY` | khóa trong `<ca.dir>/secret.key` | nên đặt |
| `log.level` | `GOTHAM_LOG_LEVEL` | `info` | không |
| `log.format` | `GOTHAM_LOG_FORMAT` | `json` | không |

- `secret_key` mã hóa thông tin xác thực đã lưu khi lưu trữ (khóa riêng SSH và bí mật
  TOTP) bằng AES-256-GCM. Khi chưa đặt, một khóa được đọc từ — hoặc tạo một lần
  trong — `<ca.dir>/secret.key`; nếu thư mục đó không ghi được, một khóa tạm được
  sinh và thông tin xác thực đã lưu không sống qua lần khởi động lại. Hãy đặt nó rõ
  ràng ở môi trường production.
- `log.level` chọn mức tối thiểu (`debug`, `info`, `warn`, `error`); một giá trị
  không nhận ra sẽ quay về `info`. `log.format` là `json` hoặc `text` và bị từ chối
  khi nạp nếu không phải một trong hai.

## Khóa ký JWT

| Khóa | Biến môi trường | Mặc định | Bắt buộc |
|---|---|---|---|
| `auth.jwt_private_key_path` | `GOTHAM_AUTH_JWT_PRIVATE_KEY_PATH` | không có — cặp khóa tạm | nên đặt |
| `auth.jwt_public_key_path` | `GOTHAM_AUTH_JWT_PUBLIC_KEY_PATH` | không có — cặp khóa tạm | nên đặt |

- Cặp khóa là Ed25519 dạng PEM: khóa riêng ở dạng PKCS#8 (`PRIVATE KEY`), khóa công
  khai ở dạng PKIX (`PUBLIC KEY`), như `openssl genpkey -algorithm ed25519` tạo ra.
- Hãy đặt cả hai đường dẫn. Thiếu một trong hai thì máy chủ sinh một cặp khóa tạm
  lúc khởi động và các phiên không sống qua lần khởi động lại. Một đường dẫn đã cấu
  hình nhưng không đọc được làm khởi động thất bại.
- `GOTHAM_AUTH_ALLOW_REGISTRATION=true` (không có khóa YAML) mở lại việc tự đăng ký
  trên một instance đã có tài khoản. Chỉ dùng cho kiểm thử và phát triển.

## OAuth (GitHub)

GitHub là nhà cung cấp OAuth duy nhất. Nhà cung cấp bị tắt khi client ID hoặc client
secret còn rỗng, và tuyến đăng nhập của nó khi đó trả lời 404.

| Khóa | Biến môi trường | Mặc định | Bắt buộc |
|---|---|---|---|
| `oauth.github.client_id` | `GOTHAM_OAUTH_GITHUB_CLIENT_ID` | rỗng — nhà cung cấp tắt | để bật |
| `oauth.github.client_secret` | `GOTHAM_OAUTH_GITHUB_CLIENT_SECRET` | rỗng — nhà cung cấp tắt | để bật |
| `oauth.github.redirect_url` | `GOTHAM_OAUTH_GITHUB_REDIRECT_URL` | suy ra từ URL control plane | không |

- URL callback ủy quyền của ứng dụng OAuth GitHub phải khớp với redirect hiệu lực:
  `/api/v1/auth/oauth/github/callback` trên origin của control plane.
- Khi `redirect_url` rỗng, callback được suy ra từ URL control plane đã cấu hình của
  instance. Không đặt cả hai thì không có redirect nào được gửi và GitHub dùng
  callback đã đăng ký của ứng dụng OAuth.

## Chính sách nguồn Git

| Khóa | Biến môi trường | Mặc định | Bắt buộc |
|---|---|---|---|
| `deploy.git_allow_private_hosts` | `GOTHAM_GIT_ALLOW_PRIVATE_HOSTS` | `false` | không |

- `false` từ chối việc git clone và dò các remote trên địa chỉ loopback, RFC1918,
  ULA, link-local (gồm địa chỉ metadata của cloud), unspecified, multicast,
  carrier-grade NAT, benchmarking, NAT64 và 6to4.
- `true` cho phép remote RFC1918, ULA và loopback, dành cho một máy chủ git tự vận
  hành. Link-local và các dải luôn bị từ chối khác vẫn bị từ chối.

## Thư mục gốc của volume được quản lý

Các bind mount của ứng dụng bị giới hạn trong `<root>/<application id>` ở cả hai
phía. Control plane suy ra đường dẫn; node kiểm tra lại nó với thư mục gốc của chính
mình, nên hai giá trị phải giống nhau.

| Biến | Đọc bởi | Mặc định | Bắt buộc |
|---|---|---|---|
| `GOTHAM_MANAGED_VOLUME_ROOT` | control plane | `/var/lib/gotham/volumes` | không |
| `GOTHAM_AGENT_MANAGED_VOLUME_ROOT` | agent trên node | `/var/lib/gotham/volumes` | không |

- Giá trị phải là đường dẫn tuyệt đối. Agent quay về
  `GOTHAM_MANAGED_VOLUME_ROOT` khi biến của chính nó chưa đặt, nên việc xuất biến
  dùng chung trên cả hai máy chủ giúp chúng khớp nhau.
- Một giá trị tương đối hoặc rỗng ở control plane quay về mặc định; agent từ chối
  giá trị tương đối lúc khởi động.

## Tự cập nhật (control plane, chỉ qua biến môi trường)

| Biến | Mặc định | Mục đích |
|---|---|---|
| `AUTO_UPDATE` | `false` | Chạy vòng lặp kiểm tra/áp dụng theo lịch. `true` bật cả kiểm tra và áp dụng. |
| `AUTO_UPDATE_INTERVAL` | `6h` | Khoảng cách của vòng lặp tự động (Go duration). |
| `GOTHAM_UPDATE_CHANNEL` | `stable` | Kênh phát hành: `stable` hoặc `beta`. |
| `GOTHAM_UPDATE_REPO` | `justindeelux/gotham` | Kho GitHub công bố các bản phát hành (`owner/name`). |
| `GOTHAM_UPDATE_BASE_URL` | `https://api.github.com` | Gốc API phát hành cho một máy chủ GitHub Enterprise hoặc một mirror. Phải là http(s); http thường chỉ cho loopback. |
| `GOTHAM_UPDATE_PUBLIC_KEY` | được nhúng trong bản build phát hành | Khóa công khai của bản phát hành (PEM hoặc base64). Ghi đè cho phát triển; chỉ dùng khi không có khóa nào được nhúng. |
| `GOTHAM_UPDATE_SCRIPT` | `/usr/libexec/gotham/gotham-update` | Wrapper khởi động lại và kiểm tra sức khỏe, thuộc sở hữu root. |
| `GOTHAM_UPDATE_STATUS` | `/var/lib/gotham-updater/update.status` | Tệp trạng thái thuộc sở hữu root do wrapper ghi. |
| `GOTHAM_UPDATE_PENDING` | `/var/lib/gotham/update.pending` | Dấu chặn cập nhật đã chuẩn bị. |
| `GOTHAM_UPDATE_BINARY` | `/var/lib/gotham/bin/gotham` | Tệp nhị phân đích; phải khớp `ExecStart` của unit dịch vụ. |
| `GOTHAM_UPDATE_LOCK` | `/var/lib/gotham/update.lock` | Khóa dùng chung với wrapper. |
| `GOTHAM_UPDATE_BACKOFF` | `/var/lib/gotham/update.backoff` | Số lần thử thất bại cho vòng lặp `AUTO_UPDATE`; được xóa bởi `gotham update reset`. |
| `GOTHAM_UPDATE_DOWNLOAD_TIMEOUT` | `10m` | Thời gian chờ cho nội dung tệp phát hành. |
| `GOTHAM_UPDATE_CURRENT` | phiên bản đang chạy | Ghi đè phiên bản được báo (cho wrapper và kiểm thử). |
| `GOTHAM_UPDATE_OFFER_CACHE_TTL` | `10m` | Thời gian cache một đề nghị cập nhật cho agent. |

Lịch trong **Cài đặt → Cập nhật** của bảng điều khiển được ưu tiên hơn `AUTO_UPDATE`
và `AUTO_UPDATE_INTERVAL` khi một lịch đã được lưu. Xem
[Cập nhật](/vi/guide/updates).

## Agent trên node (chỉ qua biến môi trường)

Cài đặt kết nối:

| Biến | Mặc định | Bắt buộc |
|---|---|---|
| `GOTHAM_AGENT_CP_ADDR` | `127.0.0.1:9442` | không — phải trỏ tới `grpc.addr` |
| `GOTHAM_AGENT_NODE_ID` | `localhost` | không |
| `GOTHAM_AGENT_LISTEN_ADDR` | `:9443` | không |
| `GOTHAM_AGENT_CA` | rỗng | có, trừ khi `GOTHAM_AGENT_INSECURE=true` |
| `GOTHAM_AGENT_ENROLLMENT_TOKEN` | rỗng | chỉ lần đăng ký đầu |
| `GOTHAM_AGENT_CERT_DIR` | `./data/agent` | không |
| `GOTHAM_AGENT_KEY` | khóa trong thư mục chứng chỉ | không |
| `GOTHAM_AGENT_LOG_LEVEL` | `info` | không |
| `GOTHAM_AGENT_INSECURE` | `false` | chỉ cho phát triển |

- `GOTHAM_AGENT_CA` là `ca.crt` của control plane. Không có nó, agent từ chối khởi
  động trừ khi `GOTHAM_AGENT_INSECURE=true`, và cờ này còn đòi một địa chỉ nghe
  loopback.
- `GOTHAM_AGENT_ENROLLMENT_TOKEN` phải mang enrollment token của control plane. Chỉ
  lần đăng ký đầu cần nó; sau đó agent xác thực bằng chứng chỉ của chính mình.

Cài đặt cập nhật:

| Biến | Mặc định | Mục đích |
|---|---|---|
| `GOTHAM_AGENT_AUTO_UPDATE` | `false` | Áp dụng một đề nghị thường từ control plane mà không cần người trực. Một đợt triển khai do quản trị viên kích hoạt luôn được áp dụng. |
| `GOTHAM_AGENT_UPDATE_INTERVAL` | `5m` | Tần suất agent hỏi đề nghị cập nhật. |
| `GOTHAM_AGENT_UPDATE_CHANNEL` | `stable` | Kênh node chấp nhận: `stable` hoặc `beta`. Node kênh stable từ chối đề nghị beta. |
| `GOTHAM_AGENT_BINARY` | `/var/lib/gotham-agent/bin/gotham-agent` | Tệp nhị phân agent cố định. |
| `GOTHAM_AGENT_UPDATE_SCRIPT` | `/usr/libexec/gotham/gotham-agent-update` | Wrapper khởi động lại, thuộc sở hữu root. |
| `GOTHAM_AGENT_UPDATE_STATUS` | `/var/lib/gotham-agent-updater/update.status` | Trạng thái cập nhật thuộc sở hữu root. |
| `GOTHAM_AGENT_UPDATE_PENDING` | `/var/lib/gotham-agent/update.pending` | Dấu chặn cập nhật đã chuẩn bị. |
| `GOTHAM_AGENT_UPDATE_LOCK` | `/var/lib/gotham-agent/update.lock` | Khóa dùng chung với wrapper. |
| `GOTHAM_AGENT_UPDATE_RETRY` | `/var/lib/gotham-agent/update.retry` | Dấu khiến agent đang chạy thử lại sau `gotham-agent update reset`. |
| `GOTHAM_AGENT_UPDATE_BACKOFF` | `/var/lib/gotham-agent/update.backoff` | Số lần thử thất bại được lưu. |
| `GOTHAM_AGENT_HEALTH_ADDR` | `127.0.0.1:8001` | Điểm cuối kiểm tra sức khỏe trên loopback mà wrapper dò. |

## Cờ tính năng

Mười công tắc này chỉ đặt qua biến môi trường — không có khóa YAML. Tất cả đều bật
theo mặc định, và chỉ đúng giá trị `false` (không phân biệt hoa thường) mới tắt một
công tắc; chưa đặt hoặc giá trị khác đều giữ module bật. Chúng được đọc khi tiến
trình khởi động, và một module bị tắt sẽ ghi log cờ của nó lúc khởi động.

| Biến | Mặc định | Module bị tắt |
|---|---|---|
| `FEATURE_APPLICATIONS` | bật | Ứng dụng và triển khai |
| `FEATURE_DATABASES` | bật | Cơ sở dữ liệu được quản lý và sao lưu |
| `FEATURE_SERVICES` | bật | Dịch vụ compose |
| `FEATURE_TEAMS` | bật | Nhóm và lời mời |
| `FEATURE_FILES` | bật | Trình quản lý tệp trên node |
| `FEATURE_NOTIFICATIONS` | bật | Kênh thông báo |
| `FEATURE_PROXY` | bật | Proxy, DNS và quản lý chứng chỉ |
| `FEATURE_METRICS` | bật | Thu thập và truy vấn chỉ số của node |
| `FEATURE_UPDATES` | bật | Bề mặt tự cập nhật và vòng lặp tự động |
| `FEATURE_PREVIEWS` | bật | Các đợt triển khai xem trước cho pull request |

Tắt một module sẽ gỡ các tuyến của nó khỏi tiến trình đó (các tuyến của trình quản
lý tệp vẫn được gắn nhưng trả lời 503), nên các lệnh gọi API của nó thất bại thay vì
chạy ở trạng thái suy giảm. Cờ tính năng không phải quyền theo từng người dùng — hãy
dùng vai trò nhóm cho việc đó.
