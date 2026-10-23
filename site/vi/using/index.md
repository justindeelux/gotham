---
title: Sử dụng Gotham
---

# Sử dụng Gotham

Lộ trình này dành cho thành viên nhóm triển khai và vận hành khối lượng công việc
trong bảng điều khiển web Gotham. Mọi thứ bạn tạo đều thuộc về một dự án và một
môi trường của dự án đó, và mọi tài nguyên đều chạy trên một node mà người vận
hành đã đăng ký.

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
- [Nhóm và thông báo](/vi/using/teams-notifications) — thành viên, lời mời, vai trò
  và kênh cảnh báo.

## Vai trò

Thành viên nhóm giữ một trong ba vai trò: `owner`, `admin` hoặc `read_only`.
Không có vai trò `member` riêng.

`owner` và `admin` ghi được. `read_only` đọc được mọi thứ nhưng không bao giờ ghi:
API từ chối mọi thao tác thay đổi từ tài khoản `read_only`. Bảng điều khiển ẩn phần
lớn điều khiển ghi, và một vài điều khiển vẫn hiện và dựa vào việc bị từ chối đó —
một nút hiện ra không có nghĩa là có quyền.
