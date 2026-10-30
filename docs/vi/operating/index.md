---
title: Vận hành Gotham
---

# Vận hành Gotham

Lộ trình này trình bày phần hạ tầng của nền tảng: các máy chủ Gotham quản lý,
các container chạy trên đó, và các công cụ cấp máy chủ mà chủ sở hữu dùng để xem
bên trong một node. Trang này không nói về triển khai ứng dụng hay cơ sở dữ liệu;
những phần đó nằm trong [Sử dụng Gotham](/vi/using/).

Mọi khối lượng công việc chạy trên một node do chủ sở hữu đăng ký, nên hãy bắt
đầu từ đây.

## Quyền truy cập

Tài khoản chủ sở hữu vận hành instance. Chủ sở hữu đăng ký máy chủ, kết nối nhà
cung cấp Git, đổi cài đặt instance và mạng, quản lý bề mặt Tên miền & SSL toàn cục,
áp dụng cập nhật, và mở terminal cùng trình quản lý tệp trên máy chủ. API token của
chủ sở hữu còn cần phạm vi `admin` cho các tuyến này.

Thành viên làm việc trong những dự án chủ sở hữu đã cấp. Họ triển khai và vận hành
ứng dụng, dịch vụ, cơ sở dữ liệu ở đó; họ không thấy máy chủ, khoá máy chủ, kết nối
Git, cài đặt instance, proxy/DNS toàn cục, cập nhật hay công cụ máy chủ. Terminal
trong container ứng dụng cũng chỉ dành cho chủ sở hữu. Thành viên mất quyền vào một
dự án sẽ mất tài nguyên của dự án đó ở yêu cầu tiếp theo.

Xem [Bảo mật](/vi/operating/security) để biết mô hình tin cậy phía sau các tuyến
này.

## Các trang

- [Node và container](/vi/operating/nodes-containers) — đăng ký và sửa node, xác
  thực lại SSH, đặt lại host key đã ghim, xem tổng quan và chỉ số node, và quản lý
  container cùng log của node.
- [Terminal và tệp](/vi/operating/terminal-files) — shell trên máy chủ và trình
  quản lý tệp từ xa, cùng các cờ tính năng gác cả hai.
- [Kho ứng dụng](/vi/operating/app-store) — cài và gỡ công cụ máy chủ trên một node.
- [Cài đặt và cài đặt máy chủ của nút](/vi/operating/instance-network) — địa chỉ
  và tên control plane, cùng múi giờ, mạng và cài đặt hệ thống của từng nút.
- [Proxy và DNS](/vi/operating/proxy-dns) — bộ định tuyến Traefik, chứng chỉ và nhà
  cung cấp DNS.
- [Bảo mật](/vi/operating/security) — tài khoản chủ sở hữu và thành viên, quyền dự
  án, API token và mô hình tin cậy phía sau quyền truy cập máy chủ.
