# Frontend Integration Guide — Admin Dashboard Overview (017)

Tài liệu bàn giao cho client về feature **Admin Dashboard Overview** (module 16 Admin).
Nó liệt kê **mọi endpoint bị ảnh hưởng** với method + path, đánh dấu **mới / đổi / không đổi**,
và mô tả đủ hình dạng response để frontend **bỏ mock data**. Nguồn authoritative vẫn là
[`docs/api-reference.md`](../../docs/api-reference.md) (mục 11) và
[`contracts/openapi.yaml`](contracts/openapi.yaml); tài liệu này là điểm vào nhanh cho frontend.

## 1. Tóm tắt một câu

Thêm **một** endpoint `GET /api/v1/admin/dashboard/overview` (role `ADMIN`) trả **toàn bộ** dashboard
vận hành trong **một** request read-only — bốn nhóm KPI, chuỗi doanh thu theo ngày, cơ cấu doanh số theo
danh mục và 5 đơn mới nhất — nên frontend **không** cần gọi thêm endpoint nào để dựng trang. Feature
**không** sửa/xoá endpoint nào khác; **không** có endpoint ghi.

## 2. Bảng endpoint bị ảnh hưởng

| Method | Path | Trạng thái | Ghi chú |
|---|---|---|---|
| GET | `/api/v1/admin/dashboard/overview` | **Mới** | Bearer access token, role `ADMIN`; query `days`, `lowStockThreshold`; response là `OverviewResponse` bọc trong envelope `{ data, meta }` |

Mọi endpoint khác **không đổi** và không liệt kê ở đây. Tác nhân lấy từ **session**, không bao giờ từ
query/body.

## 3. Chi tiết endpoint

### 3.1 `GET /api/v1/admin/dashboard/overview` — **Mới**

| | |
|---|---|
| Auth | `Authorization: Bearer <accessToken>`, role `ADMIN` |
| Query | `days` (tuỳ chọn), `lowStockThreshold` (tuỳ chọn) |
| Rate limit | Hạn mức toàn cục `/api/v1` (module không có hạn mức riêng) |
| Trả về | `200 OK` |

Query:

| Tham số | Kiểu | Mặc định | Giá trị hợp lệ | Nghĩa |
|---|---|---|---|---|
| `days` | int | `7` | `7` hoặc `30` | Số ngày của `revenueTrend` |
| `lowStockThreshold` | int | `5` | `0..10000` | Ngưỡng tồn kho thấp; echo lại ở `kpis.inventory.threshold` |

`days` **chỉ** đổi độ dài `revenueTrend`; các KPI theo tháng **không** đổi theo `days`.

Response `200` — envelope chuẩn, `data` là `OverviewResponse`:

```json
{
  "data": {
    "kpis": {
      "revenue": {
        "currentMonthAmount": { "amount": 48250000, "currency": "VND" },
        "lastMonthAmount": { "amount": 42000000, "currency": "VND" },
        "growthPercent": 14.88
      },
      "orders": {
        "totalOrders": 1482,
        "thisMonthOrders": 186,
        "lastMonthOrders": 172,
        "pendingConfirmationCount": 9,
        "growthPercent": 8.14
      },
      "customers": { "totalUsers": 412, "newUsersThisWeek": 28 },
      "inventory": { "lowStockCount": 3, "threshold": 5 }
    },
    "revenueTrend": [
      { "date": "2026-10-12", "dayLabel": "T2", "amount": { "amount": 4200000, "currency": "VND" } },
      { "date": "2026-10-13", "dayLabel": "T3", "amount": { "amount": 5800000, "currency": "VND" } }
    ],
    "categoryBreakdown": [
      { "categoryId": "019270bb-336c-76e3-85f0-f925b4fa6c01", "categoryName": "Acrylic Stand",
        "amount": { "amount": 22195000, "currency": "VND" }, "percentage": 46.0 },
      { "categoryId": null, "categoryName": "Khác",
        "amount": { "amount": 360000, "currency": "VND" }, "percentage": 0.7 }
    ],
    "recentOrders": [
      {
        "id": "019270bb-336c-76e3-85f0-f925b4fa6c10",
        "customer": {
          "id": "019270bb-336c-76e3-85f0-f925b4fa6c99",
          "displayName": "Nguyễn Minh Anh",
          "email": "minhanh.ng@gmail.com"
        },
        "itemSummary": "Acrylic Stand Elf Băng Giá (x2) +1",
        "itemCount": 2,
        "itemQuantity": 3,
        "total": { "amount": 360000, "currency": "VND" },
        "status": "COMPLETED",
        "createdAt": "2026-10-17T04:02:00Z"
      }
    ]
  },
  "meta": { "requestId": "…", "timestamp": "2026-10-17T04:15:04Z" }
}
```

#### Tiền là object ở **mọi** field tiền

**Không** có field tiền nào là number trần. Mọi giá trị tiền — kể cả trong `revenueTrend` và
`categoryBreakdown` — là object:

```json
{ "amount": <integer minor unit>, "currency": "VND" }
```

`amount` là số nguyên đơn vị nhỏ nhất (VND), **không** phải float. Client format số này để hiển thị,
đừng tự nhân/chia rồi parse float.

#### Các field nullable

| Field | `null` khi nào | Client nên làm gì |
|---|---|---|
| `kpis.revenue.growthPercent` | doanh thu cùng kỳ tháng trước = 0 | Hiển thị "không có mốc so sánh", **không** hiển thị `0%` |
| `kpis.orders.growthPercent` | số đơn cùng kỳ tháng trước = 0 | Như trên |
| `categoryBreakdown[].categoryId` | dòng đơn legacy không resolve được danh mục | Đây là bucket `"Khác"`; vẫn hiển thị |
| `recentOrders[].customer` | tài khoản khách không còn tồn tại | Hiển thị đơn với khách trống; các field còn lại vẫn đúng |

`growthPercent` khi có giá trị được làm tròn **2 chữ số thập phân** và giữ dấu (âm được phép).
`categoryBreakdown[].percentage` làm tròn **1 chữ số**; tổng có thể lệch 100 do làm tròn — **đừng** sửa
một dòng để ép đủ 100.

#### `orders` — nghĩa các field

| Field | Nghĩa |
|---|---|
| `totalOrders` | **Tổng all-time, mọi trạng thái** (kể cả `CANCELLED`) |
| `thisMonthOrders` | Số đơn tạo trong tháng hiện tại (mọi trạng thái) |
| `lastMonthOrders` | Số đơn tạo trong **cùng kỳ tháng trước** (cùng độ dài thời gian, không phải cả tháng) |
| `pendingConfirmationCount` | Số đơn `PENDING` all-time (chờ artist xác nhận; **không** gồm `PAYMENT_PENDING`) |
| `growthPercent` | `(thisMonthOrders − lastMonthOrders) / lastMonthOrders × 100`, làm tròn 2 chữ số; `null` khi mẫu = 0 |

#### `itemCount` **khác** `itemQuantity`

- `recentOrders[].itemCount` = **số dòng hàng** trong đơn.
- `recentOrders[].itemQuantity` = **tổng số lượng units** qua các dòng.

Ví dụ một đơn có 2 dòng (`x2` và `x1`): `itemCount = 2`, `itemQuantity = 3`. Đừng dùng lẫn hai giá trị.

`itemSummary` là chuỗi mô tả dòng đầu theo `position`, dạng `<name> <variantName> (x<quantity>)`, nối
` +N` nếu còn N dòng khác (ví dụ `"Acrylic Stand Elf Băng Giá (x2) +1"`).

#### `revenueTrend`

- Luôn có **đúng `days`** phần tử, **cũ đến mới**, ngày không có doanh thu được **zero-fill**
  (`amount = 0`) — client **không** cần tự bù ngày trống.
- `date` dạng `YYYY-MM-DD`; `dayLabel` xem mục 5.
- Ngày hôm nay chỉ tính đến thời điểm request.

#### `categoryBreakdown`

- Group theo **danh mục lúc bán** (snapshot trên dòng đơn), không theo danh mục hiện tại — đổi/xoá danh
  mục sau đó không làm số cũ nhảy.
- Danh sách sắp xếp `amount` giảm dần, tie-break `categoryName` tăng dần; chỉ trả danh mục có
  `amount > 0`; có thể **rỗng** (`[]`, không phải `null`).
- Tổng các `amount` khớp `kpis.revenue.currentMonthAmount`.

## 4. Trạng thái đơn — 7 giá trị thô và nhãn FE

`recentOrders[].status` là **một trong bảy** giá trị thô của backend. API **không** dịch sang nhãn hiển
thị — frontend tự ánh xạ. Gợi ý nhãn tiếng Việt:

| `status` (BE) | Nhãn FE gợi ý | Nghĩa |
|---|---|---|
| `PENDING` | Chờ xác nhận | Đơn mới, chờ artist xác nhận |
| `PAYMENT_PENDING` | Chờ thanh toán | Artist đã xác nhận, khách đang trong hạn thanh toán |
| `PAID` | Đã thanh toán | Đã nhận tiền |
| `SHIPPING` | Đang giao | Artist đã giao cho bên vận chuyển (**đổi tên từ `SHIPPED`** ở feature 019) |
| `DELIVERED` | Đã giao | Hàng đã tới tay khách (**mới** ở feature 019) |
| `COMPLETED` | Hoàn tất | Khách xác nhận hoặc hệ thống tự hoàn tất (cuối) |
| `CANCELLED` | Đã huỷ | Khách huỷ / artist từ chối / hết hạn (cuối) |

Client rẽ nhánh theo **giá trị thô**; nhãn là do FE quyết định. Không có trạng thái nào khác được trả
ra (ràng buộc ở tầng lưu trữ).

> **Cập nhật bởi feature 019:** giá trị `SHIPPED` **đổi tên** thành `SHIPPING` (đang giao) và thêm
> `DELIVERED` (giao thành công) — xem [`specs/019-order-delivery/frontend-guide.md`](../019-order-delivery/frontend-guide.md)
> và ADR 023. FE phải xử lý **bảy** giá trị; `SHIPPED` không còn được trả ra.

## 5. `dayLabel` — 7 ngày so với 30 ngày

`revenueTrend[].dayLabel` phụ thuộc `days`:

| `days` | Dạng `dayLabel` | Ví dụ |
|---|---|---|
| `7` | Thứ trong tuần: `T2`, `T3`, `T4`, `T5`, `T6`, `T7`, `CN` | `T2` (Thứ Hai), `CN` (Chủ Nhật) |
| `30` | Ngày/tháng `DD/MM` | `18/09`, `17/10` |

`date` vẫn luôn là `YYYY-MM-DD` bất kể `days`; ranh giới ngày/tháng theo **timezone cửa hàng**
(`SHOP_TIMEZONE`, mặc định `Asia/Ho_Chi_Minh`), không theo múi giờ trình duyệt.

## 6. HTTP status

- `200`: dashboard hợp lệ.
- `400`: `VALIDATION_ERROR` — `days` ngoài {7, 30} hoặc `lowStockThreshold` ngoài 0..10000;
  `error.details[].field` là `"days"` hoặc `"lowStockThreshold"`.
- `401`: `UNAUTHENTICATED` — thiếu/sai phiên (guest).
- `403`: `FORBIDDEN` — phiên hợp lệ nhưng **không** phải `ADMIN`.
- `429`: `RATE_LIMITED` — vượt hạn mức toàn cục; có header `Retry-After`.
- `500`: `INTERNAL_ERROR`.

**Không** có `404`: route tĩnh, không có định danh tài nguyên. Không có tình huống "chưa có dữ liệu"
nào là lỗi: amount bằng 0, `revenueTrend` vẫn zero-fill, các mảng có thể rỗng.

## 7. Phụ lục đồng bộ (sync appendix)

Lần đồng bộ này đọc lại source ngày **2026-10-17** và đếm theo quy tắc frontend-guide sync: **mọi** thẻ
`json:"…"` của các DTO mà feature chạm, **mọi** giá trị enum mà guide nêu, **mọi** route mà guide liệt
kê, **mọi** HTTP status mà guide tuyên bố.

### 7.1 Field của DTO (11 DTO, 40 thẻ `json` trực tiếp)

Nguồn: `internal/modules/admin/presentation/dto/overview.go`.

- `OverviewResponse` (**4**): `kpis`, `revenueTrend`, `categoryBreakdown`, `recentOrders`.
- `KPIResponse` (**4**): `revenue`, `orders`, `customers`, `inventory`.
- `RevenueKPIResponse` (**3**): `currentMonthAmount`, `lastMonthAmount`, `growthPercent`.
- `OrderKPIResponse` (**5**): `totalOrders`, `thisMonthOrders`, `lastMonthOrders`,
  `pendingConfirmationCount`, `growthPercent`.
- `CustomerKPIResponse` (**2**): `totalUsers`, `newUsersThisWeek`.
- `InventoryKPIResponse` (**2**): `lowStockCount`, `threshold`.
- `RevenuePointResponse` (**3**): `date`, `dayLabel`, `amount`.
- `CategoryShareResponse` (**4**): `categoryId`, `categoryName`, `amount`, `percentage`.
- `RecentOrderResponse` (**8**): `id`, `customer`, `itemSummary`, `itemCount`, `itemQuantity`, `total`,
  `status`, `createdAt`.
- `CustomerSummaryResponse` (**3**): `id`, `displayName`, `email`.
- `MoneyResponse` (**2**): `amount`, `currency`.

Tổng: 4+4+3+5+2+2+3+4+8+3+2 = **40** thẻ `json` trên **11** DTO. Ví dụ response ở mục 3 và danh sách
member ở mục 3 khớp **đủ 40** tên field (qua các object lồng nhau).

### 7.2 Enum (1 enum, 7 giá trị)

- `recentOrders[].status` — **7** giá trị: `PENDING`, `PAYMENT_PENDING`, `PAID`, `SHIPPING`, `DELIVERED`,
  `COMPLETED`, `CANCELLED` (feature 019 **đổi tên** `SHIPPED` → `SHIPPING` và thêm `DELIVERED`). Nguồn:
  `internal/modules/order/domain/constant/codes.go` (`StatusPending` … `StatusCancelled`) và check
  constraint `orders_status_ck` (`migrations/00018_order_delivery.sql`). Mục 4 liệt kê **đủ 7**.

### 7.3 Route (1 route, 1 thêm / 0 gỡ / 0 đổi)

- `GET /api/v1/admin/dashboard/overview` — route duy nhất. Nguồn: `@Router /admin/dashboard/overview
  [get]` trong `internal/modules/admin/presentation/http/handler.go`, `r.Get("/overview", …)` trong
  `internal/modules/admin/presentation/http/router.go`, mount `r.Mount("/admin/dashboard", …)` trong
  `cmd/api/main.go`, dưới base path `/api/v1`. Khớp api-reference mục 11 và OpenAPI.

### 7.4 HTTP status (6 status)

`200`, `400`, `401`, `403`, `429`, `500` — nguồn: `@Success 200`, `@Failure 400/401/403/429/500` trong
handler annotation, khớp `docs/swagger/swagger.yaml` (`/admin/dashboard/overview`) và mục 6 của guide.
Handler trực tiếp trả `200` (`httpx.WriteSuccess`) và `400`/`401`; `403` đến từ middleware `RequireRole`,
`429` từ limiter toàn cục, `500` từ ánh xạ lỗi ngoài dự kiến.

### 7.5 Mismatch đã tìm và sửa

Không có mismatch: guide này được viết sau khi source đã ổn định, đối chiếu với 40 thẻ `json`, 6 giá trị
enum, 1 route và 6 HTTP status — tất cả khớp. (Feature chưa có guide trước đó nên không có nội dung cũ
phải sửa.)
