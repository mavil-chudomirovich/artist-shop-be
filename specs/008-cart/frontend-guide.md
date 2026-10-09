# Frontend Integration Guide — Giỏ hàng

**Feature**: `008-cart`
**Trạng thái**: Bốn endpoint của module 06 đã được ghi vào `docs/api-reference.md` §8 và sinh vào
`docs/swagger/`.
**Đối tượng đọc**: Lập trình viên frontend (giỏ hàng của khách đã đăng nhập).
**Mục tiêu**: FE triển khai giỏ chỉ với tài liệu này — không cần đọc code backend hay Swagger.

> Nguồn authoritative là [`docs/api-reference.md`](../../docs/api-reference.md) §8. Guide này
> **phải khớp** với file đó; khi lệch, `docs/api-reference.md` thắng. Mọi tên trường dưới đây là
> **đúng** `json:"…"` mà DTO phát ra. Hợp đồng máy đọc được là
> [`contracts/openapi.yaml`](contracts/openapi.yaml).

---

## 0. Tóm tắt nhanh (TL;DR)

- **4 endpoint, tất cả của KHÁCH đã đăng nhập**, dưới `/cart`. **Không** có bề mặt quản trị.
- Giỏ là **tài nguyên đơn**: gọi `/cart` **không kèm định danh**. Chủ sở hữu là **session**; FE
  **không bao giờ** gửi id chủ sở hữu hay id giỏ.
- Một dòng định địa chỉ bằng **`productId`** (không có `lineId` riêng): mỗi sản phẩm có tối đa một
  dòng trong giỏ.
- **`GET /cart` đối chiếu lại từng dòng**; mỗi dòng mang cờ `buyable` (luôn có) và `availableQuantity`
  (**chỉ** khi dòng còn bán nhưng thiếu hàng).
- **Giá là snapshot lúc thêm** (`unitPrice`). Đọc giỏ **không** đổi giá, không đổi số lượng, không
  xoá dòng.
- **Thêm lại một sản phẩm đã có = cộng dồn số lượng** vào dòng đó, không tạo dòng thứ hai.
- **Giỏ rỗng trả `lines: []` và `subtotal: null`**, không phải `404`. Đọc giỏ cũng **không** tạo giỏ.
- Bị từ chối một thao tác (không bán được / quá tồn) thì **không có gì đổi**.
- `PRODUCT_NOT_FOUND` 404 phục vụ **hai** việc: sản phẩm không tồn tại/đã xoá, **và** dòng không
  thuộc giỏ này — FE xử lý chung một thông báo.
- Module **không** có rate limit riêng; chỉ chịu hạn mức toàn cục.

---

## 1. Quy ước

- Base path `/api/v1`; envelope `{data, meta}` / `{error}` như các guide trước.
- Mọi route yêu cầu **Bearer** của khách đã đăng nhập. Thiếu/sai token → `401 UNAUTHENTICATED`.
- Tác nhân (chủ giỏ) lấy từ **session**, không bao giờ từ body. Body có member lạ (kể cả member tên
  chủ sở hữu) → `MALFORMED_REQUEST` (`additionalProperties: false`).
- `productId` là UUID. Không phải UUID → `400 VALIDATION_ERROR` với `details[].field = "productId"`.
- `quantity` là **số nguyên dương**. Thiếu, bằng `0`, âm hoặc không nguyên → `400 VALIDATION_ERROR`
  với `details[].field = "quantity"`. Dọn một dòng bằng **xoá**, không bằng đặt `0`.
- Tiền là **số nguyên đơn vị nhỏ nhất + currency**, không bao giờ là số thực.

### Kiểu dữ liệu

`MoneyResponse` (2 member) — tiền:
```json
{ "amount": 150000, "currency": "VND" }
```
- `amount`: số nguyên, đơn vị nhỏ nhất của `currency`.
- `currency`: ba chữ in hoa.

`CartLineResponse` (8 member) — một dòng. `availableQuantity` **chỉ có mặt** khi dòng **còn bán nhưng
thiếu** (tức `buyable: false`), nên hai ví dụ dưới đây tách đúng hai trường hợp.

Dòng **mua được** — `buyable: true`, **không** có `availableQuantity` (khớp `docs/api-reference.md`
§8.1):
```json
{
  "productId": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e",
  "name": "Tranh sơn dầu",
  "slug": "tranh-son-dau",
  "quantity": 2,
  "unitPrice": { "amount": 150000, "currency": "VND" },
  "lineTotal": { "amount": 300000, "currency": "VND" },
  "buyable": true
}
```

Dòng **còn bán nhưng thiếu** — `buyable: false` **và** có `availableQuantity`:
```json
{
  "productId": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e",
  "name": "Tranh sơn dầu",
  "slug": "tranh-son-dau",
  "quantity": 2,
  "unitPrice": { "amount": 150000, "currency": "VND" },
  "lineTotal": { "amount": 300000, "currency": "VND" },
  "buyable": false,
  "availableQuantity": 1
}
```
- `productId`: UUID của sản phẩm — cũng là khoá của dòng trong URL.
- `name` / `slug`: giá trị **hiện tại** của sản phẩm; **`null`** khi sản phẩm đã bị xoá.
- `quantity`: số nguyên **≥ 1**, số khách đã chọn.
- `unitPrice`: giá **snapshot** lúc thêm — không phải giá hiện tại.
- `lineTotal`: `quantity × unitPrice`.
- `buyable`: **luôn có**; `true` khi sản phẩm đang bán **và** tồn khả dụng ≥ `quantity`.
- `availableQuantity`: **chỉ có mặt** khi `buyable: false` **vì còn bán nhưng thiếu** (tồn <
  `quantity`, kể cả 0). **Vắng mặt** khi `buyable: true`, hoặc khi sản phẩm ngừng bán/đã xoá.

`CartResponse` (2 member) — cả giỏ, response của `GET`, `POST` và `PATCH`:
```json
{
  "lines": [ /* CartLineResponse… */ ],
  "subtotal": { "amount": 300000, "currency": "VND" }
}
```
- `lines`: mảng; giỏ rỗng là `[]` (không bao giờ `null`).
- `subtotal`: tổng `quantity × unitPrice` của mọi dòng; **`null`** khi giỏ rỗng (không có dòng nào
  thì không có tiền để diễn đạt).

`AddItemRequest` (2 member) — body của `POST /cart/items`:
```json
{ "productId": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e", "quantity": 2 }
```

`QuantityRequest` (1 member) — body của `PATCH /cart/items/{productId}`:
```json
{ "quantity": 5 }
```

---

## 2. Bảng endpoint

| # | Method | Path | Auth | Trả về |
|---|---|---|---|---|
| 8.1 | GET | `/api/v1/cart` | Khách | `200` `CartResponse` |
| 8.2 | POST | `/api/v1/cart/items` | Khách | `200` `CartResponse` |
| 8.3 | PATCH | `/api/v1/cart/items/{productId}` | Khách | `200` `CartResponse` |
| 8.4 | DELETE | `/api/v1/cart/items/{productId}` | Khách | `204` (không body) |

### Chi tiết

**8.1 `GET /cart`** — đọc giỏ, **đối chiếu lại từng dòng** với trạng thái bán và tồn khả dụng hiện
tại. Giỏ rỗng trả `data.lines: []` và `data.subtotal: null`; khách chưa từng thêm gì nhận giỏ rỗng
chứ **không** phải `404`, và đọc **không** tạo giỏ. Đọc không đổi `quantity`/`unitPrice`, không xoá
dòng.
Lỗi: `UNAUTHENTICATED` 401 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500.

**8.2 `POST /cart/items`** — thêm sản phẩm với `quantity` nguyên ≥ 1. Sản phẩm **đã có trong giỏ**
thì **cộng dồn** vào dòng đó (giữ giá snapshot của lần thêm đầu), **không** tạo dòng thứ hai.
`200` trả cả giỏ sau thay đổi.
Lỗi: `VALIDATION_ERROR` 400 (`productId` không phải UUID; `quantity` thiếu/0/âm/không nguyên) ·
`MALFORMED_REQUEST` 400 (body hỏng hoặc member lạ) · `UNAUTHENTICATED` 401 · `PRODUCT_NOT_FOUND` 404 ·
`CART_PRODUCT_NOT_PURCHASABLE` 409 · `CART_QUANTITY_EXCEEDS_AVAILABLE` 409 · `RATE_LIMITED` 429 ·
`INTERNAL_ERROR` 500.

**8.3 `PATCH /cart/items/{productId}`** — đặt số lượng của dòng về giá trị mới (nguyên ≥ 1), thay
hẳn số cũ. Sản phẩm nay ngừng bán → `409 CART_PRODUCT_NOT_PURCHASABLE`; vượt tồn khả dụng hiện tại →
`409 CART_QUANTITY_EXCEEDS_AVAILABLE`; **số lượng cũ được giữ**. `productId` không phải dòng của giỏ
này → `404 PRODUCT_NOT_FOUND`.
Lỗi: `VALIDATION_ERROR` 400 (`productId`/`quantity`) · `MALFORMED_REQUEST` 400 · `UNAUTHENTICATED`
401 · `PRODUCT_NOT_FOUND` 404 · `CART_PRODUCT_NOT_PURCHASABLE` 409 ·
`CART_QUANTITY_EXCEEDS_AVAILABLE` 409 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500.

**8.4 `DELETE /cart/items/{productId}`** — xoá dòng. Thành công trả `204` **không body**. Xoá một
dòng đã xoá trước đó (hoặc không thuộc giỏ này) → `404 PRODUCT_NOT_FOUND`, không phải `204`.
Lỗi: `VALIDATION_ERROR` 400 (`productId`) · `UNAUTHENTICATED` 401 · `PRODUCT_NOT_FOUND` 404 ·
`RATE_LIMITED` 429 · `INTERNAL_ERROR` 500.

---

## 3. Bảng giá trị / trạng thái

Module **không** có enum nào trong response, nên bảng này chỉ mô tả các cờ giá trị:

| Trường | Giá trị | Ý nghĩa / FE |
|---|---|---|
| `buyable` | `true` | Sản phẩm đang bán **và** tồn khả dụng ≥ `quantity` — mua được |
| `buyable` | `false` | Không mua được. Nếu **có** `availableQuantity` → còn bán nhưng thiếu, FE gợi ý giảm về số đó. Nếu **không** có → sản phẩm ngừng bán hoặc đã xoá |
| `availableQuantity` | số nguyên ≥ 0 hoặc vắng | Chỉ có khi còn bán nhưng thiếu; vắng khi mua được hoặc khi sản phẩm ngừng bán/đã xoá |
| `subtotal` | object `MoneyResponse` hoặc `null` | `null` khi giỏ rỗng |
| `lines` | mảng | `[]` khi giỏ rỗng, không bao giờ `null` |
| `name` / `slug` | chuỗi hoặc `null` | `null` khi sản phẩm đã xoá; dòng vẫn còn, FE nên cho xoá |

---

## 4. Trước → sau

### Endpoint MỚI (4)

Toàn bộ endpoint ở §2 là **mới** trong feature này. Module 06 chưa từng có endpoint nào trước đó.

| # | Method | Path | Trước | Sau |
|---|---|---|---|---|
| 8.1 | GET | `/api/v1/cart` | *chưa tồn tại* | `200` `CartResponse` |
| 8.2 | POST | `/api/v1/cart/items` | *chưa tồn tại* | `200` `CartResponse` |
| 8.3 | PATCH | `/api/v1/cart/items/{productId}` | *chưa tồn tại* | `200` `CartResponse` |
| 8.4 | DELETE | `/api/v1/cart/items/{productId}` | *chưa tồn tại* | `204` |

Không có endpoint nào của module khác bị **đổi** hay **xoá** bởi feature này.

---

## 5. Bảng mã lỗi

**Dùng chung**: `VALIDATION_ERROR` 400 (kèm `details[].field`), `MALFORMED_REQUEST` 400,
`UNAUTHENTICATED` 401, `RATE_LIMITED` 429, `INTERNAL_ERROR` 500.

**Riêng module cart** (2 mã, đều là **conflict** — không có gì bị đổi):

| Code | HTTP | FE nên làm |
|---|---|---|
| `CART_PRODUCT_NOT_PURCHASABLE` | 409 | Sản phẩm không đang bán (announced/hết hàng/retired). `details[].field = "productId"`. Hiện "sản phẩm không còn bán", không cho thêm; **không** gửi lại mù quáng |
| `CART_QUANTITY_EXCEEDS_AVAILABLE` | 409 | Số lượng (sau khi thêm) vượt tồn khả dụng. `details[].field = "quantity"`, `issue` nêu tồn hiện tại. Hiện "chỉ còn N", gợi ý giảm về N — **không** tự sửa số rồi gửi lại mà không hỏi khách |

**Tái sử dụng từ module 04** (1 mã):

| Code | HTTP | FE nên làm |
|---|---|---|
| `PRODUCT_NOT_FOUND` | 404 | Sản phẩm không tồn tại/đã xoá, **hoặc** dòng không thuộc giỏ này. Cùng một câu trả lời, không tiết lộ giỏ khác. Hiện "không tìm thấy sản phẩm", tải lại giỏ |

---

## 6. Trạng thái dòng và giá (nhắc lại cho FE)

- Giá của dòng (`unitPrice`) **không đổi** khi khách đọc giỏ hay khi giá sản phẩm đổi; nó là giá lúc
  thêm. Checkout mới là nơi backend đối chiếu lại giá trước khi thu tiền — FE nên báo khách nếu thao
  tác thanh toán báo lệch giá.
- Giỏ **không** giữ hàng: tồn khả dụng có thể tụt giữa lúc thêm và lúc thanh toán. Vì vậy `GET /cart`
  đối chiếu lại mỗi lần đọc, và checkout đối chiếu lại lần nữa.
- Không có luồng nào để FE tự đặt chỗ/giữ hàng — đó là việc của luồng thanh toán (module 07/08 chưa
  tồn tại).

---

## 7. Checklist FE

- [ ] Mọi request giỏ gọi `/api/v1/cart*` với Bearer của **chính** khách; **không** gửi `userId`,
      `ownerId` hay `cartId` ở bất kỳ đâu.
- [ ] Đọc giỏ trả về giỏ rỗng (`lines: []`, `subtotal: null`) được xử lý như bình thường, không phải
      lỗi.
- [ ] Hiển thị `subtotal` là `null` khi giỏ rỗng (không tự bịa `0 VND`).
- [ ] Mỗi dòng hiển thị `name`/`slug` khi có; khi `null` (sản phẩm đã xoá) vẫn hiện dòng và cho xoá.
- [ ] Dùng `productId` làm khoá dòng/khoá URL; **không** giả định có `lineId` riêng.
- [ ] Hiển thị `buyable`; khi `false` và có `availableQuantity`, gợi ý khách PATCH về số đó; khi
      `false` và **không** có `availableQuantity`, báo sản phẩm không còn bán.
- [ ] Xử lý `409 CART_PRODUCT_NOT_PURCHASABLE` và `409 CART_QUANTITY_EXCEEDS_AVAILABLE` riêng, dùng
      `details[].field` để gắn lỗi vào đúng ô.
- [ ] Xử lý `404 PRODUCT_NOT_FOUND` bằng một thông báo chung rồi tải lại giỏ.
- [ ] Gửi `quantity` dạng số nguyên ≥ 1; dọn dòng bằng DELETE, không bằng số 0.
- [ ] Giá dùng `amount` (số nguyên) + `currency`; **không** tự quy đổi hay làm tròn.

---

## 8. Changelog & đối chiếu

**2026-10-09** — Viết guide lần đầu cho feature 008 (hiến pháp v1.8.0 §Governance).

Đối chiếu với implementation (T044), **có đếm** — mọi con số dưới đây được đếm lại từ chính nguồn,
không chép từ tài liệu khác.

**Endpoint và route** — 4, đối chiếu hai chiều:

| Hướng | Nguồn | Kết quả |
|---|---|---|
| Guide → code | §2 liệt kê **4** mục | `internal/modules/cart/presentation/http/router.go` khai đúng **4** route |
| Code → guide | router **4 route** | cả 4 đều có mặt ở §2 |

Cùng khớp `specs/008-cart/contracts/openapi.yaml` (**3** path dưới `paths:`, trong đó
`/api/v1/cart/items/{productId}` mang cả `PATCH` và `DELETE` → **4** operation) và
`docs/api-reference.md` §8 (4 mục `8.1`–`8.4`). **4 mới, 0 đổi.**

> **Một lệch đã sửa trong lúc đối chiếu**: bản nháp đầu ghi OpenAPI có "4 path", nhưng đếm lại
> `contracts/openapi.yaml` chỉ có **3** path vì `PATCH` và `DELETE` cùng nằm dưới
> `/api/v1/cart/items/{productId}`. Đã sửa thành 3 path / 4 operation; không lệch nào khác giữa
> guide, `docs/api-reference.md` §8, `contracts/openapi.yaml` và router.

**Trường JSON** — `internal/modules/cart/presentation/dto/dto.go` có **15 tag `json:"…"`** trên **5
kiểu**, thành **12 tên member phân biệt**:

| Kiểu | Số member | Member |
|---|---|---|
| `MoneyResponse` | 2 | `amount`, `currency` |
| `CartLineResponse` | 8 | `productId`, `name`, `slug`, `quantity`, `unitPrice`, `lineTotal`, `buyable`, `availableQuantity` |
| `CartResponse` | 2 | `lines`, `subtotal` |
| `AddItemRequest` | 2 | `productId`, `quantity` |
| `QuantityRequest` | 1 | `quantity` |

12/12 tên phân biệt đều có mặt trong guide (đối chiếu tự động: **12/12 khớp**). Hai tên `productId`
và `quantity` xuất hiện lặp ở nhiều kiểu vẫn là **một** tên phân biệt, nên 15 tag → 12 tên.

**Giá trị enum** — **0**: module không phát ra enum nào trong response (chỉ `buyable` kiểu boolean và
`availableQuantity` kiểu số nguyên). Bảng §3 ghi rõ điều này thay vì bịa một enum không tồn tại.

**Mã lỗi** — **2** mã riêng của module (`CART_PRODUCT_NOT_PURCHASABLE`, `CART_QUANTITY_EXCEEDS_AVAILABLE`,
khớp `domain/constant/codes.go` và `contracts/error-codes.md`) cộng **1** mã **tái sử dụng**
(`PRODUCT_NOT_FOUND` của module 04).

**HTTP status khẳng định** — **8**: `200`, `204`, `400`, `401`, `404`, `409`, `429`, `500`. Mỗi status
là một câu trả lời thật: `200` do handler; `204` do `DELETE`; `400` (`VALIDATION_ERROR` /
`MALFORMED_REQUEST` do handler và `mapError`); `401` (`UNAUTHENTICATED` do middleware/handler); `404`
(`PRODUCT_NOT_FOUND` do `mapError`); `409` (hai mã `CART_*` do `mapError`); `429` (`RATE_LIMITED`);
`500` (`INTERNAL_ERROR` mặc định của `mapError`). Module **không** dùng `201`, `403`, `413`, `503` nên
các status đó vắng mặt có chủ ý.
