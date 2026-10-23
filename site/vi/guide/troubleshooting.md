---
title: Xử lý sự cố
---

# Xử lý sự cố

Hãy bắt đầu với journal của dịch vụ và endpoint kiểm tra sức khỏe của control
plane. Cả hai đều nhỏ và trả lời hầu hết câu hỏi "nó có đang chạy không?" trước
khi bạn phải đào vào cấu hình.

```sh
journalctl -u gotham -n 200 --no-pager      # control plane
journalctl -u gotham-agent -n 200 --no-pager # node agent
journalctl -u gotham -f                     # follow
```

Log đi vào journal của systemd. Định dạng mặc định là JSON; đặt
`GOTHAM_LOG_FORMAT=text` để có dòng dễ đọc và `GOTHAM_LOG_LEVEL=debug` để có thêm
chi tiết. Khi có tệp `gotham.yaml`, mức log được nạp lại nóng từ đó; định dạng thì
cố định lúc khởi động và cần khởi động lại.

`GET /healthz` trên bảng điều khiển trả về `{"status":"ok","db":"ok","redis":"ok"}`
và chỉ trả 200 khi cả PostgreSQL và Redis đều phản hồi; nếu không nó trả 503 với
`"status":"degraded"`.

## Control plane không khởi động

Dịch vụ thoát kèm lý do trong journal. Các nguyên nhân thường gặp:

- **Lỗi cấu hình.** `gotham serve` in `config: ...` trước khi làm bất cứ việc gì.
  Kiểm tra `/etc/gotham/gotham.env` xem có giá trị sai định dạng không (dấu nháy
  và dấu gạch chéo ngược ở cuối bị từ chối), rồi khởi động lại.
- **Cổng đang được dùng.** Bảng điều khiển mặc định lắng nghe trên `0.0.0.0:8000`
  (`GOTHAM_SERVER_ADDR`, `GOTHAM_SERVER_PORT`). Một tiến trình `gotham` thứ hai,
  một unit cũ hoặc một dịch vụ không liên quan trên cùng cổng làm listener thất
  bại. Tìm chủ sở hữu bằng `ss -ltnp` rồi dừng nó, hoặc chuyển bảng điều khiển
  sang cổng khác.
- **Không với tới PostgreSQL.** Cơ sở dữ liệu là bắt buộc: tiến trình thoát khi
  không kết nối được. Kiểm tra `pg_isready`, dịch vụ `postgresql` và
  `GOTHAM_DATABASE_DSN`. Bản cài đặt trọn gói dùng cơ sở dữ liệu cục bộ; cơ sở dữ
  liệu được quản lý cần một DSN với tới được.
- **Không với tới Redis.** Redis không bắt buộc để khởi động — bảng điều khiển
  vẫn phục vụ — nhưng `/healthz` báo 503, và các tính năng thời gian thực cùng
  việc truyền log bị suy giảm. Kiểm tra dịch vụ `redis` và `GOTHAM_REDIS_ADDR`
  (mặc định cổng 6379) rồi xác nhận bằng `redis-cli ping`.
- **Không có certificate authority.** `gotham serve` từ chối khởi động kênh agent
  ở dạng plaintext khi không có CA (`no CA found and GOTHAM_GRPC_INSECURE is not set`).
  Chạy lại trình cài đặt, vốn tạo CA theo kiểu lũy đẳng (`gotham ca init` giữ
  nguyên CA đã có). `GOTHAM_GRPC_INSECURE=true` là lối thoát chỉ dành cho phát
  triển và không được dùng trong production.
- **Xung đột cổng gRPC.** Gateway agent mặc định lắng nghe trên cổng 9442
  (`GOTHAM_GRPC_ADDR`); cách kiểm tra xung đột cổng cũng như trên.

Nếu dịch vụ cứ khởi động lại, hãy tìm lý do thoát ở cuối journal, và kiểm tra xem
unit có đang chạy đúng tệp nhị phân mà bạn nghĩ không.

## Một node hiện ngoại tuyến

Node ở trạng thái ngoại tuyến khi không có nhịp tim nào trong 30 giây; agent gửi
một nhịp mỗi 10 giây, nên đây là vấn đề kết nối đang diễn ra chứ không phải một
dòng dữ liệu cũ.

1. **Agent có đang chạy không?** `systemctl status gotham-agent` và
   `journalctl -u gotham-agent -f`. Agent thất bại an toàn khi không có CA: thiếu
   `GOTHAM_AGENT_CA` (hoặc `--insecure` chỉ dành cho phát triển cục bộ) là nó từ
   chối khởi động.
2. **Endpoint kiểm tra sức khỏe có hoạt động không?** Agent phục vụ `/healthz`
   trên địa chỉ kiểm tra sức khỏe của nó (`GOTHAM_AGENT_HEALTH_ADDR`, mặc định
   loopback và cổng 8001). Nó chỉ báo tiến trình còn sống — vẫn trả 200 kể cả khi
   không với tới control plane, nên hãy dùng log của agent để tìm lỗi kết nối.
3. **Agent có với tới control plane không?** `GOTHAM_AGENT_CP_ADDR` phải ở dạng
   `host:port` và với tới được. Tên hoặc IP của nó cũng phải nằm trong các SAN
   của chứng chỉ listener control plane (`GOTHAM_GRPC_HOSTS`, được lưu bởi
   `gotham ca init --host`, hoặc `--cp-host` lúc cài đặt). Lỗi bắt tay như
   `certificate is valid for ..., not <name>` nghĩa là địa chỉ đó không phải SAN;
   hãy thêm nó và cấp lại chứng chỉ listener.
4. **Thông tin đăng nhập mTLS còn nguyên vẹn không?** Qua TLS, kênh là song
   hướng: node giữ chứng chỉ (`agent.crt`) và khóa (`agent.key`) trong thư mục
   chứng chỉ của nó, và tin control plane qua `/etc/gotham/ca.crt`
   (`GOTHAM_AGENT_CA`). Để cài CA công khai hiện tại và khởi động lại agent:

   ```sh
   sudo GOTHAM_AGENT_CP_ADDR=<cp-host>:9442 GOTHAM_AGENT_NODE_ID=<node> \
       gotham/deploy/install-agent.sh --ca ./ca.crt
   ```

   Không bao giờ sao chép `ca.key` sang node. Nếu CA của control plane đã bị thay,
   chứng chỉ cũ của node không còn hợp lệ và nó không thể đăng ký lại dưới cùng id
   khi khóa vẫn còn gắn: hãy xóa node (chỉ được phép khi nó không còn tài nguyên
   nào) rồi thêm lại, việc này biến nó thành lần đăng ký đầu tiên và hỏi token
   đăng ký. Điều tương tự cũng đúng khi chứng chỉ và khóa của node bị mất. Đặt lại
   host key (`DELETE /api/v1/servers/{id}/host-key`) chỉ xóa ghim SSH đã lưu dùng
   cho việc kiểm tra; nó không đụng đến chứng chỉ agent.

5. **Lệch phiên bản.** Agent báo phiên bản build trong mỗi nhịp tim, và
   `GET /api/v1/servers/agents` liệt kê phiên bản theo từng node. Control plane
   đề nghị bản phát hành agent mới nhất khi nó mới hơn phiên bản node báo; node chỉ
   nhận nếu kênh khớp (node `stable` từ chối `beta`), và không có đề nghị nào khi
   nó đã là bản mới nhất. Cập nhật node (xem [Cập nhật](/vi/guide/updates)) hoặc
   cài lại nó. Chứng chỉ node cấp trước mutual TLS sẽ không còn được chấp nhận sau
   mốc loại bỏ chứng chỉ kiểu cũ (`GOTHAM_GRPC_LEGACY_LEAF_UNTIL`); chúng được gia
   hạn ở lần kết nối tiếp theo, và node không thể gia hạn phải được cài lại kèm
   CA.

Nếu node với tới được qua SSH nhưng bước kiểm tra của wizard thất bại, hãy kiểm
tra Docker, dung lượng đĩa còn trống, và rằng người dùng SSH chạy được Docker mà
không cần nhập tương tác. Wizard có thể cài Docker cho bạn khi đó là kiểm tra duy
nhất thất bại.

## HTTPS hoặc DNS của bảng điều khiển thất bại

Chứng chỉ của bảng điều khiển được cấp qua Traefik của node đã đăng ký, nên HTTPS
phụ thuộc vào DNS và vào việc cổng 80/443 có tới được node đó không.

- **HTTP-01 là mặc định.** Bản ghi A và AAAA của tên miền phải trỏ trực tiếp vào
  máy này. Xóa các bản ghi AAAA cũ nếu máy không phục vụ được IPv6. Trên
  Cloudflare, bản ghi phải là **DNS only** (tắt proxy) cho HTTP-01; giữ proxy tắt
  cho đến khi có chứng chỉ.
- **Node phải chạy Traefik.** HTTPS cho bảng điều khiển dùng node `localhost` đã
  đăng ký, nên node đó cần Traefik và mở cổng vào 80 và 443.
- **Phiên thử phải được xác nhận.** Lưu `control_plane_url` của instance dưới dạng
  `https://...` sẽ bắt đầu một phiên xác nhận và tạo một bản ghi ý định chứng chỉ
  cho bảng điều khiển. Hãy xác nhận qua host được đề xuất trước khi phiên thử hết
  hạn. Trong phiên thử, cả host được đề xuất lẫn host trước đó đều được định tuyến;
  hoàn tác sẽ khôi phục host trước đó nếu việc cấp chứng chỉ thất bại.
- **HTTP vẫn hoạt động cho đến khi cấp thành công.** Bảng điều khiển chỉ chuyển
  hướng sang HTTPS sau khi agent cục bộ báo một chứng chỉ chưa hết hạn (trạng thái
  sẵn sàng được làm mới mỗi 30 giây). Việc cấp đang chờ hoặc thất bại vẫn giữ HTTP
  khả dụng.
- **Nơi cần xem.** Các dòng bảng điều khiển trong API routers mang
  `certificate.status` (`issued`, `pending`, `failed`) và `certificate.not_after`.
  `pending` nghĩa là chưa quan sát được chứng chỉ dùng được nào; `failed` bao gồm
  cấu hình không dùng được hoặc kho lưu trữ không khả dụng, vì agent không báo lỗi
  đơn ACME.

Giữ proxy Cloudflare bật đòi hỏi đã cấu hình một nhà cung cấp DNS và dùng DNS-01
cho chứng chỉ bảng điều khiển, được đặt qua API nền tảng chứ không phải bảng điều
khiển. Xem [Proxy và DNS](/vi/operating/proxy-dns) và
[Mạng của instance](/vi/operating/instance-network).

## Kiểm tra trạng thái migration

Sau khi nâng cấp, sau khi thay tệp nhị phân bằng tay hoặc sau một lần khởi động
thất bại, hãy xác nhận schema trước mọi việc khác:

```sh
sudo -u gotham env GOTHAM_DATABASE_DSN="<dsn>" \
  /var/lib/gotham/bin/gotham migrate status
```

Nó in một dòng cho mỗi migration — `VERSION`, `STATE`, `APPLIED AT`, `PATH` — và
chỉ đọc, nên không khóa và an toàn khi đang chẩn đoán. `gotham migrate` đọc
`gotham.yaml` và biến môi trường `GOTHAM_*` của chính nó, không đọc
`EnvironmentFile` của unit, nên hãy trỏ nó vào cơ sở dữ liệu của dịch vụ một cách
tường minh; nếu không nó sẽ kiểm tra nhầm cơ sở dữ liệu (hoặc không kiểm tra gì).

`gotham migrate up` áp dụng các migration đang chờ; `gotham migrate down` chỉ quay
lui migration được áp dụng gần nhất. Migration là một chiều: không bao giờ sửa một
migration đã chạy — hãy sửa schema bằng một migration mới. Trình cài đặt chạy
`migrate up` thay bạn, nên thiếu migration thường có nghĩa là dịch vụ được khởi
động với một DSN khác hoặc trình cài đặt bị gián đoạn.

## Vẫn chưa giải quyết được

- [Cài đặt](/vi/guide/install) — bố cục thư mục, CA, cài agent và tệp môi trường
  của dịch vụ.
- [Cập nhật](/vi/guide/updates) — quay lui, đặt lại và thời gian chờ của bản phát
  hành lỗi.
- [Tham chiếu: cấu hình](/vi/reference/configuration) — các khóa môi trường.
