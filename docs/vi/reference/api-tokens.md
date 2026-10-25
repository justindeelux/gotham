---
title: API token
---

# API token

API token là thông tin xác thực bearer cho tự động hóa: tác vụ CI, script và các
tích hợp gọi cùng API `/api/v1` mà bảng điều khiển web dùng. Một token thuộc về tài
khoản đã tạo nó, và mọi yêu cầu hành động như tài khoản đó trong phạm vi của token.

Token bắt đầu bằng `gt1_`, đây là cách máy chủ phân biệt chúng với JWT. Bản văn bản
thường là ngẫu nhiên, chỉ trả về một lần khi token được tạo hoặc xoay vòng, và chỉ
hash SHA-256 của nó được lưu. Hãy gửi nó trong header chuẩn:

```http
Authorization: Bearer gt1_<token>
```

Trong bảng điều khiển, mở **Cài đặt → API token** từ menu tài khoản để tạo, liệt
kê, xoay vòng và thu hồi token của bạn. Chúng cũng có thể được quản lý qua chính
API, dùng một phiên đã đăng nhập hoặc một token sẵn có.

## Phạm vi

| Phạm vi | Cấp quyền |
|---|---|
| `read` | Truy cập chỉ đọc: `GET` và `HEAD` trên API tài nguyên. |
| `deploy` | Mọi thứ `read` cấp, cộng các thao tác ghi (mọi phương thức không phải `GET`/`HEAD`) và các lần đọc chứa bí mật. |
| `admin` | Mọi thứ `deploy` cấp, cộng các thao tác chỉ chủ sở hữu: quản lý người dùng và quyền dự án, trình quản lý tệp trên node, proxy/DNS, và áp dụng bản cập nhật. |

Các phạm vi có thứ bậc: `admin` bao `deploy`, và `deploy` bao `read`. Một token được
tạo mà không có `scopes` (bỏ trống trường hoặc danh sách rỗng) nhận `read`; bản thân
body JSON vẫn là bắt buộc.

Việc áp phạm vi theo quy tắc phương thức read/deploy trên các tài nguyên thuộc dự
án: `GET`/`HEAD` cần `read`, mọi phương thức khác cần `deploy`. Các lần đọc trả về
bí mật vẫn bị gác theo `deploy` dù là yêu cầu `GET`:

- `GET /api/v1/databases/{id}/credentials` — mật khẩu cơ sở dữ liệu và root đã giải
  mã.
- `GET /api/v1/servers/{id}/files/content` — nội dung tệp, có thể gồm các tệp môi
  trường `0600`. Toàn bộ trình quản lý tệp còn cần phạm vi `admin` và tài khoản chủ
  sở hữu.

## Điểm cuối API

| Phương thức và đường dẫn | Mục đích |
|---|---|
| `GET /api/v1/tokens` | Liệt kê token của người gọi. Chỉ siêu dữ liệu, không bao giờ có bí mật. |
| `POST /api/v1/tokens` | Tạo một token. Trả về bản văn bản thường một lần. |
| `POST /api/v1/tokens/{id}/rotate` | Thu hồi token và cấp bản thay thế với cùng tên và phạm vi. |
| `DELETE /api/v1/tokens/{id}` | Thu hồi token. Trả lời 204. |

Một bản mô tả token gồm `id`, `name`, `scopes`, `last_used_at`, `revoked_at` và
`created_at`. Điểm cuối liệt kê không bao giờ trả `token` hay hash. `last_used_at`
được cập nhật theo mỗi yêu cầu đã xác thực theo kiểu nỗ lực tối đa.

## Kết nối MCP

Bảng điều khiển đọc cấu hình ứng dụng MCP từ một tuyến chỉ dành cho phiên đăng
nhập. Tuyến này từ chối API token, nên bản văn bản thường của token không bao giờ
đến trình duyệt:

```http
GET /api/v1/mcp/connection
Authorization: Bearer <session>
```

```json
{
  "base_url": "https://<control-plane>",
  "mcp_url": "https://<control-plane>/mcp",
  "https": true,
  "role": "member",
  "capabilities": ["context", "strict_reads"]
}
```

`base_url` là URL control-plane của instance khi đã cấu hình, ngược lại là origin
của yêu cầu. `https` là `false` khi URL gốc dùng HTTP thường, để bảng điều khiển
cảnh báo trước khi ứng dụng gửi thông tin xác thực. `role` là `owner` hoặc
`member`, và `capabilities` liệt kê những gì điểm cuối MCP hỗ trợ trên instance
này. Phản hồi không chứa token hay bí mật.

## Tạo

```sh
curl -X POST https://<control-plane>/api/v1/tokens \
  -H "Authorization: Bearer <session-or-token>" \
  -H "Content-Type: application/json" \
  -d '{"name":"ci-deploy","scopes":["deploy"]}'
```

Phản hồi (`201`) mang bản duy nhất của bí mật:

```json
{
  "id": "<uuid>",
  "name": "ci-deploy",
  "scopes": ["deploy"],
  "token": "gt1_<token>",
  "created_at": "<timestamp>"
}
```

Các quy tắc được áp khi tạo:

- `name` là bắt buộc và dài tối đa 64 ký tự.
- Một token chỉ cấp được những phạm vi nó đã có: token `read` không tạo được token
  `deploy`, và token `deploy` không tạo được token `admin`.
- Chỉ chủ sở hữu mới tạo được (hoặc xoay vòng) token phạm vi `admin`.
  Một phiên giữ mọi phạm vi, nhưng việc kiểm tra chủ sở hữu vẫn áp cho `admin`.

## Xoay vòng và thu hồi

Xoay vòng thu hồi ngay bí mật cũ và trả về một bí mật mới với cùng tên và phạm vi:

```sh
curl -X POST https://<control-plane>/api/v1/tokens/<id>/rotate \
  -H "Authorization: Bearer <session-or-token>"
```

Thu hồi là vĩnh viễn và trả lời `204`:

```sh
curl -X DELETE https://<control-plane>/api/v1/tokens/<id> \
  -H "Authorization: Bearer <session-or-token>"
```

Một token không tồn tại, đã bị thu hồi, hoặc của người khác trả lời `404`. Token đã
bị thu hồi không còn xác thực được: các yêu cầu trình nó nhận `401`. Khi người gọi
chính là một API token, nó phải giữ mọi phạm vi mà token nó quản lý mang; một phiên
bỏ qua bước kiểm tra phạm vi đó. Hãy cập nhật bí mật đã lưu ngay sau khi xoay vòng.

## Ghi chú vận hành

- Token không hết hạn. Hãy coi bản văn bản thường như mật khẩu: giữ nó ngoài lịch sử
  shell và kho mã, và xoay vòng theo lịch bạn kiểm soát.
- Xóa tài khoản sở hữu hoặc thu hồi một token sẽ chấm dứt quyền truy cập của nó ngay
  lập tức; không có gì được cache giữa các yêu cầu.
- Token không phải một phiên. Cài đặt tài khoản, đổi mật khẩu, quản lý phiên và xác
  thực hai yếu tố chỉ dùng được với phiên tương tác; hãy chỉ dùng token cho phần tự
  động hóa cần đến nó.
- Trang bảng điều khiển và API quản lý cùng một bộ token; bảng điều khiển là bề
  mặt hằng ngày, còn trang này là tài liệu tham chiếu cho tự động hóa.
