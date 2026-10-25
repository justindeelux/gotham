---
title: Thông báo
---

# Thông báo

Mở **Cài đặt → Thông báo** (`/settings/notifications`). Một kênh thuộc về một ứng
dụng hoặc một cơ sở dữ liệu, và chỉ người truy cập được tài nguyên đó mới thấy kênh
trong danh sách.

| Loại | Cấu hình |
|---|---|
| Discord | URL webhook đến. |
| Slack | URL webhook đến. |
| Telegram | Token bot và chat ID mà bot đăng tới. |
| Email (SMTP) | Host SMTP, cổng, tên người dùng và mật khẩu tùy chọn, địa chỉ gửi và ít nhất một người nhận. |

Bí mật truyền tải — URL webhook, token bot, mật khẩu SMTP — được niêm phong phía máy
chủ trước khi lưu, không bao giờ ghi log và không bao giờ được API trả về. Khi đọc,
giá trị được che, tối đa để lộ bốn ký tự cuối, và một kết nối đã lưu cho biết bí mật
đã được cấu hình hay chưa. Sửa một kênh và để nguyên giá trị đã che sẽ giữ bí mật đã
lưu; loại kênh không thể đổi sau khi tạo.

## Phạm vi và sự kiện

Một kênh theo dõi đúng một ứng dụng hoặc một cơ sở dữ liệu:

- Kênh ứng dụng nhận sự kiện triển khai của ứng dụng đó.
- Kênh cơ sở dữ liệu nhận sự kiện sao lưu của cơ sở dữ liệu đó.

Các sự kiện đăng ký được là `deploy_success`, `deploy_failure`, `backup_success` và
`backup_failure`. Kênh mới đăng ký tất cả trừ khi bạn chọn một tập con. Kênh bị tắt
không nhận gì.

## Gửi thử và lịch sử gửi

**Gửi thử** gửi một tin nhắn thử tổng hợp tới đúng kênh đó và báo kết quả ngay. Nó
được ghi vào lịch sử như một lần thử thật.

**Lịch sử gửi** hiển thị 20 lần thử mới nhất của kênh, mới nhất trước: sự kiện (hoặc
**Tin nhắn thử**), đã gửi hay thất bại, thời điểm, và một tóm tắt lỗi có giới hạn cho
các lần thất bại. Mỗi kênh giữ 50 lần thử cuối; các dòng cũ hơn bị cắt tự động. Việc
gửi không bao giờ chặn một lần triển khai hay sao lưu — nó chạy trên một hàng đợi nền
có giới hạn, và một kênh lỗi không làm dừng các kênh khác.

Tạo, sửa, xóa, bật và thử một kênh cần quyền ghi vào dự án chứa tài nguyên. Thành
viên ngoài dự án đó không thấy kênh.
