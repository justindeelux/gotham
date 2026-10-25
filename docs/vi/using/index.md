---
title: Sử dụng Gotham
---

# Sử dụng Gotham

Lộ trình này dành cho thành viên triển khai và vận hành khối lượng công việc
trong bảng điều khiển web Gotham. Mọi thứ bạn tạo đều thuộc về một dự án và một
môi trường của dự án đó, và mọi tài nguyên đều chạy trên một node mà chủ sở hữu
đã đăng ký.

Không có danh sách Ứng dụng, Cơ sở dữ liệu hay Dịch vụ ở cấp cao nhất: tài nguyên
luôn được mở qua dự án và môi trường của nó, ví dụ
`/projects/{project}/environments/{environment}/applications/{id}`.

## Các trang

- [Dự án và môi trường](/vi/using/projects-environments) — tạo dự án, thêm hoặc
  chuyển môi trường, thêm tài nguyên và quản lý biến dùng chung.
- [Ứng dụng](/vi/using/applications) — nguồn, pipeline triển khai, triển khai và
  quay lui, log, các tab chi tiết và biến môi trường.
- [Cơ sở dữ liệu](/vi/using/databases) — engine, thông tin đăng nhập, sao lưu và
  khôi phục.
- [Dịch vụ và template](/vi/using/services-templates) — mỗi dịch vụ là một dự án
  compose, và danh mục template.
- [Tên miền và bản xem trước](/vi/using/domains-previews) — gắn hostname và chứng
  chỉ, và xem các bản xem trước theo pull request.
- [Thành viên và quyền truy cập](/vi/using/members-access) — vai trò chủ sở hữu và
  thành viên, quyền dự án và lời mời.
- [Thiết lập MCP](/vi/using/mcp) — kết nối trợ lý AI bằng token chỉ đọc hoặc triển
  khai.
- [Thông báo](/vi/using/notifications) — kênh cảnh báo cho một ứng dụng hoặc cơ sở
  dữ liệu.

## Vai trò

Thành viên làm việc trong những dự án chủ sở hữu đã cấp. Không có vai trò riêng
trong từng dự án: bên trong dự án được cấp, thành viên đọc và ghi được mọi thứ; mọi
thứ ngoài quyền được cấp đều bị ẩn. Chủ sở hữu quản lý chính instance — người dùng,
máy chủ, kết nối Git, Tên miền & SSL toàn cục, cập nhật và công cụ máy chủ. Xem
[Thành viên và quyền truy cập](/vi/using/members-access).
