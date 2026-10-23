---
title: Tham chiếu
---

# Tham chiếu

Tài liệu tra cứu cho quản trị viên đã biết mình muốn đổi gì: các lệnh trên control
plane và trên máy chủ node, mọi khóa cấu hình và biến môi trường, và mô hình API
token.

- [CLI](/vi/reference/cli) — `gotham`, `gotham-agent` và `signer` của bản phát hành:
  lệnh, cờ, mặc định và ví dụ cho hai tệp nhị phân dịch vụ.
- [Cấu hình](/vi/reference/configuration) — các khóa `gotham.yaml`, biến môi trường
  `GOTHAM_*` ghi đè chúng, môi trường của agent, và mười công tắc `FEATURE_*`.
- [API token](/vi/reference/api-tokens) — token `gt1_`, các phạm vi `read`, `deploy`
  và `admin`, và cách tạo, xoay vòng và thu hồi chúng.

Các trang này mô tả hành vi đã phát hành, không phải lộ trình. Khi một trang và tệp
nhị phân nói khác nhau thì tệp nhị phân đúng: `gotham help`, `gotham admin help`,
`gotham ca help`, `gotham update help`, `gotham-agent help` và `signer help` in ra
bề mặt lệnh của bản build đang có trước mặt bạn.

## Nên bắt đầu từ đâu

- Cài control plane và đăng ký node đầu tiên: [Cài đặt](/vi/guide/install).
- Cập nhật và hoàn tác: [Cập nhật](/vi/guide/updates).
- Mô hình quyền đằng sau token và quản trị viên nền tảng:
  [Bảo mật](/vi/operating/security).
