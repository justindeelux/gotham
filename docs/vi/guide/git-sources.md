---
title: Nguồn Git và SSRF
---

# Nguồn Git và chính sách host SSRF

Khi bạn triển khai một ứng dụng từ URL Git, control plane clone URL đó. Vì URL do
người vận hành cung cấp, mọi lần clone và mọi lần quay số của nút Kiểm tra kết
nối đều đi qua một chính sách host chặn địa chỉ nội bộ và địa chỉ metadata đám
mây.

## Chính sách làm gì

Mọi lần quay số đều đi qua chính sách host SSRF
(`internal/deploy/githostpolicy.go`): loopback, link-local (kể cả địa chỉ metadata
đám mây `169.254.169.254`), unspecified, multicast, carrier-grade NAT,
benchmarking, NAT64 và 6to4 luôn bị từ chối, cũng như các mẹo dùng IP dạng số
(`2130706433`, `0x7f.0.0.1`), userinfo và zone id IPv6.

RFC1918, ULA và loopback cũng bị từ chối, trừ khi bạn cho phép host riêng cho một
máy chủ git tự host:

```yaml
# gotham.yaml
deploy:
  git_allow_private_hosts: true
```

hoặc `GOTHAM_GIT_ALLOW_PRIVATE_HOSTS=true` trong môi trường của dịch vụ (`1` và
`true` đều được tính, giống bộ đọc tệp cấu hình). Link-local và các dải luôn bị
từ chối khác vẫn bị từ chối.

Lưu ý cách phân biệt tên/IP: TÊN `localhost` vẫn bị từ chối kể cả khi bật thiết
lập này (tên có thể bị gán lại), trong khi `127.0.0.1` tường minh được phép.

Host được phân giải hai lần và câu trả lời thay đổi (DNS rebinding) sẽ từ chối
việc quay số; các remote `https` được ghim vào địa chỉ đã phân giải
(`http.curloptResolve`), và mọi lần gọi git `http(s)` đều từ chối chuyển hướng
(`http.followRedirects=false`), nên một URL công khai không thể dội sang một URL
nội bộ (kho bị đổi tên sẽ thất bại an toàn — đó là đánh đổi). Lỗi trong log triển
khai chỉ mang gợi ý đã phân loại — không bao giờ chứa địa chỉ nội bộ đã phân giải.
Đường dẫn cục bộ và URL `file://` vẫn nằm sau `GOTHAM_DEV_CLONE_LOCAL` (chỉ dùng
cho fixture phát triển).

## Chính sách không bao gồm gì

- Remote `ssh` và `git://` không được ghim (git không có tùy chọn tương đương):
  hai lần phân giải được so sánh của chúng là toàn bộ biện pháp giảm thiểu, nên
  một câu trả lời fast-flux chạy đua đúng khoảng đó vẫn tới được lần quay số.
- Một proxy `http(s)` (`HTTP(S)_PROXY` trong môi trường của dịch vụ) bỏ qua việc
  ghim: curl phân giải và kết nối qua proxy, nên môi trường có proxy phải ghép
  chính sách này với các quy tắc proxy từ chối đích nội bộ.
- Việc ghim cần git >= 2.37 (git cũ hơn lặng lẽ bỏ qua `http.curloptResolve`).
  Control plane ghi một cảnh báo lúc khởi động nêu phiên bản đã cài khi nó cũ hơn
  2.37; hãy nâng cấp git để khôi phục việc ghim.

Xem [Cấu hình](/vi/reference/configuration#chinh-sach-nguon-git) cho khóa cấu hình.
