---
title: Phát hành và ký
---

# Phát hành và ký

Trang này dành cho người bảo trì. Nó trình bày cách một bản phát hành Gotham đã ký
được build và công bố, khóa ký nằm ở đâu, và cách thử chuỗi cài đặt mà không cần
GitHub.

## Bạn cần gì

- Quyền ghi vào repository và **environment `release`** của GitHub.
- Hai secret của environment bên dưới, do chủ sở hữu dự án cấu hình.
- Bộ quy tắc thẻ `v*` và những người xem lại bắt buộc giữ trong phạm vi chủ sở hữu
  dự án: cổng đó quyết định ai có thể tạo một bản phát hành đã ký.

## Cách một bản phát hành được build

Các bản phát hành được build và công bố bởi `.github/workflows/release.yml` trên
một thẻ `v*`, trên một runner do GitHub host. Công việc phát hành nhắm tới
**environment `release`** của GitHub (với một người xem lại bắt buộc), nên nó đọc
các secret của environment và chờ chủ sở hữu phê duyệt trước khi được dùng chúng:

1. Công việc thất bại an toàn trừ khi **cả hai** secret của environment được đặt
   và khác rỗng: `GOTHAM_UPDATE_PUBLIC_KEY` (khóa công khai Ed25519 thô dạng
   base64, được nhúng vào cả hai tệp nhị phân bằng `-ldflags`) và
   `GOTHAM_UPDATE_SIGNING_KEY` (khóa riêng Ed25519 PEM PKCS#8, chỉ dùng trên
   runner).
2. Nó cũng kiểm tra rằng khóa riêng tương ứng với `GOTHAM_UPDATE_PUBLIC_KEY` và
   với `deploy/gotham-signing-key.pub` đã commit (và do đó với khóa nhúng trong
   `deploy/install.sh`).
3. GoReleaser build `gotham` và `gotham-agent` cho `linux/amd64` và `linux/arm64`
   dưới dạng tệp nhị phân thô tên `gotham-linux-<arch>` /
   `gotham-agent-linux-<arch>` (những tên mà các bộ kiểm tra cập nhật phân giải),
   nhúng phiên bản và khóa công khai, và tạo một GitHub Release **nháp** với các
   tệp nhị phân, `checksums.txt` và khóa công khai đã công bố.
4. Workflow ký các manifest theo kiến trúc (`gotham-manifest-<arch>.txt` và
   `gotham-agent-manifest-<arch>.txt`) trên đúng các byte đã build. Nó định vị
   bản **nháp** qua danh sách phát hành (bản nháp không được
   `/releases/tags/<tag>` trả về), kiểm tra 6 asset gốc mà GoReleaser đã tải lên,
   tải lên 8 tệp manifest/chữ ký, rồi khẳng định đủ bộ 14 asset (4 tệp nhị phân,
   `checksums.txt`, khóa công khai và cả 8 tệp manifest/chữ ký) trước khi công bố
   bản nháp và xác minh nó không còn là bản nháp. Bản phát hành nháp bị bộ kiểm
   tra bỏ qua, nên một bản phát hành chưa đầy đủ không bao giờ được đề nghị.

Cách đặt tên asset là một hợp đồng với mã cập nhật — xem
`internal/updates/checker.go` (control plane), `internal/updates/agents.go`
(agent) và `updatecore.ManifestName` / `ManifestNameWithPrefix`.

## Runner phát hành

Công việc phát hành và mọi workflow của PR (`ci.yml`, `e2e.yml`, `ui-e2e.yml`)
chạy trên runner do GitHub host, nên mã `pull_request` không đáng tin không bao
giờ thực thi trên máy build và ký bản phát hành. Khóa ký nằm trong environment
`release`, và một bản build từ thẻ `v*` chỉ đọc nó sau khi người xem lại bắt buộc
phê duyệt lần chạy. Cổng người xem lại vì vậy điều khiển *ai có thể tạo một bản
phát hành đã ký*: hãy giữ bộ quy tắc thẻ `v*` và những người xem lại bắt buộc
trong phạm vi chủ sở hữu dự án.

## Cặp khóa

- **Khóa công khai** (`deploy/gotham-signing-key.pub`): an toàn để công bố. Nó
  được nhúng trong cả hai tệp nhị phân, trong `deploy/install.sh`, và được tải lên
  như một asset phát hành để người dùng so sánh được.
- **Khóa riêng** (secret của environment `GOTHAM_UPDATE_SIGNING_KEY`): bí mật duy
  nhất. Nó không bao giờ được commit hay in ra. **Hãy sao lưu khóa riêng ngoại
  tuyến** — nếu mất, không bản cài đặt hiện có nào xác minh được một bản phát hành
  mới và phải xoay vòng một cặp khóa mới vào (việc này đòi hỏi cài lại hoặc nhúng
  khóa mới trong một bản phát hành thủ công). Tạo nó bằng:

  ```sh
  go run ./cmd/signer keygen -out signing.key
  # put signing.key (PKCS#8 PEM) in the GOTHAM_UPDATE_SIGNING_KEY release environment secret
  # put signing.key.pub at deploy/gotham-signing-key.pub
  # put the base64 public key from the printed -ldflags line in
  # GOTHAM_UPDATE_PUBLIC_KEY and in deploy/install.sh
  # (GOTHAM_RELEASE_PUBLIC_KEY_B64) and install-agent.sh
  ```

## Thử chuỗi cài đặt mà không cần GitHub

`deploy/test-release-install.sh` build một bộ ký và các tệp nhị phân snapshot, ký
các manifest, phục vụ chúng từ một máy chủ phát hành giả trên loopback (kể cả một
lần chuyển hướng `/releases/latest`), và chạy `deploy/install.sh` với nó dưới một
`GOTHAM_INSTALL_ROOT` tạm. Nó bao phủ đường thẻ đã ghim, việc phân giải thẻ mới
nhất mặc định, họ agent, cài lại mà không có thêm gì từ người vận hành (bản hồi
quy B1), việc giữ nguyên khi cài lại (khóa của người vận hành và DSN được quản
lý), và các trường hợp thất bại an toàn (artifact bị sửa, manifest bị sửa, khóa
ghim không khớp):

```sh
sh deploy/test-release-install.sh
```

Hai biến của trình cài đặt chỉ tồn tại cho bộ thử này: `GOTHAM_INSTALL_ROOT` cài
dưới một tiền tố không phải `/` và bật chế độ test (không cần root), còn
`GOTHAM_INSTALL_TEST_PUBLIC_KEY` thay khóa phát hành đã ghim — nó chỉ được dùng
khi có `GOTHAM_INSTALL_ROOT`, nếu không trình cài đặt cảnh báo và bỏ qua. Không
bao giờ đặt một trong hai trên máy chủ thật.
