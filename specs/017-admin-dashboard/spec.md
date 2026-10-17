# Feature Specification: Admin Dashboard Overview

**Feature Branch**: `017-admin-dashboard`

**Created**: 2026-10-17

**Status**: Draft

**Input**: User description: "Backend cung cấp 1 endpoint tổng hợp số liệu cho trang Admin Dashboard để FE bỏ mock data: KPI doanh thu/đơn hàng/khách hàng/tồn kho, doanh thu theo ngày, cơ cấu doanh số theo danh mục và đơn hàng gần đây trong 1 request."

## Clarifications

### Session 2026-10-17

- Q: Cơ cấu doanh số theo danh mục lấy dữ liệu lịch sử hay dữ liệu hiện tại? → A: **Snapshot danh mục
  (identity + tên) vào từng dòng đơn hàng tại checkout/edit**, backfill cho dữ liệu cũ; dòng không resolve được
  (variant/product đã xoá) gom vào bucket "Khác". Lý do: giữ đúng doanh thu lịch sử, không để việc đổi/xoá
  category làm sai số đã bán.
- Q: So sánh tháng để tính `growthPercent` dùng mốc nào? → A: **Month-to-date (MTD) so với cùng kỳ tháng
  trước** (cùng độ dài thời gian, clamp về cuối tháng trước nếu cần). Lý do: tháng hiện tại chưa kết thúc,
  so với cả tháng trước sẽ làm growth thấp giả tạo.
- Q: Định dạng tiền trong response? → A: **`{ "amount": <integer minor units>, "currency": "VND" }` ở mọi
  field tiền**, kể cả `revenueTrend` và `categoryBreakdown`; không dùng float cho tiền.
- Q: `lowStockCount` đếm những variant nào? → A: Chỉ variant **có stock record**, **không ở
  trạng thái `DISCONTINUED`**, và `available < lowStockThreshold`. Variant chưa từng nhập kho không tính.
- Q: `orders.totalOrders` nghĩa là gì? → A: **Tổng all-time, mọi trạng thái**. Số theo tháng tách thành
  `thisMonthOrders` và `lastMonthOrders`.
- Q: `recentOrders.itemCount` là số dòng hay tổng số lượng? → A: `itemCount` = **số dòng hàng** (giữ nghĩa
  hiện có của order summary); thêm `itemQuantity` = **tổng số lượng units**.
- Q: Có audit khi xem dashboard không? → A: Có, **một** event `ADMIN_DASHBOARD_VIEWED` cho mỗi request
  thành công, metadata chỉ gồm `days`, `lowStockThreshold`, `recentOrderCount`; **không chứa PII** và
  **không audit từng customer** được đọc cho `recentOrders`.

## Scope boundary

Trong phạm vi:

- Một endpoint đọc tổng hợp cho ADMIN, trả đủ 4 nhóm KPI, chuỗi doanh thu theo ngày, cơ cấu doanh số theo
  danh mục và danh sách đơn hàng gần đây trong một response.
- Định nghĩa thống nhất về thời gian (timezone cửa hàng), trạng thái đơn được tính doanh thu, cửa sổ so sánh
  tháng, cách tính growth/percentage, zero-fill trend, ngưỡng tồn kho thấp và phạm vi khách hàng.
- Lưu snapshot danh mục trên từng dòng đơn hàng tại thời điểm checkout/edit để cơ cấu doanh số theo danh mục
  không phụ thuộc dữ liệu sản phẩm hiện tại.
- Hợp đồng đọc liên capability cho số liệu tổng hợp; dashboard không sở hữu dữ liệu riêng và không đọc thẳng
  storage của capability khác.
- Audit một lần cho mỗi lần xem dashboard thành công; không ghi PII.

Ngoài phạm vi (giữ nguyên, ghi rõ để không mất):

- Drill-down, bộ lọc/date-range tùy ý, export báo cáo, so sánh nhiều kỳ, biểu đồ nâng cao.
- Real-time/websocket, cache, pre-aggregation, data warehouse.
- Thay đổi state machine của order/payment/inventory.
- Bất kỳ thao tác ghi nào từ dashboard (confirm, ship, complete, restock, sửa khách hàng...).
- Thông báo cho khách hàng.
- Nhận doanh thu chính xác theo thời điểm thanh toán (`paid_at`/payment event); feature này quy theo thời
  điểm tạo đơn và ghi phần này vào `deferred.md`.
- Sửa debt hiện có của product capability khi đọc dữ liệu category; feature này không thêm truy cập chéo
  storage.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Admin xem toàn bộ tình hình cửa hàng trong một lần mở dashboard (Priority: P1)

Một quản trị viên mở trang Admin Dashboard. Chỉ với một lần tải, họ thấy doanh thu tháng này so với cùng kỳ
tháng trước, số đơn hàng và số đơn đang chờ duyệt, số khách hàng và khách mới trong tuần, cảnh báo tồn kho
thấp, chuỗi doanh thu 7 hoặc 30 ngày gần nhất, cơ cấu doanh số theo danh mục và 5 đơn hàng mới nhất. Không
cần gọi thêm API nào và không thấy dữ liệu mock.

**Why this priority**: Đây là mục tiêu chính của feature; thiếu nó trang vận hành vẫn phải dùng mock data.

**Independent Test**: Với dữ liệu seed gồm đơn ở nhiều trạng thái, đủ khách hàng, tồn kho và danh mục, gọi
endpoint bằng tài khoản ADMIN và xác nhận response có đủ 4 nhóm KPI, trend đúng số ngày, category breakdown
và tối đa 5 recent orders; không cần gọi thêm endpoint nào.

**Acceptance Scenarios**:

1. **Given** một ADMIN đã đăng nhập và cửa hàng có dữ liệu, **When** ADMIN mở dashboard với tham số mặc định,
   **Then** response trả đủ KPI, trend 7 ngày, category breakdown và 5 recent orders trong một lần gọi.
2. **Given** ADMIN chọn `days=30`, **When** dashboard được tải, **Then** trend có đúng 30 phần tử, nhãn ngày
   dạng `DD/MM`, và các KPI còn lại không đổi nghĩa.
3. **Given** một tháng chưa có doanh thu, **When** ADMIN mở dashboard, **Then** các giá trị tiền là 0, trend vẫn
   đủ số ngày với amount 0, category breakdown rỗng và danh sách recent orders có thể rỗng.

---

### User Story 2 - Số liệu doanh thu và đơn hàng được đo bằng cửa sổ công bằng, đúng múi giờ (Priority: P1)

Quản trị viên tin rằng "doanh thu tháng này" và "tăng trưởng so với tháng trước" phản ánh đúng hoạt động của
cửa hàng, kể cả khi tháng hiện tại mới đi được một nửa và các đơn nằm sát ranh giới ngày/tháng. Chỉ các đơn
đã thực sự phát sinh doanh thu mới được tính; đơn chờ xác nhận, chờ thanh toán và đơn huỷ không được tính vào
doanh thu.

**Why this priority**: Sai định nghĩa doanh thu hoặc sai múi giờ làm toàn bộ dashboard mất giá trị và có thể
dẫn đến quyết định vận hành sai.

**Independent Test**: Seed các đơn ở đủ 6 trạng thái, trong đó có đơn sát 00:00 và sát cuối tháng theo
`Asia/Ho_Chi_Minh`; kiểm tra doanh thu chỉ gồm PAID/SHIPPED/COMPLETED, cửa sổ tháng hiện tại là MTD, cửa sổ
tháng trước là cùng độ dài, và growth là `null` khi tháng trước bằng 0.

**Acceptance Scenarios**:

1. **Given** đơn PAID, SHIPPED, COMPLETED và CANCELLED trong tháng, **When** tính doanh thu, **Then** chỉ
   PAID/SHIPPED/COMPLETED được cộng vào `currentMonthAmount`.
2. **Given** hôm nay là ngày 17 của tháng, **When** tính `lastMonthAmount`, **Then** cửa sổ tháng trước kéo dài
   đúng 17 ngày (cùng giờ), không phải toàn bộ tháng trước.
3. **Given** doanh thu cùng kỳ tháng trước bằng 0, **When** tính growth, **Then** `growthPercent` là `null`,
   không phải 0 hay 100.
4. **Given** một đơn được tạo lúc 23:30 UTC ngày cuối tháng, **When** quy về ngày theo timezone cửa hàng,
   **Then** đơn thuộc ngày/tháng kế tiếp và được tính đúng cửa sổ.

---

### User Story 3 - Cảnh báo tồn kho và sức khoẻ khách hàng phản ánh đúng thực tế (Priority: P1)

Quản trị viên thấy số mặt hàng sắp hết dựa trên lượng **khả dụng** thực tế (đã trừ hàng đang giữ cho đơn chưa
thanh toán), bỏ qua các variant đã ngừng kinh doanh, và thấy số khách hàng đang hoạt động cùng số khách mới
trong tuần. Những con số này dùng để quyết định nhập hàng và chăm sóc khách.

**Why this priority**: Tồn kho và khách hàng là hai tín hiệu vận hành chính; đếm sai dẫn đến nhập thiếu hàng
hoặc bỏ sót khách mới.

**Independent Test**: Seed một variant có tồn kho thấp nhưng đang bị hold bởi đơn PAYMENT_PENDING, một variant
`DISCONTINUED`, một variant chưa từng nhập kho, và các tài khoản CUSTOMER active/pending/disabled; xác nhận
`lowStockCount` và các số khách hàng đúng.

**Acceptance Scenarios**:

1. **Given** một variant có `quantity=5` và 2 units đang bị hold bởi đơn chưa thanh toán còn hạn, **When**
   `lowStockThreshold=5`, **Then** available = 3 nên variant được tính là tồn kho thấp.
2. **Given** một variant `DISCONTINUED` có available dưới ngưỡng, **When** đếm low stock, **Then** variant đó
   không được tính.
3. **Given** một variant chưa từng có stock record, **When** đếm low stock, **Then** variant đó không
   được tính.
4. **Given** tài khoản CUSTOMER active, CUSTOMER pending, CUSTOMER disabled và ADMIN, **When** đếm khách hàng,
   **Then** chỉ CUSTOMER active được tính vào `totalUsers`; khách active tạo trong tuần được tính vào
   `newUsersThisWeek`.

---

### User Story 4 - Cơ cấu doanh số theo danh mục giữ đúng lịch sử (Priority: P2)

Quản trị viên xem tỷ trọng doanh thu theo từng danh mục trong tháng. Khi một sản phẩm được đổi danh mục hoặc
bị xoá sau khi đã bán, doanh thu đã ghi nhận vẫn nằm đúng danh mục tại thời điểm bán; các dòng cũ không resolve
được danh mục được gom vào "Khác" thay vì biến mất.

**Why this priority**: P2 vì dashboard vẫn dùng được nếu tạm join dữ liệu hiện tại, nhưng số lịch sử sẽ sai
khi danh mục/sản phẩm thay đổi.

**Independent Test**: Bán một sản phẩm thuộc danh mục A, sau đó chuyển sản phẩm sang danh mục B; xác nhận
doanh thu cũ vẫn nằm ở A. Xoá một sản phẩm đã bán; xác nhận doanh thu của nó vẫn xuất hiện (không mất khỏi
tổng) và dòng legacy không resolve được nằm trong "Khác".

**Acceptance Scenarios**:

1. **Given** một dòng đơn hàng đã snapshot danh mục A, **When** sản phẩm được đổi sang danh mục B,
   **Then** doanh thu của dòng đó vẫn tính vào A.
2. **Given** một dòng đơn hàng cũ không có snapshot danh mục, **When** tính breakdown, **Then** dòng đó nằm
   trong bucket `categoryId = null`, `categoryName = "Khác"`.
3. **Given** tổng doanh thu tháng hiện tại, **When** cộng tất cả category breakdown (kể cả "Khác"),
   **Then** tổng khớp `currentMonthAmount`.

---

### User Story 5 - Đơn hàng gần đây có đủ ngữ cảnh nhưng không lộ dữ liệu ngoài phạm vi (Priority: P2)

Quản trị viên thấy 5 đơn mới nhất với mã đơn, khách hàng (tên/email), mô tả mặt hàng, tổng tiền, trạng thái
và thời điểm tạo. Nếu tài khoản khách đã bị xoá, dashboard vẫn hiển thị đơn với thông tin khách là `null`
thay vì làm hỏng cả response. Việc xem dashboard được ghi audit một lần, không ghi từng khách hàng.

**Why this priority**: P2 vì đây là phần ngữ cảnh vận hành; thiếu nó dashboard vẫn có KPI nhưng khó hành động.

**Independent Test**: Tạo 7 đơn mới, trong đó 2 đơn thuộc tài khoản đã bị xoá; xác nhận chỉ 5 đơn mới nhất
được trả, đơn của tài khoản đã xoá có `customer = null`, item summary/count/quantity đúng, và chỉ có một
audit event `ADMIN_DASHBOARD_VIEWED` được ghi.

**Acceptance Scenarios**:

1. **Given** có hơn 5 đơn, **When** lấy recent orders, **Then** chỉ trả 5 đơn mới nhất theo `createdAt` giảm
   dần, tie-break `id` giảm dần.
2. **Given** một đơn của tài khoản đã bị xoá, **When** lấy recent orders, **Then** `customer` là `null` và
   các field còn lại của đơn vẫn đúng.
3. **Given** một đơn có nhiều dòng, **When** tạo `itemSummary`, **Then** lấy dòng đầu theo `position` theo
   định dạng `<name> <variantName> (x<quantity>)`, nối ` +N` nếu còn N dòng khác; `itemCount` là số dòng và
   `itemQuantity` là tổng quantity.

---

### User Story 6 - Chỉ ADMIN xem được, tham số sai bị từ chối rõ ràng (Priority: P1)

Khách chưa đăng nhập hoặc tài khoản CUSTOMER không thể xem dashboard. ADMIN gửi `days` hoặc
`lowStockThreshold` không hợp lệ nhận lỗi 400 nêu đúng tên field để sửa.

**Why this priority**: Dashboard chứa doanh thu và PII khách hàng; kiểm soát truy cập là bắt buộc.

**Independent Test**: Gọi endpoint với guest, CUSTOMER, ADMIN; gọi ADMIN với `days=14`, `days=abc`,
`lowStockThreshold=-1`, `lowStockThreshold=10001`; xác nhận mã lỗi và `details[].field`.

**Acceptance Scenarios**:

1. **Given** không có access token, **When** gọi dashboard, **Then** trả 401 `UNAUTHENTICATED`.
2. **Given** token CUSTOMER, **When** gọi dashboard, **Then** trả 403 `FORBIDDEN`.
3. **Given** token ADMIN và `days` ngoài {7,30}, **When** gọi dashboard, **Then** trả 400
   `VALIDATION_ERROR` với `details[].field = "days"`.
4. **Given** token ADMIN và `lowStockThreshold` ngoài 0..10000, **When** gọi dashboard, **Then** trả 400
   `VALIDATION_ERROR` với `details[].field = "lowStockThreshold"`.

### Edge Cases

- **Không có đơn/doanh thu**: các amount là 0, trend zero-filled, category breakdown rỗng, recent orders rỗng.
- **Tháng trước bằng 0**: `growthPercent` là `null`; client phải hiển thị được trạng thái "không có mốc so sánh".
- **Ranh giới ngày/tháng theo timezone**: đơn tạo sát nửa đêm UTC nhưng thuộc ngày/tháng khác theo
  `Asia/Ho_Chi_Minh` phải được tính đúng cửa sổ; không dùng timezone của server/Docker (UTC).
- **Hold đã hết hạn nhưng sweeper chưa chạy**: hold có `expires_at <= now` không được trừ khỏi available.
- **Hai hold active cho cùng variant**: cộng dồn; available không bao giờ âm.
- **Variant `DISCONTINUED`**: không tính low stock dù available thấp.
- **Variant chưa từng nhập kho**: không có stock record nên không tính low stock.
- **Line legacy không có category snapshot**: vào bucket "Khác"; tổng category vẫn khớp doanh thu.
- **Doanh thu có nhiều currency**: MVP chỉ VND; đơn khác VND không được cộng vào các amount VND (ghi assumption).
- **Rounding percentage**: tổng percentage có thể lệch 100 do làm tròn 1 chữ số; không được tự ý sửa một dòng
  để ép đủ 100.
- **Rounding growth**: làm tròn 2 chữ số; growth âm phải giữ dấu.
- **days=30**: nhãn `DD/MM`, không dùng T2..CN.
- **Recent orders ít hơn 5**: trả đúng số hiện có, không pad.
- **Tài khoản khách bị xoá**: `customer = null`; đơn vẫn xuất hiện.
- **Status lạ trong dữ liệu**: không thể xảy ra do ràng buộc storage; nếu gặp, trả lỗi nội bộ thay vì bịa trạng thái.
- **Dữ liệu thay đổi giữa các truy vấn**: response là ảnh chụp tại thời điểm request; không cần snapshot giao dịch
  toàn cục, nhưng các metric phải dùng cùng cửa sổ thời gian và cùng định nghĩa trạng thái.

## Requirements *(mandatory)*

### Functional Requirements

**Truy cập và hình dạng response**

- **FR-001**: Endpoint MUST chỉ cho phép tài khoản đã đăng nhập có role ADMIN; guest MUST nhận 401, CUSTOMER
  MUST nhận 403.
- **FR-002**: Một lần gọi MUST trả đủ bốn nhóm KPI, `revenueTrend`, `categoryBreakdown` và `recentOrders`
  trong envelope chuẩn `{ data, meta: { requestId, timestamp } }`; client MUST NOT cần gọi thêm endpoint nào
  để dựng dashboard.
- **FR-003**: `days` MUST mặc định 7 và MUST chỉ chấp nhận 7 hoặc 30; giá trị khác MUST bị từ chối 400
  `VALIDATION_ERROR` với `details[].field = "days"`.
- **FR-004**: `lowStockThreshold` MUST mặc định 5 và MUST nằm trong 0..10000; giá trị khác MUST bị từ chối 400
  `VALIDATION_ERROR` với `details[].field = "lowStockThreshold"`.
- **FR-005**: Mọi giá trị tiền MUST là integer minor units + currency trong object `{ amount, currency }`;
  MUST NOT dùng float cho tiền ở bất kỳ field nào, kể cả trend và category breakdown.
- **FR-006**: Dashboard MUST là read-only: không được thay đổi order, inventory, customer hay bất kỳ trạng thái
  nghiệp vụ nào.

**Thời gian**

- **FR-007**: Hệ thống MUST có một timezone cửa hàng cấu hình được, mặc định `Asia/Ho_Chi_Minh`; mọi mốc
  ngày/tháng/tuần và nhãn ngày MUST dùng timezone này. Dữ liệu lưu trữ MUST vẫn là UTC.
- **FR-008**: `currentMonth` MUST bắt đầu từ 00:00 ngày 1 tháng hiện tại (theo timezone cửa hàng) đến thời điểm
  request. `lastMonth` MUST là cửa sổ có cùng độ dài thời gian ở tháng trước, bắt đầu từ 00:00 ngày 1 tháng
  trước, và MUST clamp về cuối tháng trước nếu vượt quá.
- **FR-009**: "Tuần này" MUST bắt đầu từ 00:00 Thứ Hai (theo timezone cửa hàng) đến thời điểm request.

**Doanh thu**

- **FR-010**: Doanh thu MUST chỉ gồm order có status `PAID`, `SHIPPED` hoặc `COMPLETED`, currency `VND`, và
  MUST quy theo `orders.created_at`.
- **FR-011**: `revenue.currentMonthAmount` và `revenue.lastMonthAmount` MUST dùng cửa sổ ở FR-008.
  `growthPercent` MUST tính `(current - last) / last * 100`, làm tròn 2 chữ số thập phân; nếu `last = 0` thì
  MUST là `null`.

**Đơn hàng**

- **FR-012**: `orders.totalOrders` MUST là tổng all-time, mọi trạng thái (kể cả `CANCELLED`).
- **FR-013**: `orders.thisMonthOrders` và `orders.lastMonthOrders` MUST đếm order tạo trong cửa sổ tương ứng,
  mọi trạng thái. `orders.growthPercent` MUST dùng cùng công thức và quy tắc nullable như FR-011.
- **FR-014**: `orders.pendingConfirmationCount` MUST là số order status `PENDING` all-time, không giới hạn
  theo tháng và không bao gồm `PAYMENT_PENDING`.

**Khách hàng**

- **FR-015**: `customers.totalUsers` MUST đếm tài khoản role `CUSTOMER` và status `active` all-time; MUST loại
  ADMIN, pending và disabled.
- **FR-016**: `customers.newUsersThisWeek` MUST đếm tài khoản role `CUSTOMER`, status `active`, có `created_at`
  trong tuần hiện tại theo FR-009.

**Tồn kho**

- **FR-017**: `available` của một variant MUST bằng `max(0, quantity vật lý - tổng quantity của các active
  hold còn hạn)`, dùng clock của inventory.
- **FR-018**: `inventory.lowStockCount` MUST đếm variant thoả đồng thời: có stock record, không ở trạng
  thái `DISCONTINUED`, và `available < lowStockThreshold`. `inventory.threshold` MUST echo lại giá trị request.

**Chuỗi doanh thu**

- **FR-019**: `revenueTrend` MUST có đúng `days` phần tử liên tiếp, kết thúc hôm nay (inclusive), sắp xếp cũ
  đến mới; ngày không có doanh thu MUST được zero-fill. Ngày hôm nay MUST chỉ tính đến thời điểm request.
- **FR-020**: Mỗi phần tử trend MUST có `date` dạng `YYYY-MM-DD` theo timezone cửa hàng. `dayLabel` MUST là
  `T2`..`T7`, `CN` khi `days=7`, và `DD/MM` khi `days=30`.

**Cơ cấu danh mục**

- **FR-021**: `categoryBreakdown` MUST dùng cùng cửa sổ current month và cùng quy tắc doanh thu như KPI; MUST
  group theo snapshot danh mục (identity + tên) trên dòng đơn hàng.
- **FR-022**: Mỗi dòng đơn hàng MUST lưu snapshot danh mục tại thời điểm checkout/edit. Hai field snapshot MUST
  cùng null hoặc cùng có giá trị; không dùng FK vì danh mục có thể bị xoá sau này.
- **FR-023**: Dòng đơn hàng không resolve được danh mục MUST được gom vào bucket `categoryId = null`,
  `categoryName = "Khác"`, không được loại khỏi tổng.
- **FR-024**: `percentage` MUST bằng `amount / tổng doanh thu current month * 100`, làm tròn 1 chữ số thập phân.
  Danh sách MUST sắp xếp `amount` giảm dần, tie-break `categoryName` tăng dần; chỉ trả danh mục có `amount > 0`.
  Tổng percentage MAY khác 100 do làm tròn.

**Đơn hàng gần đây**

- **FR-025**: `recentOrders` MUST trả tối đa 5 order mới nhất theo `created_at` giảm dần, tie-break `id` giảm
  dần; MUST gồm mọi trạng thái.
- **FR-026**: Mỗi recent order MUST có `id`, `customer`, `itemSummary`, `itemCount`, `itemQuantity`, `total`,
  `status`, `createdAt`. `status` MUST là một trong sáu giá trị của order. `customer` MUST là `null` nếu tài
  khoản không còn tồn tại; nếu có, MUST gồm `id`, `displayName`, `email`.
- **FR-027**: `itemSummary` MUST lấy dòng đầu theo `position` và có dạng `<name> <variantName> (x<quantity>)`,
  nối thêm ` +N` nếu còn N dòng khác. Khi `variantName` rỗng (legacy tombstone), hai phần MUST được trim và
  MUST NOT tạo khoảng trắng đôi. `itemCount` MUST là số dòng; `itemQuantity` MUST là tổng quantity.

**Dữ liệu, kiến trúc và an toàn**

- **FR-028**: Dashboard capability MUST không sở hữu dữ liệu riêng và MUST NOT đọc/ghi trực tiếp storage của
  capability khác; mọi dữ liệu liên capability MUST đi qua published read contracts do capability sở hữu cung cấp.
- **FR-029**: Snapshot danh mục trên dòng đơn hàng MUST được ghi tại checkout và mỗi lần edit thay dòng; dữ
  liệu lịch sử MUST được backfill ở nơi còn resolve được và giữ null ở nơi không.
- **FR-030**: Mỗi request dashboard thành công MUST ghi đúng một audit event `ADMIN_DASHBOARD_VIEWED` với
  metadata `days`, `lowStockThreshold`, `recentOrderCount`; MUST NOT ghi PII hay audit từng customer.
- **FR-031**: Mọi lỗi MUST dùng error format hiện có; vượt hạn mức MUST trả 429 `RATE_LIMITED`, lỗi ngoài dự
  kiến MUST trả 500 `INTERNAL_ERROR`. MUST NOT trả stack trace, chi tiết storage hay PII.
- **FR-032**: Hệ thống MUST tính dashboard bằng số lượng data reads bounded, không tăng theo số order/variant/
  customer.
- **FR-033**: Endpoint MUST được ghi trong tài liệu API chuẩn của dự án và contract máy đọc được trong cùng
  thay đổi.

### Key Entities

- **Dashboard overview**: ảnh chụp read-only tại thời điểm request, gồm KPI, trend, category breakdown và
  recent orders; không phải entity được lưu.
- **Revenue KPI**: doanh thu tháng hiện tại, doanh thu cùng kỳ tháng trước và growth.
- **Order KPI**: tổng đơn all-time, số đơn tháng hiện tại, số đơn cùng kỳ tháng trước, số đơn chờ xác nhận và
  growth.
- **Customer KPI**: tổng khách hàng active và khách hàng active mới trong tuần.
- **Inventory KPI**: số variant tồn kho thấp và ngưỡng đang dùng.
- **Revenue point**: một ngày trong trend, gồm ngày, nhãn ngày và money.
- **Category share**: một danh mục (hoặc "Khác") với money và percentage trong tháng.
- **Recent order**: đơn mới nhất với customer summary, item summary/count/quantity, total, status, createdAt.
- **Order line category snapshot**: cặp danh mục (identity + tên) được chụp tại thời điểm mua, gắn với
  dòng đơn hàng và không phụ thuộc danh mục hiện tại.
- **Low-stock variant**: variant có stock row, không `DISCONTINUED`, với available dưới ngưỡng.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% request ADMIN hợp lệ trả đủ 4 nhóm KPI, trend, category breakdown và recent orders trong
  một response; 0 request nào cần gọi thêm endpoint để hoàn tất dashboard.
- **SC-002**: 100% request từ guest bị từ chối 401 và 100% request từ CUSTOMER bị từ chối 403.
- **SC-003**: Với mọi `days` hợp lệ, trend có đúng `days` phần tử, zero-filled, đúng thứ tự cũ đến mới và
  đúng định dạng nhãn ngày trong 100% test case.
- **SC-004**: Trong 100% test case, tổng category breakdown (gồm "Khác") khớp `currentMonthAmount`.
- **SC-005**: Trong 100% test case, `lowStockCount` trừ đúng active holds còn hạn, bỏ qua hold hết hạn, bỏ qua
  variant `DISCONTINUED` và variant chưa từng có stock row.
- **SC-006**: Dashboard hoàn tất trong dưới 2 giây với dữ liệu thử gồm 10.000 order và 1.000 variant, và số
  truy vấn không tăng theo số order/variant/customer.
- **SC-007**: 0 field tiền nào là float; 100% giá trị tiền có đủ `amount` và `currency`.
- **SC-008**: 100% tham số không hợp lệ trả 400 với `details[].field` đúng, không rò rỉ chi tiết nội bộ.
- **SC-009**: 100% lần xem thành công tạo đúng một audit event không chứa PII.
- **SC-010**: Tài liệu API và contract máy đọc được của endpoint đồng bộ trong cùng thay đổi.

## Dependencies

- **Order (07)**: sáu trạng thái order, order lines và `created_at` là nguồn của revenue/order KPI, trend,
  category breakdown và recent orders.
- **Inventory (05)**: stock levels và active holds là nguồn của low-stock; clock của module quyết định hold nào
  còn hạn.
- **User (02) / Auth (01)**: tài khoản, role, status, email và display name là nguồn của customer KPI và
  customer summary.
- **Product (04)**: trạng thái variant `DISCONTINUED` và danh mục tại thời điểm checkout là nguồn của snapshot
  danh mục và filter low-stock.
- **Foundation**: role ADMIN, session guard, response envelope, error catalogue, audit trail và request
  pipeline đã có; feature dùng lại, không định nghĩa lại.
- **Frontend**: trang Admin Dashboard hiện dùng mock data; frontend guide của feature phải mô tả thay đổi
  contract để FE bỏ mock.

## Assumptions

- **Đã chốt với chủ sản phẩm**:
  - Snapshot danh mục (identity + tên) vào dòng đơn hàng; không join dữ liệu sản phẩm hiện tại.
  - Growth tháng dùng MTD so với cùng kỳ tháng trước, không so với toàn bộ tháng trước.
  - Money object `{ amount, currency }` ở mọi field tiền.
  - Low stock chỉ tính variant có stock row và không `DISCONTINUED`.
  - `recentOrders` cố định 5, không thêm query param.
  - `itemCount` = số dòng, `itemQuantity` = tổng units.
  - Có một audit event `ADMIN_DASHBOARD_VIEWED` mỗi lần xem thành công, không chứa PII.
  - `recentOrders[].customer.displayName` là chuỗi rỗng khi tài khoản tồn tại nhưng chưa đặt tên;
    `customer` chỉ là `null` khi tài khoản không còn tồn tại.
- **MVP chỉ VND**: mọi phép cộng tiền lọc currency `VND`; nếu sau này có currency khác, feature mới phải mở
  rộng cách biểu diễn tiền thay vì cộng trộn.
- **Doanh thu quy theo `orders.created_at`** vì chưa có `paid_at`; nhận doanh thu theo thời điểm thanh toán
  được ghi vào `deferred.md` như cải tiến sau.
- **Khách hàng active** là role `CUSTOMER`, status `active`; tài khoản pending/disabled không được tính.
- **Tuần bắt đầu Thứ Hai** theo timezone cửa hàng.
- **Không cache**: dashboard phản ánh dữ liệu tại thời điểm request; caching/pre-aggregation nằm ngoài phạm vi.
- **Không có thao tác ghi** từ dashboard; mọi hành động vận hành vẫn đi qua endpoint chuyên biệt của từng module.
- **Legacy order lines** có thể không resolve được danh mục; chúng được giữ trong bucket "Khác" thay vì bị loại.
- **Không sửa** debt product capability đang đọc dữ liệu category; feature ghi nhận vào `deferred.md` và không
  thêm truy cập chéo storage mới.
- **Dữ liệu hiện tại** đủ nhỏ để tính trực tiếp; nếu sau này volume tăng, có thể thêm read model/cache bằng
  feature riêng.
