---
title: Lưu trữ
---

# Lưu trữ

**Quản lý → Tích hợp → Lưu trữ** là nơi quản lý các điểm đến mà bản sao lưu ghi
vào. Chỉ chủ sở hữu thấy trang này, giống các trang vận hành khác; API token
cũng cần quyền `admin`.

Cấu hình một lần tại đây và mọi lần sao lưu thủ công cùng lịch chạy đều chọn
được. Trang **Bản sao lưu** không còn tab lưu trữ riêng; khi chưa cấu hình điểm
đến nào của riêng bạn, trang đó liên kết tới đây.

## Đĩa cục bộ có sẵn

Mọi bản cài đều có điểm đến **Đĩa cục bộ**, ghi vào thư mục sao lưu của máy điều
khiển. Không cần cấu hình, không thể xóa hay đổi tên, nên bản cài mới sao lưu
được ngay.

Danh sách hiện, theo từng điểm đến, số bản sao lưu và tổng dung lượng. Với Đĩa
cục bộ, danh sách còn hiện dung lượng trống.

## Lưu trữ tương thích S3

Lưu trữ tương thích S3 dùng được với AWS S3, Cloudflare R2, MinIO và các dịch vụ
tương tự. Một điểm đến có tên, endpoint, vùng, bucket, tiền tố khóa tùy chọn,
access key và secret key.

- **Khóa chỉ ghi một chiều.** Khóa được mã hóa trên máy chủ và không bao giờ
  được API trả lại hay hiển thị trong giao diện. Để trống ô khóa khi sửa sẽ giữ
  khóa đã lưu.
- **Kiểm tra** ghi, đọc lại rồi xóa một tệp thử nhỏ, nên điểm đến chỉ đọc hoặc
  chặn xóa sẽ được báo trước lần sao lưu thật đầu tiên. Với Đĩa cục bộ, thao tác
  này chứng minh cùng một vòng ghi/đọc/xóa. Kết quả hiện ngay trong danh sách.

## Xóa một điểm đến

Xóa điểm đến không xóa các bản sao lưu cũ, nhưng chúng không đọc lại được từ đó,
nên khôi phục từ các lần chạy đó sẽ thất bại và lần chạy vẫn hiện là **Nơi lưu
trữ đã bị xóa**. Giao diện hỏi xác nhận trước khi xóa.

Không thể xóa điểm đến còn được một lịch chạy dùng: hãy chuyển hoặc xóa các lịch
đó trước, để không lịch nào âm thầm quay về đĩa cục bộ. Xóa điểm đến còn giữ bản
sao lưu mà không xác nhận sẽ bị từ chối với lỗi xung đột rõ ràng.

## API

API lưu trữ độc lập nằm dưới `/api/v1/storages` và cần tài khoản chủ sở hữu cùng
quyền `admin` trên API token:

| Phương thức | Đường dẫn | Mục đích |
| --- | --- | --- |
| `GET` | `/api/v1/storages` | Liệt kê điểm đến kèm dung lượng |
| `POST` | `/api/v1/storages` | Tạo điểm đến |
| `PATCH` | `/api/v1/storages/{id}` | Sửa điểm đến |
| `DELETE` | `/api/v1/storages/{id}` | Xóa điểm đến (`confirm=true` khi còn bản sao lưu) |
| `POST` | `/api/v1/storages/{id}/test` | Chạy kiểm tra kết nối |

Các tuyến `/api/v1/backups/providers` cũ vẫn còn như bí danh tương thích đã lỗi
thời cho API token và MCP client hiện có. Chúng dùng chung dịch vụ và nhận cùng
payload; client mới nên dùng `/storages`.
