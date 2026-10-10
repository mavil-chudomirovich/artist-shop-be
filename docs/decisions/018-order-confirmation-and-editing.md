# ADR 018 - Đơn hàng: state machine sáu trạng thái, giữ hàng chỉ khi artist xác nhận, hạn thanh toán 60 phút, sửa đơn toàn bộ quay về `PENDING`, `order_version` cho module 08, và mailer dùng chung tối thiểu

- **Status**: Accepted
- **Decision Date**: 2026-10-10
- **Decision Maker**: Mavil
- **Feature**: `specs/010-order-confirmation`
- **Supersedes**: phần **state machine** của [ADR 017](017-order-checkout.md) (feature `009-order`):
  tập trạng thái, tên trạng thái và thời điểm giữ hàng
- **Liên quan**: [ADR 017](017-order-checkout.md) (checkout, snapshot dòng, `WithTx`),
  [ADR 014](014-inventory-hold-and-availability.md) (giữ chỗ/khả dụng module 05),
  [ADR 015](015-shop-first-version-plan-and-payos.md) §5 (kế hoạch V1.0, PayOS, không phí ship, không
  hoàn tiền)

## 1. Bối cảnh

Feature `009-order` dựng vòng đời đơn tối thiểu: checkout **giữ hàng ngay** và đưa đơn vào trạng thái
`PENDING_PAYMENT`; chỉ có `PENDING_PAYMENT → PAID → SHIPPED → COMPLETED` cộng `CANCELLED` (ADR 017,
ADR 015 §5). Shop là **một nghệ sĩ** (`docs/product/project_overview.md`), và kế hoạch đơn hàng
(`docs/modules/ke-hoach-don-hang.md`) chỉ ra hai vấn đề của mô hình đó:

1. **Giữ hàng ngay khi đặt là sai với thực tế shop nhỏ.** Artist không phải lúc nào cũng làm được đơn
   ngay, nhưng checkout đã khóa hàng trong một cửa sổ ngắn. Khách cần một bước **artist xác nhận**
   trước khi đơn trở thành "phải trả tiền".
2. **Một trạng thái `PENDING_PAYMENT` không diễn đạt được hai thứ khác nhau**: "chờ artist xác nhận" và
   "chờ thanh toán". Trộn chúng khiến không có chỗ cho bước xác nhận.

Ngoài ra kế hoạch yêu cầu: khách **sửa được đơn trước khi trả tiền**; có **hạn thanh toán** kể từ lúc
giữ hàng; **thông báo** cho artist (khi đơn cần xác nhận) và cho khách (mỗi lần đổi trạng thái); và một
**phiên bản đơn** để module 08 đối chiếu về sau. Feature này là feature **đầu tiên gửi email**, trong khi
module 12 (Notification) chưa tồn tại và module 01 có sender nội bộ đang bị sửa song song.

Sáu quyết định dưới đây định hình lại lược đồ, hình dạng HTTP, ranh giới liên module và cả tầng hạ tầng
dùng chung, nên chúng được ghi thành ADR chứ không chỉ nằm trong `research.md`.

## 2. Quyết định

### 2.1 State machine sáu trạng thái, và đổi tên `PENDING_PAYMENT` → `PAYMENT_PENDING`

Đơn có **sáu** trạng thái: `PENDING` (chờ artist xác nhận), `PAYMENT_PENDING` (đã xác nhận, chờ thanh
toán), `PAID`, `SHIPPED`, `COMPLETED`, `CANCELLED`. Các cạnh hợp lệ:

```
PENDING ──confirm──► PAYMENT_PENDING ──pay──► PAID ──ship──► SHIPPED ──complete──► COMPLETED
   │  ▲                      │
   │  └── edit ──────────────┘        (từ PAYMENT_PENDING; đơn quay lại PENDING)
   ├──reject──► CANCELLED
   ├──cancel (khách)──► CANCELLED
   PAYMENT_PENDING ──cancel (khách) / expire──► CANCELLED
```

`PENDING_PAYMENT` của 009 **đổi tên** thành `PAYMENT_PENDING`, và thêm `PENDING`. `CONFIRMED` (tên trong
kế hoạch) **gộp vào** `PAYMENT_PENDING` — artist xác nhận chính là mở cửa sổ thanh toán — và
`REJECTED`/`EXPIRED` **dùng lại** `CANCELLED`. Migration `00011_order_confirmation.sql` **backfill** giá
trị cũ (`UPDATE orders SET status = 'PAYMENT_PENDING' WHERE status = 'PENDING_PAYMENT'`) **trước khi**
siết `orders_status_ck` lên sáu giá trị, nên một database đang giữ đơn của 009 migrate sạch.

### 2.2 Hàng chỉ được giữ khi artist xác nhận, toàn bộ hoặc không gì cả

Checkout **không** giữ hàng: đơn `PENDING` mang snapshot dòng + địa chỉ + tổng, làm rỗng giỏ, và **không**
đụng tồn kho; đơn **không** có hạn thanh toán. Artist **xác nhận** chạy trong **một** transaction: khóa
đơn, gọi `InventoryReservation.Reserve` cho **mọi** dòng, và chỉ khi **tất cả** giữ được mới chuyển sang
`PAYMENT_PENDING`; một dòng không giữ được thì **rollback** cả transaction, đơn giữ nguyên `PENDING` và
**không** dòng nào bị giữ. Đây là bảo đảm **chống oversell**: hai đơn tranh đơn vị cuối cùng serialise
trên khóa dòng của module 05, và **nhiều nhất một** đơn commit.

### 2.3 Hạn thanh toán 60 phút, tính từ lúc xác nhận; `expires_at` → `payment_expires_at`

Khi xác nhận thành công, đơn lưu `confirmed_at = now` và `payment_expires_at = now + 60 phút`. Cột
`expires_at` của 009 **đổi tên** `payment_expires_at` và trở thành **nullable** (`NULL` khi đơn còn chờ
artist). Đơn vẫn suy ra cửa sổ từ `InventoryReservation.HoldWindow()`, mà module 05 nâng lên **60 phút**
(`HoldTTL`): cửa sổ có **một chủ sở hữu** và order không lặp lại con số.

### 2.4 Chỉ đơn chờ thanh toán mới hết hạn theo thời gian

Sweeper hết hạn chỉ chọn các đơn `PAYMENT_PENDING` quá `payment_expires_at`, huỷ chúng và nhả hàng **đúng
một lần**. Đơn `PENDING` **không bao giờ** bị chọn: nó không giữ hàng, nên không cần hạn; artist làm hàng
đợi theo nhịp của mình (FIFO, §2.7).

### 2.5 Sửa đơn là thay toàn bộ, và sửa đơn đang chờ thanh toán quay về `PENDING`

`PUT /api/v1/orders/{orderId}` **thay toàn bộ** tập dòng (thêm/bớt/đổi số lượng) và, khi nêu, **địa chỉ
giao** (chọn từ địa chỉ đã lưu bằng `addressId`, như checkout; bỏ trống thì **giữ nguyên**). Mỗi dòng
được **đối chiếu lại** và **chụp lại giá hiện tại**, tổng được tính lại, và `order_version` tăng. Một lần
sửa để **0 dòng** bị từ chối (`ORDER_EMPTY`); sửa đơn **đã trả tiền** bị từ chối (`ORDER_NOT_EDITABLE`).
Sửa đơn `PENDING` giữ `PENDING`; sửa đơn `PAYMENT_PENDING` **nhả giữ chỗ** đúng một lần, **bỏ** hạn thanh
toán, và đưa đơn **quay lại `PENDING`** để artist xác nhận lại. Mỗi lần sửa ghi một dòng
`order_edit_history` (nội dung trước/sau, người sửa, thời điểm, phiên bản) trong cùng transaction.

Khác checkout: một lần đổi giá kể từ lúc đặt **không** bị từ chối khi sửa — khách đang **chọn lại** món
nên nhận giá hiện tại; ở checkout, giá trong giỏ là giá khách đã thấy nên phải đối chiếu.

### 2.6 `order_version` là seam nội bộ cho module 08

`orders.order_version` bắt đầu ở 1 và **tăng** mỗi lần sửa được chấp nhận **và** khi artist xác nhận. Nó
là chỗ module 08 so sánh để **từ chối một callback thanh toán thuộc nội dung cũ** (đơn bị sửa sau khi mở
phiên thanh toán). Nó **không** lộ ra response của client — là seam nội bộ, không phải một trường hợp
đồng.

### 2.7 Thông báo qua một mailer dùng chung tối thiểu; danh sách admin có `status`/`sort`

Một **port** `Notifier` của module order được một adapter cài đặt, gửi qua **`internal/share/mailer`** mới
(`net/smtp`, có `LogSender` khi chưa cấu hình SMTP). Artist (email shop cấu hình) được báo khi đơn **cần
xác nhận**; khách được báo mỗi khi **trạng thái đổi**. Gửi là **best-effort**: chạy sau commit, lỗi được
ghi log và **không** làm hỏng đơn. Module 01 **không** bị đụng tới; module 12 chưa tồn tại nên một mailer
dùng chung nhỏ là đường ít rủi ro nhất.

Danh sách admin (`GET /api/v1/admin/orders`) nhận thêm `status` (lọc một trong sáu trạng thái) và `sort`
(`newest` mặc định, `oldest`); `status=PENDING&sort=oldest` là **hàng đợi xác nhận FIFO**.

## 3. Hệ quả

| Hệ quả | Chi tiết |
|---|---|
| Tích cực | **Không khóa hàng vô ích**: đơn chờ artist không giữ gì, nên artist xác nhận chậm không giam tồn kho |
| Tích cực | **Chống oversell**: xác nhận giữ hàng all-or-nothing trong một transaction; hai đơn tranh đơn vị cuối thì nhiều nhất một thắng |
| Tích cực | **Hai trạng thái trước khi trả tiền nói đúng hai việc khác nhau** ("chờ artist" vs "chờ thanh toán"), và không thêm trạng thái chết (`CONFIRMED` bị gộp) |
| Tích cực | **Hạn thanh toán có một chủ sở hữu**: order đọc `HoldWindow()` của module 05, nên không lặp con số 60 phút |
| Tích cực | **Sửa đơn an toàn**: thay toàn bộ dưới khóa dòng, nhả hàng đúng một lần, luôn để lại lịch sử và tăng phiên bản |
| Tích cực | **Module 08 có seam để nối**: `order_version` cho phép từ chối callback thuộc nội dung cũ, không cần cột `payment` nào trong order |
| Tích cực | **Email không chặn giao dịch**: gửi sau commit, lỗi bị nuốt và ghi log |
| Tiêu cực | **Thay đổi enum là breaking**: mọi client đang xử lý `PENDING_PAYMENT` phải cập nhật sang `PAYMENT_PENDING`; chấp nhận được vì chưa có gì deploy |
| Tiêu cực | **Thêm một mailer dùng chung** thứ hai cạnh sender nội bộ của module 01; hợp nhất hai cái là việc phải làm sau (module 12) |
| Tiêu cực | **`HoldTTL` 15 → 60 phút** làm một giữ chỗ có thể treo lâu hơn gấp bốn; đổi lại hạn thanh toán mới có nghĩa |
| Tiêu cực | **Sửa đơn khi đang chờ thanh toán đúng lúc có callback** phải giải quyết bằng phiên bản: hoặc đã trả tiền (không sửa được), hoặc quay về `PENDING` (callback cũ bị module 08 từ chối) — phần từ chối đó là của module 08 |
| Tiêu cực | **Bảng `order_edit_history`** chỉ đọc, lưu JSONB; đổi trade-off lấy schema chuẩn hoá chỉ để truy vấn theo trường |

**Xem lại quyết định khi**: module 08 Payment gọi `MarkPaid` (khi đó `order_version` phải được đối chiếu
thật, và việc huỷ payment session khi sửa đơn phải được đáp); hoặc khi module 12 ra đời và hai mailer được
hợp nhất; hoặc khi có nhu cầu thật về hoàn tiền/tách đơn.

## 4. Cách tiếp cận bị loại

| Phương án | Vì sao không chọn |
|---|---|
| Giữ một trạng thái `PENDING_PAYMENT` và thêm một cờ "đã xác nhận" | Một cờ song song với trạng thái tạo ra tổ hợp bất khả và không diễn đạt được "chờ artist" là một trạng thái thật |
| Thêm trạng thái `CONFIRMED` riêng cạnh `PAYMENT_PENDING` | Hai trạng thái luôn vào và ra cùng một hành động nên trạng thái thứ hai là trọng lượng chết (Constitution VII) |
| Thêm `REJECTED`/`EXPIRED` | Ai huỷ (artist hay đồng hồ) là một **sự kiện**, không phải một trạng thái; `CANCELLED` đã nghĩa "sẽ không được thực hiện" |
| **Giữ hàng ở checkout** như 009, với cửa sổ dài | Giam hàng trong lúc chờ artist, và làm mỗi lần sửa đơn đắt; trái quy tắc "chỉ giữ khi artist xác nhận" của kế hoạch |
| Xác nhận giữ hàng từng dòng, bỏ dở phần còn lại | Giữ nửa vời mâu thuẫn FR-005 và dễ oversell |
| Giữ hàng **ngoài** transaction của order rồi bù trừ khi lỗi | Bù trừ thêm code và có thể tự hỏng; một transaction là câu trả lời đơn giản, đúng |
| Giữ tên cột `expires_at` (nullable) | Một `expires_at` khi là hạn thanh toán, khi vắng mặt, là mơ hồ cho người đọc sau |
| Cửa sổ thanh toán cấu hình theo từng đơn | YAGNI: một cửa sổ shop-wide là đủ; kế hoạch chốt 60 phút |
| Sửa đơn theo từng dòng (`PATCH`/endpoint riêng mỗi dòng) | Nhiều endpoint và nhiều race hơn; thay toàn bộ dưới khóa dòng là một thao tác rõ ràng |
| Cho khách sửa địa chỉ dạng **nhập tay** | Sẽ tách mô hình địa chỉ khỏi checkout; dùng lại `addressId` giữ một mô hình |
| Cho phép sửa đơn còn **0 dòng** | Phá bất biến "đơn luôn có ≥1 dòng" và tổng dương; khách bỏ hết thì **huỷ đơn** |
| Sửa đơn đang chờ thanh toán **không** nhả hàng | Hàng sẽ bị giữ cho nội dung không còn tồn tại |
| Tăng `order_version` mỗi lần đổi trạng thái | Sinh phiên bản không gắn với **nội dung**, mà đó chính là thứ module 08 phải khớp |
| Không có phiên bản | Module 08 không phân biệt được nội dung cũ/mới khi callback đến muộn |
| Dựng module 12 Notification ngay | Ngoài phạm vi và chưa cần (Constitution VII) |
| Dùng lại sender nội bộ của module 01 | Nó là nội bộ module; import sẽ phá ranh giới module, và nó đang bị sửa song song |
| Port thông báo đặt trong `internal/contracts` | Không có provider module nào để cài đặt nó; port module-local + adapter là hình dạng trung thực |
