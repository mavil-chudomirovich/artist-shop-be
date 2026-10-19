# Module 07 — Order

- **Trạng thái Spec Kit**: Hoàn tất (feature 009 implement + converge; feature 010 — luồng xác nhận &
  chỉnh sửa đơn — implement + converge; feature 019 — giao hàng & hoàn tất — implement + converge)
- **Spec**: [`specs/009-order/spec.md`](../../specs/009-order/spec.md),
  [`specs/010-order-confirmation/spec.md`](../../specs/010-order-confirmation/spec.md),
  [`specs/019-order-delivery/spec.md`](../../specs/019-order-delivery/spec.md)
- **Plan**: [`specs/009-order/plan.md`](../../specs/009-order/plan.md),
  [`specs/010-order-confirmation/plan.md`](../../specs/010-order-confirmation/plan.md),
  [`specs/019-order-delivery/plan.md`](../../specs/019-order-delivery/plan.md)
- **Tasks**: [`specs/009-order/tasks.md`](../../specs/009-order/tasks.md),
  [`specs/010-order-confirmation/tasks.md`](../../specs/010-order-confirmation/tasks.md),
  [`specs/019-order-delivery/tasks.md`](../../specs/019-order-delivery/tasks.md)
- **Contract**: [`specs/019-order-delivery/contracts/openapi.yaml`](../../specs/019-order-delivery/contracts/openapi.yaml),
  [`error-codes.md`](../../specs/019-order-delivery/contracts/error-codes.md) — feature 019 thay thế
  contract của 010 (đổi tên `SHIPPED` → `SHIPPING`, thêm `DELIVERED`)
- **Frontend guide**: [`specs/019-order-delivery/frontend-guide.md`](../../specs/019-order-delivery/frontend-guide.md)
  — thay thế guide của 010
- **Code**: `internal/modules/order/{domain,application,infrastructure,presentation}`,
  `migrations/00009_order.sql`, `migrations/00011_order_confirmation.sql`,
  `migrations/00018_order_delivery.sql`,
  `internal/contracts/{cart.go,inventory.go,account.go}`, `internal/share/mailer`,
  `internal/share/media`
- **Ưu tiên / Giai đoạn**: V1.0
- **Phụ thuộc**: cart, product, inventory, user, auth, media (Cloudinary, dùng chung)

## Mục đích

Biến giỏ hàng thành đơn hàng, **đưa đơn qua bước artist xác nhận**, quản lý vòng đời đơn, cho khách
**chỉnh sửa đơn trước khi trả tiền**, **giao hàng có ảnh bằng chứng** và **hoàn tất do khách hoặc tự
động**, và tính tiền chính xác.

## Phạm vi MVP

Có:
- Tạo đơn từ giỏ (checkout): **snapshot** giá và thông tin từng dòng cùng địa chỉ giao, đưa đơn vào
  trạng thái **chờ artist xác nhận** và **không giữ hàng**, rồi **làm rỗng** giỏ — tất cả trong một
  transaction.
- **Artist (operator) xác nhận** đơn: **giữ toàn bộ** hàng all-or-nothing và mở **hạn thanh toán 60
  phút**; **artist từ chối** đơn đang chờ xác nhận.
- Vòng đời đơn rõ ràng (**bảy** trạng thái), từ chối bước chuyển không hợp lệ và nêu trạng thái hiện tại.
- Khách **chỉnh sửa đơn** (dòng + địa chỉ) khi đơn **chưa trả tiền**; sửa đơn đang chờ thanh toán thì
  **trả hàng** và đưa đơn **quay lại chờ artist xác nhận**.
- Đơn **chờ thanh toán** tự huỷ khi quá hạn 60 phút, trả hàng về khả dụng; đơn **chờ artist** không hết
  hạn theo thời gian.
- Danh sách/chi tiết/huỷ/sửa/xác nhận-của-khách đơn của khách (chủ sở hữu lấy từ session); admin quản
  lý mọi đơn, **lọc theo trạng thái và sắp xếp** để có hàng đợi xác nhận FIFO.
- Phối hợp giữ/bán/nhả tồn kho qua module 05 khi xác nhận/thanh toán/huỷ/hết hạn/sửa.
- **Giao hàng có bằng chứng ảnh** (feature 019): operator `ship` (`PAID → SHIPPING`) và `deliver`
  (`SHIPPING → DELIVERED`) mỗi bước kèm **0–5 ảnh tuỳ chọn**, lưu qua **media store dùng chung**; ảnh
  trả trong **chi tiết** đơn (khách và admin), không ở danh sách.
- **Hoàn tất do khách hoặc hết hạn** (feature 019): khách xác nhận đã nhận (`DELIVERED → COMPLETED`),
  hoặc đơn **tự hoàn tất sau 120 giờ (5 × 24h)** kể từ mốc giao bởi sweeper — **đúng một lần**; operator
  **không** còn đánh dấu hoàn tất trực tiếp.
- **Feedback cấp đơn** (feature 019): khách gửi **một** feedback cho artist khi đơn `DELIVERED`/`COMPLETED`.
- **Thông báo email**: artist được báo khi đơn cần xác nhận và khi có feedback; khách được báo mỗi
  khi trạng thái đơn đổi (đang giao, đã giao, hoàn tất, …).
- **Stream SSE live** (feature 020): cùng các sự kiện email trên, đẩy ngay sau commit qua
  `GET /orders/stream` (khách, `order.status_changed` của đơn mình) và `GET /admin/orders/stream`
  (admin, `order.needs_confirmation` + `order.feedback_received`); hub dùng chung ở
  `internal/share/sse/`, live-only không lưu trữ/replay.
- **Lịch sử chỉnh sửa** + **phiên bản đơn** (`order_version`) để module 08 đối chiếu về sau.
- Chuyển nhượng một đơn **đã trả tiền** sang tài khoản khác đã tồn tại (nêu bằng email).

Không (hoãn):
- Tách đơn (split), đơn định kỳ, hoàn tiền từng phần phức tạp.
- Mã giảm giá/flash sale.
- **Xác nhận thanh toán và nhà cung cấp PayOS; payment attempt/session và IPN/callback đến muộn; huỷ
  payment link PayOS khi sửa đơn** — module 08.
- **Thông báo in-app và các email khác** — module 12.
- **Phí ship và tracking** — module 09 (khách trả phí ship khi nhận hàng, ADR 015 §5).
- **Ca giao thất bại / khách không nhận hàng, trả hàng/hoàn tiền** — hoãn (feature 019,
  [`specs/019-order-delivery/deferred.md`](../../specs/019-order-delivery/deferred.md)); đơn có thể dừng
  ở `SHIPPING`/`DELIVERED`, **không** có trạng thái `FAILED`/`RETURNED`.
- **Phạm vi chuyển nhượng theo trạng thái giao hàng mới** (`SHIPPING`/`DELIVERED`) — hoãn; feature 019
  không đổi cơ chế chuyển nhượng ngoài việc áp tên trạng thái mới.

## Thực thể dữ liệu

- `orders`: chủ sở hữu (`user_id`, **tham chiếu lỏng — không foreign key**, để đơn sống lâu hơn tài
  khoản và chuyển nhượng đổi được), địa chỉ giao **chụp tại checkout** (`recipient_name`,
  `recipient_phone`, `province_code/name`, `ward_code/name`, `street_address`), tổng tiền
  (`total_amount CHECK (> 0)` + `currency`), trạng thái (`status CHECK` một trong **bảy**),
  `confirmed_at` (mốc artist xác nhận, `NULL` khi còn chờ xác nhận), `payment_expires_at` (**hạn thanh
  toán**, `NULL` khi còn chờ artist), `delivered_at` (**mốc giao thành công**, feature 019 — neo cửa sổ
  tự hoàn tất 120 giờ, `NULL` trước khi giao), `completed_at` (mốc hoàn tất, do khách hoặc sweeper),
  `completed_by` (khách đã xác nhận; **tham chiếu lỏng, không FK**, `NULL` khi sweeper hoàn tất),
  `order_version` (**phiên bản nội dung**, `CHECK (>= 1)`, seam nội
  bộ cho module 08), `created_at`/`updated_at`. Index `orders_completion_idx (status, delivered_at)`
  phục vụ sweep tự hoàn tất.
- `order_items`: dòng đơn — **snapshot** thứ đã mua tại checkout/sửa: tên sản phẩm (`name`), **mã
  sản phẩm** (`product_code`), **tên variant** (`variant_name`), **SKU** (`sku`), `slug`,
  `unit_price_amount CHECK (> 0)`, `currency`, `quantity CHECK (>= 1)`, `position`; `product_id` và
  `variant_id` là **tham chiếu thông tin — không foreign key** (dòng không bao giờ join lại sản
  phẩm), FK `order_id` → `orders(id)` **ON DELETE CASCADE**, **unique index** trên
  `(order_id, product_code, sku)` — **mỗi cặp (mã sản phẩm, SKU) là một dòng đơn**, nên một sản phẩm
  mua hai variant cho ra hai dòng.
- `order_edit_history`: **một dòng mỗi lần sửa được chấp nhận** — `order_id` (FK → `orders(id)` **ON
  DELETE CASCADE**), `version` (phiên bản mà lần sửa tạo ra), `actor_id` (tham chiếu lỏng, không FK),
  `before`/`after` (JSONB — dòng và địa chỉ trước/sau), `created_at`. Đọc trọn, không truy vấn theo
  trường.
- `order_delivery_photos` (feature 019): ảnh bằng chứng giao hàng — `id`, `order_id` (FK → `orders(id)`
  **ON DELETE CASCADE**), `stage` (CHECK `SHIPPING`/`DELIVERED` — bước giao mà ảnh làm bằng chứng),
  `public_id` (định danh của media provider), `url`, `width`/`height`, `position` (thứ tự trong stage),
  `created_at`. **Nhị phân không lưu trong DB**: chỉ tham chiếu tới media provider. Index
  `(order_id, stage, position, id)`. Trần **5 ảnh mỗi stage** là **đếm** (không phải unique index) nên
  ép trong use case dưới khóa dòng.
- `order_feedback` (feature 019): feedback cấp đơn — `id`, `order_id` (FK → `orders(id)` **ON DELETE
  CASCADE**), `author_id` (tham chiếu lỏng, không FK), `content` (CHECK `length(btrim(content)) BETWEEN
  1 AND 2000`), `created_at`. **Unique index `(order_id)`**: mỗi đơn **tối đa một** feedback, và đây là
  ràng buộc ở tầng lưu trữ.

## Luồng nghiệp vụ chính

1. **Checkout** (`POST /orders`): đọc giỏ (module 06), **đối chiếu lại** từng dòng với trạng thái
   bán/giá hiện tại của **variant** (module 04) và tồn khả dụng (module 05); từ chối cả lần nếu bất
   kỳ dòng không đạt; chụp địa chỉ (module 02) và chụp dòng (tên sản phẩm + mã sản phẩm + tên variant
   + SKU + giá + số lượng); tạo đơn `PENDING` với `order_version = 1` và **không** hạn thanh toán;
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
7. **Giao hàng có ảnh bằng chứng** (feature 019): operator `ship` (`PAID → SHIPPING`) và `deliver`
   (`SHIPPING → DELIVERED`, ghi `delivered_at`); mỗi bước kèm **0–5 ảnh tuỳ chọn** lưu qua **media
   store dùng chung** và ghi `audit_logs` (`ORDER_SHIPPED`/`ORDER_DELIVERED`); khách được thông báo.
8. **Hoàn tất** (feature 019): khách `complete` một đơn `DELIVERED` **của mình**, hoặc **sweeper tự hoàn
   tất sau 120 giờ** kể từ `delivered_at`; ghi `completed_at`/`completed_by` (không tác nhân khi
   sweeper), audit `ORDER_COMPLETED`, thông báo khách. Operator **không** đánh dấu hoàn tất trực tiếp.
9. **Feedback cấp đơn** (feature 019): khách gửi **một** feedback cho đơn `DELIVERED`/`COMPLETED` của
   mình; lưu `order_feedback` và thông báo artist.
10. **Chuyển nhượng**: admin `transfer` một đơn **đã trả tiền** cho tài khoản mang email nêu; chỉ đổi
   chủ, **không** đổi trạng thái/dòng/tổng và **không** đổi tồn kho.

## Trạng thái (state machine)

```
PENDING ──confirm──► PAYMENT_PENDING ──pay──► PAID ──ship──► SHIPPING ──deliver──► DELIVERED
   │  ▲                      │                                                          │
   │  └── edit ──────────────┘                 (từ PAYMENT_PENDING; đơn quay lại PENDING) │
   ├──reject──► CANCELLED                                        khách xác nhận / tự động │
   ├──cancel (khách)──► CANCELLED                                sau 5 ngày (120h) ──────┤
   PAYMENT_PENDING ──cancel (khách) / expire──► CANCELLED                               ▼
                                                                                   COMPLETED
```

Các cạnh **hợp lệ duy nhất**: `PENDING → PAYMENT_PENDING` (artist xác nhận); `PAYMENT_PENDING → PAID`;
`PAID → SHIPPING` (operator `ship`); `SHIPPING → DELIVERED` (operator `deliver`); `DELIVERED →
COMPLETED` (khách xác nhận **hoặc** sweeper tự hoàn tất sau 120 giờ); `PENDING → CANCELLED` (artist từ
chối hoặc khách huỷ); `PAYMENT_PENDING → CANCELLED` (khách huỷ hoặc hết hạn). `edit` không phải một
cạnh trạng thái thuần: nó thay nội dung và có thể đưa `PAYMENT_PENDING → PENDING`.
`PAID`/`SHIPPING`/`DELIVERED`/`COMPLETED`/`CANCELLED` không có đường lùi; `COMPLETED` là cuối.
`CONFIRMED` và `REJECTED`/`EXPIRED` **không** là trạng thái riêng — artist xác nhận chính là mở cửa sổ
thanh toán, và từ chối/hết hạn đều là `CANCELLED`. Mọi bước chuyển là phương thức trên entity, trạng
thái **chỉ** đổi qua đó, và `status` là **check constraint** ở tầng lưu trữ nên không ghi thẳng giá trị
ngoài tập được. Một bước chuyển bị từ chối nêu **trạng thái hiện tại** và để đơn nguyên vẹn.

> Feature **010 thay thế state machine của 009**: `PENDING_PAYMENT` **đổi tên** thành `PAYMENT_PENDING`
> và thêm `PENDING`; migration `00011` backfill giá trị cũ (`UPDATE ... SET status = 'PAYMENT_PENDING'
> WHERE status = 'PENDING_PAYMENT'`) trước khi siết check constraint. Xem
> [decisions/018](../decisions/018-order-confirmation-and-editing.md).
>
> Feature **019 thay thế state machine của 010** ở các cạnh **sau `PAID`**: `SHIPPED` **đổi tên**
> `SHIPPING` (đang giao), thêm `DELIVERED` (giao thành công), và `COMPLETED` nay do **khách hoặc
> sweeper** điều khiển thay vì admin. Migration `00018` drop check cũ, backfill `SHIPPED → SHIPPING`,
> rồi siết check **bảy** giá trị. Xem
> [decisions/023](../decisions/023-order-delivery-and-completion.md).

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

## Giao hàng có bằng chứng, hoàn tất và feedback (feature 019)

- **Hai bước giao, có ảnh tuỳ chọn**: operator `ship` (`PAID → SHIPPING`) khi bàn giao cho bên vận
  chuyển, rồi `deliver` (`SHIPPING → DELIVERED`) khi hàng tới tay khách; mỗi bước nhận **0–5 ảnh**. Ảnh
  là **bằng chứng giao hàng**, không phải nội dung sản phẩm, nên chúng lưu qua **media store dùng
  chung** (`internal/share/media`) — module **application** import **chỉ interface `Store`** (alias
  `MediaStore`); **nhị phân không vào database**, chỉ ghi tham chiếu (`public_id`, `url`,
  `width`/`height`) vào `order_delivery_photos`.
- **Kiểm ảnh trước khi lưu**: định dạng nhận theo **chữ ký nội dung** (JPEG/PNG/WebP), kích thước trần
  **2 MB/ảnh**, số lượng trần **5/step** — tất cả ở trần **trong `domain` của order** (stdlib-only,
  mirror avatar module 02 và ảnh module 04), nên **`domain` không import gói media nào** và một yêu cầu
  quá trần bị từ chối **trước khi** gọi nhà cung cấp media. Upload chạy **ngoài** transaction; hàng ghi
  chèn **trong** transaction dưới khóa dòng (đếm lại dưới khóa để nhiều nhất 5).
- **Hoàn tất do khách hoặc theo thời gian**: khách xác nhận đã nhận đơn `DELIVERED` của mình → đơn
  `COMPLETED`, ghi `completed_at`/`completed_by`; nếu khách im lặng, **sweeper** tự hoàn tất đơn
  `DELIVERED` quá **120 giờ (5 × 24h)** kể từ `delivered_at` (index `orders_completion_idx`), **đúng
  một lần**. Cả hai đường cùng chạy một bước DOMAIN `DELIVERED → COMPLETED` dưới khóa dòng, nên nếu
  khách xác nhận **đúng lúc** sweeper chạy, đơn vẫn `COMPLETED` **một lần**. Operator **không** hoàn tất
  thay khách.
- **Feedback cấp đơn**: khách gửi **một** feedback (1–2000 ký tự) cho đơn `DELIVERED`/`COMPLETED` của
  mình; `order_feedback` có **unique index trên `order_id`** nên "một feedback mỗi đơn" là ràng buộc ở
  tầng lưu trữ. Artist được báo qua email.
- **Không đụng tiền/tồn**: mọi bước giao và hoàn tất **không** tạo `inventory_transactions` và **không**
  đổi `total_amount` — hàng đã bán ở bước `PAID`.
- **Morning note — chuyển nhượng**: feature 019 **không** đổi cơ chế chuyển nhượng; nó chỉ áp tên trạng
  thái mới. Giới hạn/mở rộng chuyển nhượng theo `SHIPPING`/`DELIVERED` là **hoãn** (feature 019
  `deferred.md`).

## Thông báo (email)

- **Artist** (email shop đã cấu hình) được báo khi đơn **cần xác nhận** — khi checkout tạo đơn, và khi
  một lần sửa để đơn quay lại `PENDING`; và khi khách gửi **feedback** (feature 019).
- **Khách** được báo mỗi khi **trạng thái đơn đổi** (xác nhận, từ chối, đã trả tiền, đang giao, đã
  giao, hoàn tất, huỷ, hết hạn) — gồm cả lần **tự hoàn tất** của sweeper.
- Gửi **best-effort**: chạy **sau khi transaction commit**, lỗi gửi được ghi log và **không** bao giờ
  làm hỏng hay rollback thao tác đơn. Cơ chế là một port `Notifier` của module + adapter dùng
  `internal/share/mailer` (`net/smtp`, có `LogSender` khi chưa cấu hình SMTP). Thông báo in-app và các
  email khác thuộc **module 12** (đã hoãn).

## Yêu cầu chức năng sơ bộ

- Tổng tiền = tổng `quantity × unit_price_amount` của các dòng, dùng đơn vị tiền tệ nhỏ, **không** số
  thực, **không** làm tròn, **không** gồm phí ship (FR-025, ADR 015 §5).
- Checkout chạy trong **một** transaction; không tạo đơn thiếu dòng, không giữ nửa vời. Xác nhận giữ
  hàng all-or-nothing trong **một** transaction, chống oversell.
- Mỗi dòng là **snapshot** (tên sản phẩm, mã sản phẩm, tên variant, SKU, giá, số lượng), nên đơn
  không phụ thuộc sản phẩm/variant/địa chỉ còn tồn tại.
- Trạng thái đơn là state machine **bảy** trạng thái có danh sách chuyển hợp lệ; ghi thẳng trạng thái
  là bất khả (check constraint ở tầng lưu trữ). Mỗi lần sửa và mỗi lần xác nhận tăng `order_version`.
- Chủ sở hữu lấy từ **session**, không bao giờ từ request; mọi route của admin cần vai trò `ADMIN`.
- Sửa/huỷ chỉ khi đơn **chưa trả tiền**; mỗi lần sửa có lịch sử; đơn của khách khác trả cùng
  `404 ORDER_NOT_FOUND`.
- **Ảnh bằng chứng giao hàng** (feature 019) MUST kiểm định dạng/kích thước/số lượng **trước khi** lưu
  trữ, lưu qua media provider (không nhị phân trong DB), và MUST trả lại trong **chi tiết** đơn (khách
  và admin), không ở danh sách.
- **Hoàn tất** (feature 019) MUST do **khách** xác nhận `DELIVERED → COMPLETED`, hoặc **tự động đúng
  một lần** sau **120 giờ** kể từ mốc giao; đơn chưa `DELIVERED` MUST **không** tự hoàn tất.
- **Feedback cấp đơn** (feature 019) MUST **một lần mỗi đơn** (unique index), 1–2000 ký tự, chỉ khi đơn
  `DELIVERED`/`COMPLETED`.

## Tiêu chí hoàn thành

- [x] Checkout có test: tạo đơn `PENDING` kèm snapshot dòng (tên sản phẩm + mã sản phẩm + tên variant
  + SKU + giá) + địa chỉ, **không** giữ hàng (khả dụng không đổi), làm rỗng giỏ, trong một transaction;
  và các từ chối — giỏ rỗng, variant không bán/đã xoá, giá đổi sau khi thêm, số lượng vượt khả dụng,
  khách không có địa chỉ — mỗi ca để lại **không có gì** bị tạo (SC-001, SC-002). Một sản phẩm mua hai
  variant cho ra **hai** dòng (mỗi cặp mã sản phẩm + SKU một dòng). Hai checkout đồng thời một giỏ sinh
  **đúng một** đơn trên PostgreSQL thật.
- [x] **Xác nhận** giữ **toàn bộ** dòng all-or-nothing, mở hạn 60 phút, chuyển sang `PAYMENT_PENDING`;
  một dòng thiếu thì bị từ chối và **không** giữ gì; hai xác nhận tranh đơn vị cuối chỉ **một** thắng
  (SC-002, SC-005) — chứng minh trên PostgreSQL thật.
- [x] State machine **bảy** trạng thái có test **cả hai chiều mỗi cạnh**; bước chuyển bị từ chối nêu
  trạng thái hiện tại và để đơn nguyên vẹn (SC-005). Xác nhận thanh toán **bán** hàng giữ chỗ đúng **một
  lần**; đơn quá hạn **tự huỷ** và nhả hàng đúng **một lần** (SC-004) — chứng minh trên PostgreSQL thật.
- [x] Khách **sửa** đơn trước khi trả tiền: sửa đơn `PENDING` giữ `PENDING`; sửa đơn `PAYMENT_PENDING`
  nhả hàng đúng **một lần** và đưa đơn về `PENDING` với một dòng lịch sử sửa (SC-003).
- [x] Khách chỉ thấy/đổi được đơn của mình (chủ từ session), đơn khách khác trả `404 ORDER_NOT_FOUND`;
  hành động của admin (`confirm`/`reject`/`ship`/`deliver`/`transfer`) cần `ADMIN` và ghi `audit_logs`
  (SC-007).
- [x] Artist được báo khi đơn cần xác nhận, khách được báo mỗi lần đổi trạng thái; lỗi gửi thông báo
  **không** làm hỏng đơn (SC-006).
- [x] Chuyển nhượng đơn đã trả tiền đổi chủ, giữ dòng/trạng thái/tổng, không đổi tồn kho (SC-007).
- [x] **Giao hàng có ảnh** (feature 019): `PAID → SHIPPING` và `SHIPPING → DELIVERED` kèm **0–5 ảnh**;
  ảnh sai định dạng/quá cỡ/quá 5 bị từ chối **trước khi** gọi media; ảnh trả trong chi tiết đơn; bước
  giao **không** đụng tồn kho/tổng tiền (SC-001, SC-004, FR-020) — chốt bằng test và integration.
- [x] **Hoàn tất** (feature 019): khách xác nhận đơn `DELIVERED` của mình → `COMPLETED` ghi
  `completed_at`/`completed_by`; đơn `DELIVERED` quá **120 giờ** tự hoàn tất **đúng một lần** (kể cả
  khi lặp, và khi đua với khách xác nhận); đơn chưa `DELIVERED` **không** tự hoàn tất (SC-002).
- [x] **Feedback cấp đơn** (feature 019): một feedback mỗi đơn (unique index chặn lần hai trên PostgreSQL
  thật); từ chối khi đơn chưa giao; nội dung 1–2000 ký tự; artist được báo (SC-005).

Mười bốn endpoint của module (**bảy** của khách dưới `/orders`, **bảy** của admin dưới `/admin/orders`)
nằm ở [api-reference.md](../api-reference.md) mục 9.

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
[`specs/010-order-confirmation/deferred.md`](../../specs/010-order-confirmation/deferred.md) và
[`specs/019-order-delivery/deferred.md`](../../specs/019-order-delivery/deferred.md).

| Điểm chưa giao | Vì sao | Gỡ ở đâu |
|---|---|---|
| **Xác nhận thanh toán và PayOS; payment attempt/session; IPN/callback đến muộn** | Bước chuyển `PAYMENT_PENDING → PAID` đã giao và test nhưng **module 08** điều khiển; chưa có bề mặt HTTP, nhà cung cấp PayOS chưa tồn tại | **Module 08 Payment** gọi `MarkPaid` với `sourceReference` ổn định và đối chiếu `order_version`. `deferred.md` D1, D2 |
| **Huỷ payment link PayOS khi sửa đơn** | Sửa đơn chưa trả tiền chỉ **tăng phiên bản** để vô hiệu xác nhận/phiên cũ; huỷ session ở provider là việc của module 08 (chưa tồn tại) | **Module 08 Payment**. `deferred.md` D2 |
| **Thông báo in-app và các email khác** | Feature chỉ gửi email artist/customer; in-app là module 12 (chưa tồn tại) | **Module 12 Notification** (V1.2). `specs/019-order-delivery/deferred.md` D5 |
| **Phí ship và tracking hãng vận chuyển** | Đơn chỉ gồm tiền hàng; khách trả phí ship khi nhận hàng (ADR 015 §5). Tracking là module 09 | **Module 09 Shipping** (V1.2). `specs/019-order-delivery/deferred.md` D3 |
| **Ca giao thất bại / khách không nhận hàng, trả hàng/hoàn tiền** | Không có trạng thái `FAILED`/`RETURNED` và không có luồng hoàn tiền (Constitution VII, YAGNI); đơn có thể dừng ở `SHIPPING`/`DELIVERED` | **Feature riêng** khi có nhu cầu. `specs/019-order-delivery/deferred.md` D1 |
| **Phạm vi chuyển nhượng theo `SHIPPING`/`DELIVERED`** | Feature 019 giữ nguyên cơ chế chuyển nhượng, chỉ áp tên trạng thái mới; chưa chốt giới hạn theo trạng thái giao | **Feature riêng**. `specs/019-order-delivery/deferred.md` D2 |
| **Đánh giá sản phẩm (có điểm số, kiểm duyệt)** | Feedback cấp đơn của feature 019 không phải review sản phẩm | **Module 13 Review** (V2.0). `specs/019-order-delivery/deferred.md` D4 |
| **Hoàn tiền, tách đơn, mã giảm giá** | Module doc tự hoãn; spec khẳng định **không** ở MVP (Constitution VII, YAGNI) | Feature riêng khi có nhu cầu |

## Ghi chú / câu hỏi mở

- Câu hỏi mở của module — *"Danh sách trạng thái đơn chuẩn cần chốt (kèm sơ đồ chuyển)"* — đã được
  trả lời **ba** lần: feature 009 chốt một trục trạng thái (`PENDING_PAYMENT → PAID → SHIPPED →
  COMPLETED`, cộng `CANCELLED`, ADR 015 §5); feature **010** chèn bước xác nhận, **đổi tên**
  `PENDING_PAYMENT → PAYMENT_PENDING` và thêm `PENDING` (ADR 018); feature **019** **đổi tên**
  `SHIPPED → SHIPPING`, thêm `DELIVERED`, và chuyển quyền hoàn tất từ operator sang **khách hoặc sweeper**
  120 giờ (ADR 023) — bộ trạng thái còn **bảy** giá trị. Một trục trạng thái duy nhất: `PAID` nghĩa là
  đã xác nhận thanh toán; module 08 giữ trạng thái thanh toán riêng trong bảng của nó.
- **Không có endpoint `pay`**: trả tiền là luồng của module 08, không phải của order.
- Quyết định dài hạn ở [decisions/017](../decisions/017-order-checkout.md) (checkout),
  [decisions/018](../decisions/018-order-confirmation-and-editing.md) (xác nhận & chỉnh sửa) và
  [decisions/023](../decisions/023-order-delivery-and-completion.md) (giao hàng & hoàn tất).

## Tham chiếu

- `docs/product/backend-spec.md` (Database: `orders`, `order_items`; Inventory)
- [`specs/019-order-delivery/deferred.md`](../../specs/019-order-delivery/deferred.md) — các điểm cố ý hoãn
  của feature 019
- [`specs/010-order-confirmation/deferred.md`](../../specs/010-order-confirmation/deferred.md) — các điểm
  cố ý hoãn của feature 010
- [`specs/009-order/deferred.md`](../../specs/009-order/deferred.md) — các điểm cố ý hoãn của feature 009
- [decisions/023](../decisions/023-order-delivery-and-completion.md) — state machine bảy trạng thái sau
  `PAID`, ảnh bằng chứng giao hàng, hoàn tất do khách/sweeper 120 giờ, feedback cấp đơn, luật ảnh
  dependency-free trong `domain`
- [decisions/018](../decisions/018-order-confirmation-and-editing.md) — state machine sáu trạng thái,
  giữ hàng chỉ khi xác nhận, hạn 60 phút, sửa đơn toàn bộ quay về `PENDING`, `order_version`, mailer dùng
  chung
- [decisions/017](../decisions/017-order-checkout.md) — snapshot dòng, `expires_at` do order sở hữu,
  chuyển nhượng là đổi chủ, bước `PAID` giao nhưng để module 08 điều khiển, và `WithTx` tái dùng
  transaction trong context
- [decisions/014](../decisions/014-inventory-hold-and-availability.md) — giữ chỗ và khả dụng của module
  05 mà order tiêu thụ
- [decisions/021](../decisions/021-product-sku-model.md) — dòng đơn chụp mã sản phẩm + SKU + tên
  variant; đơn vị mua là variant
- [decisions/015](../decisions/015-shop-first-version-plan-and-payos.md) — kế hoạch V1.0 và PayOS
