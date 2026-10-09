# Module 07 — Order

- **Trạng thái Spec Kit**: Hoàn tất (implement + converge)
- **Spec**: [`specs/009-order/spec.md`](../../specs/009-order/spec.md)
- **Plan**: [`specs/009-order/plan.md`](../../specs/009-order/plan.md)
- **Tasks**: [`specs/009-order/tasks.md`](../../specs/009-order/tasks.md)
- **Contract**: [`specs/009-order/contracts/openapi.yaml`](../../specs/009-order/contracts/openapi.yaml),
  [`error-codes.md`](../../specs/009-order/contracts/error-codes.md)
- **Code**: `internal/modules/order/{domain,application,infrastructure,presentation}`,
  `migrations/00009_order.sql`, `internal/contracts/{cart.go,inventory.go,account.go}` (ba hợp đồng
  liên module)
- **Ưu tiên / Giai đoạn**: V1.0
- **Phụ thuộc**: cart, product, inventory, user, auth

## Mục đích

Biến giỏ hàng thành đơn hàng, quản lý vòng đời đơn và tính tiền chính xác.

## Phạm vi MVP

Có:
- Tạo đơn từ giỏ (checkout): **snapshot** giá và thông tin từng dòng cùng địa chỉ giao, **giữ** hàng
  cho khách trong lúc chờ thanh toán, và **làm rỗng** giỏ — tất cả trong một transaction.
- Vòng đời đơn rõ ràng, từ chối bước chuyển không hợp lệ và nêu trạng thái hiện tại.
- Đơn chưa trả tiền **tự huỷ** khi quá cửa sổ giữ chỗ, trả hàng về khả dụng.
- Danh sách/chi tiết/huỷ đơn của khách (chủ sở hữu lấy từ session); admin quản lý mọi đơn.
- Phối hợp giữ/bán/nhả tồn kho qua module 05 khi checkout/thanh toán/huỷ.
- Chuyển nhượng một đơn **đã trả tiền** sang tài khoản khác đã tồn tại (nêu bằng email).

Không (hoãn):
- Tách đơn (split), đơn định kỳ, hoàn tiền từng phần phức tạp.
- Mã giảm giá/flash sale.
- **Xác nhận thanh toán và nhà cung cấp PayOS** — module 08.
- **Phí ship và tracking** — module 09 (khách trả phí ship khi nhận hàng, ADR 015 §5).

## Thực thể dữ liệu

- `orders`: chủ sở hữu (`user_id`, **tham chiếu lỏng — không foreign key**, để đơn sống lâu hơn tài
  khoản và chuyển nhượng đổi được), địa chỉ giao **chụp tại checkout** (`recipient_name`,
  `recipient_phone`, `province_code/name`, `ward_code/name`, `street_address`), tổng tiền
  (`total_amount CHECK (> 0)` + `currency`), trạng thái (`status CHECK` một trong năm),
  `expires_at` (mốc đơn tự huỷ), `created_at`/`updated_at`.
- `order_items`: dòng đơn — **snapshot** sản phẩm tại checkout (`name`, `slug`, `unit_price_amount
  CHECK (> 0)`, `currency`) và `quantity CHECK (>= 1)`, `position`; `product_id` là **tham chiếu
  thông tin — không foreign key** (dòng không bao giờ join lại sản phẩm), FK `order_id` →
  `orders(id)` **ON DELETE CASCADE**, **unique index** trên `(order_id, product_id)`.

## Luồng nghiệp vụ chính

1. **Checkout** (`POST /orders`): đọc giỏ (module 06), **đối chiếu lại** từng dòng với trạng thái
   bán/giá hiện tại (module 04) và tồn khả dụng (module 05); từ chối cả lần nếu bất kỳ dòng không
   đạt; chụp địa chỉ (module 02); tạo đơn `PENDING_PAYMENT` với `expires_at = now + HoldTTL`; **giữ**
   hàng từng dòng (module 05); **làm rỗng** giỏ — trong **một** transaction.
2. **Thanh toán thành công** (module 08 điều khiển, chưa có bề mặt HTTP): `MarkPaid` chuyển
   `PENDING_PAYMENT → PAID` và gọi `ApplySale` từng dòng (giữ chỗ → bán), **đúng một lần** kể cả khi
   sự kiện lặp lại.
3. **Hủy đơn**: khách huỷ (`POST /orders/{orderId}/cancel`) hoặc **sweeper** tự huỷ khi quá
   `expires_at`; đơn `PENDING_PAYMENT → CANCELLED`, **nhả giữ chỗ** (không đổi số vật lý), mỗi lần
   huỷ/hết hạn áp dụng **đúng một lần**.
4. **Giao và hoàn tất**: admin `ship` (`PAID → SHIPPED`), `complete` (`SHIPPED → COMPLETED`); mỗi
   thao tác ghi `audit_logs`.
5. **Chuyển nhượng**: admin `transfer` một đơn **đã trả tiền** cho tài khoản mang email nêu; chỉ đổi
   chủ, **không** đổi trạng thái/dòng/tổng và **không** đổi tồn kho.

## Trạng thái (state machine)

```
PENDING_PAYMENT ──pay──► PAID ──ship──► SHIPPED ──complete──► COMPLETED
        │
        └──cancel (khách hoặc hết hạn)──► CANCELLED   (trạng thái cuối)
```

Các cạnh **hợp lệ duy nhất**: `PENDING_PAYMENT → PAID`; `PAID → SHIPPED`; `SHIPPED → COMPLETED`;
`PENDING_PAYMENT → CANCELLED`. `PAID`/`SHIPPED`/`COMPLETED`/`CANCELLED` không có đường lùi;
`COMPLETED` là cuối. Mọi bước chuyển là phương thức trên entity, trạng thái **chỉ** đổi qua đó, và
`status` là **check constraint** ở tầng lưu trữ nên không ghi thẳng giá trị ngoài tập được. Một bước
chuyển bị từ chối nêu **trạng thái hiện tại** và để đơn nguyên vẹn.

## Giữ chỗ, hết hạn và chuyển nhượng

- **Giữ chỗ** là năng lực của **module 05**, gọi khi checkout (không phải của giỏ, không lưu số lượng
  trong `orders`). Checkout **giữ** từng dòng; thanh toán **bán** (giữ chỗ → bán); huỷ/hết hạn **nhả**.
  Cửa sổ giữ chỗ và quy tắc hết hạn là của module 05; module 07 đọc cửa sổ qua `HoldWindow()` chứ
  không lặp lại con số (FR-016).
- **Sweeper hết hạn** là background worker **thứ hai** của dự án, mô phỏng module 05: mỗi 30 giây tìm
  các đơn còn `PENDING_PAYMENT` quá `expires_at` (index `orders_expiry_idx`) và huỷ rồi nhả hàng, lặp
  lại an toàn với sweeper của module 05 (nhả hai lần là no-op), nên hai sweeper **không** nhả đúp.
- **Chuyển nhượng** không phải một trạng thái: nó đổi **chủ sở hữu** sang tài khoản nhận (tra qua
  module 01 bằng email), giữ nguyên dòng/trạng thái/tổng, và **không** chạm tồn kho. Đây là cách thay
  thế cho "huỷ một đơn đã trả tiền" mà hệ thống không có.

## Yêu cầu chức năng sơ bộ

- Tổng tiền = tổng `quantity × unit_price_amount` của các dòng, dùng đơn vị tiền tệ nhỏ, **không** số
  thực, **không** làm tròn, **không** gồm phí ship (FR-008, ADR 015 §5).
- Checkout chạy trong **một** transaction; không tạo đơn thiếu dòng, không giữ nửa vời.
- Mỗi dòng là **snapshot**, nên đơn không phụ thuộc sản phẩm/địa chỉ còn tồn tại.
- Trạng thái đơn là state machine có danh sách chuyển hợp lệ; ghi thẳng trạng thái là bất khả.
- Chủ sở hữu lấy từ **session**, không bao giờ từ request; mọi route của admin cần vai trò `ADMIN`.

## Tiêu chí hoàn thành

- [x] Checkout có test: tạo đơn kèm snapshot dòng + địa chỉ, **giữ** hàng (vật lý không đổi, khả dụng
  giảm), làm rỗng giỏ, trong một transaction; và các từ chối — giỏ rỗng, sản phẩm không bán/đã xoá,
  giá đổi sau khi thêm, số lượng vượt khả dụng, khách không có địa chỉ, hàng không giữ được — mỗi ca
  để lại **không có gì** bị tạo (SC-001, SC-002). Hai checkout đồng thời một giỏ sinh **đúng một**
  đơn trên PostgreSQL thật.
- [x] State machine có test **cả hai chiều mỗi cạnh**; bước chuyển bị từ chối nêu trạng thái hiện tại
  và để đơn nguyên vẹn (SC-003). Xác nhận thanh toán **bán** hàng giữ chỗ đúng **một lần** kể cả khi
  lặp; đơn quá hạn **tự huỷ** và nhả hàng đúng **một lần** (SC-004) — chứng minh trên PostgreSQL thật.
- [x] Khách chỉ thấy/đổi được đơn của mình (chủ từ session), đơn khách khác trả `404 ORDER_NOT_FOUND`;
  hành động của admin cần `ADMIN` và ghi `audit_logs` (SC-005).
- [x] Chuyển nhượng đơn đã trả tiền đổi chủ, giữ dòng/trạng thái/tổng, không đổi tồn kho (SC-006).

Chín endpoint của module (bốn của khách dưới `/orders`, năm của admin dưới `/admin/orders`) nằm ở
[api-reference.md](../api-reference.md) mục 9.

### Ba món nợ đã trả

Feature này đóng ba mục mà các feature trước để lại cho module Order:

| Món nợ | Nội dung | Đóng bằng |
|---|---|---|
| Feature 007 `deferred.md` **D1** | Hợp đồng **đặt chỗ** (`InventoryReservation`) liên module chưa publish | Module 07 là consumer đầu tiên: publish `internal/contracts/inventory.go` (`InventoryReservation`), adapter ở module 05, wire ở composition root |
| Feature 007 `deferred.md` **D2** | Không có luồng huỷ đơn đã trả tiền; **chuyển nhượng** không đổi tồn kho | Module 07 giao `POST /admin/orders/{orderId}/transfer` chỉ đổi chủ, không chạm tồn kho |
| Feature 006 & 007 `deferred.md` **D3** | Dòng đơn làm gì khi sản phẩm bị xoá cứng | Module 07 **chụp** sản phẩm vào dòng (`order_items`), nên đơn không trỏ vào sản phẩm đã mất; `product_id` là tham chiếu thông tin, không FK |

### Phần chưa giao

Đây là phần module doc mô tả nhưng feature này **không giao end to end**, **không** phải phần đã xong.
Ghi rõ ở đây để một luật chưa xong không trở nên vô hình khi module được đánh dấu hoàn tất:
[`specs/009-order/deferred.md`](../../specs/009-order/deferred.md).

| Điểm chưa giao | Vì sao | Gỡ ở đâu |
|---|---|---|
| **Xác nhận thanh toán và PayOS** | Bước chuyển `PENDING_PAYMENT → PAID` đã giao và test nhưng **module 08** điều khiển; chưa có bề mặt HTTP, nhà cung cấp PayOS chưa tồn tại | **Module 08 Payment** gọi `MarkPaid` với `sourceReference` ổn định. `deferred.md` D1 |
| **Phí ship và tracking** | Đơn chỉ gồm tiền hàng; khách trả phí ship khi nhận hàng (ADR 015 §5). Tracking là module 09 | **Module 09 Shipping** (V1.2). `deferred.md` D2 |
| **Hoàn tiền, tách đơn, mã giảm giá** | Module doc tự hoãn; spec khẳng định **không** ở MVP (Constitution VII, YAGNI) | Feature riêng khi có nhu cầu. `deferred.md` D3 |

## Ghi chú / câu hỏi mở

- Câu hỏi mở của module — *"Danh sách trạng thái đơn chuẩn cần chốt (kèm sơ đồ chuyển)"* — đã được
  feature 009 trả lời: **`PENDING_PAYMENT → PAID → SHIPPED → COMPLETED`, cộng `CANCELLED`** (ADR 015
  §5, `specs/009-order/research.md` D9). Một trục trạng thái duy nhất: `paid` nghĩa là đã xác nhận
  thanh toán; module 08 giữ trạng thái thanh toán riêng trong bảng của nó (clarification 2026-10-09).
- **Không có endpoint `pay`**: trả tiền là luồng của module 08, không phải của order (research D13).
- Quyết định dài hạn ở [decisions/017](../decisions/017-order-checkout.md).

## Tham chiếu

- `docs/product/backend-spec.md` (Database: `orders`, `order_items`; Inventory)
- [`specs/009-order/deferred.md`](../../specs/009-order/deferred.md) — các điểm cố ý hoãn
- [decisions/017](../decisions/017-order-checkout.md) — snapshot dòng, `expires_at` do order sở hữu,
  chuyển nhượng là đổi chủ, bước `PAID` giao nhưng để module 08 điều khiển, và `WithTx` tái dùng
  transaction trong context
- [decisions/014](../decisions/014-inventory-hold-and-availability.md) — giữ chỗ và khả dụng của module
  05 mà order tiêu thụ
- [decisions/015](../decisions/015-shop-first-version-plan-and-payos.md) — kế hoạch V1.0 và PayOS
