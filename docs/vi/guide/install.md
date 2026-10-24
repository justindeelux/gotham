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
- Ít nhất 1 GB RAM và 1 CPU; 2 GB là thoải mái.
- HTTPS ra ngoài tới `github.com`, nơi tải các bản phát hành đã ký.
- Tùy chọn: một tên miền cho bảng điều khiển. Nếu dùng, cổng 80 và 443 phải còn
  trống trên máy chủ này.

Trình cài đặt tự cài PostgreSQL, Redis và Docker cho bạn. Nó không cần clone
repository.

## Cài tự động (khuyến nghị)

Chạy một lệnh này trên máy chủ. Lệnh tải trình cài đặt và chạy nó với quyền root:

<<< @/snippets/install-command.sh{sh}

Trình cài đặt hỏi ba câu. Nhấn Enter để giữ giá trị mặc định:

| Câu hỏi | Bạn trả lời gì |
|---|---|
| `Admin email [admin@example.com]` | Địa chỉ của tài khoản đầu tiên. Nhấn Enter để dùng `admin@example.com`, hoặc nhập địa chỉ của bạn. |
| `Admin password (empty to generate)` | Mật khẩu cho tài khoản đó, nhập hai lần (8 đến 128 ký tự). Nhấn Enter để trình cài đặt tạo một mật khẩu mạnh và in một lần ở cuối. |
| `Panel domain (optional, Enter skips)` | Một tên miền như `panel.example.com`, nếu nó đã trỏ về máy chủ này. Nhấn Enter để bỏ qua và dùng địa chỉ máy chủ trước. |

Trong khi chạy, trình cài đặt:

1. chọn bản phát hành mới nhất cho CPU của bạn và xác minh chữ ký Ed25519 trước
   khi dùng bất kỳ tệp nào từ đó;
2. cài PostgreSQL và Redis từ gói của bản phân phối (trừ khi bạn trỏ tới dịch vụ
   được quản lý);
3. cài control plane (tệp nhị phân, updater và unit systemd), rồi Docker và
   node agent trên cùng máy chủ;
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
Nếu chỉ bước cuối đó thất bại, bảng điều khiển vẫn được cài và trình cài đặt in
đúng lệnh để thử lại.

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

### Cài không tương tác

Trên máy chủ không có terminal, trình cài đặt không bao giờ hỏi. Hãy chạy nó với
`GOTHAM_ADMIN_EMAIL` được đặt (và tùy chọn `GOTHAM_ADMIN_PASSWORD_FILE` trỏ tới
một tệp chỉ root đọc được chứa mật khẩu) để tạo tài khoản mà không cần hỏi.
Không có email thì trình cài đặt bỏ qua việc tạo tài khoản và in lệnh thủ công.

Để xem mọi bước mà không thay đổi máy chủ, truyền `--dry-run` theo cùng cách như
`--domain`.

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
| `GOTHAM_ADMIN_EMAIL` | Email của tài khoản quản trị đầu tiên cho lần cài không tương tác. |
| `GOTHAM_ADMIN_PASSWORD_FILE` | Tệp chứa mật khẩu quản trị đầu tiên (một dòng, chỉ root đọc được). |
| `GOTHAM_INSTALL_ROOT` | Cài dưới một tiền tố thay vì `/` (chỉ để test; bật chế độ test). |
| `GOTHAM_INSTALL_TEST_PUBLIC_KEY` | Chỉ để test: thay khóa phát hành đã ghim; chỉ được dùng với `GOTHAM_INSTALL_ROOT`. |
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
