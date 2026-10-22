---
title: Dự án và môi trường
---

# Dự án và môi trường

Một dự án nhóm các ứng dụng, dịch vụ và cơ sở dữ liệu tạo nên một sản phẩm. Mỗi
dự án có một hoặc nhiều môi trường — `production`, `staging`, v.v. — và mọi tài
nguyên trong một môi trường đều gắn với một node.

## Tạo dự án

Mở **Dự án** và chọn **Dự án mới**. Đặt tên (1–64 ký tự, duy nhất trong nhóm) và
mô tả tùy chọn.

Một môi trường **production** được tạo cùng với dự án, nên dự án mới dùng được
ngay. Mở nó để xem các môi trường và số tài nguyên của từng môi trường.

## Đổi tên hoặc xóa dự án

Trên trang dự án, **Đổi tên** thay đổi tên và **Xóa dự án** gỡ nó. Dự án còn chứa
tài nguyên thì không thể xóa: hãy di chuyển hoặc xóa mọi tài nguyên trong mọi môi
trường trước. Xóa sẽ gỡ dự án, toàn bộ môi trường và biến dùng chung của nó, và
không thể hoàn tác.

## Môi trường

Trang dự án liệt kê từng môi trường kèm số lượng ứng dụng, dịch vụ và cơ sở dữ
liệu của nó. Từ đó bạn có thể:

- **Thêm môi trường** — tên phải duy nhất trong dự án (1–64 ký tự).
- **Mở** một môi trường.
- **Đổi tên** hoặc **Xóa** một môi trường. Môi trường còn chứa tài nguyên thì không
  thể xóa; hộp thoại sẽ nêu rõ cái gì đang cản trở.

Các môi trường độc lập với nhau: tài nguyên và biến ở cấp môi trường thuộc về một
môi trường và không được chia sẻ với các môi trường anh em.

Trang môi trường giữ dự án trong địa chỉ:
`/projects/{project}/environments/{environment}`. Trang hiển thị dự án và môi
trường trong breadcrumb và liệt kê mọi thứ đang chạy ở đó.

## Chuyển giữa các môi trường anh em

Khi một dự án có nhiều hơn một môi trường, trang môi trường hiển thị bộ chọn
**Môi trường**. Chọn một môi trường anh em sẽ đưa bạn tới cùng khung nhìn trong môi
trường đó; dự án giữ nguyên. Dùng nó để so sánh một bản phát hành giữa `production`
và `staging`, chẳng hạn.

## Tài nguyên trong một môi trường

Bảng tài nguyên có bốn tab: **Tất cả**, **Ứng dụng**, **Dịch vụ** và
**Cơ sở dữ liệu**. Mỗi tab hiển thị số lượng bên cạnh tên.

Bảng có bốn cột: **Tên**, **Loại**, **Máy chủ** và **Trạng thái**. Chọn một tên để
mở tài nguyên. Ô tìm kiếm khớp với tên tài nguyên, dòng chi tiết hoặc node của nó.

Trạng thái được báo theo từng tài nguyên: `running`, `deploying`, `not deployed`,
`creating`, `failed`, `error` hoặc `unknown`. Với ứng dụng, nó phản ánh lần triển
khai mới nhất; các bản triển khai xem trước xuất hiện kèm nhãn **Xem trước**.

**Hiện bản xem trước** liệt kê các bản triển khai xem trước theo pull request trong
bảng, lồng dưới ứng dụng mà chúng thuộc về. Bản xem trước chạy trong cùng môi
trường và trên cùng node với ứng dụng gốc.

## Thêm tài nguyên

**Thêm tài nguyên** mở bộ chọn với ba tab:

- **Ứng dụng** — chọn một nguồn (git công khai hoặc riêng, tài khoản GitHub hoặc
  GitLab đã kết nối, một Dockerfile, một tài liệu compose hoặc một container
  image) rồi đi tiếp vào wizard ứng dụng.
- **Dịch vụ** — chọn một template từ danh mục, hoặc nhập một tệp Docker Compose.
- **Cơ sở dữ liệu** — chọn một engine để cấp phát.

Bộ chọn đã biết dự án và môi trường mà bạn mở nó từ đó, và phần còn lại của wizard
giữ nguyên phạm vi đó. **Thêm tài nguyên** là thao tác ghi: nó không hiện với tài
khoản `read_only`.

Xem [Ứng dụng](/vi/using/applications),
[Dịch vụ và template](/vi/using/services-templates) và
[Cơ sở dữ liệu](/vi/using/databases) để biết điều gì xảy ra sau khi bạn chọn một
loại.

## Biến dùng chung

Biến có thể được định nghĩa ở hai phạm vi dùng chung và một phạm vi ứng dụng:

- **Dự án → Biến dùng chung** áp dụng cho mọi tài nguyên trong mọi môi trường của
  dự án.
- **Môi trường → Biến** (nút trong phần đầu trang, hoặc mục bên dưới bảng tài
  nguyên) áp dụng cho mọi tài nguyên trong môi trường đó. Các dòng của dự án được
  hiển thị chỉ đọc phía trên trình soạn thảo để tham khảo.
- **Ứng dụng → Biến môi trường** chỉ áp dụng cho một ứng dụng.

Giá trị cụ thể nhất thắng: ứng dụng đè lên môi trường, và môi trường đè lên dự án.
Quy tắc này được hiển thị trong từng trình soạn thảo.

Một biến có thể được đánh dấu **bí mật**. Giá trị bí mật là chỉ ghi: nó được lưu
niêm phong, API không bao giờ trả về, và lưu một dòng bí mật không đổi sẽ giữ
nguyên giá trị đã lưu. **Lưu** thay thế toàn bộ tập hợp, nên gỡ một dòng là gỡ biến
đó khỏi phạm vi.

## Vai trò và quyền

`owner` và `admin` có thể tạo, đổi tên, xóa và sửa dự án, môi trường, tài nguyên và
biến dùng chung. `read_only` mở và đọc được mọi thứ nhưng không bao giờ ghi.

API là điểm thực thi: một thao tác ghi từ tài khoản `read_only` bị từ chối phía máy
chủ ngay cả khi điều khiển đó vẫn hiện trên bảng điều khiển. Không có vai trò
`member`.
