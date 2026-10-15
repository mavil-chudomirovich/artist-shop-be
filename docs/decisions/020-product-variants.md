# ADR 020 - Product Variants: nhóm là bảng riêng, lựa chọn là sản phẩm thường, danh mục gộp bằng một câu SQL union, hai không gian tên slug, và xoá nhóm RESTRICT

- **Status**: Accepted
- **Decision Date**: 2026-10-15
- **Decision Maker**: Mavil
- **Feature**: `specs/014-product-variants` (module 04 Product)
- **Liên quan**: [ADR 013](013-product-visibility-media-and-hard-delete.md) (sản phẩm: hợp đồng hiển thị
  liên module, nhà chung của media, hard delete), [ADR 016](016-cart-singleton-loose-reference-and-snapshot.md)
  (giỏ: tham chiếu sản phẩm lỏng, giá snapshot), [ADR 012](012-category-folding-and-two-response-shapes.md)
  (danh mục: chuẩn hoá và hai hình dạng response)

## 1. Bối cảnh

Yêu cầu: một sản phẩm có thể có nhiều loại — ví dụ bộ card "Card Genshin" khi bấm vào thì chọn card
Flins, card Dottore, giống Shopee khi mua balo thì chọn màu hoặc chọn loại. `docs/modules/04-product.md`
xếp "biến thể phức tạp (size/màu) nâng cao" vào mục **Không (hoãn)**, và
`specs/006-product-catalog/deferred.md` D7 ghi mỗi biến thể là một feature riêng khi có nhu cầu. Feature
`014` đóng một phần khoảng hoãn đó: biến thể **một chiều**.

Bốn ràng buộc định hình cách làm:

1. **Constitution I** cấm module này đọc bảng của module khác, và mọi giao tiếp liên module phải qua
   `internal/contracts`.
2. **Constitution II** buộc tiền là **integer minor units + currency** (không dùng float) và mọi thay đổi
   tồn kho nằm trong một transaction có ghi `inventory_transactions`.
3. **Constitution VII (YAGNI)** cấm dựng máy móc khi chưa đo được nhu cầu: giữ modular monolith, không
   thêm hạ tầng, không thêm dependency.
4. `docs/development` và các ADR trước đã chốt: giỏ/đơn/kho đều khoá theo **product id** (ADR 016), và
   module 04 sở hữu bảng `products` cùng bộ ảnh `product_images` (ADR 013).

Câu hỏi thật là: làm sao để thêm "sản phẩm có nhiều lựa chọn" mà **không** phải chạm vào giỏ, đơn, kho,
payment hay bất kỳ hợp đồng liên module nào — vì mỗi thay đổi ở đó là một rủi ro tiền/tồn. Các quyết
định dưới đây (research D1–D14) vì thế có ảnh hưởng lâu dài và được ghi thành ADR, không chỉ nằm trong
`research.md`.

## 2. Quyết định

### 2.1 Nhóm là một bảng riêng; lựa chọn là một sản phẩm thường (D1)

Một bảng mới **`product_variant_groups`** (name, slug, description, category_id, position, option_name)
cộng hai cột nullable trên `products` — `variant_group_id` (FK tới nhóm) và `variant_label` (nhãn ngắn
của lựa chọn). Một sản phẩm có `variant_group_id` là một **lựa chọn**; sản phẩm có nó null là sản phẩm
thường đúng như hôm nay. **Không có dòng "sản phẩm cha"**: nhóm không phải sản phẩm và không bao giờ
mua được.

Vì mọi consumer hiện có (giỏ, đơn, kho, payment, hợp đồng `ProductCatalog`) khoá theo **product id**, và
một lựa chọn **chính là** một dòng `products`, nên **không** consumer nào phải đổi. Bảng nhóm riêng còn
làm "nhóm không có giá, tồn kho hay trạng thái bán" trở thành đúng về mặt cấu trúc — các cột đó đơn giản
là **không tồn tại**, nên không đường code nào lỡ bán một nhóm.

### 2.2 Bộ ảnh của nhóm là một bảng riêng (D2)

Bảng **`product_variant_group_images`** mô phỏng `product_images` (id, group_id, public_id, secure_url,
width, height, position, is_primary, created_at) với cùng partial unique index "tối đa một ảnh chính".
Nhóm có **bộ ảnh riêng** (banner dùng chung) song song ảnh riêng của từng lựa chọn; khi khách chọn một
lựa chọn, ảnh hiển thị chuyển sang ảnh của lựa chọn đó. Mô phỏng bảng giữ nguyên media adapter và hình
dạng đọc/ghi hiện có, thay vì phát minh một bảng ảnh đa hình.

### 2.3 Ràng buộc quan trọng nằm ở tầng lưu trữ (D3, D9, D10)

Constitution buộc ràng buộc dữ liệu quan trọng phải có ở tầng lưu trữ, không chỉ ở code. Cụ thể:

- `products.variant_group_id` → FK `product_variant_groups(id)` **ON DELETE RESTRICT** (nhóm còn lựa
  chọn thì không xoá được) (D10).
- Partial unique index `(variant_group_id, variant_label) WHERE variant_group_id IS NOT NULL` — nhãn lựa
  chọn duy nhất trong nhóm.
- Check `(variant_group_id IS NULL) = (variant_label IS NULL)` — nhãn thuộc một sản phẩm đã gắn nhóm và
  không thuộc gì khác.
- Check `NOT (is_set AND variant_group_id IS NOT NULL)` — combo set không thể là lựa chọn (D9).
- Chỉ mục `products.variant_group_id`; và phía nhóm: unique index trên `slug`, check định dạng slug, các
  check độ dài name/slug/description, FK tới `categories(id)` RESTRICT, chỉ mục sắp xếp
  `(position, created_at, id)`.

**Một nhóm cho mỗi sản phẩm** không cần check: dòng sản phẩm mang **một** `variant_group_id`, nên "thuộc
nhiều nhất một nhóm" là một sự thật cấu trúc, không phải một điều kiện kiểm.

### 2.4 Danh mục công khai gộp: một câu SQL union (D4, D12)

`GET /products` trở thành **một** câu SQL — `UNION ALL` (trong CTE) của hai nguồn cùng hình dạng, phân
biệt bằng `type` (`'variant-group'` | `'product'`):

- **nhóm**: các dòng nhóm hiển thị (D7), ảnh chính của nhóm, giá của lựa chọn **hiển thị** rẻ nhất;
- **sản phẩm thường**: `products WHERE variant_group_id IS NULL` dưới predicate hiển thị hiện có.

Toàn bộ predicate chạy trong câu này; `COUNT(*)` trên cùng CTE cho tổng của trang; thứ tự
`position, created_at, id`. Quy tắc đã lập của module là predicate hiển thị **và** tổng phải tính trong
SQL để phân trang đúng — lọc trong Go sẽ trả một trang thiếu và một tổng sai. Một câu union giữ đúng điều
đó. Đây là chỗ duy nhất feature làm việc nhiều hơn catalogue cũ, đã ghi trong **Complexity Tracking** của
`plan.md`.

`type` là cách viết mà hợp đồng máy đọc (`contracts/openapi.yaml`), `quickstart.md` và bàn giao frontend
đã dùng, và `"variant-group"` khớp tên thực thể nên người đọc không phải dịch.

### 2.5 Một chiều lựa chọn; thứ tự lựa chọn theo `position` (D5, Clarifications)

Mỗi nhóm có **đúng một chiều chọn** (ví dụ "Card" hoặc "Màu"), khách chọn một giá trị trong chiều đó.
Nhiều chiều kết hợp (màu × size) **hoãn** — cần một ma trận tổ hợp và cách tính giá/tồn theo tổ hợp, đó
là data model khác, không phải cách diễn đạt khác. Thứ tự lựa chọn trong nhóm dùng chính `position` của
sản phẩm (rồi `created_at`, `id`) — trường đã có và operator đã đặt được qua product edit, nên không cần
cột và endpoint sắp xếp mới.

### 2.6 Hai không gian tên slug, hai route công khai (D6)

`slug` của nhóm duy nhất trong các nhóm (unique index trên bảng nhóm); `slug` của sản phẩm vẫn duy nhất
trong các sản phẩm (index hiện có). Hai loại được định địa chỉ qua hai route công khai khác nhau: nhóm ở
`GET /product-groups/{slug}`, sản phẩm ở `GET /products/{slug}`. Cùng một giá trị vì thế có thể xuất hiện
một lần ở mỗi namespace. Dự án đã tách namespace theo loại (categories ở `/categories/{slug}`, products ở
`/products/{slug}`), nên đây là lựa chọn nhất quán và **không** cần cơ chế chống trùng xuyên bảng.

### 2.7 Predicate hiển thị của lựa chọn và nhóm (D7)

- Một **lựa chọn** hiển thị y hệt một sản phẩm: category của nó hiển thị **và** nó đang bán hoặc là
  pre-order đã công bố chưa nghỉ (predicate `ListVisible` hiện có). Không luật mới.
- Một **nhóm** hiển thị khi **category** của nó hiển thị **và** ít nhất một lựa chọn của nó hiển thị
  (`EXISTS` trong SQL). Nhóm không có lựa chọn hiển thị trả lời cùng một not-found như nhóm ẩn/không tồn
  tại (FR-006, FR-008).
- Giá danh mục của nhóm = `MIN(giá lựa chọn hiển thị)`; ảnh đại diện = ảnh chính của nhóm (nhóm không có
  ảnh thì hiện không ảnh, y như một sản phẩm — không fallback sang ảnh lựa chọn).

### 2.8 Bề mặt quản trị; attach/detach tái dùng product create/edit (D8, D13)

- CRUD nhóm ở `/api/v1/admin/product-groups` (`POST`, `GET`, `GET /{id}`, `PATCH /{id}`, `DELETE /{id}`),
  và bộ ảnh nhóm mô phỏng route ảnh sản phẩm (`POST /{id}/images`, `DELETE /{id}/images/{imageId}`,
  `POST /{id}/images/{imageId}/primary`), tất cả sau guard ADMIN.
- **Gắn / gỡ lựa chọn tái dùng chính create/edit của sản phẩm**: `CreateProductRequest` và
  `UpdateProductRequest` nhận thêm `variantGroupId` và `variantLabel` tuỳ chọn. Gắn là một `PATCH` đặt cả
  hai; gỡ là một `PATCH` xoá cả hai. Không có endpoint gắn riêng.
- `variantGroupId`/`variantLabel` là **tri-state theo sự hiện diện của khoá JSON**: vắng mặt = giữ nguyên;
  có mặt và `null` = gỡ; có mặt và khác null = gắn (chỉ khi sản phẩm chưa thuộc nhóm nào — sản phẩm đã
  thuộc nhóm bị từ chối với `PRODUCT_ALREADY_IN_VARIANT_GROUP` và phải gỡ trước). Presentation bỏ dấu
  từng khoá bằng `OptionalUUID`/`OptionalString`; application mang `*uuid.UUID` kèm cờ `...Set bool`. Một
  `*uuid.UUID` thuần không phân biệt được "vắng" với "null", nên gỡ và "giữ nguyên" sẽ trùng nhau.

### 2.9 Không đổi hợp đồng liên module (D11)

**Không** đổi `internal/contracts/` (cart `ProductCatalog`, inventory `InventoryAvailability` /
`InventoryReservation`, product `ProductAvailability` / `ProductLookup`), không đổi module 05–08, không
thêm hạ tầng, không thêm cross-module read. Vì một lựa chọn là một sản phẩm, mọi luồng liên module đã
vận hành trên nó. Hệ quả có chủ đích: **khả dụng của từng lựa chọn không hiển thị trên trang nhóm** (một
cross-module read spec cố ý để ngoài phạm vi); khách biết được khi thêm vào giỏ, y như hôm nay.

### 2.10 Vị trí code (D14)

- DTO HTTP của nhóm nằm ở `internal/modules/product/presentation/dto/dto.go`, cạnh DTO sản phẩm hiện có
  (module **không** có package `presentation/http/dto`; constitution xếp `dto` là sibling của `http` trong
  `presentation`).
- Use case nhóm và catalogue là method trên **một** `Service` hiện có của module; `ProductService` được
  mở rộng chứ không tách service anh em, nên handler vẫn giữ một dependency.
- Test double dùng chung nằm ở `internal/modules/product/application/implement/fakes_test.go`.

## 3. Hệ quả

| Hệ quả | Chi tiết |
|---|---|
| Tích cực | **Không đổi giỏ/đơn/kho/payment**: lựa chọn là product row, nên mọi consumer đang khoá theo product id chạy nguyên vẹn (SC-005) |
| Tích cực | **Nhóm không thể bị bán**: nó không có cột giá/tồn/trạng thái, nên "bán nhầm nhóm" là bất khả về cấu trúc (D1) |
| Tích cực | **Ràng buộc ở tầng lưu trữ**: nhãn duy nhất trong nhóm, một nhóm cho mỗi sản phẩm, set↔option loại trừ, slug duy nhất theo namespace, xoá nhóm còn lựa chọn bị RESTRICT — không đường code nào tạo được dữ liệu sai (FR-022, SC-004) |
| Tích cực | **Phân trang và tổng luôn đúng**: predicate hiển thị và `COUNT(*)` chạy trong cùng một câu union (D4) |
| Tích cực | **Bề mặt nhỏ**: gắn/gỡ lựa chọn chỉ thêm hai member tuỳ chọn vào payload sản phẩm sẵn có; không endpoint gắn riêng, không bảng link riêng (D8, D13) |
| Tiêu cực | **Danh mục nặng hơn**: `ListCatalogue` là UNION ALL hai bảng kèm subquery `MIN`/`EXISTS`/ảnh chính; đây là một truy vấn/trang nhưng đắt hơn catalogue cũ (ghi ở Complexity Tracking) |
| Tiêu cực | **Hai namespace slug** đòi hỏi hai route riêng và client phải biết mình mở loại nào; đổi lại không cần cơ chế chống trùng xuyên bảng (D6) |
| Tiêu cực | **Trần 10 ảnh của gallery nhóm không khoá dòng**: port nhóm để lộ count chứ không lộ lock, nên hai upload đồng thời ở ngưỡng có thể lọt — khác đường ảnh sản phẩm dùng `LockProduct` (ghi ở `specs/014-product-variants/deferred.md`) |
| Tiêu cực | **Khả dụng từng lựa chọn không hiện trên trang nhóm**: cần một cross-module read mới; spec cố ý để ngoài phạm vi (D11) |

**Xem lại quyết định khi**: có nhu cầu thật về biến thể **nhiều chiều** (màu × size) hoặc ma trận SKU;
hoặc cần hiển thị tồn kho theo từng lựa chọn trên trang nhóm (khi đó cần một hợp đồng đọc liên module
mới); hoặc cần một sản phẩm xuất hiện dưới nhiều nhóm; hoặc trần ảnh gallery nhóm thành vấn đề đồng thời
thật.

## 4. Cách tiếp cận bị loại

| Phương án | Vì sao không chọn |
|---|---|
| Một dòng "sản phẩm cha" (self-reference trên `products`) | Cha phải có giá placeholder (`price_amount NOT NULL CHECK (> 0)`), một sell state và cờ "không bán được" — làm bẩn state machine của sản phẩm và có nguy cơ bán nhầm cha; `catalog.go` map `OnSale = product.Buyable()` nên một cha trông-bán-được có thể lọt vào giỏ |
| Cột `variant_group_slug` text tự do, không có bảng nhóm | Tên/mô tả/category/position/gallery của nhóm không có chỗ ở, và không có dòng để ràng buộc nhãn duy nhất hay chặn xoá nhóm |
| Bảng ảnh đa hình `images(owner_type, owner_id)` | Viết lại đường ảnh sản phẩm và mọi test của nó mà chẳng được gì — module 04 đã sở hữu cả hai bảng |
| Không có gallery nhóm (chỉ dùng ảnh lựa chọn) | Clarification Q2 chọn **cả** banner chung của nhóm **lẫn** ảnh riêng từng lựa chọn |
| Chỉ kiểm ràng buộc ở tầng ứng dụng (hoặc dùng trigger) | Một defect ở một đường sẽ tạo dữ liệu sai; trigger đặt business logic vào schema, vô hình với domain và test của nó (module 04 vốn từ chối trigger khi chưa đo được nhu cầu) |
| Hai truy vấn phân trang gộp trong Go | Không cho được `total` xuyên loại đúng và thứ tự xen kẽ ổn định |
| Một DB VIEW cho danh mục gộp | Nặng hơn để migrate và giữ đồng bộ; union là một read trong một adapter vốn tự dựng SQL |
| Liệt kê luôn cả sản phẩm đã gắn nhóm như mục riêng | FR-001/SC-002 cấm — nhóm phải hiện đúng một lần, nên sản phẩm có nhóm bị loại khỏi phía standalone |
| Cột `variant_option_position` + endpoint sắp xếp lựa chọn riêng | Bề mặt thừa cho v1; `position` của sản phẩm đã có và operator đã đặt được (D5) |
| Một namespace slug dùng chung | Đòi hỏi một slug registry dùng chung hoặc check xuyên bảng ở tầng ứng dụng để tránh hai trang cùng slug, cho một lợi ích spec không yêu cầu (D6) |
| Cờ "nhóm hiển thị" lưu sẵn | Nguồn sự thật thứ hai có thể lệch với các lựa chọn (D7) |
| Endpoint gắn riêng `POST /admin/product-groups/{id}/options` | Đường ghi thứ hai vào cùng các cột sản phẩm, với validation riêng phải giữ đồng bộ (D8) |
| Bảng link riêng, không cột trên `products` | Khó giữ "một nhóm cho mỗi sản phẩm" ở dạng cấu trúc, và gắn/gỡ không còn là một câu product statement (D8) |
| `ON DELETE SET NULL` khi xoá nhóm | Âm thầm gỡ lựa chọn — che một hành động phá huỷ (D10) |
| `ON DELETE CASCADE` khi xoá nhóm | Xoá luôn các lựa chọn — FR-012 cấm (D10) |
| Mở rộng `ProductCatalog` / thêm port đọc cho catalogue | Vô ích: consumer đã nhận product id của lựa chọn (D11) |
| Endpoint tách kiểu `kind`/`"group"` cho danh mục gộp | Cách viết không nhất quán giữa các artifact và `"group"` mơ hồ cạnh "group" của tồn kho; `type`/`"variant-group"` là cách đã dùng (D12) |
| Object lồng `variantGroup: {id, label}` trong body sản phẩm | Đổi hình dạng trường mà spec đã chốt là phẳng; presence-tracking giữ nguyên hình dạng (D13) |
| Endpoint gỡ riêng | Bề mặt thừa spec không yêu cầu (D13) |

## 5. Bổ sung (2026-10-15)

Các việc **cố ý hoãn** của feature này — hiển thị khả dụng theo từng lựa chọn trên trang nhóm, biến thể
nhiều chiều, sắp xếp lựa chọn ngoài `position`, trần 10 ảnh gallery nhóm không khoá dòng, slug registry
dùng chung — nằm ở [`specs/014-product-variants/deferred.md`](../../specs/014-product-variants/deferred.md).
