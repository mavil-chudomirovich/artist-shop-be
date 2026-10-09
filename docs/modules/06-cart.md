# Module 06 — Cart

- **Trạng thái Spec Kit**: Hoàn tất (implement + converge)
- **Spec**: [`specs/008-cart/spec.md`](../../specs/008-cart/spec.md)
- **Plan**: [`specs/008-cart/plan.md`](../../specs/008-cart/plan.md)
- **Tasks**: [`specs/008-cart/tasks.md`](../../specs/008-cart/tasks.md)
- **Contract**: [`specs/008-cart/contracts/openapi.yaml`](../../specs/008-cart/contracts/openapi.yaml),
  [`error-codes.md`](../../specs/008-cart/contracts/error-codes.md)
- **Code**: `internal/modules/cart/{domain,application,infrastructure,presentation}`,
  `migrations/00008_cart.sql`, `internal/contracts/product.go` và
  `internal/contracts/inventory.go` (hai hợp đồng liên module)
- **Ưu tiên / Giai đoạn**: V1.0
- **Phụ thuộc**: product, inventory

## Mục đích

Cho khách đã đăng nhập tập hợp sản phẩm muốn mua trước khi thanh toán.

## Phạm vi MVP

Có:
- Thêm/xóa sản phẩm, cập nhật số lượng.
- Tính tạm tính (giá × số lượng).
- Kiểm tra tồn kho và trạng thái bán tại thời điểm thao tác (khi thêm/đổi) **và mỗi lần đọc**.
- Gắn giỏ với tài khoản (khách đã đăng nhập); mỗi tài khoản **một** giỏ.

Không (hoãn):
- Giỏ cho khách vãng lai (chưa đăng nhập) — cân nhắc sau.
- Lưu giỏ, mã giảm giá, upsell.

## Thực thể dữ liệu

- `carts`: thuộc về người dùng; **unique index** trên `user_id` (một tài khoản một giỏ), FK cascade
  tới `users`.
- `cart_items`: sản phẩm, số lượng (`CHECK (quantity >= 1)`), giá tại thời điểm thêm
  (`unit_price_amount CHECK (> 0)` + `currency`), **unique index** trên `(cart_id, product_id)`
  (một sản phẩm một dòng). `product_id` là **tham chiếu lỏng — không có foreign key**: xoá sản phẩm
  không xoá dòng, để giỏ báo "không còn bán" thay vì âm thầm mất dòng (FR-012).

## Luồng nghiệp vụ chính

1. Khách thêm sản phẩm → dòng mới hoặc nâng số lượng dòng đã có; giá **snapshot** lúc thêm.
2. Khách đổi số lượng/xóa dòng.
3. Khách đọc giỏ → backend **đối chiếu lại** từng dòng với trạng thái bán và tồn khả dụng hiện tại,
   báo `buyable` và (khi còn bán nhưng thiếu) `availableQuantity`.
4. Khách chuyển sang thanh toán → **module 07 Order** (chưa tồn tại); checkout đối chiếu lại giá và
   tồn trước khi tạo đơn. Giỏ **không** giữ chỗ tồn kho.

## Yêu cầu chức năng sơ bộ

- Không thêm sản phẩm không bán được (không ACTIVE).
- Số lượng không vượt tồn kho khả dụng (kể cả khi hai cập nhật tới cùng lúc).
- Tạm tính dùng giá snapshot nhất quán với bước tạo đơn.
- Một người dùng có một giỏ hoạt động.
- Chủ sở hữu lấy từ **session**, không bao giờ từ request; không route nào nêu định danh giỏ.

## Tiêu chí hoàn thành

- [x] CRUD giỏ có test, kể cả ràng buộc tồn kho/trạng thái — thêm/đổi/xoá/đọc, từ chối sản phẩm
  không đang bán (`409 CART_PRODUCT_NOT_PURCHASABLE`) và số lượng vượt tồn
  (`409 CART_QUANTITY_EXCEEDS_AVAILABLE`), sản phẩm không tồn tại (`404 PRODUCT_NOT_FOUND`); ràng
  buộc một-giỏ-một-tài-khoản và một-dòng-một-sản-phẩm nằm ở **tầng lưu trữ** (hai unique index) và
  được chứng minh trên PostgreSQL thật (tag `integration`).
- [x] Tính tạm tính chính xác (dùng đơn vị tiền tệ nhỏ) có test — `int64` minor units, không dùng số
  thực, không làm tròn; `subtotal` là tổng `quantity × unitPrice` (SC-001, SC-005).
- [x] Đọc giỏ **đối chiếu lại** từng dòng (FR-012): `buyable` luôn có; `availableQuantity` chỉ có khi
  dòng còn bán nhưng thiếu; dòng sản phẩm đã xoá có `name`/`slug` là `null` và vẫn sống để khách xoá.
- [x] Một giỏ một tài khoản, không truy cập chéo (FR-001, FR-010): chủ sở hữu từ session, không route
  nào nêu định danh giỏ; request không session → `401 UNAUTHENTICATED`.

Bốn endpoint của khách (`GET /api/v1/cart`, `POST /api/v1/cart/items`,
`PATCH`/`DELETE /api/v1/cart/items/{productId}`) nằm ở [api-reference.md](../api-reference.md) mục 8.

### Phần chưa giao

Đây là phần module doc mô tả nhưng feature này **không giao end to end**, **không** phải phần đã
xong. Ghi rõ ở đây để một luật chưa xong không trở nên vô hình khi module được đánh dấu hoàn tất:
[`specs/008-cart/deferred.md`](../../specs/008-cart/deferred.md).

| Điểm chưa giao | Vì sao | Gỡ ở đâu |
|---|---|---|
| **Checkout biến giỏ thành đơn** | Order (07) chưa tồn tại — chưa có gì trong hệ thống có thể tạo đơn từ giỏ, cũng chưa có nơi đối chiếu lại giá/tồn trước khi thu tiền | **Module 07 Order** wire và test end to end; đối chiếu lại giá (FR-008) và tồn (FR-011) trước khi tạo đơn. `deferred.md` D1 |
| **Giữ chỗ tồn kho khi khách trả tiền** | Là năng lực của module 05, **được gọi lúc thanh toán** chứ không bởi giỏ; giỏ chỉ *kiểm tra* tồn (FR-011). Nguồn sự kiện (đơn/thanh toán) chưa có | Module 07/08 gọi hợp đồng đặt chỗ của module 05 (module 05 `deferred.md` D1, vẫn mở). `deferred.md` D2 |
| **Giỏ vãng lai, giỏ lưu/later, mã giảm giá, upsell** | Module doc tự hoãn; spec khẳng định **không** ở MVP (Constitution VII, YAGNI) | Feature riêng khi có nhu cầu. `deferred.md` D3 |

## Ghi chú / câu hỏi mở

Câu hỏi mở duy nhất của module — *"Giá trong giỏ: cố định theo thời điểm thêm hay cập nhật theo giá
hiện tại?"* — đã được feature 008 trả lời: **giá snapshot lúc thêm, đối chiếu lại ở checkout**. Giá
được chụp khi sản phẩm được thêm và giữ nguyên khi khách đọc giỏ; checkout đối chiếu lại trước khi
thu tiền, nên giỏ không bao giờ âm thầm tính giá cũ (FR-008, `specs/008-cart/research.md` D4). Quyết
định này ở [decisions/016](../decisions/016-cart-singleton-loose-reference-and-snapshot.md).

Giỏ **không** giữ chỗ tồn kho; việc giữ chỗ là của module 05 và xảy ra khi khách bắt đầu thanh toán
(FR-011, `research.md` D12).

Hợp đồng HTTP của module nằm ở [api-reference.md](../api-reference.md) mục 8 — danh sách endpoint và
mã lỗi chỉ có ở đó, file này không nhân bản.

## Tham chiếu

- `docs/product/backend-spec.md` (Cart)
- [`specs/008-cart/deferred.md`](../../specs/008-cart/deferred.md) — các điểm cố ý hoãn
- [decisions/016](../decisions/016-cart-singleton-loose-reference-and-snapshot.md) — tài nguyên đơn
  không định danh, tham chiếu sản phẩm lỏng, hai hợp đồng đọc bulk, và giá snapshot
- [decisions/014](../decisions/014-inventory-hold-and-availability.md) — hợp đồng liên module và mô
  hình tồn kho mà giỏ đọc khả dụng qua đó
