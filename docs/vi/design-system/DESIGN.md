---
title: Hệ thống thiết kế
---

# Hệ thống thiết kế

Bảng điều khiển Gotham là một bề mặt vận hành tối, dày thông tin, dành cho cả
những người chưa từng chạy lệnh máy chủ. Trang này ghi lại hệ thống thiết kế
đang được phát hành: các token, ánh xạ Naive UI, quy tắc bố cục và thành phần,
cùng những chỗ mã nguồn và ý tưởng tốt nhất chưa khớp nhau. Tài liệu dành cho
người đóng góp và cho chủ sở hữu muốn thay đổi giao diện mà không phá vỡ nhịp
điệu của nó.

Trang mô tả hành vi đã phát hành. Khi một giá trị và trang này nói khác nhau thì
CSS đang chạy là đúng:

| Nội dung | Vị trí |
| --- | --- |
| Token (nguồn chuẩn) | `web/src/shared/styles/tokens.css` |
| Style toàn cục, quy ước modal | `web/src/shared/styles/main.css` |
| Ghi đè theme Naive UI | `web/src/app/App.vue` |
| Bố cục khung bảng điều khiển | `web/src/app/layouts/AppLayout.vue` |
| Artifact token di động | `/design-system/design-tokens.json`, `/design-system/design-tokens.css` |
| Danh mục tương tác | [`/design-system/design-preview.html`](/design-system/design-preview.html) |

## Nguyên tắc

1. **Dày nhưng điềm tĩnh.** Bảng điều khiển hiển thị nhiều thứ cùng lúc — dự án,
   node, lần triển khai, log. Thứ bậc đến từ khoảng cách, độ đậm và màu bề mặt,
   không đến từ trang trí.
2. **Một bản sắc tối.** Không có theme sáng cho ứng dụng và không có nút chuyển
   theme. Bảng màu sáng trong token thuộc về menu giao diện riêng của terminal.
3. **Naive UI là nền thành phần.** Button, input, modal, table, tag và message
   đến từ Naive UI với phần ghi đè theme và một ít quy tắc toàn cục. Đừng dựng
   lại control bằng CSS thô; hãy mở rộng theme.
4. **Ngôn ngữ đơn giản trước.** Nhãn gọi đúng tên đối tượng, hành động bắt đầu
   bằng động từ, lỗi nói rõ cần làm gì tiếp. Thuật ngữ chuyên môn thuộc về phần
   tham chiếu, không thuộc biểu mẫu.
5. **Tiết lộ dần.** Wizard, tab và panel bên giữ màn hình đầu gọn; cài đặt nâng
   cao vẫn tới được mà không làm chật đường đi mặc định.
6. **Truy cập được theo mặc định, không phải đã chứng nhận.** Vòng focus, đường
   đi bàn phím, nhãn và xử lý giảm chuyển động là một phần của hệ thống. Gotham
   chưa được kiểm toán theo WCAG, nên đừng tuyên bố tuân thủ (xem
   [Khả năng truy cập](#kha-nang-truy-cap)).

## Token

`tokens.css` khai báo hai khối `:root`. Khối thứ hai cố ý ghi đè và mở rộng
khối thứ nhất, nên giá trị **hiệu lực** là giá trị trùng lặp cuối cùng. Hiện có
**144 custom property** hiệu lực. Các tệp tải về `design-tokens.json`,
`design-tokens.css` và những khối token nội tuyến của trang xem trước được sinh
từ các giá trị hiệu lực đó và được script đối chiếu với tệp đang chạy (xem
[Làm mới và kiểm tra](#lam-moi-va-kiem-tra)); phần văn xuôi và bảng trong trang
này do người viết và có thể chậm hơn mã nguồn.

Giá trị được sao chép nguyên văn, kể cả alias `color-mix()`. Đừng làm phẳng một
`color-mix()` thành mã hex trong tài liệu, nếu không hai bên sẽ lệch nhau.

### Màu

| Token | Giá trị | Vai trò |
| --- | --- | --- |
| `--bg` | `#313338` | Nền ứng dụng và trang |
| `--surface` | `#2b2d31` | Card, sidebar, modal, thân bảng |
| `--surface-warm` | `#1e1f22` | Input, bề mặt nhúng, nền terminal |
| `--fg` | `#dbdee1` | Chữ thân mặc định |
| `--fg-2` | `#f2f3f5` | Tiêu đề và chữ được nhấn |
| `--muted` | `#949ba4` | Nhãn phụ, mô tả |
| `--meta` | `#80848e` | Metadata, gợi ý, placeholder, chữ bị vô hiệu |
| `--border` | `rgba(255, 255, 255, 0.06)` | Đường phân cách, viền card, đường kẻ bảng |
| `--border-soft` | `#3f4147` | Viền hover của input và control |
| `--accent` | `#5865f2` | Hành động chính, liên kết, focus, trạng thái active |
| `--accent-on` | `#ffffff` | Chữ và icon trên nền accent |
| `--accent-hover` | `#4752c4` | Hover của accent |
| `--accent-active` | `color-mix(in oklab, var(--accent), black 14%)` | Trạng thái nhấn của accent |
| `--success` | `#23a55a` | Nền thành công, chấm trạng thái |
| `--warn` | `#f0b232` | Nền cảnh báo, chấm trạng thái |
| `--danger` | `#f23f43` | Nền lỗi, chấm trạng thái |

Khối thứ hai bổ sung các biến thể nhạt và biến thể an toàn cho chữ:

| Token | Vai trò |
| --- | --- |
| `--fg-soft` | Lớp phủ chữ rất nhạt (6% của `--fg`) |
| `--hover-row`, `--selected-row` | Hover và chọn dòng trong bảng |
| `--accent-soft`, `--accent-softer` | Nền khi chọn, lớp accent lót bên trong |
| `--accent-ink`, `--accent-ink-hover`, `--link` | Accent dùng làm **chữ hoặc icon** trên bề mặt tối |
| `--success-soft`, `--warn-soft`, `--danger-soft` | Nền alert và tag |
| `--success-ink`, `--warn-ink`, `--danger-ink` | Chữ và icon trạng thái trên bề mặt tối |
| `--float` | Bề mặt nổi (popover, cửa sổ nổi) |

**Quy tắc:** dùng màu gốc (`--accent`, `--success`, `--warn`, `--danger`) cho
nền, chấm, viền và trang trí. Dùng biến thể `*-ink` khi màu đó mang chữ nhỏ hoặc
icon cần đọc được. Chấm trạng thái có thể là `--success`; chữ bên cạnh phải là
`--success-ink`, không phải màu xanh gốc.

#### Độ tương phản đo được

Các tỷ lệ này được đo ngày 2026-10-27 bằng công thức độ sáng tương đối của WCAG
so với `--bg` (`#313338`). Hãy coi đây là số đo, không phải chứng nhận.

| Token | Giá trị | Tương phản so với `--bg` | Hướng dẫn |
| --- | --- | --- | --- |
| `--fg-2` | `#f2f3f5` | 11,39:1 | Tiêu đề và giá trị chính |
| `--fg` | `#dbdee1` | 9,36:1 | Chữ thân |
| `--muted` | `#949ba4` | 4,50:1 trên `--bg`; 4,92:1 trên `--surface` | Chữ phụ; ít biên |
| `--meta` | `#80848e` | 3,38:1 | Chỉ metadata, không dùng cho chữ nhỏ quan trọng |
| `--accent` | `#5865f2` | 2,74:1 | Nền, chấm, viền — không dùng cho chữ |
| `--success` | `#23a55a` | 3,97:1 | Nền, chấm — không dùng cho chữ |
| `--warn` | `#f0b232` | 6,69:1 | Dùng làm chữ được, nhưng ưu tiên `--warn-ink` cho nhất quán |
| `--danger` | `#f23f43` | 3,35:1 | Nền, chấm — không dùng cho chữ |

Những hệ quả dễ bỏ sót:

- `--meta`, `--accent`, `--success` và `--danger` không đạt ngưỡng 4,5:1 cho
  chữ thường. Đừng dùng chúng cho chi tiết lỗi, gợi ý có ý nghĩa hay nhãn trạng
  thái nhỏ. `--warn` (~6,69:1) đạt, nhưng quy tắc thiết kế vẫn ưu tiên biến thể
  `*-ink` cho chữ trạng thái để vai trò màu giữ nhất quán.
- `--muted` đo được 4,50:1 trên `--bg` và 4,92:1 trên `--surface` (tối hơn), nên
  đạt ngưỡng 4,5:1 cho chữ thường với rất ít biên. Tỷ lệ không đổi theo cỡ chữ;
  ngưỡng thì có (chữ lớn có thể dùng 3:1). Hãy đo lại nếu token thay đổi.
- Theme tag của Naive tự suy ra màu từ bảng màu common, nên nhãn tag không tự
  động bằng token trạng thái gốc. Nếu cần tag riêng, hãy đo màu chữ đã tính,
  không chỉ màu chấm.
- Biến thể ink được trộn với trắng lúc chạy. Hãy đo trong trình duyệt thay vì
  giả định một tỷ lệ cố định.

Tham khảo: [ngưỡng tương phản tối thiểu WCAG
2.2](https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html) và
[kích thước mục tiêu tối
thiểu](https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html).

### Kiểu chữ

| Token | Giá trị | Cách dùng |
| --- | --- | --- |
| `--font-display` | `"Google Sans", "Helvetica Neue", Helvetica, Arial, sans-serif` | Tiêu đề, thương hiệu |
| `--font-body` | cùng stack với display | Chữ thân và control |
| `--font-mono` | `"JetBrains Mono", Consolas, "Andale Mono", "Courier New", monospace` | ID, địa chỉ, cổng, con số |
| `--text-xs` | `12px` | Metadata, eyebrow, gợi ý |
| `--text-sm` | `14px` | Cỡ chữ thân mặc định trong bảng điều khiển |
| `--text-base` | `16px` | Đọc dài, chữ ở trang đăng nhập |
| `--text-lg` | `18px` | Tiêu đề mục |
| `--text-xl` | `20px` | Tiêu đề nhóm trên trang |
| `--text-2xl` | `24px` | Tiêu đề trang |
| `--text-3xl` | `32px` | Hiển thị marketing/đăng nhập |
| `--text-4xl` | `56px` | Chỉ dùng cho trang giới thiệu |
| `--leading-body` | `1.375` | Chữ thân |
| `--leading-tight` | `1.1` | Dòng hiển thị gọn |
| `--leading-title` | `1.25` | Tiêu đề |
| `--tracking-display` | `-0.02em` | Tiêu đề hiển thị lớn |
| `--tracking-eyebrow` | `0.08em` | Nhãn eyebrow in hoa |

Quy tắc dùng:

- Chữ thân của bảng điều khiển là `14px` (`main.css` đặt
  `font-size: var(--text-sm)` cho `body`). Đừng tăng cục bộ để trang trông to
  hơn; hãy dùng khoảng cách và thứ bậc.
- `h1` là `--text-2xl`/700, `h2` là `--text-xl`/700, `h3` là `--text-lg`/600.
  Tiêu đề cấp trang đi qua `PageHeader`/`.page-head`, không dùng `<h1>` trần.
- `--text-3xl` và `--text-4xl` dành cho bề mặt đăng nhập và marketing. Một trang
  bảng điều khiển dùng chúng sẽ trông như landing page.
- Dùng `.mono` (hoặc `--font-mono`) cho mọi thứ người dùng có thể sao chép hoặc
  so sánh từng ký tự: ID container, image tag, địa chỉ IP, cổng, tên miền, tiền
  tố token. Dùng `.num` khi các chữ số cần thẳng cột.
- Eyebrow in hoa là mono, 12px, với `--tracking-eyebrow` và `--muted`; dùng tiết
  kiệm để đặt tên một nhóm, không dùng cho mọi card.

**Font dự phòng khi ngoại tuyến.** Bảng điều khiển phục vụ Google Sans và
JetBrains Mono từ `/fonts/*.woff2` qua `@font-face` trong `tokens.css`. Nếu các
tệp đó thiếu, bị chặn hoặc trang được mở từ hệ thống tệp, chữ sẽ rơi về
Helvetica/Arial và font mono cục bộ. Tránh bố cục phụ thuộc độ rộng glyph chính
xác; bản dự phòng làm việc ngắt dòng hơi khác.

### Khoảng cách, bo góc, đổ bóng và chuyển động

| Nhóm | Token | Ghi chú |
| --- | --- | --- |
| Khoảng cách | `--space-1` 4px, `--space-2` 8px, `--space-3` 12px, `--space-4` 16px, `--space-5` 20px, `--space-6` 24px, `--space-8` 32px, `--space-12` 40px | Dùng đúng thang. `--space-12` là **40px**, không phải 48px; đừng "sửa" nó theo lưới 4/8 bỏ qua giá trị này. |
| Nhịp mục | `--section-y-desktop` 80px, `--section-y-tablet` 48px, `--section-y-phone` 32px | Đã khai báo nhưng hiện không được tham chiếu; trang bảng điều khiển dùng `--space-*` và padding trang. |
| Bo góc | `--radius-sm` 4px, `--radius-md` 8px, `--radius-lg` 16px, `--radius-pill` 9999px | Control dùng 4px, card và modal 8px; `lg` hiếm (hero/minh hoạ). Pill dành cho chấm trạng thái, chip đếm và segmented control. |
| Đổ bóng | `--elev-flat`, `--elev-ring`, `--elev-raised`, `--elev-floating` | Ring là viền sáng `1px`; bề mặt nổi dùng bóng sâu cộng ring. Ưu tiên `--elev-ring` thay vì tự tạo màu viền. |
| Chuyển động | `--motion-fast` 80ms, `--motion-base` 200ms, `--ease-standard` `cubic-bezier(0.2, 0, 0, 1)`, `--ease-morph` `cubic-bezier(0.215, 0.61, 0.355, 1)` | Phản hồi hover/nhấn dùng fast; panel và thay đổi bố cục dùng base. Không có vòng lặp trang trí. |
| Focus | `--focus-ring` = `0 0 0 3px var(--accent-ink)` | `:focus-visible` toàn cục cũng đặt outline 2px với offset 2px. Mỗi control chỉ dùng một mẫu, không dùng cả hai. |

`prefers-reduced-motion: reduce` được khai báo trong `AppLayout.vue` (style
scoped) và thu gọn thời lượng animation, transition cho khung. Chưa kiểm chứng
độ phủ bên trong các component tính năng; animation mới nên tự có hành vi giảm
chuyển động. Trang xem trước token có quy tắc riêng như vậy cho specimen.

### Bố cục và hành vi responsive

Khung là lưới ba cột trong `AppLayout.vue`:

- `--rail-w` 72px cho rail máy chủ (chỉ chủ sở hữu; ẩn với người khác),
- `--sidebar-w` 260px cho điều hướng,
- phần còn lại cho trang, với cột giữa `minmax(0, 1fr)` để bảng và log dài
  không kéo khung rộng ra.

Topbar dính ở `--topbar-h` 48px. Nội dung trang nằm trong `.page` với padding
`--space-6` và padding đáy `--space-12`, **max-width 1360px**. Giá trị 1360px
này được viết cứng trong layout, không do `--container-max` (1200px) điều khiển;
token đó hiện không được tham chiếu. Đừng giả định `--container-max` quản khung
bảng điều khiển; nó không làm vậy.

| Độ rộng | Hành vi |
| --- | --- |
| `> 1024px` | Rail + sidebar + trang; rộng tối đa 1360px |
| `≤ 1024px` | Khung thành một cột; rail máy chủ bị ẩn hoàn toàn (`v-if` trong `AppLayout.vue`); điều hướng chuyển vào drawer sidebar; padding trang còn 20px |
| `≤ 860px` | `.page-actions` xuống dưới tiêu đề và mất margin tự động |
| `≤ 640px` | Padding trang còn 16px; trang cao đầy dùng padding dọc 8px |
| Container `≤ 480px` | `.form-row` (lưới biểu mẫu hai cột) thu về một cột bằng container query, độc lập với viewport |

`page--wide` bỏ max-width cho các trang kiểu log/terminal. `page--fill` biến
trang thành cột flex cao cố định cho các trình xem tự quản việc cuộn (terminal,
files). Dùng chúng qua route meta (`wide`, `fillViewport`) thay vì thêm class
tùy tiện.

## Thành phần

### Tiêu đề trang

`web/src/shared/ui/PageHeader.vue` là quy ước cho danh tính và hành động của
trang: slot identity tùy chọn (icon/thương hiệu), breadcrumb, tiêu đề, badge,
dòng meta mono, mô tả và actions. Nó dựa trên `.page-head`, `.page-desc` và
`.page-actions` trong `main.css`.

- Tiêu đề là một `h1` duy nhất; badge nằm cạnh tiêu đề, không nằm trong mô tả.
- Meta là mono và `--muted`: dự án/môi trường, ID, dấu thời gian.
- Actions xuống dòng ở 860px và chuyển xuống dưới tiêu đề; giữ một hành động
  chính cộng các nút phụ.

### Biểu mẫu

- Dùng `NForm`/`NFormItem`; nhãn qua khóa i18n, không viết chuỗi trực tiếp.
- Kiểm tra dữ liệu dùng schema zod qua `web/src/shared/validation` (`ruleFrom`,
  `rulesFor`, `fieldErrors`). Schema của tính năng nằm trong
  `features/<module>/schemas/`. Đừng thêm closure `validator:` tùy tiện trong
  component.
- Phản hồi API được parse ở ranh giới tin cậy bằng `parseWith` từ
  `web/src/shared/validation/parse`; rule biểu mẫu dùng `ruleFrom`. Cả hai chặn
  dữ liệu sai hình dạng trước khi tới UI.
- `field-hint` là gợi ý 12px màu `--meta` dưới control; nó ẩn khi field đang lỗi
  (`n-form-item-blank--error`), để thông báo lỗi thay thế chứ không chồng lên.
- `.form-row` là lưới hai cột thu về một cột ở **container** rộng 480px. Ưu tiên
  nó hơn media query viewport bên trong modal, vì modal có thể hẹp hơn viewport.
- Chữ feedback là 12px ở mọi cỡ Naive; nhãn là `--muted` (ghi đè Naive), lỗi
  dùng `#f67c7c`.

### Modal và wizard

Quy ước modal dùng chung trong `main.css`, áp dụng cho `.app-modal`,
`.wizard-modal` và `.edit-server-modal`:

- Card không vượt quá `calc(100vh - 64px)` và là cột flex.
- Header, footer và vùng action cố định; vùng nội dung cuộn
  (`flex: 1 1 auto; overflow-y: auto; min-height: 0`).
- Biến thể wizard giữ nội dung là cột flex với `overflow: hidden` và để phần tử
  `.wizard` con tự quản việc cuộn, vì thân bước và footer của nó cần hành vi
  riêng.

**Ngoại lệ hiện tại với chiều cao thấp.** Dưới chiều cao viewport 480px,
`.app-modal` ngừng ghim header/footer: bản thân card cuộn (`overflow-y: auto`)
và nội dung thành `flex: 0 0 auto; overflow-y: visible`. Đây là phương án dự
phòng có chủ đích cho cửa sổ ngang nhỏ, không phải mẫu chung. Hãy giữ nó khi
tái cấu trúc; nếu đổi, phải thử ở viewport cao 480px, không chỉ hẹp.

`CreateAppWizard.vue` là ví dụ theo quy ước dùng chung: `NModal preset="card"`
với `class="wizard-modal"`, `width: 880px; max-width: 96vw`, `.wizard-foot` cố
định, và `.wizard-body` cuộn (`overflow-y: auto; flex: 1 1 auto;
min-height: 0`). Rail hai cột của nó thu về một cột ở 720px. Hãy lấy cấu trúc đó
thay vì tự phát minh khung modal mới.

`CreateDatabaseWizard.vue` là ngoại lệ hiện tại: nó dùng
`class="form-container"` và card 640px, nên **không** tham gia quy ước
`wizard-modal`. Đừng sao chép nó làm mẫu modal; chuyển nó sang quy ước dùng
chung là một thay đổi hành vi riêng.

Dùng hành vi [`NModal`](https://www.naiveui.com/en-US/dark/components/modal) và
theo [mẫu dialog WAI-ARIA](https://www.w3.org/WAI/ARIA/apg/patterns/dialog-modal/)
cho dialog tự viết: tiêu đề có nhãn, đưa focus vào trong, Escape huỷ và đưa focus
về phần tử kích hoạt. Đừng chặn Escape chỉ vì dialog có hành động phá huỷ; chỉ
chặn đóng trong một thao tác đang chạy chưa an toàn khi có lý do rõ ràng.

### Trạng thái, bảng và phản hồi

- Trạng thái là tag có chấm và chữ (`DatabaseStatusTag.vue`): creating = accent
  + nhấp nháy, running = success, stopped = warning, error = danger, deleting =
  muted. Màu không bao giờ là tín hiệu duy nhất; chữ mang nghĩa.
- Bảng là `NDataTable` với thân `--surface` và đường kẻ `--border`. Giá trị dài
  ngắt trong ô (`.mono`/`.error-text` dùng `overflow-wrap: anywhere`) thay vì
  làm bảng rộng ra.
- Trạng thái rỗng, đang tải và lỗi là một phần của thành phần, không phải việc
  làm sau: bảng có thể rỗng cần một câu nói người dùng nên làm gì tiếp.
- Hành động phá huỷ dùng xác nhận kiểu lỗi và gọi đúng tên đối tượng.

### Tooltip

`AppTooltip.vue` bọc tooltip của Naive: hoãn 500ms khi hover, 120ms ân hạn khi
rời, kích hoạt bằng bàn phím sau một khoảng hoãn, Escape đóng, chạm cảm ứng
không mở. Cung cấp `aria-label` thật; khi nó khác nội dung tooltip, tooltip còn
phát một bản chỉ dành cho trình đọc màn hình. Đừng đặt thông tin quan trọng chỉ
trong tooltip.

## Ánh xạ Naive UI: hiện tại và khuyến nghị

`App.vue` truyền `darkTheme` cùng `GlobalThemeOverrides`. Ánh xạ khá sát, nhưng
nhiều giá trị là literal trùng lặp thay vì tham chiếu token:

| Khóa Naive | Giá trị hiện tại | Token tương đương | Đích căn chỉnh và lưu ý |
| --- | --- | --- | --- |
| `common.primaryColor` | `#5865f2` | `--accent` | Giữ literal; căn chỉnh khi sửa ánh xạ |
| `common.primaryColorHover` | `#4752c4` | `--accent-hover` | Giữ literal |
| `common.primaryColorPressed` | `#4c57d0` | `--accent-active` (`color-mix(…, black 14%)`) | Giữ literal; hiện lệch khỏi trạng thái nhấn trong CSS |
| `common.bodyColor` | `#313338` | `--bg` | Giữ literal |
| `common.cardColor`, `modalColor`, `tableColor` | `#2b2d31` | `--surface` | Giữ literal |
| `common.popoverColor` | `#121315` | `--float` (`color-mix(surface-warm 78%, black)`) | Hai nguồn cho bề mặt nổi; thống nhất về một giá trị |
| `common.inputColor` | `#1e1f22` | `--surface-warm` | Giữ literal |
| `common.textColor1/2/3` | `#f2f3f5`, `#dbdee1`, `#949ba4` | `--fg-2`, `--fg`, `--muted` | Giữ literal |
| `common.placeholderColor` | `#80848e` | `--meta` | Giữ literal |
| `common.dividerColor`, `borderColor` | `rgba(255,255,255,0.06)` | `--border` | Giữ literal |
| `common.fontFamily` | stack Google Sans | `--font-body` | Giữ literal; font stack không phải phép phân tích màu |
| `common.fontFamilyMono` | stack JetBrains Mono | `--font-mono` | Giữ literal |
| `common.borderRadius`, `borderRadiusSmall` | `4px` | `--radius-sm` | Giữ literal |
| `Card.borderRadius` | `8px` | `--radius-md` | Giữ literal |
| `Alert.colorSuccess/Warning/Error/Info` | nền mờ 16%/10% | `--success-soft`, `--warn-soft`, `--danger-soft`, `--accent-soft` | Trùng lặp; chỉ thống nhất khi có kiểm tra cụ thể |
| `Alert.iconColor*` | `#61be88`, `#f3c057`, `#f67c7c`, `#97a0f7` | xấp xỉ `--success-ink`, `--warn-ink`, `--danger-ink`, `--accent-ink` | Giá trị cứng không theo thay đổi `color-mix` |
| `Button.colorPrimary*` | các mã hex accent | `--accent`, `--accent-hover`, `--accent-active` | Đích là token accent; kiểm tra các trạng thái suy ra trước khi đổi nền |
| `Input.borderFocus`, `boxShadowFocus` | `var(--accent-ink)` + vòng `var(--bg)` | đã dùng token | `var()` ở cấp component chạy được; `main.css` bỏ bóng state-border nên viền mang dấu hiệu focus |

Vì sao quan trọng: thay đổi một token hiện cập nhật phần lớn bảng điều khiển
nhưng không cập nhật nút primary khi nhấn, bề mặt popover hay nền alert, vì
chúng là literal trong `App.vue`.

**Đừng thay toàn bộ màu `common` bằng `var()`.** Theme tag tối của Naive chạy
`changeColor` trên màu common: truyền `common.primaryColor: "var(--accent)"` sẽ
ném `Invalid color value var(--accent)` (đã kiểm chứng với gói đã cài). Các ghi
đè cấp component đang dùng `var()` — màu chữ Button, viền Input — là thuộc tính
CSS trực tiếp và không chứng minh `common` nhận `var()`. Hãy coi token là đích
căn chỉnh, dùng màu cụ thể cho `common`, và kiểm tra màu hover, pressed và tag
suy ra trong trình duyệt trước khi đổi ánh xạ.

## Chuyển động

- 80ms cho màu hover/nhấn và biến đổi nhỏ; 200ms cho panel, drawer, mở rộng
  dòng và thay đổi bố cục; 200ms+ cho modal vào chỉ khi không làm chậm biểu mẫu.
- Dùng `--ease-standard` cho chuyển động chuẩn và `--ease-morph` cho morph
  kích thước/hình dạng. Không thêm cubic-bezier mới.
- Chấm trạng thái có thể nhấp nháy để báo việc đang chạy hoặc node cần chú ý;
  xem `DatabaseStatusTag`, `DeploymentStatusTag` và `ServerStatusTag` cho đúng
  tập trạng thái. `AppLayout` có khai báo giảm chuyển động, nhưng chưa kiểm
  chứng độ phủ qua các component tính năng, nên animation mới nên tự có hành vi
  giảm chuyển động.
- Trạng thái tải phải trung thực: spinner khi chưa biết thời lượng, giá trị
  tiến độ khi backend báo, và một dòng chữ cho việc kéo dài qua điều hướng
  trang.

## Khả năng truy cập

Hành vi đang phát hành:

- `:focus-visible` dùng outline 2px `--accent-ink` với offset 2px. `main.css`
  bỏ quầng state-border của input Naive
  (`.n-input--focus .n-input__state-border`), nên dấu hiệu input đang focus là
  **viền** `--accent-ink`, không phải quầng.
- Khung có landmark: `<aside aria-label>` cho sidebar, danh sách nav, header
  topbar, main view. Drawer điều hướng trên di động có nhãn và đóng được.
- Tooltip có đường đi bàn phím và Escape; chạm cảm ứng tránh nội dung chỉ hiện
  khi hover.
- `AppLayout.vue` có khai báo giảm chuyển động cho khung; chưa kiểm chứng độ phủ
  bên trong component tính năng, nên hãy tự thêm quy tắc giảm chuyển động cho
  animation mới.
- Quy ước modal giữ nội dung cuộn trong card có giới hạn, nhờ đó tiêu đề và
  hành động của dialog vẫn nằm trong viewport.

Cần kiểm tra ở trang bạn sửa (chưa được kiểm toán ở đây):

- Nút chỉ có icon cần `aria-label`; icon trang trí nên có `aria-hidden`. Hãy
  kiểm tra component bạn sửa thay vì giả định.
- Đo kích thước mục tiêu bạn thêm. WCAG 2.2 yêu cầu tối thiểu 24×24 CSS pixel
  cho mục tiêu trỏ; tag trạng thái tĩnh không phải mục tiêu trỏ.
- Thử luồng bằng bàn phím, và bằng trình đọc màn hình khi có thể. Chưa có kiểm
  toán trình đọc màn hình.
- Chưa có kiểm toán WCAG, và không trang nào tuyên bố tuân thủ. Giá trị tương
  phản trong tài liệu này là số đo của từng cặp token cụ thể, không phải kết
  luận cho toàn sản phẩm.

## Chữ và ngôn ngữ

- Chữ giao diện song ngữ: tiếng Anh và tiếng Việt nằm trong
  `web/src/shared/i18n/locales/`. Khóa mới cần cả hai locale; thiếu khóa sẽ rơi
  về bản dự phòng, nhưng dự phòng không phải bản dịch.
- Viết cho người chưa từng mở terminal: nói "Thêm node", không nói "Cấp phát
  đăng ký agent"; giải thích field trong một câu; nêu hậu quả của hành động phá
  huỷ trong dialog xác nhận.
- Câu thường cho nhãn và nút. Không dùng dấu chấm than. Không đổ lỗi
  ("Invalid input" → "Nhập tên chỉ gồm chữ, số hoặc dấu gạch ngang").
- Giữ cùng một danh từ ở mọi nơi: "environment", "node", "application" — không
  dùng "env", "server", "app" trong cùng màn hình.
- Tài liệu này có bản tiếng Anh và tiếng Việt dưới `/vi/`; giữ cấu trúc trang và
  tên token giống nhau để hai bản so sánh được.

## Những gì học từ bảng điều khiển tương tự

Các quan sát này được thực hiện trên trang chủ công khai ngày 2026-10-27. Đây
không phải kiểm toán sản phẩm đầy đủ, và không có luồng đăng nhập nào được thử.
Gotham không sao chép màu thương hiệu, minh hoạ hay animation marketing.

- [Coolify](https://coolify.io/) — bảng tài nguyên tối đặt trạng thái, tên miền
  và máy chủ cạnh nhau, cùng một hành động "tài nguyên mới" rõ ràng. Bài học:
  giữ trạng thái và ngữ cảnh trong cùng một dòng; làm hành động tiếp theo rõ
  ràng.
- [Dokploy](https://dokploy.com/) — nhóm khả năng theo Applications &
  Databases, Compose, Logs, Monitoring, Backups và Templates. Bài học: nhóm
  theo việc người dùng đang làm, không theo tên dịch vụ nội bộ.
- [Railway](https://railway.com/) — một hành động Deploy nổi bật và ngữ cảnh
  dự án/môi trường hiển thị rõ; ảnh chụp cho thấy cảnh báo bộ nhớ theo ngữ cảnh.
  Bài học: hiển thị phạm vi (dự án/môi trường) gần hành động, và cảnh báo trong
  ngữ cảnh khi giới hạn tài nguyên có ý nghĩa.

Cả ba là bề mặt marketing, nên mật độ thấp hơn bảng điều khiển. Dùng chúng cho
kiến trúc thông tin, không dùng cho kích thước hay màu.

## Nên và không nên

Nên:

- dùng thang khoảng cách và token có sẵn; chỉ thêm token khi một giá trị lặp lại
  và cần tên;
- ghép thành phần Naive và UI dùng chung (`PageHeader`, `AppTooltip`,
  `GothamIcon`) thay vì tạo primitive mới;
- giữ quy ước modal: header/footer cố định, nội dung cuộn, chiều cao có giới
  hạn;
- đặt phạm vi và trạng thái gần hành động cần chúng;
- thử ở 1024px, 640px và viewport cao 480px, bật giảm chuyển động;
- chạy kiểm tra token sau khi sửa `tokens.css`.

Không nên:

- thêm nút chuyển theme sáng hay bảng màu thứ hai cho ứng dụng;
- dùng `--meta` hoặc màu trạng thái gốc cho chữ nhỏ quan trọng;
- viết cứng mã hex trong component khi đã có token;
- tuyên bố tuân thủ WCAG hoặc "accessible" mà không có kiểm toán;
- để trang rộng quá 1360px theo mặc định, hoặc dùng cỡ chữ marketing trong bảng
  điều khiển;
- dựng lại control Naive thành mẫu CSS riêng trong trang sản phẩm.

## Làm mới và kiểm tra

Các artifact tài liệu được sinh từ CSS đang chạy; đừng sửa tay
`design-tokens.json`, `design-tokens.css` hay các khối được sinh trong
`design-preview.html`.

```sh
cd docs

# sau khi đổi web/src/shared/styles/tokens.css
node scripts/design-tokens.mjs --write

# kiểm tra CSS đang chạy, JSON, bản CSS và token nội tuyến của preview khớp nhau
node scripts/design-tokens.mjs --check

# kiểm tra trang tài liệu vẫn build được
npm ci
npm run docs:build
```

`--check` sinh lại trong bộ nhớ rồi so sánh: JSON, bản CSS và `:root` cùng dữ
liệu token nội tuyến của preview phải bằng kết quả từ CSS đang chạy; mọi token
phải được phân vào một nhóm preview; CSS di động và preview không được tải font,
import hay tài nguyên từ xa; và script nội tuyến của preview phải biên dịch được.
Script không kiểm tra hành vi lọc và không ghim giá trị token nào — tính khớp
theo `tokens.css`. Nó in số token hiệu lực (hiện là 144). Hãy chạy tay trước khi
push; job CI của tài liệu chỉ chạy `npm run docs:build`.

Không được làm tròn giá trị: script chỉ chuẩn hoá khoảng trắng, nên biểu thức
`color-mix()` và chuỗi alias được giữ nguyên.

## Bản đồ tệp

| Đường dẫn | Mục đích |
| --- | --- |
| `docs/design-system/DESIGN.md` | Trang này (tiếng Anh) |
| `docs/vi/design-system/DESIGN.md` | Bản dịch tiếng Việt |
| `docs/public/design-system/design-tokens.json` | Bản đồ token phẳng kèm nhóm cho preview |
| `docs/public/design-system/design-tokens.css` | Bản `:root` di động, không có font face |
| `docs/public/design-system/design-preview.html` | Danh mục tương tác ngoại tuyến |
| `docs/scripts/design-tokens.mjs` | Script sinh và kiểm tra khớp |
