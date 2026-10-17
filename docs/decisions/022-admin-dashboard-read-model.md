# ADR 022 - Admin Dashboard Overview: module admin read-only trên hợp đồng đọc công bố, snapshot danh mục trên dòng đơn, và timezone cửa hàng + so sánh MTD

- **Status**: Accepted
- **Decision Date**: 2026-10-17
- **Decision Maker**: Mavil
- **Feature**: `specs/017-admin-dashboard` (module 16 Admin)
- **Liên quan**: [ADR 013](013-product-visibility-media-and-hard-delete.md) (sản phẩm: hợp đồng liên module),
  [ADR 014](014-inventory-hold-and-availability.md) (tồn kho: hợp đồng liên module, hold), [ADR 016](016-cart-singleton-loose-reference-and-snapshot.md)
  (snapshot không FK), [ADR 017](017-order-checkout.md) / [ADR 018](018-order-confirmation-and-editing.md)
  (đơn hàng: dòng snapshot), [ADR 019](019-payos-payment.md) (payment: hợp đồng liên module),
  [ADR 021](021-product-sku-model.md) (variant là đơn vị bán)

## 1. Bối cảnh

Trang Admin Dashboard của frontend đang dùng **mock data**. Cần một endpoint backend trả **toàn bộ**
số liệu vận hành trong **một** request: KPI doanh thu/đơn hàng/khách hàng/tồn kho, chuỗi doanh thu theo
ngày, cơ cấu doanh số theo danh mục và các đơn mới nhất. Đây là capability **đọc thuần**, dữ liệu đến từ
nhiều module đã có (order, user, inventory, product).

Bốn ràng buộc định hình cách làm:

1. **Constitution I** cấm một module đọc bảng của module khác; mọi giao tiếp liên module qua
   `internal/contracts`. Admin **không** được `JOIN` sang `orders`/`users`/`stock_levels`.
2. **Constitution II** buộc tiền là **integer minor units + currency**, không dùng float; nhưng dashboard
   **không** thay đổi tiền/tồn kho nào nên **không** mở transaction.
3. **Constitution VII (YAGNI)**: không cache, không pre-aggregation, không read model lưu trữ, không
   microservice; dữ liệu MVP đủ nhỏ để tính trực tiếp.
4. **Constitution VIII** buộc endpoint có annotation Swagger, `docs/swagger/` sinh lại, và
   `docs/api-reference.md` cập nhật cùng thay đổi.

Câu hỏi thật: làm sao trả đủ số liệu liên module mà **không** phá ranh giới module, **không** để việc
đổi/xoá danh mục làm sai doanh thu lịch sử, và đo được "doanh thu tháng này" một cách công bằng khi
tháng chưa kết thúc. Các quyết định dưới đây có ảnh hưởng lâu dài (đặc biệt snapshot danh mục trên dòng
đơn, chạm mọi lần checkout/edit sau này) nên được ghi thành ADR, không chỉ nằm trong `research.md`.

## 2. Quyết định

### 2.1 Admin là orchestrator read-only, sở hữu **không** bảng nào (D1)

`internal/modules/admin` là một module bốn lớp bình thường, nhưng **không** có storage: `domain/repository`
và `domain/error` chỉ chứa tài liệu gói. Application nhận bốn **contract port** và gọi chúng; adapter do
**module sở hữu** cấp ở composition root (`cmd/api/main.go`). Dashboard **không** mở transaction vì
**không** ghi gì (Constitution II không áp dụng cho nó ở chiều ghi). Bốn hợp đồng đọc công bố trong
`internal/contracts/dashboard.go`: `OrderStatistics`, `CustomerStatistics`, `InventoryStatistics`,
`ProductVariantStates`.

### 2.2 Hợp đồng đọc thô (coarse-grained), đọc **bounded** (D6, FR-032)

`OrderStatistics.KPIs` trả **mọi** chỉ số order/revenue trong **một** câu conditional-aggregate;
`CustomerStatistics.KPIs` trả cả hai số khách trong **một** câu; các mục còn lại mỗi cái một câu. Tổng
cộng application thực hiện **8 lời gọi read cố định** (4 order + 2 customer + 1 inventory + 1 product),
**không** tăng theo số order/variant/khách. T043/T066 khoá bất biến này bằng test (SC-006, FR-032).

### 2.3 Snapshot danh mục trên mỗi dòng đơn hàng (D2, FR-022, FR-029)

Thêm hai cột nullable `order_items.category_id`/`category_name` (`migrations/00017_...`), **cùng null
hoặc cùng có giá trị**, **không** FK tới `categories` (dòng đơn là bản ghi bán, phải sống lâu hơn danh
mục). Checkout/edit copy cặp này từ fact của variant vào từng dòng mới; backfill dữ liệu cũ ở nơi còn
resolve được. Cơ cấu doanh số group theo snapshot này, nên đổi danh mục hoặc xoá sản phẩm sau khi bán
**không** làm số đã bán nhảy sang danh mục khác hay biến mất; dòng không resolve được gom vào bucket
`categoryId = null`, `categoryName = "Khác"` và **không** bị loại khỏi tổng.

### 2.4 Timezone cửa hàng cấu hình được; so sánh tháng theo MTD (D3, D4, FR-007, FR-008)

`SHOP_TIMEZONE` (mặc định `Asia/Ho_Chi_Minh`), xác thực bằng `time.LoadLocation` ở `ValidateForAPI()`
(API từ chối khởi động nếu tên IANA sai). Mọi ranh giới ngày/tuần/tháng và nhãn ngày dùng múi giờ này;
**dữ liệu lưu vẫn UTC**. Cửa sổ tháng hiện tại là **month-to-date** (00:00 ngày 1 đến thời điểm request);
cửa sổ tháng trước là **cùng độ dài thời gian**, clamp về cuối tháng trước nếu vượt — **không** so với
toàn bộ tháng trước (sẽ làm growth thấp giả tạo giữa tháng). `growthPercent` = `(current - last) / last *
100`, làm tròn 2 chữ số, **`null`** khi `last = 0`.

### 2.5 Doanh thu quy theo `created_at`, chỉ ba trạng thái, chỉ VND (D3, FR-010)

Doanh thu chỉ gồm đơn `PAID`/`SHIPPED`/`COMPLETED`, currency `VND`, quy theo `orders.created_at` (chưa
có `paid_at`). MVP chỉ VND; đơn currency khác **không** được cộng vào amount VND. Việc nhận doanh thu
theo **thời điểm thanh toán** là cải tiến sau, ghi ở
[`specs/017-admin-dashboard/deferred.md`](../../specs/017-admin-dashboard/deferred.md).

### 2.6 Tồn kho thấp tách hai hợp đồng (D9, FR-017, FR-018)

Inventory trả candidate `available = max(0, quantity vật lý − active hold còn hạn)` cho variant **có
stock record**; product trả tập variant `DISCONTINUED`; **admin** loại tập sau trước khi đếm. Nhờ vậy
dashboard không đọc bảng của module 04/05, và hold hết hạn / variant chưa từng nhập kho / variant
`DISCONTINUED` được xử lý đúng.

### 2.7 Recent orders: 5 đơn, customer nullable (D10, FR-025, FR-026)

Cố định 5 đơn mới nhất (`created_at DESC, id DESC`), mọi trạng thái; dòng đầu theo `position` dựng
`itemSummary`; `itemCount` = số dòng, `itemQuantity` = tổng số lượng. Customer id resolve bằng **một**
bulk read; tài khoản đã xoá → `customer = null`, đơn vẫn xuất hiện.

### 2.8 Audit một lần, không PII (D11, FR-030)

Mỗi lần xem thành công ghi **đúng một** event `ADMIN_DASHBOARD_VIEWED` (actor từ session, outcome
`SUCCESS`, target `dashboard/overview`, metadata `{days, lowStockThreshold, recentOrderCount}`), qua
shared audit emitter. **Không** audit từng khách hàng được đọc cho `recentOrders`. Actor **luôn** lấy từ
session, không bao giờ từ request.

### 2.9 Không cache, không read model lưu trữ (D12, YAGNI)

Dashboard phản ánh dữ liệu tại thời điểm request. Chưa thêm bảng/view/materialised view hay cache; nếu
volume tăng, thêm read model/cache bằng **feature riêng** (ghi ở `deferred.md`).

## 3. Hệ quả

| Hệ quả | Chi tiết |
|---|---|
| Tích cực | **Giữ ranh giới module**: admin không `JOIN`/đọc bảng module khác; mọi số liệu qua hợp đồng (FR-028, SC-010) |
| Tích cực | **Số lịch sử đúng**: snapshot danh mục khiến đổi/xoá danh mục không làm sai doanh thu đã bán (FR-021, SC-004) |
| Tích cực | **Đọc bounded**: 8 lời gọi read cố định; mục tiêu < 2 giây ở 10k order/1k variant (SC-006, FR-032) |
| Tích cực | **Doanh thu công bằng**: MTD so với cùng kỳ tháng trước; growth `null` khi không có mốc so sánh |
| Tiêu cực | **Schema chạm order**: thêm hai cột vào `order_items`; checkout/edit phải copy snapshot (một chi tiết mới cho mọi lần mua) |
| Tiêu cực | **Backfill chỉ dùng danh mục hiện tại**: dòng cũ resolve được sẽ mang danh mục **hiện tại** của sản phẩm, không phải danh mục lúc bán (không có dữ liệu lịch sử); dòng không resolve được vào `"Khác"` — giới hạn ghi ở `deferred.md` |
| Tiêu cực | **Admin module không có repository/error nghiệp vụ**: hai gói chỉ có tài liệu; đây là ngoại lệ có chủ ý, ghi ở Complexity Tracking của `plan.md` |
| Tiêu cực | **Doanh thu quy theo `created_at`**, chưa theo thời điểm thanh toán — sai lệch khi đơn tạo tháng này trả tháng sau |

**Xem lại quyết định khi**: có `paid_at`/payment event để quy doanh thu theo thời điểm thanh toán; có
currency khác VND; volume vượt mức MVP và cần read model/cache; hoặc product→`categories` join hiện có
được refactor để bỏ đọc chéo module.

## 4. Cách tiếp cận bị loại

| Phương án | Vì sao không chọn |
|---|---|
| Admin đọc thẳng bảng của module khác | Vi phạm Constitution I; module không sở hữu bảng không được đọc |
| Một read model/cache/materialised view riêng cho dashboard | YAGNI: dữ liệu MVP nhỏ, chỉ đọc; thêm nguồn sự thật thứ hai và đồng bộ |
| Admin sở hữu bảng riêng (dashboard snapshot) | Mâu thuẫn scope module 16 ("không có bảng riêng"); tạo dữ liệu trùng |
| Join `order_items` → `products` → `categories` lúc đọc | Việc đổi/xoá danh mục làm sai/làm mất doanh thu lịch sử (spec Clarifications) |
| So sánh growth với **toàn bộ** tháng trước | Tháng hiện tại chưa kết thúc → growth thấp giả tạo |
| Dùng múi giờ server/UTC cho ranh giới ngày/tháng | Đơn sát nửa đêm bị tính sai ngày/tháng; operator thấy số liệu lệch |
| `growthPercent = 0` khi mốc so sánh bằng 0 | 0 là một giá trị tăng trưởng thật; phải là `null` để client hiển thị "không có mốc so sánh" |
| Trả tiền là number thay vì object | Vi phạm Constitution II (integer minor unit + currency); không mở rộng được sang currency khác |
| Nhiều hợp đồng hạt mịn (mỗi metric một method) | Số lời gọi read phình theo metric; coarse contract giữ đọc bounded |
| Audit từng khách hàng được đọc cho `recentOrders` | Làm rác trail bằng PII mỗi lần refresh; một event không PII là đủ (Constitution VI) |

## 5. Bổ sung (2026-10-17)

Các mục cố ý hoãn của feature 017 — quy doanh thu theo `paid_at`, khoảng trống snapshot danh mục legacy,
nợ join product→`categories` liên module, index/cache khi volume tăng, hỗ trợ đa tiền tệ — nằm ở
[`specs/017-admin-dashboard/deferred.md`](../../specs/017-admin-dashboard/deferred.md). Bàn giao frontend
nằm ở [`specs/017-admin-dashboard/frontend-guide.md`](../../specs/017-admin-dashboard/frontend-guide.md).
