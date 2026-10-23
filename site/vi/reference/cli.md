---
title: CLI
---

# Tham chiếu dòng lệnh

Gotham phát hành hai tệp nhị phân dịch vụ và một công cụ phát hành:

| Tệp nhị phân | Chạy trên | Vai trò |
|---|---|---|
| `gotham` | máy chủ control plane | Bảng điều khiển/API HTTP, gateway gRPC, di trú, khôi phục tài khoản quản trị viên |
| `gotham-agent` | mọi node | Các dịch vụ Docker/build/tệp/terminal mà control plane gọi tới |
| `signer` | máy trạm phát hành | Ký các tệp phát hành trước khi chúng được công bố |

Cờ có thể viết bằng một hoặc hai dấu gạch đầu dòng (`-email` và `--email` là như
nhau). Mã thoát là `0` khi thành công, `1` khi thao tác thất bại và `2` khi lỗi
cách dùng.

## gotham

| Lệnh | Cờ | Mục đích |
|---|---|---|
| `serve` | không | Khởi động control plane: listener HTTP, gateway gRPC, các dịch vụ nền. |
| `admin create` | `--email <address>` (bắt buộc), `--password <pw>`, `--password-stdin`, `--generate-password`, `--force` | Tạo tài khoản đầu tiên. Từ chối khi instance đã có tài khoản, trừ khi có `--force`. |
| `admin exists` | không | In `admin-exists: true` hoặc `admin-exists: false` rồi thoát với `0`; thoát với `1` khi có lỗi. |
| `admin reset-password` | `--email <address>` (bắt buộc), `--password <pw>` | Thay mật khẩu, thu hồi các phiên làm mới của tài khoản và xóa xác thực hai yếu tố của tài khoản đó. |
| `ca init` | `--host <name-or-ip>` (lặp lại được, nhận giá trị cách nhau bằng dấu phẩy) | Tạo tổ chức phát hành chứng chỉ mTLS cho gRPC dưới `ca.dir` và ghi thêm các máy chủ SAN cho listener. Có tính lũy đẳng: CA đã có được nạp, không bao giờ bị thay thế. |
| `migrate [up\|down\|status]` | chỉ lệnh con; mặc định `up` | Áp dụng mọi bản di trú đang chờ (`up`), hoàn tác bản được áp dụng gần nhất (`down`), hoặc in trạng thái di trú (`status`). |
| `update check` | không | Báo bản phát hành mới nhất trên kênh đã cấu hình, hoặc cho biết phiên bản đang chạy đã là mới nhất. |
| `update apply` | `--channel stable\|beta` (mặc định: kênh đã cấu hình của bản cài) | Tải, xác minh và chuẩn bị bản phát hành mới nhất. |
| `update rollback` | không | Khôi phục tệp nhị phân trước đó từ `<binary>.old`. |
| `update reset` | không | Xóa dấu cập nhật đang chờ và thời gian chờ sau lần phát hành lỗi. |
| `version` | không | In phiên bản bản build. `-v` và `--version` là bí danh. |

Ví dụ:

```sh
gotham serve
gotham admin create --email ops@example.com
gotham admin exists
gotham admin reset-password --email ops@example.com
gotham ca init --host cp.example.com
gotham migrate status
gotham update check
gotham update apply --channel beta
gotham update rollback
gotham update reset
gotham version
```

Ghi chú:

- `serve`, `migrate`, `ca` và `admin` đọc cấu hình từ `gotham.yaml` trong thư mục
  làm việc cùng các biến môi trường `GOTHAM_*` (xem
  [Cấu hình](/vi/reference/configuration)). `update` chỉ lấy cài đặt của nó từ môi
  trường, dù vẫn kiểm tra `gotham.yaml`.
- Các lệnh `admin` cũng đọc `/etc/gotham/gotham.env` khi tệp này tồn tại, mà không
  ghi đè các biến đã đặt trong môi trường. Hãy chạy chúng với cơ sở dữ liệu của
  dịch vụ, ví dụ
  `sudo -u gotham /var/lib/gotham/bin/gotham admin create --email ops@example.com`.
- Có ba nguồn mật khẩu cho `admin create` và chúng loại trừ nhau: `--password`,
  `--password-stdin` (một dòng được đưa vào) và `--generate-password` (in
  `generated-password: <value>` một lần). Không dùng nguồn nào thì lệnh hỏi hai lần
  với đầu vào ẩn.
- `admin reset-password` thu hồi các phiên làm mới và xóa TOTP. Các access token đã
  cấp vẫn hợp lệ cho tới khi hết hạn (15 phút).
- `serve` thoát khi không kết nối được PostgreSQL, và từ chối khởi động kênh agent
  khi không có CA trừ khi đặt `grpc.insecure: true`. `update apply` và
  `update rollback` từ chối chạy khi tệp nhị phân đích thuộc người dùng khác; hãy
  chạy chúng với người dùng dịch vụ, không phải root.
- `migrate` dừng ngay khi gặp lỗi: lần chạy thứ hai không lấy được khóa di trú sẽ
  trả lỗi thay vì xếp hàng chờ.

## gotham-agent

| Lệnh | Cờ | Mục đích |
|---|---|---|
| `serve` | không | Chạy agent trên node. `gotham-agent` không kèm tham số làm điều tương tự; `run` là bí danh. |
| `update reset` | không | Xóa trạng thái cập nhật lỗi và thời gian chờ để một bản phát hành đã sửa được thử lại. |
| `version` | không | In phiên bản bản build. `-v` và `--version` là bí danh. |

Ví dụ:

```sh
gotham-agent serve
gotham-agent update reset
gotham-agent version
```

Ghi chú:

- Agent đọc cài đặt của nó từ môi trường; xem
  [Cấu hình](/vi/reference/configuration) để biết các cài đặt kết nối, cập nhật và
  volume.
- `update reset` phân giải đường dẫn từ môi trường tiến trình, rồi tới
  `/etc/gotham/agent.env` (ghi đè đường dẫn bằng `GOTHAM_AGENT_ENV_FILE`), rồi tới
  các giá trị mặc định tích hợp. Chạy nó dưới quyền root để cũng gỡ được tệp trạng
  thái thuộc sở hữu root.
- Một bản phát hành mới hơn được cài trong một đợt triển khai do quản trị viên kích
  hoạt; `reset` chỉ cần để thử lại cùng phiên bản sau khi hoàn tác.

## signer

`signer` (`keygen`, `sign`, `verify`, `manifest`) chỉ cần cho quản trị viên tự build
bản phát hành Gotham; mọi tệp phát hành đều được xác minh chữ ký tự động khi cài,
nên máy chủ không bao giờ phải giữ khóa. Xem [Cập nhật](/vi/guide/updates) để biết
quy trình phát hành và xác minh, và `signer help` để biết cờ của các lệnh con.
