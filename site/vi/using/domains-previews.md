---
title: Tên miền và bản xem trước
---

# Tên miền và bản xem trước

Gắn hostname vào một ứng dụng, ghi lại chứng chỉ mà nó sẽ nhận, kết nối một nguồn
Git, và để Gotham dựng một bản sao dùng một lần của ứng dụng cho mỗi pull request.
Việc cấp chứng chỉ và các kết nối Git do control plane xử lý; node kết thúc lưu
lượng và thực hiện ACME.

## Gắn tên miền

Mở một ứng dụng và dùng tab **Tên miền** của nó. **Thêm tên miền** hỏi:

- **Tên miền** — một tên DNS như `app.example.com`.
- **Cổng** — cổng container mà ứng dụng lắng nghe (1–65535).
- **Giao thức** — `http` hoặc `https`.
- **Service** — chỉ với ứng dụng compose, để định tuyến tới một compose service
  thay vì dịch vụ của bản phát hành. Chọn một service sẽ điền sẵn cổng mà nó khai
  báo, và cổng đó vẫn sửa được.

Tên miền đầu tiên được gắn vào một ứng dụng trở thành tên miền **chính** của nó.
Dùng **Đặt làm chính** trên một dòng khác để đổi; tên miền chính là thứ các bề mặt
hiển thị và việc cấp phát bản xem trước dùng đến. Gỡ tên miền chính sẽ nâng tên
miền đang bật còn lại lâu nhất, nên một ứng dụng đã có tên miền luôn giữ được một
tên.

Một hostname được nhận trên toàn nền tảng: một host đã gắn vào ứng dụng hoặc dịch
vụ khác, hoặc đang được dùng làm nguồn chuyển hướng, sẽ bị từ chối. Hãy trỏ bản ghi
A/AAAA của host vào node đang chạy ứng dụng trước khi dựa vào tuyến; lần triển khai
đầu tiên của một ứng dụng mới bắt đầu không có tuyến cho đến khi một tên miền được
gắn.

Đổi tên một host sẽ gỡ bản ghi chứng chỉ tự động của hostname cũ, nên hãy ghi chứng
chỉ cho host mới sau đó. Gỡ một tên miền chỉ gỡ các tuyến của host đó.

## Chứng chỉ

Chứng chỉ được cấu hình theo từng tên miền — một cấu hình cho mỗi tên miền — từ
cùng tab **Tên miền**. Control plane lưu ý định; node thực hiện đơn ACME và giữ
chứng chỉ trong kho của chính nó. Xóa một cấu hình sẽ quay về HTTP thường cho host
đó.

Mỗi cấu hình có một thử thách:

| Thử thách | Dùng khi nào | Yêu cầu |
|---|---|---|
| `http-01` | Mặc định. | Host phân giải trực tiếp về node và cổng 80 với tới được từ internet. Một bản ghi Cloudflare có proxy phải là **DNS only**. |
| `dns-01` | Host không phục vụ được cổng 80, hoặc bạn cần wildcard. | Một nhà cung cấp DNS có vùng đã cấu hình bao phủ tên miền. Chứng chỉ wildcard chỉ có với `dns-01`. |

Gắn một tên miền `https` sẽ tự động ghi một ý định `http-01` mặc định cho host đó.
Bạn có thể sửa nó, chuyển sang `dns-01`, hoặc tắt nó. Một cấu hình `dns-01` phải nêu
tên một nhà cung cấp, và nhà cung cấp đó phải giữ một vùng bao phủ tên miền (và cả
phần gốc của wildcard, khi yêu cầu wildcard). Nameserver của vùng phải thực sự được
ủy quyền cho nhà cung cấp đó, nếu không đơn sẽ thất bại.

**Thông tin đăng nhập của nhà cung cấp DNS chỉ dành cho người vận hành.** Chúng được
thêm, xoay vòng và xóa trên trang Tên miền & SSL của người vận hành, và API không
bao giờ trả về thông tin đăng nhập đã lưu. Xem
[Proxy và DNS](/vi/operating/proxy-dns).

Trạng thái hiển thị cạnh một cấu hình được quan sát trực tiếp từ node, không bao giờ
được lưu: `present` nghĩa là node có chứng chỉ bao phủ tên miền và thời điểm hết hạn
của nó được hiển thị, `absent` nghĩa là đã đọc kho lưu trữ và không có chứng chỉ nào,
và `unknown` nghĩa là không đọc được node. `unknown` không phải một đơn thất bại —
nó nghĩa là control plane không có quan sát nào để báo.

### Chuyển hướng

Các quy tắc chuyển hướng (một host nguồn chính xác tới một host đích chính xác, với
mã vĩnh viễn hoặc tạm thời và tùy chọn giữ đường dẫn) được quản lý trên trang Tên
miền & SSL của người vận hành; xem [Proxy và DNS](/vi/operating/proxy-dns).

## Nguồn Git

Mở **Cài đặt → Nguồn Git** (`/settings/git-sources`). Một kết nối thuộc về tài
khoản của bạn, không thuộc về một nhóm: vai trò trong nhóm không bảo vệ nó, phạm vi
của API token mới bảo vệ. Bí mật — client secret, admin token và webhook secret —
là chỉ ghi và không bao giờ hiển thị sau khi lưu.

- **GitHub App** — **Kết nối GitHub** gửi một manifest ứng dụng tới GitHub dưới tên
  bạn đặt (chữ cái, chữ số và dấu gạch ngang) và chuyển bạn trở lại đây. Sau đó cài
  ứng dụng trên các kho mà nó cần thấy.
- **GitLab** — nhập URL của instance (`gitlab.com` hoặc một instance tự host). Việc
  cấp phát tự động gọi instance một lần bằng admin token dùng một lần và không bao
  giờ được lưu; cách khác, dùng các trường thủ công với application ID, secret và
  đúng redirect URI hiển thị trong hộp thoại. Sau khi lưu, dùng **Kết nối lại** để
  hoàn tất đăng nhập.
- **Gitea** — được lớp nhà cung cấp hỗ trợ và cấu hình bằng URL instance cùng ứng
  dụng OAuth của nó. Bảng điều khiển không có luồng kết nối Gitea: một kết nối Gitea
  được tạo qua API và dòng của nó được liệt kê chỉ đọc. Các ứng dụng đã triển khai
  qua nó vẫn tiếp tục hoạt động.

Danh sách hiển thị trạng thái, tài khoản hoặc instance, số bản cài đặt và số kho của
mỗi kết nối, và có bao nhiêu ứng dụng trong nhóm đang hoạt động nêu tên nó. Wizard
ứng dụng liệt kê các kho mà một kết nối thấy được và, sau khi chọn một kho, các
nhánh của nó; nhánh bạn chọn là nhánh mà các lần push sẽ triển khai từ đó.

Việc ngắt kết nối một kết nối GitLab bị từ chối khi các ứng dụng trong nhóm hiện
tại vẫn triển khai qua nó, và lỗi sẽ nêu tên chúng. Một GitHub App luôn ngắt kết nối
được: Gotham báo có bao nhiêu ứng dụng đã nêu tên nó, và các ứng dụng đó phải được
trỏ sang nguồn khác trước lần triển khai tiếp theo.

## Bản xem trước

Một bản xem trước là một ứng dụng anh em được dựng từ nhánh đầu của một pull
request. Nó cho phép bạn xem lại một thay đổi trên URL riêng của nó mà không đụng
tới việc triển khai của ứng dụng gốc. Bản xem trước cần ba thứ: một ứng dụng đã cài
webhook Git, một nhánh cần theo dõi, và một tên miền chính. Gotham tự cài webhook khi
bạn tạo ứng dụng từ một nguồn đã kết nối — không có bước cài webhook thủ công nào
trên bảng điều khiển.

### Gửi và xác minh

Git host gửi các sự kiện `pull_request` tới endpoint webhook theo từng nhà cung cấp
của Gotham. Mọi lần gửi đều được xác minh trước khi bất cứ thứ gì được build:

- GitHub ký phần thân bằng HMAC SHA-256 trong `X-Hub-Signature-256`.
- GitLab gửi bí mật chia sẻ trong `X-Gitlab-Token`.
- Gitea ký phần thân bằng HMAC SHA-256 trong `X-Gitea-Signature`.

Việc so sánh là thời gian hằng đối với bí mật được tạo khi hook được cài; một lần gửi
không xác minh được sẽ bị từ chối và không bao giờ tới dịch vụ triển khai. Các lần
gửi còn bị giới hạn tốc độ theo IP của client, và một lần gửi phát lại cho cùng
nhánh đầu của pull request được coi là trùng lặp thay vì khởi động một lần build thứ
hai.

### Vòng đời

Một bản xem trước được tạo khi một pull request được mở hoặc mở lại **nhắm vào nhánh
mà ứng dụng theo dõi**, và được làm mới khi có commit mới được đẩy lên đó. Đóng hoặc
merge pull request sẽ gỡ bản xem trước; dòng ràng buộc vẫn ở lại như một bản ghi
kiểm toán. Mọi thứ khác đều bị bỏ qua: pull request từ fork (mã của chúng không phải
kho mà ứng dụng tin tưởng), các nhánh gốc khác, và các thao tác chỉ đổi metadata như
nhãn hay review.

Mỗi bản xem trước là một ứng dụng anh em có tên `<application>-pr-<number>` với host
`http://pr-<number>-<slug>.<primary-domain>`. Nó chỉ dùng HTTP: ứng dụng anh em không
nhận chứng chỉ riêng. Nó mang biến môi trường thường và khóa triển khai dùng chung
của ứng dụng gốc, nhưng cố ý không mang bí mật đã niêm phong hay volume của ứng dụng
đó.

Một ứng dụng có tối đa năm bản xem trước đang sống. Pull request thứ sáu được ghi
nhận và bỏ qua cho đến khi một trong năm bản đóng. Khi pull request đã có bản xem
trước, Gotham đăng một bình luận cố gắng hết sức lên đó với URL và kết quả triển
khai mới nhất.

### Tab Bản xem trước

Tab **Bản xem trước** trên trang ứng dụng liệt kê các ràng buộc bản xem trước của ứng
dụng, mới nhất trước: pull request, nhánh đầu, URL bản xem trước, trạng thái, commit
đầu, và thời điểm tạo/xóa. Các trạng thái là `active`, `deploying`, `closing`,
`failed` và `deleted`.

Tab này hiển thị trên trang ứng dụng khi bản xem trước được bật trên instance, kể cả
khi ứng dụng chưa có bản xem trước nào. Với `FEATURE_PREVIEWS=false` trên control
plane, không có tuyến bản xem trước nào được gắn: tab bị ẩn thay vì báo lỗi, và các
lần gửi `pull_request` bị bỏ qua. Triển khai khi push không bị ảnh hưởng bởi cờ đó —
các lần push lên nhánh được theo dõi vẫn xếp hàng triển khai.
