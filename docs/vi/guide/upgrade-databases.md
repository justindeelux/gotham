---
title: Phiên bản cơ sở dữ liệu và nâng cấp
---

# Phiên bản cơ sở dữ liệu và nâng cấp

Gotham lưu dữ liệu trong PostgreSQL và dùng Redis làm bộ nhớ đệm và hàng đợi.
Khi cài mới, trình cài đặt tự thiết lập cả hai và chọn phiên bản đã được kiểm
thử với Gotham:

| Dịch vụ | Bản cài mới nhận được |
|---|---|
| PostgreSQL | 16, lấy từ kho chính thức của PostgreSQL (PGDG) khi bản phân phối của bạn có phiên bản cũ hơn |
| Redis | Redis của bản phân phối khi đã là 6.2 trở lên, ngược lại là 7.2 từ kho chính thức của Redis |

Bạn không cần chọn hay cấu hình gì. Trình cài đặt in ra phiên bản cuối cùng khi
chạy xong, và mật khẩu cơ sở dữ liệu cục bộ được tạo lúc cài rồi lưu trong
`/etc/gotham/gotham.env` (chỉ root và dịch vụ `gotham` đọc được).

## Kiểm tra phiên bản đang cài

```sh
psql --version
redis-server --version
sudo cat /etc/gotham/gotham.env
```

Dòng `GOTHAM_DATABASE_DSN` trong tệp đó là chuỗi kết nối Gotham dùng, bao gồm
mật khẩu đã tạo.

## Bản cài sẵn không bao giờ tự nâng cấp

Chạy lại trình cài đặt để sửa Gotham nhưng không thay thế cơ sở dữ liệu đã có.
Nếu PostgreSQL hoặc Redis cũ hơn các phiên bản trên, trình cài đặt in cảnh báo
và giữ nguyên dịch vụ hiện tại, vì nâng cấp major của PostgreSQL cần được lên
kế hoạch và sao lưu. Trang này dành cho việc đó.

Khi thấy cảnh báo, Gotham vẫn chạy bình thường. Hãy nâng cấp khi thuận tiện.

## Trước khi bắt đầu

- Sao lưu và giữ bản sao ngoài máy chủ này:
  ```sh
  sudo -u postgres pg_dumpall > ~/gotham-db-before-upgrade.sql
  sudo cp /etc/gotham/gotham.env ~/gotham-env-before-upgrade
  ```
- Dừng Gotham để không có gì ghi dữ liệu trong lúc nâng cấp:
  ```sh
  sudo systemctl stop gotham
  ```
- Các lệnh này cần quyền root và chạy trên máy chủ Gotham. Thời gian gián đoạn
  khoảng vài phút.

## Nâng cấp PostgreSQL

Ubuntu và Debian có sẵn một công cụ bao quanh `pg_upgrade` của PostgreSQL, giữ
nguyên dữ liệu, role `gotham` và mật khẩu. Trong ví dụ, phiên bản cũ là 16 và
mới là 17; hãy dùng đúng phiên bản bạn có (`pg_lsclusters` hiển thị chúng).

1. Cài phiên bản PostgreSQL mới từ kho PGDG theo hướng dẫn tại
   [postgresql.org/download/linux/ubuntu](https://www.postgresql.org/download/linux/ubuntu/).
   Ví dụ:
   ```sh
   sudo apt-get install postgresql-17
   ```
2. Nâng cấp cluster. Nếu việc cài phiên bản mới đã tạo một cluster trống (ví
   dụ `17 main`, xem bằng `pg_lsclusters`), hãy xóa nó trước:
   ```sh
   sudo pg_dropcluster --stop 17 main
   sudo pg_upgradecluster -v 17 16 main
   ```
3. Khởi động PostgreSQL và Gotham:
   ```sh
   sudo systemctl start postgresql
   sudo systemctl start gotham
   ```
4. Kiểm tra bảng điều khiển mở được và `psql --version` hiện phiên bản mới. Khi
   đã hài lòng, xóa cluster cũ để giải phóng dung lượng:
   ```sh
   sudo pg_dropcluster --stop 16 main
   ```

Nếu không dùng được `pg_upgradecluster`, cách thay thế là dump và restore: dump
bằng `pg_dumpall`, tạo cluster mới, rồi restore bằng `psql`. [Tài liệu
PostgreSQL](https://www.postgresql.org/docs/current/upgrading.html) trình bày cả
hai cách.

## Nâng cấp Redis

Redis 6.2 trở lên có thể thay thế tại chỗ; nó đọc được dữ liệu do phiên bản cũ
ghi ra.

- Nếu dùng gói của bản phân phối:
  ```sh
  sudo apt-get update
  sudo apt-get install --only-upgrade redis-server redis-tools
  ```
- Nếu dùng kho chính thức của Redis, làm theo
  [redis.io/docs/latest/operate/oss_and_stack/install/install-stack/apt](https://redis.io/docs/latest/operate/oss_and_stack/install/install-stack/apt/)
  và nâng cấp các gói `redis`.

Sau đó khởi động lại Redis và Gotham:

```sh
sudo systemctl restart redis-server
sudo systemctl start gotham
```

Redis không giữ dữ liệu mà Gotham không thể tạo lại (phiên và hàng đợi), nên
mất bộ nhớ đệm không phải mất dữ liệu. PostgreSQL mới là cơ sở dữ liệu quan
trọng: luôn sao lưu nó trước.

## Cơ sở dữ liệu được quản lý

Nếu bạn đặt `GOTHAM_DATABASE_DSN` hoặc `GOTHAM_REDIS_ADDR`, Gotham không quản lý
các dịch vụ đó và trình cài đặt không bao giờ chạm vào chúng. Hãy nâng cấp theo
quy trình của nhà cung cấp.
