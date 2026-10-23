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

**Cài đặt → Cập nhật** chỉ dành cho quản trị viên nền tảng. Trang này hiển thị
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

Agent không tự cập nhật trừ khi bạn bật. Theo mặc định, mỗi agent hỏi control
plane qua kênh đã xác minh bằng CA mỗi 5 phút. Với mỗi kiến trúc được hỗ trợ
(`amd64`, `arm64`), control plane xác định bản phát hành agent mới nhất, xác minh
manifest đã ký của nó, và chỉ đề nghị khi nó mới hơn phiên bản agent báo trong nhịp
tim gần nhất.

Kích hoạt một đợt triển khai bằng API của quản trị viên nền tảng:

```sh
curl -X POST -H "Authorization: Bearer <admin-token>" \
  https://<panel>/api/v1/servers/agents/update-all
```

Lệnh gọi này ghi lại mục tiêu triển khai và trả về ngay, không chờ: mỗi agent nhận
mục tiêu ở lần hỏi tiếp theo. Chưa có điều khiển nào trên bảng điều khiển cho việc
này. `GET /api/v1/servers/agents` trả về phiên bản từng node báo cáo, mục tiêu
triển khai đang hoạt động và bản phát hành mới nhất mà control plane thấy được.

Để cho phép một node tự cập nhật không cần giám sát, đặt
`GOTHAM_AGENT_AUTO_UPDATE=true` trong môi trường agent của node đó. Một lần kích
hoạt thất bại sẽ quay lui và giãn thời gian chờ giống control plane; muốn thử lại
cùng phiên bản cần người vận hành đặt lại trên node:

```sh
sudo gotham-agent update reset
```

Cài lại một phiên bản cụ thể là một lần cài bình thường với
`GOTHAM_VERSION=vX.Y.Z`; xem [Cài đặt](/vi/guide/install).

## Phạm vi xác minh

Với cả hai tệp nhị phân, manifest được ký bằng khóa phát hành nhúng trong tệp nhị
phân đang chạy, và nó ràng buộc phiên bản, kiến trúc, tên tệp và kênh. SHA-256 của
artifact tải về được so với manifest đã ký; bất kỳ sai lệch nào đều hủy bản cập
nhật. Bản phát hành nháp bị bỏ qua, và bản phát hành thiếu asset hoặc manifest
không bao giờ được đề nghị. Không có tài liệu khóa ký nào được lưu trên máy chủ
hay cần đến trên máy chủ.

Nếu một lần cập nhật thất bại, xem [Xử lý sự cố](/vi/guide/troubleshooting).
