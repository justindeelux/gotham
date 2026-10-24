---
title: Cài đặt
---

# Cài đặt Gotham

Gotham chạy trên máy chủ Linux của bạn. Trình cài đặt thiết lập mọi thứ trên một
máy: bảng điều khiển web, cơ sở dữ liệu và agent của node đầu tiên. Bạn có thể
thêm máy chủ khác sau.

Có hai cách cài. Cả hai dùng cùng một trình cài đặt và kết thúc bằng một bảng điều
khiển đang chạy; hãy chọn cách phù hợp với bạn.

## Bạn cần gì

- Một máy chủ Ubuntu 22.04 hoặc Debian (64-bit Intel/AMD hoặc ARM) mà bạn có thể
  chạy lệnh với quyền root, ví dụ qua SSH với `sudo`.
- `curl` và `openssl` 3. Cả hai đều có sẵn trên một image tiêu chuẩn.
- Ít nhất 1 GB RAM và 1 CPU.
- HTTPS ra ngoài tới `github.com`, nơi tải các bản phát hành đã ký.
- Tùy chọn: một tên miền cho bảng điều khiển. Nếu dùng, cổng 80 và 443 phải còn
  trống trên máy chủ này.

Trình cài đặt tự cài PostgreSQL, Redis và Docker cho bạn. Nó không cần clone
repository.

## Cài tự động (khuyến nghị)

Chạy một lệnh này trên máy chủ. Lệnh tải trình cài đặt và chạy nó với quyền root:

<<< @/snippets/install-command.sh#plain{sh}

Trình cài đặt hỏi những câu này, theo thứ tự. Nhấn Enter để nhận gợi ý:

| Câu hỏi | Bạn trả lời gì |
|---|---|
| `Admin email [admin@example.com] (empty keeps it, - to skip)` | Địa chỉ của tài khoản đầu tiên. Nhấn Enter để dùng `admin@example.com`, nhập địa chỉ của bạn, hoặc nhập `-` để chưa tạo tài khoản. |
| `Admin password (empty to generate)` | Mật khẩu cho tài khoản đó, 8 đến 128 ký tự. Không nhập gì và nhấn Enter để trình cài đặt tạo một mật khẩu mạnh và in một lần ở cuối. |
| `Confirm password` | Nhập lại mật khẩu. Nếu không khớp hoặc ngoài giới hạn độ dài, trình cài đặt hỏi lại. |
| `Panel domain (optional, Enter skips)` | Một tên miền như `panel.example.com`, nếu nó đã trỏ về máy chủ này. Nhấn Enter để bỏ qua và dùng địa chỉ máy chủ trước. |
| `Enable the file manager on this node? [y/N]` | **Không**, trừ khi bạn muốn người vận hành nền tảng duyệt và sửa mọi tệp trên máy này. Chọn có sẽ chạy agent dưới root mà không có sandbox của systemd (xem [Dịch vụ tệp từ xa](/vi/guide/file-service)). Bạn có thể đổi câu trả lời sau bằng cách chạy lại `deploy/install-agent.sh`. |

Câu hỏi về trình quản lý tệp xuất hiện trong lúc cài agent của máy chủ, sau bốn
câu đầu. `--no-file-manager` trả lời câu đó mà không hỏi, và một lần chạy không
có terminal sẽ bỏ qua nó (câu trả lời là Không).

Trong khi chạy, trình cài đặt:

1. chọn bản phát hành mới nhất cho CPU của bạn, xác minh chữ ký Ed25519, và kiểm
   tra SHA-256 của artifact với manifest đã ký (manifest được ràng buộc với phiên
   bản, kiến trúc và tên tệp);
2. cài PostgreSQL 16 và Redis 6.2 trở lên (7.2 từ kho chính thức của Redis khi
   bản phân phối của bạn cũ hơn) — bản cài mới tạo mật khẩu cơ sở dữ liệu cục
   bộ — trừ khi bạn trỏ tới dịch vụ được quản lý;
   [Phiên bản cơ sở dữ liệu và nâng cấp](/vi/guide/upgrade-databases) giải thích
   những gì được cài và cách nâng cấp thủ công;
3. cài control plane (tệp nhị phân trong `/var/lib/gotham/bin`, updater
   `/usr/libexec/gotham/gotham-update`, unit
   `/etc/systemd/system/gotham.service`), rồi Docker và node agent trên cùng máy
   chủ (`install-agent.sh --full --ca`, ghim cùng thẻ phát hành);
4. áp dụng các migration cơ sở dữ liệu và tạo tài khoản quản trị trước khi
   bảng điều khiển nhận kết nối, nên không ai đăng ký trước được;
5. in URL bảng điều khiển, thông tin đăng nhập, và mật khẩu tự tạo nếu bạn đã
   chọn cách đó.

Các dòng cuối trông như sau:

```text
Gotham v1.2.3 is installed.

  Web UI:   http://203.0.113.10:8000
  Status:   systemctl status gotham
  Logs:     journalctl -u gotham -f

First login:
  Account:  ops@example.com
  Password: <in một lần nếu bạn để trống mật khẩu>
  Login:    http://203.0.113.10:8000
```

Mở URL đó và đăng nhập. Máy chủ cục bộ đã được liệt kê trong **Máy chủ** với
tên `localhost`. Tiếp tục với [Đăng nhập lần đầu](/vi/guide/first-login).

Agent cho chính máy chủ này là một phần của cùng lần chạy và không thể bỏ qua.
Nếu chỉ bước cuối đó thất bại, không có gì bị quay lui: bảng điều khiển vẫn được
cài và đang chạy, và trình cài đặt thoát với mã lỗi cùng đúng lệnh để thử lại.

Nếu quá trình cài dừng lại, [Xử lý sự cố](/vi/guide/troubleshooting) nêu các
nguyên nhân thường gặp.

### Đặt tên miền trong cùng lần chạy

Thêm `-s -- --domain panel.example.com` sau `sh` trong lệnh trên. Tên miền phải
đã trỏ về máy chủ này. Trình cài đặt chờ tối đa 60 giây để agent cục bộ sẵn sàng,
đặt URL bảng điều khiển thành `https://panel.example.com`, và in địa chỉ HTTP để
mở xác nhận. HTTPS được cấp sau khi bạn xác nhận trong **Cài đặt chung** trong
vòng 2 phút. Nếu tên miền chưa phân giải được, quá trình cài tiếp tục trên địa
chỉ máy chủ và bạn có thể đặt tên miền sau trong bảng điều khiển. Xem
[Tên miền và HTTPS](/vi/guide/domain-and-https).

### Cài theo script (cloud-init, CI)

Công thức này chỉ thực sự không cần tương tác khi **không có terminal**:
cloud-init, một job CI, hoặc một lệnh `ssh` chạy không có pty. Trong một phiên
SSH tương tác bình thường, trình cài đặt vẫn hỏi ba câu — email quản trị (điền sẵn
từ `GOTHAM_ADMIN_EMAIL`), tên miền bảng điều khiển, và câu hỏi trình quản lý tệp.
Hãy trả lời trước bằng cách thêm `-s -- --domain panel.example.com
--no-file-manager` sau `sh` trong lệnh bên dưới; `--domain` đặt tên miền, Enter
(hoặc bỏ qua nó) để bỏ qua, và `-` ở câu hỏi email để bỏ qua việc tạo tài khoản.

Trong một lần chạy `curl | sudo sh`, các biến phải nằm **sau** `sudo` và trước
`sh`; đặt chúng trên `curl` chỉ cấu hình `curl`, còn `sudo` khởi động trình cài
đặt với môi trường mới. Hãy tạo tệp mật khẩu trước (một dòng, chỉ root đọc được):

```sh
sudo install -m 0600 -o root -g root /dev/null /root/gotham-admin-password
sudo nano /root/gotham-admin-password   # nhập mật khẩu trên một dòng rồi lưu
```

Rồi chạy:

<<< @/snippets/install-command.sh#unattended{sh}

Nếu chính sách sudoers của bạn từ chối biến trên dòng lệnh, hãy đặt chúng trong
`env` (thay phần bên phải của pipe bằng lệnh này):

```sh
sudo env GOTHAM_ADMIN_EMAIL=ops@example.com \
  GOTHAM_ADMIN_PASSWORD_FILE=/root/gotham-admin-password sh
```

Trình cài đặt tạo tài khoản quản trị mà không hỏi và in thông tin đăng nhập ở
cuối. Mật khẩu đi từ tệp qua stdin, nên nó không bao giờ xuất hiện trong danh
sách tiến trình hay lịch sử shell. Bỏ `GOTHAM_ADMIN_PASSWORD_FILE` thì một mật
khẩu ngẫu nhiên được tạo và in một lần; bỏ cả `GOTHAM_ADMIN_EMAIL` thì chưa tài
khoản nào được tạo, và trình cài đặt in lệnh thủ công.

Với một bản checkout thủ công, truyền cùng các biến trước script:

```sh
sudo GOTHAM_ADMIN_EMAIL=ops@example.com \
  GOTHAM_ADMIN_PASSWORD_FILE=/root/gotham-admin-password \
  deploy/install.sh
```

Để xem mọi bước mà không thay đổi máy chủ, truyền `--dry-run`:

<<< @/snippets/install-command.sh#dry-run{sh}

## Cài thủ công (từ bản checkout)

Chọn cách này nếu bạn muốn đọc các script trước khi chạy, cài qua một mirror nội
bộ, hoặc làm việc trên chính Gotham.

```sh
git clone https://github.com/justindeelux/gotham.git
cd gotham
sudo deploy/install.sh
```

Đây là cùng một trình cài đặt, cùng câu hỏi và cùng kết quả. Khác biệt là nguồn
tệp: nó chạy từ bản checkout của bạn, nên hãy giữ phần còn lại của `deploy/` cạnh
script thay vì sao chép riêng script. `--domain panel.example.com` và `--dry-run`
hoạt động như trên.

### Build từ mã nguồn (tùy chọn)

Việc build dành cho phát triển và kiểm tra; trình cài đặt luôn cài một bản phát
hành đã ký, không phải bản build cục bộ. Bạn cần Go 1.27+ và Node 20+:

```sh
git clone https://github.com/justindeelux/gotham.git
cd gotham
make build     # tạo bin/gotham và bin/gotham-agent
```

`make build` cũng làm mới bảng điều khiển web khi các phụ thuộc trong `web/` đã
được cài. Hai tệp nhị phân này cũng là thứ bản phát hành đã ký cung cấp.

## Tùy chọn nâng cao

Trình cài đặt nhận một vài biến môi trường và cờ cho các thiết lập không tiêu
chuẩn (mirror, phiên bản ghim, PostgreSQL/Redis được quản lý, hoặc sandbox thử
nghiệm). Bảng dưới liệt kê chúng; [Cấu hình](/vi/reference/configuration) giải
thích các thiết lập lúc chạy tương ứng.

| Biến hoặc cờ | Mục đích |
|---|---|
| `GOTHAM_VERSION` | Cài một thẻ cụ thể thay vì bản phát hành mới nhất. |
| `GOTHAM_RELEASES_URL` | Gốc phát hành kiểu GitHub cho một mirror (mặc định `https://github.com/<repo>/releases`). |
| `GOTHAM_BASE_URL` | URL gốc đầy đủ của asset tĩnh; cần `GOTHAM_VERSION`. |
| `GOTHAM_REPO` | `owner/name` của repository phát hành (mặc định `justindeelux/gotham`). |
| `GOTHAM_DATABASE_DSN` | DSN PostgreSQL được quản lý; bỏ qua việc cấp phát cục bộ. |
| `GOTHAM_REDIS_ADDR` | Redis `host:port` (mặc định `localhost:6379`). |
| `GOTHAM_SKIP_DEPS=1` | Không cài hay cấu hình PostgreSQL/Redis. |
| `GOTHAM_ADMIN_EMAIL` | Email của tài khoản quản trị đầu tiên cho lần cài theo script. |
| `GOTHAM_ADMIN_PASSWORD_FILE` | Tệp chứa mật khẩu quản trị đầu tiên (một dòng, chỉ root đọc được). |
| `--dry-run` | In mọi bước mà không thay đổi máy chủ. |
| `--cp-host <name-or-ip>` | Thêm một tên hoặc IP vào chứng chỉ gateway của agent; xem [Thêm node](/vi/guide/nodes). |

## Sau khi cài

- [Đăng nhập lần đầu](/vi/guide/first-login) — đăng nhập, tạo lời mời, đặt lại
  mật khẩu.
- [Tên miền và HTTPS](/vi/guide/domain-and-https) — dùng tên miền riêng và
  chứng chỉ.
- [Thêm node](/vi/guide/nodes) — thêm máy chủ và cài công cụ máy chủ của chúng.
- [Dịch vụ tệp từ xa](/vi/guide/file-service) — duyệt và sửa tệp trên node từ
  bảng điều khiển.
- [Cập nhật và quay lui](/vi/guide/updates) — giữ control plane và các node luôn
  mới.
- [Nguồn Git và chính sách SSRF](/vi/guide/git-sources) — cho phép một máy chủ
  git riêng.
- [Phát hành và ký](/vi/guide/release-signing) — chỉ dành cho người bảo trì.
- [Xử lý sự cố](/vi/guide/troubleshooting) — khi có thứ gì đó không khởi động.

## Tìm một mục từ trang cài đặt cũ?

Trang hướng dẫn cài đặt trước đây chứa mọi thứ. Các trang này đã được tách ra:

- <span id="https-for-the-panel"></span>[HTTPS cho bảng điều khiển](/vi/guide/domain-and-https)
- <span id="remote-file-service"></span><span id="dich-vu-tep-tu-xa"></span>[Dịch vụ tệp từ xa](/vi/guide/file-service)
- [Đăng nhập lần đầu](/vi/guide/first-login)
- [Node agent](/vi/guide/nodes)
- [Công cụ máy chủ của node](/vi/guide/nodes)
- [Nâng cấp một control plane đã có](/vi/guide/updates#cai-lai-tren-mot-may-đa-co)
- [Nguồn Git và chính sách host SSRF](/vi/guide/git-sources)
- [Luồng phát hành và ký](/vi/guide/release-signing)
