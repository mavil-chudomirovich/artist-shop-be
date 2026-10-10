# Module 07 — Order

- **Trạng thái Spec Kit**: Hoàn tất (feature 009 implement + converge; feature 010 — luồng xác nhận &
  chỉnh sửa đơn — implement + converge)
- **Spec**: [`specs/009-order/spec.md`](../../specs/009-order/spec.md),
  [`specs/010-order-confirmation/spec.md`](../../specs/010-order-confirmation/spec.md)
- **Plan**: [`specs/009-order/plan.md`](../../specs/009-order/plan.md),
  [`specs/010-order-confirmation/plan.md`](../../specs/010-order-confirmation/plan.md)
- **Tasks**: [`specs/009-order/tasks.md`](../../specs/009-order/tasks.md),
  [`specs/010-order-confirmation/tasks.md`](../../specs/010-order-confirmation/tasks.md)
- **Contract**: [`specs/010-order-confirmation/contracts/openapi.yaml`](../../specs/010-order-confirmation/contracts/openapi.yaml),
  [`error-codes.md`](../../specs/010-order-confirmation/contracts/error-codes.md) — feature 010 thay thế
  contract của 009
- **Frontend guide**: [`specs/010-order-confirmation/frontend-guide.md`](../../specs/010-order-confirmation/frontend-guide.md)
  — thay thế guide của 009
- **Code**: `internal/modules/order/{domain,application,infrastructure,presentation}`,
  `migrations/00009_order.sql`, `migrations/00011_order_confirmation.sql`,
  `internal/contracts/{cart.go,inventory.go,account.go}`, `internal/share/mailer`
- **Ưu tiên / Giai đoạn**: V1.0
- **Phụ thuộc**: cart, product, inventory, user, auth

## Mục đích

Biến giỏ hàng thành đơn hàng, **đưa đơn qua bước artist xác nhận**, quản lý vòng đời đơn, cho khách
**chỉnh sửa đơn trước khi trả tiền**, và tính tiền chính xác.

## Phạm vi MVP

Có:
- Tạo đơn từ giỏ (checkout): **snapshot** giá và thông tin từng dòng cùng địa chỉ giao, đưa đơn vào
  trạng thái **chờ artist xác nhận** và **không giữ hàng**, rồi **làm rỗng** giỏ — tất cả trong một
  transaction.
- **Artist (operator) xác nhận** đơn: **giữ toàn bộ** hàng all-or-nothing và mở **hạn thanh toán 60
  phút**; **artist từ chối** đơn đang chờ xác nhận.
- Vòng đời đơn rõ ràng (sáu trạng thái), từ chối bước chuyển không hợp lệ và nêu trạng thái hiện tại.
- Khách **chỉnh sửa đơn** (dòng + địa chỉ) khi đơn **chưa trả tiền**; sửa đơn đang chờ thanh toán thì
  **trả hàng** và đưa đơn **quay lại chờ artist xác nhận**.
- Đơn **chờ thanh toán** tự huỷ khi quá hạn 60 phút, trả hàng về khả dụng; đơn **chờ artist** không hết
  hạn theo thời gian.
- Danh sách/chi tiết/huỷ/sửa đơn của khách (chủ sở hữu lấy từ session); admin quản lý mọi đơn, **lọc
  theo trạng thái và sắp xếp** để có hàng đợi xác nhận FIFO.
- Phối hợp giữ/bán/nhả tồn kho qua module 05 khi xác nhận/thanh toán/huỷ/hết hạn/sửa.
- **Thông báo email**: artist được báo khi đơn cần xác nhận; khách được báo mỗi khi trạng thái đơn đổi.
- **Lịch sử chỉnh sửa** + **phiên bản đơn** (`order_version`) để module 08 đối chiếu về sau.
- Chuyển nhượng một đơn **đã trả tiền** sang tài khoản khác đã tồn tại (nêu bằng email).

Không (hoãn):
- Tách đơn (split), đơn định kỳ, hoàn tiền từng phần phức tạp.
- Mã giảm giá/flash sale.
- **Xác nhận thanh toán và nhà cung cấp PayOS; payment attempt/session và IPN/callback đến muộn; huỷ
  payment link PayOS khi sửa đơn** — module 08.
- **Thông báo in-app và các email khác** — module 12.
- **Phí ship và tracking** — module 09 (khách trả phí ship khi nhận hàng, ADR 015 §5).

## Thực thể dữ liệu

- `orders`: chủ sở hữu (`user_id`, **tham chiếu lỏng — không foreign key**, để đơn sống lâu hơn tài
  khoản và chuyển nhượng đổi được), địa chỉ giao **chụp tại checkout** (`recipient_name`,
  `recipient_phone`, `province_code/name`, `ward_code/name`, `street_address`), tổng tiền
  (`total_amount CHECK (> 0)` + `currency`), trạng thái (`status CHECK` một trong **sáu**),
  `confirmed_at` (mốc artist xác nhận, `NULL` khi còn chờ xác nhận), `payment_expires_at` (**hạn thanh
  toán**, `NULL` khi còn chờ artist), `order_version` (**phiên bản nội dung**, `CHECK (>= 1)`, seam nội
  bộ cho module 08), `created_at`/`updated_at`.
- `order_items`: dòng đơn — **snapshot** sản phẩm tại checkout/sửa (`name`, `slug`, `unit_price_amount
  CHECK (> 0)`, `currency`) và `quantity CHECK (>= 1)`, `position`; `product_id` là **tham chiếu thông
  tin — không foreign key** (dòng không bao giờ join lại sản phẩm), FK `order_id` → `orders(id)`
  **ON DELETE CASCADE**, **unique index** trên `(order_id, product_id)`.
- `order_edit_history`: **một dòng mỗi lần sửa được chấp nhận** — `order_id` (FK → `orders(id)` **ON
  DELETE CASCADE**), `version` (phiên bản mà lần sửa tạo ra), `actor_id` (tham chiếu lỏng, không FK),
  `before`/`after` (JSONB — dòng và địa chỉ trước/sau), `created_at`. Đọc trọn, không truy vấn theo
  trường.

## Luồng nghiệp vụ chính

1. **Checkout** (`POST /orders`): đọc giỏ (module 06), **đối chiếu lại** từng dòng với trạng thái
   bán/giá hiện tại (module 04) và tồn khả dụng (module 05); từ chối cả lần nếu bất kỳ dòng không đạt;
   chụp địa chỉ (module 02); tạo đơn `PENDING` với `order_version = 1` và **không** hạn thanh toán;
   **làm rỗng** giỏ — trong **một** transaction. **Không** giữ hàng. Thông báo artist cần xác nhận.
2. **Artist xác nhận** (`POST /admin/orders/{orderId}/confirm`): **giữ toàn bộ** các dòng (module 05)
   trong **một** transaction (all-or-nothing); đặt `confirmed_at = now`, `payment_expires_at = now +
   60 phút`, tăng `order_version`, đưa đơn `PENDING → PAYMENT_PENDING`, ghi audit `ORDER_CONFIRMED`,
   thông báo khách.
3. **Artist từ chối** (`POST /admin/orders/{orderId}/reject`): đơn `PENDING → CANCELLED`; không hàng nào
   bị giữ nên không cần trả; ghi audit `ORDER_REJECTED`, thông báo khách.
4. **Khách sửa đơn** (`PUT /orders/{orderId}`): thay toàn bộ tập dòng + (khi nêu) địa chỉ, đối chiếu lại
   và chụp lại giá hiện tại, tính lại tổng, tăng `order_version`, ghi `order_edit_history`. Đơn `PENDING`
   giữ `PENDING`; đơn `PAYMENT_PENDING` **nhả giữ chỗ** đúng một lần, bỏ hạn thanh toán, và về `PENDING`
   (thông báo artist).
5. **Khách huỷ** (`POST /orders/{orderId}/cancel`): đơn `PENDING` **hoặc** `PAYMENT_PENDING` →
   `CANCELLED`; huỷ đơn `PAYMENT_PENDING` nhả giữ chỗ đúng một lần, đơn `PENDING` không có gì để nhả.
   Hoặc **sweeper** tự huỷ đơn `PAYMENT_PENDING` quá `payment_expires_at`.
6. **Thanh toán thành công** (module 08 điều khiển, chưa có bề mặt HTTP): `MarkPaid` chuyển
   `PAYMENT_PENDING → PAID` và gọi `ApplySale` từng dòng (giữ chỗ → bán), **đúng một lần** kể cả khi sự
   kiện lặp lại.
7. **Giao và hoàn tất**: admin `ship` (`PAID → SHIPPED`), `complete` (`SHIPPED → COMPLETED`); mỗi thao
   tác ghi `audit_logs`.
8. **Chuyển nhượng**: admin `transfer` một đơn **đã trả tiền** cho tài khoản mang email nêu; chỉ đổi
   chủ, **không** đổi trạng thái/dòng/tổng và **không** đổi tồn kho.

## Trạng thái (state machine)

```
PENDING ──confirm──► PAYMENT_PENDING ──pay──► PAID ──ship──► SHIPPED ──complete──► COMPLETED
   │  ▲                      │
   │  └── edit ──────────────┘                 (từ PAYMENT_PENDING; đơn quay lại PENDING)
   ├──reject──► CANCELLED
   ├──cancel (khách)──► CANCELLED
   PAYMENT_PENDING ──cancel (khách) / expire──► CANCELLED
```

Các cạnh **hợp lệ duy nhất**: `PENDING → PAYMENT_PENDING` (artist xác nhận); `PAYMENT_PENDING → PAID`;
`PAID → SHIPPED`; `SHIPPED → COMPLETED`; `PENDING → CANCELLED` (artist từ chối hoặc khách huỷ);
`PAYMENT_PENDING → CANCELLED` (khách huỷ hoặc hết hạn). `edit` không phải một cạnh trạng thái thuần:
nó thay nội dung và có thể đưa `PAYMENT_PENDING → PENDING`. `PAID`/`SHIPPED`/`COMPLETED`/`CANCELLED`
không có đường lùi; `COMPLETED` là cuối. `CONFIRMED` và `REJECTED`/`EXPIRED` **không** là trạng thái
riêng — artist xác nhận chính là mở cửa sổ thanh toán, và từ chối/hết hạn đều là `CANCELLED`. Mọi bước
chuyển là phương thức trên entity, trạng thái **chỉ** đổi qua đó, và `status` là **check constraint** ở
tầng lưu trữ nên không ghi thẳng giá trị ngoài tập được. Một bước chuyển bị từ chối nêu **trạng thái
hiện tại** và để đơn nguyên vẹn.

> Feature **010 thay thế state machine của 009**: `PENDING_PAYMENT` **đổi tên** thành `PAYMENT_PENDING`
> và thêm `PENDING`; migration `00011` backfill giá trị cũ (`UPDATE ... SET status = 'PAYMENT_PENDING'
> WHERE status = 'PENDING_PAYMENT'`) trước khi siết check constraint. Xem
> [decisions/018](../decisions/018-order-confirmation-and-editing.md).

## Giữ chỗ, hết hạn, chỉnh sửa và chuyển nhượng

- **Giữ chỗ chỉ khi artist xác nhận** — năng lực của **module 05**, gọi từ use case confirm (không phải
  của giỏ, không lưu số lượng trong `orders`). Checkout **không** giữ hàng. Xác nhận **giữ** toàn bộ
  dòng all-or-nothing; thanh toán **bán** (giữ chỗ → bán); huỷ/hết hạn/sửa-đang-chờ-thanh-toán **nhả**.
  Cửa sổ giữ chỗ là của module 05 (`HoldTTL`, nay **60 phút**); module 07 đọc cửa sổ qua `HoldWindow()`
  chứ không lặp lại con số.
- **Sweeper hết hạn** là background worker **thứ hai** của dự án, mô phỏng module 05: mỗi 30 giây tìm
  các đơn còn `PAYMENT_PENDING` quá `payment_expires_at` (index `orders_expiry_idx`) và huỷ rồi nhả
  hàng, lặp lại an toàn với sweeper của module 05 (nhả hai lần là no-op), nên hai sweeper **không** nhả
  đúp. Đơn `PENDING` **không** bao giờ bị chọn: nó không giữ hàng và không có hạn.
- **Chỉnh sửa là thay toàn bộ đơn** dưới khóa dòng: nội dung cũ được ghi vào `order_edit_history`, phiên
  bản tăng, và mỗi dòng được chụp lại giá hiện tại. Sửa để **0 dòng** bị từ chối (`ORDER_EMPTY`); sửa
  đơn đã trả tiền bị từ chối (`ORDER_NOT_EDITABLE`).
- **Chuyển nhượng** không phải một trạng thái: nó đổi **chủ sở hữu** sang tài khoản nhận (tra qua
  module 01 bằng email), giữ nguyên dòng/trạng thái/tổng, và **không** chạm tồn kho. Đây là cách thay
  thế cho "huỷ một đơn đã trả tiền" mà hệ thống không có.

## Thông báo (email)

- **Artist** (email shop đã cấu hình) được báo khi đơn **cần xác nhận** — khi checkout tạo đơn, và khi
  một lần sửa để đơn quay lại `PENDING`.
- **Khách** được báo mỗi khi **trạng thái đơn đổi** (xác nhận, từ chối, đã trả tiền, giao, hoàn tất,
  huỷ, hết hạn).
- Gửi **best-effort**: chạy **sau khi transaction commit**, lỗi gửi được ghi log và **không** bao giờ
  làm hỏng hay rollback thao tác đơn. Cơ chế là một port `Notifier` của module + adapter dùng
  `internal/share/mailer` (`net/smtp`, có `LogSender` khi chưa cấu hình SMTP). Thông báo in-app và các
  email khác thuộc **module 12** (đã hoãn).

## Yêu cầu chức năng sơ bộ

- Tổng tiền = tổng `quantity × unit_price_amount` của các dòng, dùng đơn vị tiền tệ nhỏ, **không** số
  thực, **không** làm tròn, **không** gồm phí ship (FR-025, ADR 015 §5).
- Checkout chạy trong **một** transaction; không tạo đơn thiếu dòng, không giữ nửa vời. Xác nhận giữ
  hàng all-or-nothing trong **một** transaction, chống oversell.
- Mỗi dòng là **snapshot**, nên đơn không phụ thuộc sản phẩm/địa chỉ còn tồn tại.
- Trạng thái đơn là state machine sáu trạng thái có danh sách chuyển hợp lệ; ghi thẳng trạng thái là
  bất khả. Mỗi lần sửa và mỗi lần xác nhận tăng `order_version`.
- Chủ sở hữu lấy từ **session**, không bao giờ từ request; mọi route của admin cần vai trò `ADMIN`.
- Sửa/huỷ chỉ khi đơn **chưa trả tiền**; mỗi lần sửa có lịch sử; đơn của khách khác trả cùng
  `404 ORDER_NOT_FOUND`.

## Tiêu chí hoàn thành

- [x] Checkout có test: tạo đơn `PENDING` kèm snapshot dòng + địa chỉ, **không** giữ hàng (khả dụng
  không đổi), làm rỗng giỏ, trong một transaction; và các từ chối — giỏ rỗng, sản phẩm không bán/đã xoá,
  giá đổi sau khi thêm, số lượng vượt khả dụng, khách không có địa chỉ — mỗi ca để lại **không có gì**
  bị tạo (SC-001, SC-002). Hai checkout đồng thời một giỏ sinh **đúng một** đơn trên PostgreSQL thật.
- [x] **Xác nhận** giữ **toàn bộ** dòng all-or-nothing, mở hạn 60 phút, chuyển sang `PAYMENT_PENDING`;
  một dòng thiếu thì bị từ chối và **không** giữ gì; hai xác nhận tranh đơn vị cuối chỉ **một** thắng
  (SC-002, SC-005) — chứng minh trên PostgreSQL thật.
- [x] State machine sáu trạng thái có test **cả hai chiều mỗi cạnh**; bước chuyển bị từ chối nêu trạng
  thái hiện tại và để đơn nguyên vẹn (SC-005). Xác nhận thanh toán **bán** hàng giữ chỗ đúng **một
  lần**; đơn quá hạn **tự huỷ** và nhả hàng đúng **một lần** (SC-004) — chứng minh trên PostgreSQL thật.
- [x] Khách **sửa** đơn trước khi trả tiền: sửa đơn `PENDING` giữ `PENDING`; sửa đơn `PAYMENT_PENDING`
  nhả hàng đúng **một lần** và đưa đơn về `PENDING` với một dòng lịch sử sửa (SC-003).
- [x] Khách chỉ thấy/đổi được đơn của mình (chủ từ session), đơn khách khác trả `404 ORDER_NOT_FOUND`;
  hành động của admin (`confirm`/`reject`/`ship`/`complete`/`transfer`) cần `ADMIN` và ghi `audit_logs`
  (SC-007).
- [x] Artist được báo khi đơn cần xác nhận, khách được báo mỗi lần đổi trạng thái; lỗi gửi thông báo
  **không** làm hỏng đơn (SC-006).
- [x] Chuyển nhượng đơn đã trả tiền đổi chủ, giữ dòng/trạng thái/tổng, không đổi tồn kho (SC-007).

Mười hai endpoint của module (năm của khách dưới `/orders`, bảy của admin dưới `/admin/orders`) nằm ở
[api-reference.md](../api-reference.md) mục 9.

### Ba món nợ đã trả

Feature 009 đóng ba mục mà các feature trước để lại cho module Order:

| Món nợ | Nội dung | Đóng bằng |
|---|---|---|
| Feature 007 `deferred.md` **D1** | Hợp đồng **đặt chỗ** (`InventoryReservation`) liên module chưa publish | Module 07 là consumer đầu tiên: publish `internal/contracts/inventory.go` (`InventoryReservation`), adapter ở module 05, wire ở composition root |
| Feature 007 `deferred.md` **D2** | Không có luồng huỷ đơn đã trả tiền; **chuyển nhượng** không đổi tồn kho | Module 07 giao `POST /admin/orders/{orderId}/transfer` chỉ đổi chủ, không chạm tồn kho |
| Feature 006 & 007 `deferred.md` **D3** | Dòng đơn làm gì khi sản phẩm bị xoá cứng | Module 07 **chụp** sản phẩm vào dòng (`order_items`), nên đơn không trỏ vào sản phẩm đã mất; `product_id` là tham chiếu thông tin, không FK |

### Phần chưa giao

Đây là phần module doc mô tả nhưng feature này **không giao end to end**, **không** phải phần đã xong.
Ghi rõ ở đây để một luật chưa xong không trở nên vô hình khi module được đánh dấu hoàn tất:
[`specs/010-order-confirmation/deferred.md`](../../specs/010-order-confirmation/deferred.md).

| Điểm chưa giao | Vì sao | Gỡ ở đâu |
|---|---|---|
| **Xác nhận thanh toán và PayOS; payment attempt/session; IPN/callback đến muộn** | Bước chuyển `PAYMENT_PENDING → PAID` đã giao và test nhưng **module 08** điều khiển; chưa có bề mặt HTTP, nhà cung cấp PayOS chưa tồn tại | **Module 08 Payment** gọi `MarkPaid` với `sourceReference` ổn định và đối chiếu `order_version`. `deferred.md` D1, D2 |
| **Huỷ payment link PayOS khi sửa đơn** | Sửa đơn chưa trả tiền chỉ **tăng phiên bản** để vô hiệu xác nhận/phiên cũ; huỷ session ở provider là việc của module 08 (chưa tồn tại) | **Module 08 Payment**. `deferred.md` D2 |
| **Thông báo in-app và các email khác** | Feature chỉ gửi email artist/customer; in-app là module 12 (chưa tồn tại) | **Module 12 Notification** (V1.2). `deferred.md` D3 |
| **Phí ship và tracking** | Đơn chỉ gồm tiền hàng; khách trả phí ship khi nhận hàng (ADR 015 §5). Tracking là module 09 | **Module 09 Shipping** (V1.2). `specs/009-order/deferred.md` D2 |
| **Hoàn tiền, tách đơn, mã giảm giá** | Module doc tự hoãn; spec khẳng định **không** ở MVP (Constitution VII, YAGNI) | Feature riêng khi có nhu cầu |

## Ghi chú / câu hỏi mở

- Câu hỏi mở của module — *"Danh sách trạng thái đơn chuẩn cần chốt (kèm sơ đồ chuyển)"* — đã được
  trả lời hai lần: feature 009 chốt một trục trạng thái (`PENDING_PAYMENT → PAID → SHIPPED →
  COMPLETED`, cộng `CANCELLED`, ADR 015 §5); feature **010** chèn bước xác nhận, **đổi tên**
  `PENDING_PAYMENT → PAYMENT_PENDING` và thêm `PENDING` (ADR 018). Một trục trạng thái duy nhất:
  `PAID` nghĩa là đã xác nhận thanh toán; module 08 giữ trạng thái thanh toán riêng trong bảng của nó.
- **Không có endpoint `pay`**: trả tiền là luồng của module 08, không phải của order.
- Quyết định dài hạn ở [decisions/017](../decisions/017-order-checkout.md) (checkout) và
  [decisions/018](../decisions/018-order-confirmation-and-editing.md) (xác nhận & chỉnh sửa).

## Tham chiếu

- `docs/product/backend-spec.md` (Database: `orders`, `order_items`; Inventory)
- [`specs/010-order-confirmation/deferred.md`](../../specs/010-order-confirmation/deferred.md) — các điểm
  cố ý hoãn của feature 010
- [`specs/009-order/deferred.md`](../../specs/009-order/deferred.md) — các điểm cố ý hoãn của feature 009
- [decisions/018](../decisions/018-order-confirmation-and-editing.md) — state machine sáu trạng thái,
  giữ hàng chỉ khi xác nhận, hạn 60 phút, sửa đơn toàn bộ quay về `PENDING`, `order_version`, mailer dùng
  chung
- [decisions/017](../decisions/017-order-checkout.md) — snapshot dòng, `expires_at` do order sở hữu,
  chuyển nhượng là đổi chủ, bước `PAID` giao nhưng để module 08 điều khiển, và `WithTx` tái dùng
  transaction trong context
- [decisions/014](../decisions/014-inventory-hold-and-availability.md) — giữ chỗ và khả dụng của module
  05 mà order tiêu thụ
- [decisions/015](../decisions/015-shop-first-version-plan-and-payos.md) — kế hoạch V1.0 và PayOS
