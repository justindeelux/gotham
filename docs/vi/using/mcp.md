---
title: Thiết lập MCP
---

# Thiết lập MCP

MCP (Model Context Protocol) cho phép trợ lý AI như Claude Code hay Cursor làm
việc với instance Gotham của bạn: liệt kê dự án, máy chủ và ứng dụng, đọc log và
số liệu, và — khi bạn cho phép — tạo và triển khai tài nguyên. Ứng dụng đăng nhập
bằng API token; phiên bảng điều khiển của bạn không bao giờ được trao cho nó.

## Trước khi bắt đầu

MCP cần một địa chỉ HTTPS. Nếu URL của instance là HTTP thường, trang thiết lập
cảnh báo rằng token và dữ liệu công cụ sẽ truyền đi không được mã hóa. Hãy đặt địa
chỉ HTTPS trước khi kết nối ứng dụng; HTTP thường chỉ phù hợp với địa chỉ loopback
trên cùng máy khi phát triển.

Chủ sở hữu đặt địa chỉ trong **Hệ thống Gotham → Chung → URL control plane**.
Thành viên không đổi được, nên hãy đề nghị chủ sở hữu khi thấy cảnh báo.

## Tạo kết nối

1. Mở **Thiết lập MCP** ở sidebar trong mục **Truy cập** (hoặc vào
   `/settings/mcp`).
2. Chọn mức truy cập:
   - **Chỉ đọc** — xem dự án, máy chủ, ứng dụng và tài nguyên. Không thể tạo,
     thay đổi hay triển khai gì.
   - **Đọc và triển khai** — mọi quyền của chỉ đọc, cộng thêm công cụ tạo, thay
     đổi và triển khai. Chọn mức này khi muốn trợ lý thực hiện thay đổi.
3. Chọn **Tạo token**.
4. Sao chép một trong các cấu hình được hiện. Token chỉ hiển thị một lần và các
   đoạn cấu hình chỉ giữ nó khi hộp thoại mở, nên hãy sao chép trước khi đóng.

Trang thiết lập không bao giờ cung cấp phạm vi `admin`. Phạm vi đó dành cho các thao
tác instance chỉ chủ sở hữu và không thuộc kết nối của trợ lý. Cả chủ sở hữu và
thành viên đều tạo được token chỉ đọc hoặc triển khai ở đây.

Token có tên `MCP setup <ngày>` và xuất hiện trong **Cài đặt → API token**, nơi
bạn có thể xoay vòng hoặc thu hồi sau này.

## Ứng dụng của thành viên chạm tới được gì

Token hành động như tài khoản đã tạo nó. Ứng dụng của thành viên chỉ chạm tới những
dự án chủ sở hữu đã cấp cho thành viên đó; mọi thứ khác đều bị ẩn, và một yêu cầu
trực tiếp trả lời như thể tài nguyên không tồn tại. Quyền được kiểm tra ở mọi yêu
cầu, nên thay đổi quyền có hiệu lực ngay.

## Dán cấu hình

Trang này cung cấp ba dạng, tất cả đều dùng URL MCP `<URL instance>/mcp`.

**Claude Code (dòng lệnh)**

```sh
claude mcp add --transport http gotham https://gotham.example.com/mcp --header "Authorization: Bearer <YOUR_TOKEN>"
```

**Cursor và ứng dụng khác (url + headers)**

```json
{
  "mcpServers": {
    "gotham": {
      "url": "https://gotham.example.com/mcp",
      "headers": {
        "Authorization": "Bearer <YOUR_TOKEN>"
      }
    }
  }
}
```

**Ứng dụng khác (URL và token)**

```text
URL: https://gotham.example.com/mcp
Token: <YOUR_TOKEN>
```

Mọi ứng dụng MCP hỗ trợ streamable HTTP đều dùng được URL thô và gửi token dưới
dạng `Authorization: Bearer <YOUR_TOKEN>`.

## Xoay vòng hoặc thu hồi

Mở **Cài đặt → API token** từ menu tài khoản. **Xoay vòng** thay bí mật và dừng
bí mật cũ ngay lập tức; hãy cập nhật token mới vào ứng dụng. **Thu hồi** kết thúc
quyền truy cập ngay và không thể hoàn tác. Token đã thu hồi nhận `401`, nên hãy xóa
mục cũ trong ứng dụng nữa.
