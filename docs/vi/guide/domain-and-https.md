---
title: Tên miền và HTTPS
---

# Tên miền và HTTPS

Bạn có thể dùng Gotham tại `http://<server-ip>:8000` ngay sau khi cài. Trang này
đổi địa chỉ đó thành tên miền của bạn với một chứng chỉ tin cậy, trong lúc cài
hoặc sau đó trong bảng điều khiển.

## Bạn cần gì

- Một tên miền (hoặc tên miền con) có bản ghi A/AAAA trỏ về máy chủ control
  plane.
- Cổng vào 80 và 443 mở trên máy chủ đó.
- Node `localhost` của control plane chạy Traefik. Trình cài đặt thiết lập việc
  này mặc định.
- Nếu tên miền nằm sau Cloudflare, đặt bản ghi thành **DNS only** (tắt proxy) cho
  luồng HTTP-01 thông thường, và xóa các bản ghi AAAA cũ khi máy chủ không phục vụ
  được IPv6.

## Đặt tên miền trong lúc cài

Trình cài đặt hỏi `Panel domain (optional, Enter skips)`. Nhập tên miền, hoặc
truyền nó mà không cần hỏi:

Thêm `-s -- --domain panel.example.com` sau `sh` trong lệnh cài (xem
[Cài đặt](/vi/guide/install)).

Trước khi áp dụng tên miền, trình cài đặt chờ tối đa **60 giây** để agent
localhost sẵn sàng. Nếu hết thời gian chờ, địa chỉ HTTP của máy chủ vẫn khả dụng.
Sau đó nó:

1. đặt `control_plane_url` của instance thành `https://panel.example.com`;
2. khởi động lại dịch vụ để bảng điều khiển trả lời ở địa chỉ mới;
3. in địa chỉ HTTP để mở.

Mở `http://panel.example.com` và xác nhận tên miền trong **Cài đặt** trong
vòng **2 phút** để bắt đầu cấp chứng chỉ HTTPS. Một phiên thử không được xác nhận
sẽ tự khôi phục. Tên miền không hợp lệ và lỗi DNS sẽ in cảnh báo và để địa chỉ
HTTP của máy chủ khả dụng; bạn có thể đặt tên miền sau trong bảng điều khiển.

## Đặt tên miền sau

1. Mở bảng điều khiển tại `http://<server-ip>:8000`.
2. Trong **Cài đặt**, đặt `control_plane_url` của instance thành
   `https://panel.example.com` rồi lưu.
3. Mở host được đề xuất qua HTTP và xác nhận thay đổi trước khi phiên thử hết hạn.

Việc lưu bắt đầu một phiên xác nhận và tạo một bản ghi ý định chứng chỉ thuộc
bảng điều khiển; không cần ứng dụng nào. Trong phiên thử, cả host được đề xuất
lẫn host trước đó đều được định tuyến, và hoàn tác sẽ khôi phục host trước đó nếu
việc cấp chứng chỉ thất bại.

Router HTTPS bắt đầu việc cấp ACME trong khi HTTP vẫn chuyển tiếp tới bảng điều
khiển. HTTP chỉ chuyển hướng sang HTTPS sau khi agent cục bộ báo một chứng chỉ
chưa hết hạn; trạng thái sẵn sàng được làm mới mỗi 30 giây. Việc cấp đang chờ
hoặc thất bại vẫn giữ HTTP khả dụng.

## HTTP-01 và DNS-01

HTTP-01 là mặc định: bản ghi A/AAAA của tên miền phải trỏ trực tiếp vào máy chủ
này, và các bản ghi Cloudflare phải là **DNS only** (tắt proxy).

Để giữ proxy của Cloudflare bật, hãy cấu hình một nhà cung cấp DNS và chọn DNS-01
cho ý định chứng chỉ của bảng điều khiển qua API
`PATCH /api/v1/proxy/panel-certificates/{id}` chỉ dành cho chủ sở hữu,
dùng các trường `challenge` và `dns_provider_id` sẵn có. DNS-01 cần một token
được phép cập nhật vùng đã cấu hình; dùng Full (strict) khi chứng chỉ origin đã
được cấp.

## Bảng điều khiển và API báo gì

API routers báo host được đề xuất trong một phiên thử và thêm
`certificate.status` (`issued`, `pending` hoặc `failed`) cùng
`certificate.not_after` cho các dòng bảng điều khiển. API chỉ dành cho chủ sở hữu
`GET /api/v1/proxy/panel-certificates` liệt kê các ý định chứng chỉ của bảng điều
khiển với `status` sẵn có của kho lưu trữ cùng các trường bổ sung `panel_state`
và `owner_kind`. `pending` nghĩa là chưa quan sát được chứng chỉ dùng được nào;
`failed` bao gồm cấu hình không dùng được hoặc kho lưu trữ không khả dụng, vì
agent không báo lỗi đơn ACME. Các API chứng chỉ ứng dụng theo quyền dự án của ứng
dụng.

Nếu HTTPS hoặc DNS vẫn thất bại, xem
[Xử lý sự cố](/vi/guide/troubleshooting#https-hoac-dns-cua-bang-đieu-khien-that-bai).
