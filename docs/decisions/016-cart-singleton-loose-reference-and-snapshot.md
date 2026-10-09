# ADR 016 - Giỏ: tài nguyên đơn không định danh, tham chiếu sản phẩm lỏng, hai hợp đồng đọc bulk, và giá snapshot

- **Status**: Accepted
- **Decision Date**: 2026-10-09
- **Decision Maker**: Mavil
- **Feature**: `specs/008-cart`

## 1. Bối cảnh

Module 06 Cart là module **đầu tiên của V1.0** và là module **đầu tiên tiêu thụ hai module đã giao
cùng lúc**: module 04 Product trả lời sản phẩm có đang bán không, tên là gì và giá bao nhiêu; module
05 Inventory trả lời còn bao nhiêu hàng. Cả hai dữ kiện **không** thuộc giỏ, và Hiến pháp I cấm một
module đọc bảng của module khác, nên câu hỏi "giỏ đọc hai dữ kiện đó bằng cách nào" phải có câu trả
lời có tên chứ không lẻn vào code.

Bốn quyết định trong feature này khiến người đọc code sau phải dừng lại, vì mỗi cái đều có một lựa
chọn thay thế trông hợp lý hơn ở bề mặt:

1. **Vì sao đường dẫn là `/cart` trần, không có định danh giỏ?** Mọi tài nguyên khác của dự án đều
   định địa chỉ bằng id hoặc slug. Ở đây không có id nào trên bất kỳ route nào.
2. **Vì sao `cart_items.product_id` cố ý KHÔNG có foreign key** — một tham chiếu lỏng, thứ mà mọi
   dòng khác trong schema đều có?
3. **Vì sao giỏ đọc product và inventory qua hai interface **bulk** trong `internal/contracts`, thay
   vì gọi từng sản phẩm một hay đọc thẳng bảng?** Và vì sao lại publish một hợp đồng **đọc khả dụng**
   trong khi module 05 vẫn còn nợ một hợp đồng **đặt chỗ** (D1 của nó)?
4. **Vì sao giá của một dòng là giá **chụp lúc thêm** (snapshot) chứ không phải giá hiện tại?** Đây
   là câu hỏi mở mà chính module doc để lại ("Giá trong giỏ: cố định theo thời điểm thêm hay cập nhật
   theo giá hiện tại?").

Bốn quyết định này định hình hình dạng HTTP, lược đồ dữ liệu, ranh giới giữa ba module, và nghĩa vụ
của checkout, nên chúng được ghi thành ADR chứ không chỉ nằm trong `research.md` của feature.

## 2. Quyết định

### 2.1 Giỏ là tài nguyên đơn, định địa chỉ tại `/cart` không kèm định danh

Một khách đã đăng nhập có **đúng một** giỏ, và giỏ được định địa chỉ bằng chính **session**:

| Method | Path | Ai |
|---|---|---|
| GET | `/api/v1/cart` | khách đã đăng nhập |
| POST | `/api/v1/cart/items` | khách đã đăng nhập |
| PATCH | `/api/v1/cart/items/{productId}` | khách đã đăng nhập |
| DELETE | `/api/v1/cart/items/{productId}` | khách đã đăng nhập |

Không route nào mang `cartId`, `userId` hay bất kỳ định danh chủ sở hữu nào. Một **dòng** định địa
chỉ bằng `productId`, vì giỏ có tối đa một dòng cho mỗi sản phẩm (ràng buộc `(cart_id, product_id)`
unique), nên sản phẩm đã là khoá của dòng; thêm một `lineId` riêng là khoá thứ hai cho một thứ đã
duy nhất. Chủ sở hữu luôn lấy từ session (FR-010); tầng lưu trữ bảo đảm một-giỏ-một-tài-khoản bằng
**unique index** trên `carts.user_id`.

Hệ quả trực tiếp: **không tồn tại đường nào để truy cập chéo**. Một client không thể nêu tên giỏ của
người khác vì không có chỗ nào để nêu; đây là cách chống truy cập chéo **bằng cấu trúc**, không bằng
một kiểm tra quyền có thể quên.

### 2.2 `cart_items.product_id` là tham chiếu lỏng, không foreign key

`cart_items.product_id` **không** có `FOREIGN KEY` tới `products`. Đây là lựa chọn **có chủ ý**, đối
xứng ngược với một dòng đơn hàng (sẽ chụp sản phẩm ở module 07):

- Nếu có `ON DELETE CASCADE`, xoá sản phẩm sẽ **âm thầm xoá dòng** của khách — đúng thứ FR-012 sinh
  ra để ngăn.
- Nếu có `ON DELETE RESTRICT`, xoá sản phẩm sẽ **thất bại** khi còn ai đó giữ nó trong giỏ, trong khi
  module 04 đã cố ý cho phép hard delete sản phẩm.

Không có FK, dòng **sống sót** khi sản phẩm bị xoá, và `GET /cart` báo dòng đó là `buyable: false`
với `name`/`slug` là `null` — khách thấy và tự xoá nó. Ràng buộc một-dòng-một-sản-phẩm và
quantity ≥ 1 vẫn ở **tầng lưu trữ** (unique index `(cart_id, product_id)`,
`CHECK (quantity >= 1)`), đúng Hiến pháp.

### 2.3 Hai hợp đồng đọc **bulk** trong `internal/contracts`, adapter ở phía bên cung cấp

Giỏ đọc hai dữ kiện qua hai interface khai trong `internal/contracts` (gói không import module nào),
mỗi interface do **bên cung cấp** viết adapter và wire ở composition root — dependency một chiều
`cart → product`, `cart → inventory`:

```go
// product.go — module 04 cung cấp (adapter ở product/infrastructure/implement/catalog/)
type ProductCatalog interface {
    Products(ctx context.Context, productIDs []uuid.UUID) ([]ProductSummary, error)
}
// ProductSummary: ID, Name, Slug, OnSale (bool, KHÔNG phải enum bốn trạng thái của module 04), Price

// inventory.go — module 05 cung cấp (adapter ở inventory/infrastructure/implement/availability/)
type InventoryAvailability interface {
    AvailableQuantity(ctx context.Context, productIDs []uuid.UUID) ([]Availability, error)
}
// Availability: ProductID, Available
```

Cả hai là **bulk**: một lần đọc trả cả giỏ, và adapter trả cả tập trong **một truy vấn có chỉ mục**
(`= ANY(...)`), nên **số lời gọi liên module và số truy vấn không tăng theo số dòng** (mục tiêu hiệu
năng của `plan.md`). `ProductSummary.OnSale` là **boolean** chứ không phải bốn trạng thái của module
04: câu hỏi của giỏ là "có bán được không", không phải "đang ở trạng thái nào"; lộ enum sẽ đẩy state
machine của module 04 vào module 06.

`InventoryAvailability` là một **đọc khả dụng mới**, đã có consumer (giỏ) nên publish bây giờ đúng
Hiến pháp VII. Nó **khác** hợp đồng **đặt chỗ** (`InventoryReservation`) mà module 05 để mở cho module
07 (`specs/007-inventory-tracking/deferred.md` D1): giỏ chỉ *kiểm tra* khả dụng, **không** giữ chỗ.
ADR này không dùng và không đóng D1. Hợp đồng **không** nhận tham số thời điểm: adapter tính khả dụng
bằng **clock của module 05**, nên quy tắc hết hạn giữ một chủ sở hữu.

### 2.4 Giá là snapshot lúc thêm

Một dòng lưu `unit_price_amount` + `currency` **chụp tại thời điểm thêm** và trả lại nguyên giá đó
mọi lần đọc. Giá sản phẩm đổi sau đó **không** làm đổi `unitPrice`/`subtotal` của giỏ. Checkout mới
là nơi **đối chiếu lại** giá trước khi thu tiền (nghĩa vụ của module 07). `name`/`slug` thì ngược
lại: đọc **sống** mỗi lần xem (và `null` khi sản phẩm đã xoá), vì một lần đổi tên phải hiện đúng và
tên không phải là tiền.

Thêm lại một sản phẩm đã có **cộng dồn số lượng** và **giữ giá của lần thêm đầu**, nên giá của dòng
không nhảy dưới chân khách đang mua.

## 3. Hệ quả

| Hệ quả | Chi tiết |
|---|---|
| Tích cực | **Truy cập chéo bất khả thi bằng cấu trúc**: không route nào nêu định danh giỏ, nên không có gì để đoán; chủ sở hữu là session (FR-010, SC-004) |
| Tích cực | Ranh giới module giữ vững: giỏ không đọc bảng `products`, `stock_levels` hay `stock_holds`; cả ba module nói qua hợp đồng khai một lần ở `internal/contracts` (Hiến pháp I) |
| Tích cực | **Chi phí đọc giỏ cố định**: hai lời gọi liên module và hai truy vấn có chỉ mục cho cả giỏ, không tăng theo số dòng |
| Tích cực | Dòng **sống sót** khi sản phẩm bị xoá, nên giỏ báo "không còn bán" (`buyable: false`, `name: null`) thay vì âm thầm mất dòng (FR-012, SC-003) |
| Tích cực | Tổng tiền **không đổi dưới chân khách**: giá snapshot giữ `subtotal` ổn định trong lúc mua, và checkout mới là nơi bắt lệch giá (FR-008) |
| Tiêu cực | **Giá hiển thị có thể lệch giá hiện tại** giữa lúc thêm và lúc thanh toán. Đây là đánh đổi **cố ý**: giỏ trung thực với giá khách đã thấy, và việc bắt lệch thuộc checkout (module 07). Nếu checkout không làm, giá cũ có thể bị tính — nghĩa vụ này được ghi ở `specs/008-cart/deferred.md` D1 |
| Tiêu cực | **Không có FK trên `product_id`** nghĩa là tính toàn vẹn tham chiếu không được database bảo vệ; một dòng có thể trỏ tới sản phẩm không tồn tại. Đây là cái giá đã chọn để dòng sống sót, và đường đọc được thiết kế để xử lý đúng trạng thái đó |
| Tiêu cực | **Giỏ phụ thuộc hai module cùng lúc** và bắt buộc cả hai được wire ở composition root; thiếu một adapter thì giỏ không hoạt động |
| Tiêu cực | **Module 04 có thêm một hợp đồng đọc** (`ProductCatalog`) bên cạnh `ProductLookup`/`ProductAvailability`, và **module 05 có thêm một hợp đồng** (`InventoryAvailability`) chưa từng có. Mỗi hợp đồng là một bề mặt phải giữ ổn định |

**Xem lại quyết định khi**: module 07 Order được specify (khi đó nó gọi hợp đồng đặt chỗ của module
05 và tự đối chiếu lại giá/tồn trước khi tạo đơn — `specs/008-cart/deferred.md` D1, D2); hoặc khi có
nhu cầu thật về giỏ vãng lai / giỏ lưu / mã giảm giá / upsell (khi đó mỗi thứ là một feature riêng).

## 4. Cách tiếp cận bị loại

| Phương án | Vì sao không chọn |
|---|---|
| `/users/me/cart` | "me" chính là session, nên path lặp lại điều token đã nói, và gợi ý rằng có thể truyền một cart id nào đó |
| Đặt tên giỏ bằng `cartId` trên route | Mở ra khả năng đoán id giỏ của người khác; phá bảo đảm truy cập chéo bằng cấu trúc |
| `PATCH /cart` thay cả giỏ bằng một body | Các thao tác là **theo dòng**, và thay cả giỏ sẽ mất từ chối theo từng dòng cùng giá snapshot của từng dòng |
| `ON DELETE CASCADE` trên `product_id` | Xoá sản phẩm sẽ âm thầm xoá dòng của khách — đúng thứ FR-012 ngăn |
| `ON DELETE RESTRICT` trên `product_id` | Xoá sản phẩm sẽ thất bại khi còn ai giữ nó, trong khi module 04 cố ý cho phép hard delete |
| Chụp cả sản phẩm (tên, slug) vào dòng | Tên sẽ cũ đi khi đổi tên mà không đem lại lợi ích giỏ cần; việc chụp sản phẩm vào dòng đơn thuộc module 07 |
| Đọc từng sản phẩm một (một lời gọi/dòng) | Chi phí đọc giỏ tăng theo số dòng; cả hai hợp đồng vì thế là **bulk** |
| Giỏ đọc thẳng `stock_levels`/`stock_holds` | Hiến pháp I cấm một module chạm bảng của module khác |
| Trả enum bốn trạng thái của module 04 qua hợp đồng | Đẩy state machine của module 04 vào module 06 và biến mọi trạng thái tương lai thành thay đổi của consumer |
| Hiển thị giá hiện tại thay vì giá snapshot | `subtotal` sẽ nhảy dưới chân khách đang mua; FR-008 chốt giá snapshot và để checkout đối chiếu |
| Giỏ giữ chỗ tồn kho khi thêm | Cho một khách chặn hàng của người khác vô thời hạn mà không có đơn nào; giữ chỗ là năng lực của module 05, gọi lúc thanh toán (FR-011, `research.md` D12) |
| Publish hợp đồng `InventoryReservation` ngay trong feature này | Chưa có consumer (Order); viết bây giờ là đoán nhu cầu — Hiến pháp VII cấm. D1 của module 05 vẫn mở |
