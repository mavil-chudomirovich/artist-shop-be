# ADR 017 - Đơn hàng: dòng snapshot không FK, `expires_at` do order sở hữu, chuyển nhượng là đổi chủ, bước `PAID` giao nhưng để module 08 điều khiển, và `WithTx` tái dùng transaction

- **Status**: Accepted
- **Decision Date**: 2026-10-09
- **Decision Maker**: Mavil
- **Feature**: `specs/009-order`
- **Liên quan**: [ADR 014](014-inventory-hold-and-availability.md) (giữ chỗ/khả dụng module 05),
  [ADR 015](015-shop-first-version-plan-and-payos.md) §5 (kế hoạch V1.0, PayOS, không phí ship),
  [ADR 016](016-cart-singleton-loose-reference-and-snapshot.md) (giá snapshot lúc thêm, đối chiếu lại
  ở checkout)

## 1. Bối cảnh

Module 07 Order là module **thứ tư của V1.0** và là module **tiêu thụ nhiều module nhất từ trước tới
nay**: nó chạm **năm** module cùng lúc — cart (đọc và làm rỗng), product (dữ kiện dòng), inventory
(giữ/bán/nhả), user (địa chỉ giao) và auth (tra tài khoản nhận chuyển nhượng). Nó cũng là nơi **ba
món nợ** của các feature trước đáo hạn: hợp đồng đặt chỗ của module 05 (feature 007 D1), câu hỏi dòng
đơn làm gì khi sản phẩm bị xoá (feature 006/007 D3), và "huỷ một đơn đã trả tiền" (feature 007 D2).

Năm quyết định trong feature này khiến người đọc code sau phải dừng lại, vì mỗi cái có một lựa chọn
thay thế trông hợp lý hơn ở bề mặt:

1. **Vì sao `order_items.product_id` là tham chiếu thông tin, KHÔNG foreign key, và dòng lại chụp cả
   `name`/`slug`/giá?** Mọi dòng khác trong schema đều có FK, và mọi module khác đọc dữ kiện sản phẩm
   **sống** mỗi lần.
2. **Vì sao `orders` tự giữ `expires_at` và tự có sweeper, trong khi module 05 **đã** hết hạn giữ
   chỗ?** Có hai sweeper cho cùng một cửa sổ.
3. **Vì sao chuyển nhượng là một endpoint riêng `transfer` chứ không phải một trạng thái, và vì sao nó
   **không** đổi tồn kho?**
4. **Vì sao bước `PENDING_PAYMENT → PAID` được giao và test nhưng **không** có endpoint?**
5. **Vì sao `share/database.WithTx` được sửa để tái dùng transaction đang có trong context?** Đây là
   thay đổi **hạ tầng dùng chung**, không phải thay đổi của riêng module order.

Năm quyết định này định hình lược đồ dữ liệu, hình dạng HTTP, ranh giới liên module, và cả tầng
transaction dùng chung, nên chúng được ghi thành ADR chứ không chỉ nằm trong `research.md`.

## 2. Quyết định

### 2.1 Dòng đơn là **snapshot**, `product_id` là tham chiếu thông tin không FK

`order_items` lưu **giá trị chụp tại checkout**: `name`, `slug`, `unit_price_amount`, `currency`, cộng
`quantity` và `position`. `product_id` được giữ — vì module doc liệt kê một trường sản phẩm trên dòng
và plan ghi nhớ đã mua gì — nhưng **không có foreign key**. Dòng **không bao giờ** join lại `products`.

Hệ quả: một sản phẩm bị module 04 xoá cứng (feature 006 chọn hard delete) **không** cascade vào và
**không** chặn đơn; một lần đổi tên/re-giá sản phẩm sau đó **không** đổi thứ đơn hiển thị. Đây là
quyết định dòng đơn mà feature **006 và 007** đã để lại cho module Order (`deferred.md` D3 của cả
hai); feature này đóng nó.

### 2.2 Order sở hữu `expires_at` và sweeper riêng, bên cạnh giữ chỗ của module 05

`orders` mang cột `expires_at` (= thời điểm tạo + cửa sổ giữ chỗ mà module 05 trả qua `HoldWindow()`,
**không** lặp lại con số 15 phút). Một **sweeper nền** — sweeper thứ hai của dự án, mô phỏng module 05,
mỗi 30 giây — tìm các đơn còn `PENDING_PAYMENT` quá `expires_at` (index `orders_expiry_idx`), huỷ
chúng và gọi `Release` cho từng dòng.

Đây không phải trùng lặp với sweeper của module 05: module 05 chỉ **nhả hàng** khi giữ chỗ hết hạn,
còn **chỉ order** mới chuyển được trạng thái của chính nó. Nếu chỉ dựa vào module 05, hàng được nhả
nhưng đơn vẫn `PENDING_PAYMENT` — tức vẫn **có thể trả tiền** dù không còn gì để bán, đúng trạng thái
mâu thuẫn mà FR-012 sinh ra để chặn. Hai sweeper nhả cùng một giữ chỗ, và `Release` của module 05 là
**idempotent** (lần thứ hai là no-op), nên hai đường **không** nhả đúp.

### 2.3 Chuyển nhượng là **đổi chủ sở hữu**, không phải một trạng thái, và không đổi tồn kho

`POST /admin/orders/{orderId}/transfer` chỉ đổi `orders.user_id` sang tài khoản nhận, tra bằng
**email** qua `AccountLookup` (module 01). Dòng, trạng thái và tổng tiền **không** bị ghi; **không**
số vật lý lẫn khả dụng nào đổi. Chỉ áp dụng cho đơn `PAID`; đơn chưa trả tiền nhận
`409 ORDER_NOT_TRANSFERABLE`.

Hệ thống **không** có luồng hoàn tiền (ADR 015 §5). Một đơn đã trả tiền **không** huỷ được; chuyển
nhượng là thứ thay thế nó. Việc tài khoản nhận **tồn tại** được kiểm ở **nguồn sự thật** (module 01),
không bằng khóa ngoại — nên `orders.user_id` vẫn là tham chiếu lỏng và một đơn sống lâu hơn tài khoản
mà nó nêu. Điều này đóng feature 007 `deferred.md` **D2**.

### 2.4 Bước `PENDING_PAYMENT → PAID` được giao và test, nhưng để module 08 điều khiển

Use case `MarkPaid(orderID, sourceRef)` chuyển `PENDING_PAYMENT → PAID` và gọi `ApplySale` từng dòng
**trong một transaction**. Nó được giao và **test trực tiếp** (unit + integration), nhưng **không có
endpoint HTTP**: một payment thật là của module 08 Payment, chưa tồn tại.

`sourceReference` của mỗi dòng là **định danh sự kiện thanh toán + định danh sản phẩm**, vì
`inventory_transactions.source_reference` là **duy nhất toàn cục** và một đơn đã trả tiền tạo **N**
dòng chuyển kho: một reference cho cả đơn sẽ đụng unique index ở dòng thứ hai. Nghĩa vụ của module 08
— gọi `MarkPaid` với một **định danh sự kiện ổn định** — được ghi ở `deferred.md`. Đây là cùng cách
feature 007 giao năng lực bán mà chưa có nguồn phát, và feature 006 để trạng thái bán cho 007.

### 2.5 `share/database.WithTx` tái dùng transaction đang có trong context

Checkout gọi **nhiều** use case liên module (đọc giỏ, đọc product/inventory, giữ hàng, làm rỗng giỏ),
và mỗi use case của bên cung cấp có thể tự mở transaction riêng. Để tất cả commit **cùng nhau hoặc
không gì**, `WithTx` được sửa: nếu context **đã** mang một `pgx.Tx`, nó **tái dùng** transaction đó và
chạy `fn` trực tiếp — **không** mở transaction thứ hai và **không** commit/rollback (chỉ lời gọi ngoài
cùng sở hữu ranh giới). Một `*pgxpool.Pool` để trong context vẫn là pool, **không** phải transaction,
nên vẫn mở transaction như trước.

Đây là thay đổi **hạ tầng dùng chung** phục vụ mọi module dùng `UnitOfWork`; nó là nền tảng cho
Constitution II ở feature này: giữ hàng ở module 05 và ghi đơn ở module 07 nằm trong **một** transaction.

## 3. Hệ quả

| Hệ quả | Chi tiết |
|---|---|
| Tích cực | **Đơn không phụ thuộc sản phẩm/địa chỉ còn tồn tại**: dòng và địa chỉ là snapshot, nên xoá cứng sản phẩm (module 04) không cascade vào và không chặn đơn — đóng D3 của 006/007 |
| Tích cực | **Giữ chỗ có một chủ sở hữu**: cửa sổ và quy tắc hết hạn vẫn của module 05; order đọc cửa sổ qua `HoldWindow()` nên không lặp con số |
| Tích cực | **Đơn tự rời "chờ thanh toán"**: sweeper của order chuyển trạng thái, sweeper của inventory nhả hàng; `Release` idempotent nên không nhả đúp |
| Tích cực | **Truy cập chéo bất khả thi bằng cấu trúc**: không route nào nêu định danh chủ; chủ là session (FR-020). Đơn khách khác trả cùng `404 ORDER_NOT_FOUND` |
| Tích cực | **Một transaction liên module**: `WithTx` tái dùng transaction trong context, nên ghi đơn + giữ hàng + làm rỗng giỏ commit cùng nhau (Constitution II) |
| Tích cực | **Chuyển nhượng không chạm tồn kho** và kiểm tài khoản nhận ở nguồn sự thật, nên "chuyển cho tài khoản không tồn tại" từ chối được mà không cần FK |
| Tiêu cực | **`orders.user_id` không FK**: tính toàn vẹn tham chiếu không được database bảo vệ; một đơn có thể trỏ tới tài khoản không tồn tại. Đây là cái giá đã chọn để đơn sống lâu hơn tài khoản và để chuyển nhượng được |
| Tiêu cực | **`product_id` không FK** nghĩa là dòng có thể trỏ tới sản phẩm đã xoá; nhưng dòng không bao giờ đọc lại sản phẩm nên đường đọc vẫn đúng — đây là cái giá đã chọn để dòng sống sót |
| Tiêu cực | **Hai sweeper** cho cùng một cửa sổ: thêm một worker nền để vận hành, đổi lấy việc đơn tự rời trạng thái |
| Tiêu cực | **Bước `PAID` chưa dùng được qua HTTP** cho tới khi module 08 tồn tại; ship/complete/transfer phụ thuộc nó, nên phải test bằng đường dùng trực tiếp |
| Tiêu cực | **`WithTx` đổi hành vi tầng dùng chung**: một `fn` chạy trong context đã có transaction sẽ **không** tự commit/rollback — đúng ý đồ, nhưng là hành vi phải hiểu ở mọi module dùng `UnitOfWork` |

**Xem lại quyết định khi**: module 08 Payment gọi `MarkPaid` (khi đó endpoint `pay`/webhook xuất hiện
và nghĩa vụ `sourceReference` ổn định phải được đáp); hoặc khi có nhu cầu thật về hoàn tiền / huỷ đơn
đã trả tiền (một thay đổi contract của order và inventory, xem `specs/009-order/deferred.md`).

## 4. Cách tiếp cận bị loại

| Phương án | Vì sao không chọn |
|---|---|
| `order_items.product_id` với `ON DELETE CASCADE` | Xoá sản phẩm sẽ **âm thầm xoá dòng đơn** — phá snapshot và mất thứ đã bán |
| `order_items.product_id` với `ON DELETE RESTRICT` | Sẽ **chặn** module 04 xoá sản phẩm, trong khi nó cố ý cho phép hard delete |
| Dòng đọc lại sản phẩm sống (join) mỗi lần | Một lần đổi tên/re-giá/xoá sẽ đổi thứ đơn hiển thị — đúng thứ snapshot ngăn |
| Bỏ hẳn `product_id` | Mất việc ghi nhớ đã mua sản phẩm nào, mà module doc liệt kê |
| Chỉ dựa vào sweeper của module 05 để đơn hết hạn | Hàng được nhả nhưng đơn vẫn `PENDING_PAYMENT` — vẫn trả tiền được dù không còn gì bán |
| Nhả hàng đồng bộ đúng lúc hết hạn | Không có gì đánh thức ở một mốc đã lưu nếu không có scheduler — sweeper chính là scheduler đó |
| Thêm trạng thái `EXPIRED` riêng | ADR 015 §5 nêu **một** trạng thái `CANCELLED`; ai huỷ (khách hay đồng hồ) là một mốc thời gian, không phải trạng thái |
| `transfer` như một trạng thái, hoặc qua `PATCH /orders/{id}` | Chuyển nhượng **không** đổi trạng thái; gộp nó vào một field edit sẽ khiến một request vừa đổi field vừa đổi chủ |
| Endpoint `pay` cho admin để trả tiền ngay | Không có gì **xác thực** một payment; "đánh dấu đã trả" không có tiền đằng sau là ghi một thao tác bán shop không hề làm |
| Trì hoãn bước `PAID` hoàn toàn | Nó là trạng thái của chính order, và giữ-chỗ-thành-bán là hành vi của order phải chứng minh |
| `WithTx` luôn mở transaction mới | Checkout liên module sẽ có nhiều transaction rời, và ghi đơn có thể commit khi giữ hàng rollback — phá Constitution II |
| Đọc bảng của module khác thay vì dùng hợp đồng | Constitution I cấm; checkout chạm năm module, tất cả qua `internal/contracts` |
| Nêu người nhận chuyển nhượng bằng định danh thay vì email | Clarification chốt **email**; operator biết email khách hơn là mã tài khoản, và email là dữ kiện của module 01 |
