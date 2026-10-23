---
title: Vận hành Gotham
---

# Vận hành Gotham

Phần này nói về phía hạ tầng của nền tảng: các máy chủ Gotham quản lý, các
container chạy trên đó, và những công cụ cấp máy chủ mà quản trị viên dùng để
xem bên trong một node. Đây không phải phần triển khai ứng dụng hay cơ sở dữ
liệu; những nội dung đó nằm trong [Sử dụng Gotham](/vi/using/).

Mọi khối lượng công việc đều chạy trên một node do quản trị viên đăng ký, nên
hãy bắt đầu tại đây.

## Truy cập

Phần này áp dụng hai mức quyền:

- **Quản lý node và container** theo phạm vi nhóm. Thành viên của nhóm sở hữu
  node có thể đọc node và các container của nó; để khởi động, dừng, sửa hoặc
  xóa cần vai trò nhóm `owner` hoặc `admin`. Xem
  [Bảo mật](/vi/operating/security).
- **Chỉ quản trị viên nền tảng**: shell trên máy chủ và trình quản lý tệp. Quản
  trị viên nền tảng là một phiên có claim vai trò `admin`, một tài khoản có email
  nằm trong `PLATFORM_ADMINS`, hoặc một API token mang phạm vi `admin`. Chủ sở
  hữu nhóm không có quyền này sẽ nhận `403` từ các tuyến đó.

Terminal của container ứng dụng là một bề mặt riêng ở cấp nhóm: shell bên trong
container ứng dụng đang chạy cần quyền ghi vào nhóm của ứng dụng đó, không phải
quyền quản trị viên nền tảng. Xem
[Ứng dụng](/vi/using/applications#cac-tab-chi-tiet).

## Các trang

- [Node và container](/vi/operating/nodes-containers) — đăng ký và sửa node,
  kiểm tra lại SSH, đặt lại khóa máy chủ đã ghim, xem tổng quan và chỉ số của
  node, và quản lý container cùng nhật ký của node.
- [Terminal và tệp](/vi/operating/terminal-files) — shell trên máy chủ của một
  node và trình quản lý tệp từ xa, kèm các cờ tính năng điều khiển cả hai.
- [Cửa hàng ứng dụng](/vi/operating/app-store) — cài đặt và gỡ bỏ công cụ máy
  chủ trên một node.
- [Mạng của instance](/vi/operating/instance-network) — địa chỉ của control
  plane, hostname, DNS và cài đặt thời gian.
- [Proxy và DNS](/vi/operating/proxy-dns) — bộ định tuyến Traefik, chứng chỉ và
  nhà cung cấp DNS.
- [Bảo mật](/vi/operating/security) — quản trị viên, nhóm và mô hình tin cậy
  đằng sau quyền truy cập cấp máy chủ.
