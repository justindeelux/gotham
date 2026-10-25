---
title: Dịch vụ tệp từ xa
---

# Dịch vụ tệp từ xa

Agent của node có thể mở một trình duyệt và trình sửa tệp cho bảng điều khiển, để
chủ sở hữu đọc, tải lên, sửa, nén và giải nén tệp trên một node mà
không cần SSH.

## Bạn cần gì

- Agent của node đang chạy trên node (xem [Thêm node](/vi/guide/nodes)).
- API tệp của control plane đang bật, đây là mặc định (`FEATURE_FILES=false` sẽ
  tắt nó).
- Tài khoản chủ sở hữu, hoặc API token của chủ sở hữu có phạm vi `admin`.

Dịch vụ **bật mặc định** và, ở chế độ mặc định, phơi toàn bộ hệ thống tệp. Hãy
đọc phần đầu trước khi triển khai rộng.

## Chế độ toàn bộ hệ thống tệp (mặc định)

Dịch vụ tệp bật trừ khi `GOTHAM_AGENT_FILES=false`, và mặc định nó phơi một gốc
ghi được tên `system` chính là `/`: mọi tệp mà tiến trình agent với tới được,
không loại trừ đường dẫn nguy hiểm hay được bảo vệ nào (kể cả khóa và chứng chỉ
của chính agent). Chủ sở hữu đã chấp nhận rủi ro này; chỉ chủ sở hữu
mới dùng được nó.

Những gì agent thực sự đọc hay thay đổi được bị giới hạn bởi người dùng Unix của
nó. Vì vậy trình cài đặt hỏi bạn có muốn trình quản lý tệp không (mặc định
**không**) và, nếu có, chạy agent dưới root qua drop-in `file-manager.conf`. Khi
chạy dưới người dùng `gotham-agent` không đặc quyền, cùng gốc đó gần như chỉ đọc.
Các node hiện có tự cập nhật lên phiên bản này sẽ phơi `/` cho chủ sở hữu với
quyền của người dùng hiện tại cho đến khi `GOTHAM_AGENT_FILES=false` được đặt.

Trong chế độ này, rào chắn thư mục ứng dụng vẫn bảo vệ các thư mục ứng dụng cấp
cao nhất của gốc volume được quản lý (không đổi tên/xóa khi một container đang gắn
chúng). `os.Root` từ chối đi theo một symlink *tuyệt đối* ngay cả bên trong `/`,
nên một đường dẫn qua nó (ví dụ `/var/run`) không thể đi vào qua liên kết; hãy
dùng đường dẫn thật. Trả lời không sau khi đã trả lời có sẽ chạy
`chown -R gotham-agent` trên `/var/lib/gotham-agent` để chứng chỉ và khóa mà agent
chạy dưới root đã ghi lại thuộc về người dùng dịch vụ.

`GOTHAM_AGENT_FILES` và `GOTHAM_AGENT_FILE_FULL_ACCESS` bị tắt bởi `false`, `0`,
`no` hoặc `off`; mọi giá trị khác đều để chúng bật.

## Chế độ bị giới hạn

Với `GOTHAM_AGENT_FILE_FULL_ACCESS=false`, các gốc là một danh sách cho phép cục
bộ của agent:

- `GOTHAM_AGENT_MANAGED_VOLUME_ROOT` — gốc ghi được, chứa các bind mount của ứng
  dụng.
- `GOTHAM_AGENT_COMPOSE_ROOT` — cây compose, duyệt được ở chế độ chỉ đọc (lần
  `ComposeUp` tiếp theo sẽ ghi đè các chỉnh sửa).
- `GOTHAM_AGENT_FILE_ROOTS` — các gốc của người vận hành phân tách bằng dấu hai
  chấm, chỉ đọc trong giai đoạn này.

Mọi đường dẫn được phân giải và kiểm tra lúc khởi động. Một gốc không tuyệt đối,
phân giải vào `/etc /proc /sys /dev /run /boot /usr /home /root`, vào gốc dữ liệu
của Docker (`data-root` từ `/etc/docker/daemon.json`, nếu không thì
`/var/lib/docker`) hoặc vào trạng thái riêng của agent sẽ bị từ chối khởi động;
một gốc chồng lấn gốc khác cũng vậy. Một gốc dựng sẵn chưa tồn tại (Docker và dịch
vụ compose tạo chúng một cách lười) sẽ bị bỏ qua kèm cảnh báo thay vì hủy agent.

Bản thân việc giới hạn là `os.OpenRoot`: mọi truy cập được phân giải tương đối với
một descriptor thư mục đang giữ, nên một thành phần `..`, một đường dẫn tuyệt đối
hoặc một symlink rời khỏi cây đều bị từ chối bởi bước duyệt descriptor. Dịch vụ
không được phơi ra khi chưa cấu hình CA của agent, vì việc giới hạn không phải xác
thực. Danh tính của mỗi gốc được lấy từ descriptor đã mở, nên một thành phần đường
dẫn bị hoán đổi trong lúc xác thực các gốc sẽ khiến khởi động bị từ chối thay vì
đăng ký thư mục bị hoán đổi.

Một lần ghi giữ nguyên inode khi người dùng agent không sở hữu tệp hoặc tệp có
nhiều hơn một hard link: ghi đè tại chỗ giữ được chủ sở hữu, mode và mount thay vì
thay bằng inode mới. Vì vậy một hard link có tên khác nằm ngoài gốc cũng chia sẻ
lần ghi đó, nên sửa tệp qua gốc cũng đổi tên bên ngoài. Đây là rủi ro được chấp
nhận: tạo liên kết đó vốn đã cần quyền ghi vào đích, các gốc do người vận hành
kiểm soát, và một tệp một liên kết thuộc người dùng agent vẫn được thay thế nguyên
tử bằng rename. Hãy đổi tên hoặc xóa mục để phá liên kết nếu tên bên ngoài không
được đổi.

`List` trả về tối đa 5.000 mục mỗi trang, sắp theo tên. Một trang có nhiều mục hơn
một phản hồi mang được sẽ báo `truncated=true` và một `cursor` base64 không trong
suốt; hãy truyền cursor trở lại để tiếp tục, và tiếp tục phân trang cho đến khi
`truncated=false` — một trang bị cắt luôn mang cursor khác rỗng và tiến về phía
trước, nên client không bao giờ lặp vô hạn. Cursor là base64 vì một tên ở ranh
giới có thể chứa byte không phải UTF-8 hợp lệ (hợp lệ trên Linux), thứ mà một
chuỗi proto3 thô không mang được; `name` hiển thị của một mục như vậy được làm
sạch thành U+FFFD và do đó không thể đưa lại vào `Stat`/`Read`, nhưng cursor vẫn
trỏ tới các byte thật. Một cursor sai định dạng bị từ chối với `InvalidArgument`.
Một lần `List` quét tối đa 100.000 mục thư mục và chỉ giữ trang trong bộ nhớ; một
thư mục lớn hơn thế thất bại với `ResourceExhausted` ("directory too large") thay
vì trả về một danh sách thiếu mà không báo.

## Archive (nén / giải nén)

`Compress` và `Extract` tạo và giải nén `zip`, `tar` và `tar.gz` **không dùng
shell** (chỉ `archive/zip`, `archive/tar`, `compress/gzip`; không có `os/exec` ở
bất kỳ đâu). Compress bỏ qua symlink và tệp đặc biệt (không bao giờ đi theo liên
kết) và đếm chúng trong thông điệp tiến trình cuối (thứ mà control plane không
hiển thị lên trình duyệt — xem bên dưới); archive được dàn dựng trong một tệp tạm
và đổi tên vào vị trí, nên một lần nén thất bại hoặc bị hủy không bao giờ để lại
archive dở dang.

Extract từ chối các mục symlink, hard-link, device và FIFO, gỡ các bit
setuid/setgid/sticky, và ghi mọi mục qua `os.Root`, nên một tên tuyệt đối, một
đoạn `..` hay một thư mục cha là symlink đều không thể thoát khỏi gốc đích. Một
tệp đã tồn tại bị từ chối trừ khi bên gọi đặt `overwrite`; một thư mục đã tồn tại
được dùng lại, không bao giờ bị gỡ. Mỗi tệp được ghi vào một tệp tạm cùng thư mục
rồi đổi tên đè lên đích, nên một lần ghi đè thất bại không bao giờ cắt cụt tệp
hiện có. Cả hai đều bảo vệ thư mục ứng dụng đang chạy (một archive không thể thay
một thư mục ứng dụng cấp cao nhất của gốc volume được quản lý), từ chối gốc chỉ
đọc, và tuân theo phần dự trữ dung lượng trống cùng ngân sách truyền của đường
ghi.

**Extract không có tính giao dịch.** Một lỗi giữa chừng để các mục đã giải nén
nằm nguyên tại chỗ (chỉ tệp đang ghi được dàn dựng và gỡ), nên bên gọi phải coi
một lần giải nén thất bại là kết quả một phần và dọn dẹp nếu cần.

Bốn giới hạn bảo vệ một archive: **kích thước trên đĩa của tệp archive** mà
extract sẽ đọc (`GOTHAM_AGENT_ARCHIVE_MAX_INPUT_BYTES`, mặc định 1 GiB, được kiểm
tra trước khi bất kỳ bộ phân tích nào chạy và cho mọi định dạng); tổng số byte
chưa nén (`GOTHAM_AGENT_ARCHIVE_MAX_BYTES`, mặc định 4 GiB); số mục
(`GOTHAM_AGENT_ARCHIVE_MAX_ENTRIES`, mặc định 100000); và một tỉ lệ nén **cố
định 200:1** so với kích thước của chính archive (không cấu hình được qua biến
môi trường). Các mục thư mục cũng tính vào giới hạn số mục. Với zip, bản ghi
end-of-central-directory (và locator/record zip64 khi có) được đọc từ phần đuôi
tệp trước khi `zip.NewReader` chạy, và archive bị từ chối khi số mục khai báo vượt
`GOTHAM_AGENT_ARCHIVE_MAX_ENTRIES` hoặc kích thước central-directory khai báo vượt
64 MiB — nên một archive được dàn dựng khai báo hàng triệu mục bị từ chối trước
khi bất kỳ header nào được cấp phát. Một tên mục sâu hơn 64 đoạn bị từ chối. Một
archive vượt giới hạn bị từ chối trước khi nó làm đầy đĩa. Mọi giới hạn được đọc
dưới dạng số nguyên dương; một giá trị không phân tích được hoặc không dương lặng
lẽ quay về mặc định (giống các giới hạn `GOTHAM_AGENT_FILE_*` khác), nên một lỗi
gõ không tắt được giới hạn.

Số mục bị bỏ qua mà một lần nén phát ra **không được hiển thị lên trình duyệt**:
agent báo nó trong `Progress.skipped` cuối của luồng, nhưng control plane rút cạn
luồng và chỉ trả về archive đã tạo, nên giao diện không hiển thị được.

Một giới hạn archive được trả lời bằng **413** với mã body riêng `archive_limit`,
một mục archive bị từ chối (liên kết, device, FIFO hoặc loại không được hỗ trợ)
bằng **409** với mã `archive_entry`, và một body archive bị cắt hoặc hỏng bằng
**400** với mã `invalid_archive`, nên giao diện hiển thị thông báo riêng cho
archive thay vì "file is too large to edit/upload", "conflict" hay một lỗi nội bộ.

Một yêu cầu archive **mở suốt toàn bộ quá trình truyền**: control plane giữ kết
nối đến hết ngân sách truyền của nó, và trình duyệt tự tắt timeout phía client,
nhưng một reverse proxy đặt trước control plane phải cho phép một phản hồi sống
lâu, nếu không yêu cầu sẽ bị cắt trong khi agent vẫn đang làm việc. Traefik ở biên
của node không đứng trước API tệp, nên các timeout của nó không áp dụng.

## Thùng rác và khôi phục

Xóa một mục sẽ chuyển nó vào thùng rác của gốc (`.gotham-trash`) thay vì xóa
hẳn. Thùng rác bị ẩn khỏi danh sách, tìm kiếm và kích thước thư mục và không
thể truy cập bằng các thao tác tệp thông thường. Thùng rác có hơn 5.000 mục chỉ
liệt kê các mục mới nhất và báo `truncated=true`; các mục cũ nhất bị lược bỏ,
không bao giờ âm thầm.

**Khôi phục** đưa một mục về đường dẫn gốc đã ghi, hoặc tới đích do bên gọi
cung cấp. Các thư mục cha bị thiếu được tạo lại, dùng mode đã ghi lúc mục vào
thùng rác và `0755` cho thư mục cha chưa từng thấy; một thư mục cha đang là tệp
hoặc tệp đặc biệt vẫn bị từ chối thay vì bị thay thế. Đích đã tồn tại trả `409`
trừ khi chọn ghi đè. **Xóa vĩnh viễn** xóa một mục và **Làm trống thùng rác**
xóa mọi mục trong thùng rác của gốc đó; không thể hoàn tác cả hai.

### Hết hạn lưu giữ

Agent tự hết hạn thùng rác của từng gốc ghi được, lúc khởi động rồi mỗi giờ.
Cả hai giới hạn đều tính theo từng gốc, và giá trị 0 tắt giới hạn đó:

| Biến | Mặc định | Tác dụng |
|---|---|---|
| `GOTHAM_AGENT_FILE_TRASH_MAX_AGE_DAYS` | `30` | Xóa các mục đã bị xóa quá số ngày này. `0` giữ mọi mục, đúng hành vi trước khi có thiết lập này. Giá trị trên 36500 (100 năm) bị chặn để phép tính mốc thời gian không tràn. |
| `GOTHAM_AGENT_FILE_TRASH_MAX_BYTES` | `0` | Giới hạn số byte thùng rác của một gốc được giữ. Khi vượt, các mục cũ nhất bị xóa cho tới khi thùng rác vừa giới hạn. `0` là không giới hạn. Có mức trần kích thước thì lần quét phải duyệt từng cây trong thùng rác để tính dung lượng, tốn hơn trên thùng rác lớn. |

**Cảnh báo nâng cấp.** Trước bản phát hành này, thùng rác không bao giờ hết hạn.
Mặc định 30 ngày áp dụng cho cả các mục đã có trong thùng rác, nên lần khởi động
đầu tiên sau khi nâng cấp sẽ xóa vĩnh viễn mọi thứ đã bị xóa quá 30 ngày. Đặt
`GOTHAM_AGENT_FILE_TRASH_MAX_AGE_DAYS=0` trước khi nâng cấp để giữ các mục đó.

Lần quét theo kích thước luôn bỏ qua mục được xóa trong vòng một giờ qua, nên
một lần xóa lớn vẫn khôi phục được trong thời gian ngắn kể cả khi riêng nó đã
vượt mức trần; nó sẽ bị xóa ở lần quét sau.

Lần quét chỉ xóa các id mục hợp lệ nằm dưới `.gotham-trash`, và ghi log mỗi lần
xóa với gốc, id, đường dẫn gốc, số byte và lý do (`age` hoặc `size`). Nó không
bao giờ chạm vào thứ khác trong gốc, kể cả đích của một liên kết tượng trưng đã
vào thùng rác. Nó khóa từng mục một, nên một lần xóa lâu không chặn phần còn
lại của thùng rác, nhưng xóa một cây rất lớn (ví dụ cây con được sao chép ra từ
bind mount khác thiết bị) giữ khóa đường dẫn của mục đó tới khi bước duyệt xong
và sẽ được thử lại ở lần quét sau nếu thất bại giữa chừng.

### Thứ tự triển khai

Nâng cấp control plane (hoặc bật trình quản lý tệp trong console) **trước** node
agent:

- Agent mới sau control plane cũ vẫn đưa các mục đã xóa vào thùng rác, nhưng
  control plane cũ không có tuyến thùng rác, nên các mục này vô hình cho tới khi
  nâng cấp. Việc hết hạn lưu giữ vẫn xóa chúng.
- Agent cũ sau control plane mới trả **501** cho các tuyến thùng rác; bên gọi báo
  node agent đã lỗi thời.

## Phía control plane

Các tuyến `/api/v1/servers/{id}/files` được gắn mặc định; đặt `FEATURE_FILES=false`
để tắt chúng (khi đó chúng trả **503**). Toàn bộ bề mặt bị giới hạn cho tài khoản
chủ sở hữu (API token của chủ sở hữu cần phạm vi `admin`): mọi tuyến tệp, trừ bản
tải có cổng vé, đều nằm sau cổng đó, nên token của thành viên, hoặc token chỉ có
phạm vi read hay deploy, nhận **404** hoặc **403**, vì API phơi hệ thống tệp của
node.

Bên trong cổng chỉ chủ sở hữu dùng được API tệp, kể cả đọc và mọi thao tác thay
đổi. Body của tệp có thể chứa bí mật (một `.env` compose `0600`).
`GET /files/download` có cổng vé nằm ngoài nhóm xác thực, nên vé dùng một lần chính
là thông tin đăng nhập của nó.

| Biến | Mặc định | Mục đích |
|---|---|---|
| `FEATURE_FILES` | bật | Đặt `false` để tắt API tệp (503 khi tắt). |
| `FILES_MAX_UPLOAD_BYTES` | `1073741824` (1 GiB) | Mức trần cho một body ghi/tải lên (413 khi vượt); nên khớp với `GOTHAM_AGENT_FILE_MAX_UPLOAD` của agent. |
| `FILES_SPOOL_DIR` | `<tmpdir>/gotham-spool` (0700) | Thư mục dàn dựng một body tải lên dạng luồng trước khi nó tới agent. |
| `FILES_SPOOL_CONCURRENCY` | `4` | Số body tải lên được dàn dựng cùng lúc tối đa; vượt quá thì một lần tải lên trả 429. |

Một body dạng luồng được dàn dựng trên control plane (header ghi của agent phải
khai báo kích thước chính xác, thứ mà một phần multipart không mang). Spool được
quét lúc khởi động và mỗi giờ, và dung lượng trống của nó được kiểm tra với mức
trần trước mọi lần ghi. API tệp do control plane phục vụ trực tiếp, nên nó
**không** đi qua Traefik của node.

`FILES_SPOOL_CONCURRENCY` là giới hạn dàn dựng duy nhất; một lần tải lên nữa khi
mọi slot đang bị giữ sẽ trả 429.

## Quyền (chmod)

`POST /files/chmod` (chỉ chủ sở hữu; API token cần scope admin) đổi bit quyền
của một mục. Body là `{root, path, mode}` và **`mode` là bắt buộc**: trường bị
thiếu bị từ chối **400** thay vì lặng lẽ áp dụng `000`. Chỉ các bit thấp `0o777`
được chấp nhận: một yêu cầu mang setuid (`0o4000`), setgid (`0o2000`), sticky
(`0o1000`) hoặc bất kỳ bit cao hơn bị từ chối **400** trước khi agent được gọi.

Một symlink bị từ chối **400** (nó không bao giờ đi theo liên kết để đổi đích của
nó), một tệp đặc biệt (FIFO/socket/device) **409**, và một gốc chỉ đọc **403**.
Agent cũng từ chối một thư mục sẽ mất bit execute của chủ sở hữu (nó phải còn đi
qua được) và một tệp thường có hard-link trong một gốc bị giới hạn (inode của nó
có thể với tới được từ ngoài gốc); gốc toàn bộ hệ thống tệp không có "bên ngoài",
nên việc kiểm tra hard-link ở đó không cần thiết.

Các bit setuid/setgid/sticky hiện có được **giữ nguyên, không sửa được**: chỉ các
bit quyền thường thay đổi và một bit đặc biệt không bao giờ được tạo. Một agent cũ
trước RPC này trả lời **501**. chmod không bao giờ đổi loại của một mục, nên rào
chắn thư mục ứng dụng (chỉ cho đổi tên/xóa) không áp dụng; đệ quy không được hỗ
trợ.

Việc giới hạn mở gốc và mọi thư mục cha bằng `O_PATH` (cần quyền tìm, không cần
đọc), `openat` mục với `O_NOFOLLOW`, `fstat` descriptor để phân loại, và chmod
đúng inode đó — qua `fchmodat2(AT_EMPTY_PATH)` ở nơi kernel hỗ trợ (Linux 6.6+) và
qua liên kết ma thuật `/proc/self/fd` ở nơi khác. Không gì bên ngoài gốc có thể
thay đổi, một liên kết bị hoán đổi vào không thể bị đi theo, và một mục mà agent
vừa làm thành không đọc được (`000` hoặc `0o111`) vẫn cứu được qua giao diện, kể
cả bởi một agent không chạy root.

## Mô hình quyền

Agent chạy dưới người dùng không đặc quyền `gotham-agent` và không có
`CAP_DAC_OVERRIDE`, nên **ai sở hữu một tệp quyết định trình quản lý tệp làm được
gì**:

- Gốc `volumes` (mặc định `/var/lib/gotham/volumes`) là gốc ghi được, nhưng Docker
  tạo mỗi thư mục bind của ứng dụng với `root:root 0755`. Agent liệt kê được nó và
  đọc được các tệp ai cũng đọc được, nhưng **không tạo, đổi tên, xóa hay sửa**
  được bất cứ thứ gì một container đã ghi; một tệp `0600` là không đọc được. Đây
  là trải nghiệm chủ yếu đọc có chủ đích, với trạng thái "permission denied" theo
  từng mục.
- Gốc `compose` và `GOTHAM_AGENT_FILE_ROOTS` là **chỉ đọc** trong dịch vụ dù hệ
  thống tệp có thể cho phép ghi (một chỉnh sửa compose sẽ bị lần `ComposeUp` tiếp
  theo ghi đè).

Để container sửa được, việc cấp phát phải làm cho `<root>/<appID>` ghi được bởi
agent, bằng **hoặc** control plane chown nó cho `gotham-agent`, **hoặc** một nhóm
dùng chung cộng thư mục ứng dụng có setgid (`2775`) (agent cũng phải thuộc nhóm
đó). Đây là một **quyết định của chủ sở hữu** cần có trước khi bật một trong hai
cờ cho các node như vậy; chỉ setgid là không đủ cho đến khi agent tham gia nhóm.
