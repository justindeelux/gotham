---
title: Thêm node
---

# Thêm node

Một bản cài đặt bắt đầu với một node: chính máy chủ control plane, tên
`localhost`. Trang này thêm máy chủ khác và cài các công cụ máy chủ mà bảng điều
khiển dùng để chạy build và container trên chúng.

Trình cài đặt đã thêm node `localhost` cho bạn. Bạn chỉ làm các bước này cho node
**bổ sung**.

## Bạn cần gì

- Một máy chủ Linux (`amd64` hoặc `arm64`) với `systemd`, `curl`, `openssl` 3,
  `sudo` (kèm `visudo`) và `coreutils`.
- Docker Engine và plugin compose, hoặc truyền `--full` trên Ubuntu/Debian để
  trình cài đặt thêm Docker từ kho chính thức.
- HTTPS ra ngoài tới `github.com` (hoặc mirror của bạn qua `GOTHAM_RELEASES_URL`).
- Chứng chỉ CA công khai của control plane, sao chép từ máy chủ control plane.
  Không bao giờ sao chép `ca.key`.
- Một token đăng ký cho lần đăng ký đầu tiên của node.

Bảng điều khiển cũng có wizard **Máy chủ → Thêm máy chủ** tạo sẵn khối cài đặt
cho bạn, kèm token; trang này dành cho việc làm bằng tay.

## Trước khi thêm node: địa chỉ control plane

Agent từ xa kết nối tới control plane qua TLS và xác minh chứng chỉ của nó. Chứng
chỉ listener luôn bao gồm các tên loopback (`localhost`, `127.0.0.1`, `::1`) và
hostname của máy. Tên hoặc IP mà agent quay số (`GOTHAM_AGENT_CP_ADDR`) cũng phải
nằm trong danh sách đó, nếu không việc bắt tay thất bại với
`certificate is valid for …, not <name>`.

Hãy thêm địa chỉ của control plane khi cài (hoặc cài lại):

```sh
sudo deploy/install.sh --cp-host cp.example.com --cp-host 203.0.113.10
```

`--cp-host` lặp lại được, và `GOTHAM_GRPC_HOSTS=<a,b>` là dạng biến môi trường.
Danh sách được lưu cạnh CA; chạy lại trình cài đặt mà không có cờ này sẽ giữ danh
sách đã lưu. `GOTHAM_GRPC_HOSTS` trong môi trường của dịch vụ đè lên danh sách đã
lưu lúc chạy, và `gotham ca init --host <name-or-ip>` có thể đặt trực tiếp. Một
node quay số tới địa chỉ không có trong danh sách sẽ thất bại an toàn (không bao
giờ dùng plaintext).

Người vận hành chạy `gotham serve` trực tiếp mà không có CA sẽ nhận phương án dự
phòng cho phát triển (gRPC plaintext, có ghi cảnh báo); trình cài đặt không bao
giờ để một máy chủ production ở trạng thái đó.

## Cài agent trên node

Trên node mới, sao chép chứng chỉ CA công khai rồi chạy trình cài agent:

```sh
scp root@<cp-host>:/var/lib/gotham/ca/ca.crt .
git clone https://github.com/justindeelux/gotham.git
sudo GOTHAM_AGENT_CP_ADDR=<cp-host>:9442 GOTHAM_AGENT_NODE_ID=<node> \
    gotham/deploy/install-agent.sh --ca ./ca.crt
```

Lần đăng ký đầu tiên cũng cần token đăng ký. Control plane tạo một token tại
`<GOTHAM_CA_DIR>/enrollment-token` (mặc định
`/var/lib/gotham/ca/enrollment-token`), trừ khi bạn đặt
`GOTHAM_GRPC_ENROLLMENT_TOKEN`. Hãy sao chép nó vào một tệp thuộc sở hữu root, chế
độ 0600, không phải symlink, rồi truyền tệp đó:

```sh
scp root@<cp-host>:/var/lib/gotham/ca/enrollment-token /root/token
sudo chown root:root /root/token && sudo chmod 0600 /root/token
sudo GOTHAM_AGENT_CP_ADDR=<cp-host>:9442 GOTHAM_AGENT_NODE_ID=<node> \
    gotham/deploy/install-agent.sh --ca ./ca.crt --enrollment-token-file /root/token
```

Hoặc truyền nó mà không echo dưới dạng biến môi trường
`GOTHAM_AGENT_ENROLLMENT_TOKEN` với `sudo -E`. Không bao giờ viết nó dưới dạng
`sudo VAR=<token> …`: cách đó phơi bí mật trong `ps`/`/proc` và lịch sử shell.
Chỉ lần đăng ký đầu tiên cần nó. Không có token (và không có tùy chọn rõ ràng
`GOTHAM_GRPC_ALLOW_OPEN_ENROLLMENT=true`), lần đăng ký đầu tiên bị từ chối. Các
node đã đăng ký trước thay đổi này vẫn hoạt động, và một lần tự cập nhật không bao
giờ đụng tới khóa hay chứng chỉ của node.

Trình cài đặt **thất bại an toàn** khi không có CA: nó từ chối chạy trừ khi có
`--ca <path>` (hoặc `GOTHAM_AGENT_CA_FILE=<path>`). Nó cài chứng chỉ thành
`/etc/gotham/ca.crt` (thuộc sở hữu root, `0644`) và ghi
`GOTHAM_AGENT_CA=/etc/gotham/ca.crt` vào `/etc/gotham/agent.env`, nên agent xác
minh control plane qua TLS.

`--insecure` là ghi đè **chỉ dành cho phát triển cục bộ** đã được ghi nhận: nó bỏ
qua kiểm tra CA, ghi `GOTHAM_AGENT_INSECURE=true`, và để `GOTHAM_AGENT_CA` chưa
đặt. Bản thân agent cũng thất bại an toàn: không có CA thì nó từ chối khởi động
trừ khi `GOTHAM_AGENT_INSECURE=true`, và một listener plaintext bị giới hạn ở
loopback (trình cài đặt mặc định địa chỉ là `127.0.0.1:9443`). Không bao giờ dùng
nó trên một node thật.

## Cách agent và control plane tin nhau

Qua TLS, kênh là song hướng: agent tạo một khóa, gửi CSR trong `Register`, nhận
một chứng chỉ node, và xuất trình nó trong mọi kết nối về sau. Control plane ràng
buộc định danh chứng chỉ và khóa với node, và mỗi agent chỉ chấp nhận control
plane (một chứng chỉ mang định danh `spiffe://gotham/control-plane`), nên một node
không thể đóng vai control plane với node khác.

Lần đăng ký đầu tiên của một node chưa có chứng chỉ và cần token đăng ký mô tả
bên trên. Sau đó nó xác thực bằng chứng chỉ và khóa của chính nó.

## Trình cài agent cài gì

Nó dùng cùng việc xác minh manifest đã ký như control plane
(`deploy/release-verify.sh`) cho `gotham-agent-linux-<arch>` và
`gotham-agent-manifest-<arch>.txt`, với khóa công khai phát hành đã ghim và không
có ghi đè lúc chạy. Nó cài:

- tệp nhị phân agent vào `/var/lib/gotham-agent/bin/gotham-agent`;
- wrapper cập nhật dùng chung thành `gotham-agent-update`;
- cấu hình `/etc/gotham/agent-updater.conf`;
- quy tắc sudoers và unit systemd.

Hãy đặt `GOTHAM_AGENT_CP_ADDR` (địa chỉ gRPC của control plane) và
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
| `--enrollment-token-file <path>` | Tệp thuộc sở hữu root chứa token đăng ký lần đầu. |
| `GOTHAM_AGENT_CA` | Đường dẫn phía agent mà trình cài đặt ghi (`/etc/gotham/ca.crt`). Nó **không** được đọc từ môi trường xung quanh; khi cài lại, trình cài đặt giữ giá trị đã có trong `agent.env`. Dùng `--ca`/`GOTHAM_AGENT_CA_FILE` để đổi nó. |
| `GOTHAM_AGENT_UPDATE_CHANNEL` | Kênh phát hành node này chấp nhận, `stable` (mặc định) hoặc `beta`. Node `beta` nhận cả đề nghị stable và beta; node `stable` từ chối đề nghị beta; một kênh rỗng hoặc không rõ bị từ chối. |
| `GOTHAM_AGENT_MANAGED_VOLUME_ROOT` | Thư mục cha của mọi bind mount ứng dụng mà node chấp nhận (mặc định `/var/lib/gotham/volumes`); quay về `GOTHAM_MANAGED_VOLUME_ROOT` dùng chung nếu không có. Phải khớp với control plane. |
| `--file-manager` / `--no-file-manager` / `GOTHAM_AGENT_FILE_MANAGER=yes\|no` | Trả lời câu hỏi của trình cài đặt về trình quản lý tệp mà không cần hỏi. Trình cài đặt có hỏi (mặc định **no**; không có terminal cũng nghĩa là no). Yes ghi `GOTHAM_AGENT_FILES=true` và một drop-in `gotham-agent.service.d/file-manager.conf` chạy agent **dưới root** mà không có sandbox của systemd; no ghi `GOTHAM_AGENT_FILES=false` và gỡ drop-in. Chạy lại sẽ giữ câu trả lời trước đó (một `GOTHAM_AGENT_FILES=true` do bạn đặt tay được giữ nguyên, không kèm drop-in root); một `GOTHAM_AGENT_FILES` từ môi trường bị bỏ qua để dùng câu trả lời đó. |
| `GOTHAM_AGENT_FILES` | Dịch vụ tệp từ xa trên node. Bật trừ khi đặt là `false` (trình cài đặt ghi câu trả lời ở trên). |
| `GOTHAM_AGENT_FILE_FULL_ACCESS` | Bật trừ khi đặt là `false`: dịch vụ phơi **toàn bộ hệ thống tệp** thành một gốc ghi được tên `system` (`/`), không có đường dẫn được bảo vệ — người vận hành đã chấp nhận rủi ro đó. `false` khôi phục các gốc bị giới hạn. Xem [Dịch vụ tệp từ xa](/vi/guide/file-service). |
| `GOTHAM_AGENT_FILE_ROOTS` | Các gốc duyệt thêm, là đường dẫn tuyệt đối phân tách bằng dấu hai chấm (mặc định không có). Các gốc của người vận hành là **chỉ đọc** trong giai đoạn này; một gốc bị thiếu, không tuyệt đối, nằm trong cây nguy hiểm hoặc chồng lấn gốc khác sẽ bị từ chối lúc khởi động. |
| `GOTHAM_AGENT_FILE_MAX_UPLOAD` | Mức trần cho một body tải lên, tính bằng byte (mặc định `1073741824`, 1 GiB); body lớn hơn bị từ chối với `ResourceExhausted` (413). Nên khớp với `FILES_MAX_UPLOAD_BYTES` của control plane. |
| `GOTHAM_AGENT_ARCHIVE_MAX_BYTES` | Tổng số byte chưa nén mà một lần **nén hoặc giải nén** được đọc/ghi trước khi bị từ chối với `ResourceExhausted` (413) — giới hạn kích thước và một nửa biện pháp chống zip-bomb. Mặc định `4294967296` (4 GiB). |
| `GOTHAM_AGENT_ARCHIVE_MAX_ENTRIES` | Số mục tối đa mà một thao tác archive được duyệt hoặc giải nén (mặc định `100000`); vượt quá thì thao tác bị từ chối với `ResourceExhausted` (413). |
| `GOTHAM_AGENT_ARCHIVE_MAX_INPUT_BYTES` | Kích thước trên đĩa tối đa của một tệp archive mà **giải nén** sẽ đọc (mặc định `1073741824`, 1 GiB); archive lớn hơn bị từ chối với `ResourceExhausted` (413) trước khi bất kỳ bộ phân tích nào chạy. Áp dụng cho mọi định dạng. |
| `GOTHAM_AGENT_COMPOSE_ROOT` | Thư mục chứa một thư mục con cho mỗi dự án compose (mặc định `/var/lib/gotham-agent/compose`); duyệt được ở chế độ chỉ đọc khi dịch vụ tệp được bật. |

## Kiểm tra địa chỉ và thiết lập của agent

`GOTHAM_AGENT_CP_ADDR` phải dùng một tên hoặc IP nằm trong các SAN của listener
control plane (xem bên trên); nếu không, việc bắt tay TLS thất bại và node không
bao giờ đăng ký được. Nó phải ở dạng `host:port` không có khoảng trắng, dấu nháy
hay `=`. Mọi giá trị khác trong `agent.env` được kiểm tra trước khi trình cài đặt
thay đổi bất cứ thứ gì:

- ký tự điều khiển và dấu gạch chéo ngược ở cuối bị từ chối ở mọi nơi;
- node id bị giới hạn theo những gì agent chấp nhận (tối đa 253 byte; không có
  khoảng trắng, tab, `*`, `/` hay dấu gạch chéo ngược) và từ chối thêm dấu nháy,
  vì bộ phân tích EnvironmentFile của systemd sẽ bỏ hoặc nhóm lại chúng;
- đường dẫn phải tuyệt đối;
- endpoint docker phải là `unix:///abs/path`, `/abs/path` hoặc `tcp://host:port`;
- tự cập nhật phải đúng `true`/`false`;
- khoảng cách phải là một Go duration (đơn vị ngày/tuần và giá trị tràn bị từ
  chối).

`install.sh` chạy các kiểm tra này trước khi thay đổi và luôn cài agent cục bộ của
nó dưới tên `localhost`, kết nối tới `127.0.0.1:9442`.

## Node localhost

Control plane tạo hoặc sửa node `localhost` dùng chung lúc khởi động; trạng thái
của nó chỉ thành sẵn sàng sau một nhịp tim từ agent. Xóa nó trả về HTTP 409;
PATCH cũng bị từ chối.

Migration nâng cấp 49 đổi tên định danh agent cục bộ đã lưu tại chỗ, giữ nguyên
UUID máy chủ, ứng dụng, bản triển khai và router. Trình cài đặt chuyển id của
`agent.env` trước đó cho migration, nên đổi hostname hoặc chạy migration trong
container là an toàn. Với một lần migration thủ công, đặt
`GOTHAM_LEGACY_LOCAL_NODE_ID` thành node ID đã lưu đó.

Migration tiến 51 cũng phát hiện một node duy nhất có địa chỉ loopback với id tùy
chỉnh (ví dụ `test-node-1` tại `127.0.0.1`), kể cả các địa chỉ agent cục bộ như
`localhost:9443` và `[::1]:9443`. Lúc khởi động, bước kiểm tra này được lặp lại
trước khi tạo `localhost`, giữ nguyên UUID, ứng dụng, cơ sở dữ liệu, dịch vụ và
router. Migration 51 chỉ gỡ placeholder `localhost` rỗng, đang chờ, chưa từng đăng
ký của một lần khởi động trước sau khi kiểm tra không có tham chiếu khóa ngoại
nào, rồi đổi tên dòng cũ. Một `localhost` đã đăng ký hoặc đang được tham chiếu,
hoặc nhiều ứng viên cục bộ, sẽ dừng migration với lỗi có thể xử lý được. Hãy kiểm
tra `agent.env` và các bản ghi máy chủ đã lưu, đối chiếu khối lượng công việc của
chúng và gỡ các mục agent cục bộ trùng lặp trước khi thử lại; không có khối lượng
công việc nào bị gộp hay xóa.

Lúc khởi động, việc quét chỉ chạy khi `localhost` vắng mặt; các lần đăng ký về sau
không chặn được việc khởi động lại. Việc phát hiện dùng IP/địa chỉ agent đã ghi,
không bao giờ chỉ dùng node ID; các IP được sao chép trực tiếp từ node ID dạng IP
do agent chọn bị loại trừ. Ứng viên phải có dữ liệu đăng ký agent và không có
người dùng, khóa hay mật khẩu SSH; các node loopback/tunnel do SSH quản lý giữ
nguyên định danh và nhóm của chúng. Với một node cũ có IP công khai hoặc thiếu
IP, hãy cung cấp định danh đã lưu cho migration 49 như mô tả ở trên; migration 51
không diễn giải lại gợi ý môi trường đó.

Sau khi nâng cấp thủ công, hãy đổi `GOTHAM_AGENT_NODE_ID` trong môi trường đã lưu
của agent cục bộ (thường là `/etc/gotham/agent.env`) thành `localhost` và khởi
động lại agent. Trình cài đặt làm việc này tự động; giữ id cũ có thể đăng ký lại
node cũ và để lại hai bản ghi cùng trỏ tới một agent.

Việc chính sách host của URL Git từ chối tên `localhost` chỉ áp dụng cho remote
Git; nó không kiểm tra node ID hay đường dẫn quay số loopback của agent. Node ID
`localhost` chính xác chỉ được chấp nhận từ một peer loopback. Chứng chỉ của nó
dùng SAN riêng `localhost.agent.gotham.invalid`, và control plane xác minh tên đó
khi quay số tới `127.0.0.1:9443`. Nó không thể được dùng làm chứng chỉ control
plane cho `localhost` hoặc một IP loopback. Đừng đưa SAN của agent vào danh sách
host của listener control plane; việc đăng ký sẽ từ chối sự trùng lặp đó. Các định
danh và bí danh khác của listener control plane vẫn được dành riêng.

Agent từ xa phải đặt node ID riêng và địa chỉ control plane riêng:

```sh
sudo GOTHAM_AGENT_CP_ADDR=<cp-host>:9442 GOTHAM_AGENT_NODE_ID=worker-1 \
    gotham/deploy/install-agent.sh --ca ./ca.crt
```

## Công cụ máy chủ của node (cài / gỡ)

Agent phơi `ToolService` (ListTools/InstallTool/UninstallTool) qua mTLS; control
plane điều khiển nó từ trình quản lý công cụ của node. Mọi bước đặc quyền đều
chạy qua một helper cố định, thuộc sở hữu root — cùng mô hình với
`gotham-hostctl` — không bao giờ dùng `sudo` tràn lan:

```sh
sudo install -D -m 0755 -o root -g root deploy/gotham-toolctl.sh /usr/libexec/gotham/gotham-toolctl
sudo deploy/install-toolctl-sudoers.sh gotham-agent
```

`install-agent.sh` cài cả hai, nên một lần cài agent bình thường không cần bước
thêm. Drop-in sudoers (`/etc/sudoers.d/gotham-toolctl`) cấp đúng một quy tắc cho
mỗi động từ **và** mỗi tool id:

- `install-tool <id>` — `railpack`, `buildkit`, `pack`, `docker`, `git`;
- `uninstall-tool <id>` — `railpack`, `buildkit`, `pack`, `git` (`docker` không
  bao giờ bị gỡ: nó sẽ phá hủy mọi khối lượng công việc trên node).

Agent chỉ truyền một id trong danh mục; helper sở hữu toàn bộ argv thực thi và
kiểm tra lại id với cùng danh sách cho phép, nên một agent bị xâm nhập không thể
nêu tên một gói, một URL hay một cờ thêm. Các unit máy chủ chỉ để phát hiện
(`systemd-networkd`, `systemd-resolved`, `networkctl`, `systemctl`, …) bị từ chối
với `FAILED_PRECONDITION` và không có trình cài nào cả.

Hành vi:

- **Phụ thuộc được phân giải** bởi agent, phụ thuộc trước:
  `railpack → buildkit → docker`. Mỗi công cụ được cài bằng một lần gọi helper.
- **Được ghim và xác minh.** Railpack và pack được cài từ tarball phát hành có
  phiên bản mà sha256 được viết cứng trong helper (không bao giờ dùng `install.sh`
  không ghim piped vào shell); BuildKit là image `moby/buildkit:v0.34.0` được ghim
  digest, chạy thành container đặc quyền được quản lý tên `buildkit`. Checksum
  không khớp sẽ hủy bỏ.
- **Lũy đẳng.** Công cụ đã phát hiện trên node sẽ bị bỏ qua. Việc cài BuildKit xóa
  một container cũ trước khi tạo lại và chỉ gắn tham chiếu phát hiện sau khi
  container chạy, nên một lần chạy thất bại không để công cụ trông như đã cài.
- **Mỗi lần một việc.** Một khóa toàn node cho phép một lần cài hoặc gỡ tại một
  thời điểm; lần gọi thứ hai bị từ chối với `RESOURCE_EXHAUSTED`. Yêu cầu bị hủy
  khi control plane ngắt luồng, và bị giới hạn bởi timeout 15 phút. Helper giữ
  `flock` riêng cho toàn bộ công việc, nên ngay cả một công việc mà yêu cầu từ
  agent đã bị hủy (unit tạm thời vẫn chạy cho đến khi xong hoặc đến khi chạm giới
  hạn `RuntimeMaxSec=840`, tùy cái nào tới trước) cũng chặn lần thử lại bằng một
  lời từ chối rõ ràng "already running" thay vì chạy chồng lên; một lần hủy nhẹ
  nhàng (`SIGTERM`) có thể hoãn trap dừng của helper cho đến khi lệnh `systemd-run`
  phía trước trả về, nên nó không bảo đảm dừng ngay.
- **Nâng cấp một node.** Helper và quy tắc sudoers của nó được `install-agent.sh`
  cài, không phải bởi việc hoán đổi tệp nhị phân tự cập nhật: một node chỉ cập nhật
  qua tự cập nhật `gotham-agent` vẫn giữ bản cài cũ và các lời gọi cài/gỡ công cụ
  của nó thất bại với thông báo "not allowed" của sudo. Chạy lại
  `deploy/install-agent.sh` trên node (lũy đẳng) để cài chúng.
- **Gỡ bị từ chối khi một công cụ đang được dùng** (`FAILED_PRECONDITION`): khi
  một bản build đang chạy, và khi một công cụ đã cài khác phụ thuộc vào nó (ví dụ
  BuildKit không thể đi khi Railpack đã cài). Chỉ các *bản build* đang diễn ra được
  theo dõi là "đang được dùng"; một dịch vụ compose hoặc một container thường dùng
  Docker thì không (Docker không gỡ được, nên chỉ các lần gỡ `buildkit`/`git` cần
  rào chắn này).
- **Sandbox.** Unit của agent chạy với `ProtectSystem=full`, nên `/usr`, `/etc` và
  `/boot` là chỉ đọc trong mount namespace của nó và một tiến trình `sudo` con thừa
  hưởng điều đó. Vì vậy helper tự thực thi lại chính nó qua
  `systemd-run --pipe --wait --collect`, việc này khởi động một unit tạm thời mới
  bên ngoài namespace của agent; node cần systemd (unit của agent vốn đã yêu cầu).
  Đầu ra được truyền là stdout/stderr của chính helper, rồi một
  `ToolResult{status, exit_code}` ở cuối.

Helper cũng chạy độc lập để gỡ lỗi (dưới root, không có `systemd-run`):

```sh
sudo GOTHAM_TOOLCTL_DRYRUN=1 GOTHAM_TOOLCTL_ARCH=amd64 GOTHAM_TOOLCTL_DISTRO=apt \
    /usr/libexec/gotham/gotham-toolctl install-tool railpack
```
