# Frontend Integration Guide — Tồn kho

**Feature**: `007-inventory-tracking`
**Trạng thái**: Hoàn thiện phần endpoint; 5 endpoint của module 05 đã được ghi vào
`docs/api-reference.md` §7.
**Đối tượng đọc**: Lập trình viên frontend (màn quản trị tồn kho).
**Mục tiêu**: FE triển khai màn tồn kho chỉ với tài liệu này — không cần đọc code backend hay
Swagger.

> Nguồn authoritative là [`docs/api-reference.md`](../../docs/api-reference.md) §7. Guide này
> **phải khớp** với file đó; khi lệch, `docs/api-reference.md` thắng. Mọi tên trường dưới đây là
> **đúng** `json:"…"` mà DTO phát ra. Hợp đồng máy đọc được là
> [`contracts/openapi.yaml`](contracts/openapi.yaml).

---

## 0. Tóm tắt nhanh (TL;DR)

- **5 endpoint, tất cả của ADMIN** dưới `/admin/inventory/{productId}`. **Không** có bề mặt tồn
  kho cho khách.
- **Hai hình dạng response.** `StockView` (**3 member**) cho đọc và cho mọi lần ghi;
  `StockMovement` (**9 member**) cho lịch sử.
- **Sản phẩm chưa từng nhập kho trả `0`, không phải `404`.** Chỉ định danh không trỏ tới sản phẩm
  nào mới `404 PRODUCT_NOT_FOUND`.
- **Số lượng là số nguyên không âm.** Đẩy vật lý (hoặc khả dụng) xuống dưới 0 → `409
  INVENTORY_INSUFFICIENT_STOCK`; đưa đúng về 0 thì **thành công**.
- **Điều chỉnh ghi phần chênh lệch**, không phải giá trị tuyệt đối; điều chỉnh về đúng giá trị đang
  lưu **không đổi gì** nhưng vẫn `200`.
- **Giữ chỗ (hold) không có HTTP surface** — không có endpoint nào để FE gọi.
- `physicalQuantity − heldQuantity = availableQuantity`, luôn đúng.
- **Trạng thái bán tự chuyển** khi khả dụng cắt qua 0 (xem §6 dưới, và module 04 §6.8).
- **`meta.total` vắng mặt khi bằng 0** — FE đọc thiếu `total` là `0`.

---

## 1. Quy ước

- Base path `/api/v1`; envelope `{data, meta}` / `{error}` như các guide trước.
- Mọi route là **ADMIN**: `Bearer` + role `ADMIN` (thiếu quyền → `403 FORBIDDEN`, ghi audit
  `AUTH_PRIVILEGE_DENIED`).
- Phân trang lịch sử: `page` default `1` (min `1`), `pageSize` default `20` (`1..100`).
- Module **không** có rate limit riêng; chỉ chịu hạn mức toàn cục (`RATE_LIMIT_RPS`).
- Tác nhân (ai thao tác) lấy từ **session**, không bao giờ từ body. Body có member lạ →
  `MALFORMED_REQUEST` (`additionalProperties: false`).
- `productId` là UUID. Không phải UUID → `400 VALIDATION_ERROR` với `details[].field =
  "productId"`.

### Kiểu dữ liệu

`StockView` (3 member) — đọc tồn kho và response của mọi lần ghi:
```json
{
  "physicalQuantity": 10,
  "heldQuantity": 2,
  "availableQuantity": 8
}
```
- `physicalQuantity`: số trên kệ.
- `heldQuantity`: đang giữ cho các đơn đang thanh toán.
- `availableQuantity`: `physicalQuantity − heldQuantity` — thứ khách có thể lấy.

Cả ba là số nguyên không âm. Định danh sản phẩm nằm ở **đường dẫn**, không lặp trong body.

`StockMovement` (9 member) — một phần tử của lịch sử:
```json
{
  "id": "0f5c6e0c-1a44-4a1e-9b3d-9a1b2c3d4e5f",
  "productId": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e",
  "kind": "RESTOCK",
  "delta": 10,
  "resultingQuantity": 10,
  "sourceReference": null,
  "actorId": "11111111-1111-1111-1111-111111111111",
  "note": "hàng về kho",
  "createdAt": "2026-10-09T08:15:04Z"
}
```
- `delta`: **có dấu** — dương khi nhập thêm, âm khi hư hỏng / bán.
- `resultingQuantity`: số vật lý sau thay đổi.
- `sourceReference`: `null` với thao tác thủ công; có giá trị khi một sự kiện ngoài gây ra.
- `actorId`: `null` khi hệ thống gây ra (một đơn được thanh toán); có giá trị khi admin làm.
- `note`: `null` khi không có.

`QuantityRequest` (2 member) — body của `restock` và `damage`:
```json
{ "quantity": 10, "note": "hàng về kho" }
```
`quantity` bắt buộc, số nguyên **≥ 1**; `note` tuỳ chọn.

`AdjustmentRequest` (2 member) — body của `adjustment`:
```json
{ "quantity": 7, "note": "kiểm kê" }
```
`quantity` bắt buộc, số nguyên **≥ 0** (0 là giá trị đếm hợp lệ); `note` tuỳ chọn.

---

## 2. Bảng endpoint

| # | Method | Path | Auth | Trả về |
|---|---|---|---|---|
| 7.1 | GET | `/api/v1/admin/inventory/{productId}` | ADMIN | `200` `StockView` |
| 7.2 | GET | `/api/v1/admin/inventory/{productId}/movements` | ADMIN | `200` list `StockMovement` + phân trang |
| 7.3 | POST | `/api/v1/admin/inventory/{productId}/restock` | ADMIN | `200` `StockView` |
| 7.4 | POST | `/api/v1/admin/inventory/{productId}/damage` | ADMIN | `200` `StockView` |
| 7.5 | POST | `/api/v1/admin/inventory/{productId}/adjustment` | ADMIN | `200` `StockView` |

### Chi tiết

**7.1 `GET /admin/inventory/{productId}`** — đọc ba số. Sản phẩm **chưa từng nhập kho** trả cả ba
bằng `0`. Định danh không trỏ sản phẩm nào → `404 PRODUCT_NOT_FOUND`.
Lỗi: `VALIDATION_ERROR` 400 (`productId`) · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 ·
`PRODUCT_NOT_FOUND` 404 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500.

**7.2 `GET /admin/inventory/{productId}/movements`** — lịch sử **cũ nhất trước**, phân trang.
Sản phẩm chưa có biến động trả `data: []` (không phải `null`, không phải lỗi). Query `page`,
`pageSize`.
Lỗi: `VALIDATION_ERROR` 400 (`productId`, `page`, `pageSize`) · `UNAUTHENTICATED` 401 ·
`FORBIDDEN` 403 · `PRODUCT_NOT_FOUND` 404 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500.

**7.3 `POST /admin/inventory/{productId}/restock`** — cộng thêm một lượng dương. Ghi ledger
`RESTOCK` + audit `INVENTORY_RESTOCKED`.
Lỗi: `VALIDATION_ERROR` 400 (`quantity` thiếu/không nguyên/âm/bằng 0) · `MALFORMED_REQUEST` 400 ·
`UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `PRODUCT_NOT_FOUND` 404 · `RATE_LIMITED` 429 ·
`INTERNAL_ERROR` 500.

**7.4 `POST /admin/inventory/{productId}/damage`** — trừ đi một lượng dương. Bị từ chối `409
INVENTORY_INSUFFICIENT_STOCK` nếu vượt quá số trên kệ, **hoặc** nếu để lại trên kệ ít hơn phần đang
giữ; khi đó **không có gì đổi** và **không** có dòng ledger mới. Ghi ledger `DAMAGE` + audit
`INVENTORY_DAMAGED`.
Lỗi: `VALIDATION_ERROR` 400 (`quantity`) · `MALFORMED_REQUEST` 400 · `UNAUTHENTICATED` 401 ·
`FORBIDDEN` 403 · `PRODUCT_NOT_FOUND` 404 · `INVENTORY_INSUFFICIENT_STOCK` 409 · `RATE_LIMITED` 429 ·
`INTERNAL_ERROR` 500.

**7.5 `POST /admin/inventory/{productId}/adjustment`** — đặt số vật lý về **giá trị đã đếm**, ghi
phần chênh lệch có dấu. Điều chỉnh về **đúng** giá trị đang lưu vẫn `200` nhưng **không** ghi ledger
(vẫn ghi audit `INVENTORY_ADJUSTED`). Giá trị đếm thấp hơn phần đang giữ → `409`.
Lỗi: `VALIDATION_ERROR` 400 (`quantity` thiếu/không nguyên/âm) · `MALFORMED_REQUEST` 400 ·
`UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `PRODUCT_NOT_FOUND` 404 ·
`INVENTORY_INSUFFICIENT_STOCK` 409 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500.

---

## 3. Bảng giá trị / trạng thái

| Trường | Giá trị | Ý nghĩa / FE |
|---|---|---|
| `kind` | `RESTOCK` | Nhập thêm hàng (tăng) |
| `kind` | `DAMAGE` | Hư hỏng (giảm) |
| `kind` | `ADJUSTMENT` | Điều chỉnh về giá trị đã đếm (tăng hoặc giảm, `delta` có dấu) |
| `kind` | `SALE` | Một đơn đã thanh toán biến giữ chỗ thành giảm (hệ thống, không do admin) |
| `delta` | số nguyên ≠ 0 | Có dấu; ledger không bao giờ chứa `delta: 0` |
| `resultingQuantity` | số nguyên ≥ 0 | Số vật lý sau thay đổi |
| `sourceReference` | chuỗi hoặc `null` | Có khi sự kiện ngoài gây ra; `null` với thao tác thủ công |
| `actorId` | uuid hoặc `null` | Có khi admin làm; `null` khi hệ thống làm |
| `physicalQuantity` / `heldQuantity` / `availableQuantity` | số nguyên ≥ 0 | Vật lý / đang giữ / khả dụng; hiệu bằng `availableQuantity` |

**Giữ chỗ không có HTTP surface.** FE **không** có cách nào tự đặt chỗ; `heldQuantity` chỉ là con
số đọc được, do luồng thanh toán (module 07/08 chưa tồn tại) tạo ra.

---

## 4. Trước → sau

### Endpoint MỚI (5)

Toàn bộ endpoint ở §2 là **mới** trong feature này. Module 05 chưa từng có endpoint nào trước đó.

Không có endpoint nào của module khác bị **đổi** bởi feature này.

---

## 5. Bảng mã lỗi

**Dùng chung**: `VALIDATION_ERROR` 400, `MALFORMED_REQUEST` 400, `UNAUTHENTICATED` 401,
`FORBIDDEN` 403, `RATE_LIMITED` 429, `INTERNAL_ERROR` 500.

**Riêng module inventory** (1 mã):

| Code | HTTP | FE nên làm |
|---|---|---|
| `INVENTORY_INSUFFICIENT_STOCK` | 409 | Không đủ hàng để trừ, hoặc thao tác sẽ phá một giữ chỗ. Hiện thông báo "không đủ tồn kho"; gợi ý nhập thêm hàng/chờ giữ chỗ được giải quyết. **Không** sửa số rồi gửi lại mù quáng |

**Tái sử dụng từ module 04** (1 mã, xuất hiện trên **cả năm** route):

| Code | HTTP | FE nên làm |
|---|---|---|
| `PRODUCT_NOT_FOUND` | 404 | `productId` không trỏ sản phẩm nào. Hiện "không tìm thấy sản phẩm", quay về danh sách |

---

## 6. Trạng thái bán tự chuyển (module 04)

Khi khả dụng cắt qua 0 trong cùng thao tác, backend tự đổi `sellState` của sản phẩm (module 04 §6.8)
mà **không** cần FE gọi gì:

- khả dụng về 0 → sản phẩm thành `OUT_OF_STOCK`;
- khả dụng từ 0 lên dương → sản phẩm trở lại `ACTIVE`.

Một thay đổi **không** cắt qua 0 **không** đụng tới trạng thái bán — nhãn `OUT_OF_STOCK` do operator
đặt tay được giữ nguyên. Sản phẩm `COMING_SOON` và `DISCONTINUED` không bao giờ bị tồn kho thay đổi.

Đây là nửa tự động của luồng "trạng thái tự chuyển khi hết/hồi kho" mà feature 006 để lại (nghĩa vụ
D1). FE chỉ cần tải lại sản phẩm nếu muốn thấy trạng thái mới.

---

## 7. Checklist FE

- [ ] Màn tồn kho chỉ gọi `/admin/inventory*` với token `ADMIN`.
- [ ] Hiển thị đủ ba số: vật lý, đang giữ, khả dụng; không tự suy ra một số thứ tư.
- [ ] Ba nút riêng: Nhập thêm (restock), Hư hỏng (damage), Điều chỉnh (adjustment) — **không** dùng
      chung một ô "loại".
- [ ] Xử lý `409 INVENTORY_INSUFFICIENT_STOCK` riêng, hiển thị cạnh ô số lượng.
- [ ] Xử lý `404 PRODUCT_NOT_FOUND` bằng một thông báo chung.
- [ ] Điều chỉnh về đúng giá trị hiện tại vẫn thành công — không báo lỗi.
- [ ] `quantity` gửi dạng số nguyên; restock/damage ≥ 1, adjustment ≥ 0.
- [ ] Chỉ gửi đúng member trong contract (`additionalProperties: false` → member lạ trả
      `MALFORMED_REQUEST`); **không** gửi `actorId` hay định danh chủ sở hữu.
- [ ] Đọc `meta.total` thiếu là `0`; xử lý `data: []`.
- [ ] `kind` là một trong `RESTOCK`, `DAMAGE`, `ADJUSTMENT`, `SALE`.

---

## 8. Changelog & đối chiếu

**2026-10-09** — Viết guide lần đầu cho feature 007 (hiến pháp v1.8.0 §Governance).

Đối chiếu với implementation (T066), **có đếm** — mọi con số dưới đây được đếm lại từ chính nguồn,
không chép từ tài liệu khác. **Không phát hiện lệch; không phải sửa gì.**

**Endpoint và route** — 5, đối chiếu hai chiều:

| Hướng | Nguồn | Kết quả |
|---|---|---|
| Guide → code | §2 liệt kê **5** mục | `internal/modules/inventory/presentation/http/router.go` khai đúng **5** route |
| Code → guide | router **5 route** | cả 5 đều có mặt ở §2 |

Cùng khớp `specs/007-inventory-tracking/contracts/openapi.yaml` (5 path dưới `paths:`) và
`docs/api-reference.md` §7 (5 mục `7.1`–`7.5`). **5 mới, 0 đổi.**

**Trường JSON** — `internal/modules/inventory/presentation/dto/dto.go` có **16 tag `json:"…"`**
trên **4 kiểu**, thành **13 tên member phân biệt**:

| Kiểu | Số member | Member |
|---|---|---|
| `StockResponse` | 3 | `physicalQuantity`, `heldQuantity`, `availableQuantity` |
| `MovementResponse` | 9 | `id`, `productId`, `kind`, `delta`, `resultingQuantity`, `sourceReference`, `actorId`, `note`, `createdAt` |
| `QuantityRequest` | 2 | `quantity`, `note` |
| `AdjustmentRequest` | 2 | `quantity`, `note` |

13/13 tên phân biệt đều có mặt trong guide (đối chiếu tự động: **13/13 khớp**). Hai tên `quantity`
và `note` xuất hiện ở hai kiểu request vẫn là **một** tên phân biệt, nên 16 tag → 13 tên.

**Giá trị `kind`** — **4**, đối chiếu hai chiều với
`internal/modules/inventory/domain/constant/movement.go`: `RESTOCK`, `DAMAGE`, `ADJUSTMENT`, `SALE`.
Mọi giá trị guide nêu đều có trong code, và mọi hằng trong code đều có trong guide (§3).

**Mã lỗi** — **1** mã riêng của module (`INVENTORY_INSUFFICIENT_STOCK`, khớp
`domain/constant/codes.go` và `contracts/error-codes.md`) cộng **1** mã **tái sử dụng**
(`PRODUCT_NOT_FOUND` của module 04, xuất hiện trên cả 5 route).

**HTTP status khẳng định** — **8**: `200`, `400`, `401`, `403`, `404`, `409`, `429`, `500`. Mỗi
status là một câu trả lời thật: `200` do handler; `400` (`VALIDATION_ERROR` / `MALFORMED_REQUEST`);
`401` (`UNAUTHENTICATED` do middleware/handler); `403` (`FORBIDDEN` do guard `ADMIN`); `404`
(`PRODUCT_NOT_FOUND`); `409` (`INVENTORY_INSUFFICIENT_STOCK` do `mapError`); `429` (`RATE_LIMITED`);
`500` (`INTERNAL_ERROR` mặc định của `mapError`). Module **không** dùng `201`, `204`, `413`, `503`
nên các status đó vắng mặt có chủ ý.
