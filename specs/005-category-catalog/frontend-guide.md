# Frontend Integration Guide — Danh mục Sản phẩm

**Feature**: `005-category-catalog`
**Trạng thái**: Hoàn thiện (44/44 task); đã wire vào `main.go` và ghi vào `docs/api-reference.md` §5. Backfill sau amendment `frontend-guide` (hiến pháp v1.7.0 §Governance).
**Đối tượng đọc**: Lập trình viên frontend (trang khách + trang quản trị).
**Mục tiêu**: FE triển khai duyệt danh mục và màn quản trị danh mục chỉ với tài liệu này — không cần đọc code backend hay Swagger.

> Nguồn authoritative là [`docs/api-reference.md`](../../docs/api-reference.md) §5. Guide này **phải khớp** với file đó; khi lệch, `docs/api-reference.md` thắng.

---

## 0. Tóm tắt nhanh (TL;DR)

- **7 endpoint**: 2 công khai (`/categories`, địa chỉ theo **slug**) + 5 quản trị (`/admin/categories`, vai trò `ADMIN`, địa chỉ theo **định danh**).
- **Một bảng, hai bề mặt, hai hình dạng response.** Công khai chỉ có **4 member** `id, name, slug, description`; quản trị có thêm `position, isVisible, createdAt, updatedAt`.
- **Danh mục bị ẩn, đã xoá và slug chưa từng tồn tại trả lời y hệt nhau** (`404 CATEGORY_NOT_FOUND`). FE **không thể** và không nên cố phân biệt.
- **Va chạm tên/slug là `409`, không phải `400`** — giá trị hợp lệ, chỉ đang bị chiếm. Hai mã riêng nói **field nào** va chạm.
- **`slug` do operator viết và sửa được** — đổi slug làm hỏng link đã lưu. FE phải cảnh báo operator.
- **`meta.total` vắng mặt khi bằng 0** — FE đọc thiếu `total` là `0`.
- Xoá là **hard delete** (dòng biến mất, audit ở lại).

---

## 1. Quy ước

- Base path `/api/v1`; envelope `{data, meta}` / `{error}` như guide `002`.
- Công khai: **không cần token**. Quản trị: `Bearer` + role `ADMIN` (thiếu quyền → `403 FORBIDDEN`, ghi audit `AUTH_PRIVILEGE_DENIED`).
- Phân trang (cả hai danh sách): `page` default `1` (min `1`), `pageSize` default `20` (`1..100`).
- Module **không** có rate limit riêng; chỉ chịu hạn mức toàn cục.
- Thứ tự danh sách **ổn định**: `position`, rồi `createdAt`, rồi `id` — hai request giống nhau trả cùng thứ tự.

### Kiểu dữ liệu

`PublicCategory` (4 member):
```json
{ "id": "<uuid>", "name": "Điêu khắc", "slug": "diau-khac", "description": "Tác phẩm điêu khắc" }
```

`AdminCategory` (8 member):
```json
{
  "id": "<uuid>", "name": "Điêu khắc", "slug": "diau-khac", "description": "...",
  "position": 1, "isVisible": true,
  "createdAt": "2026-10-07T08:15:04Z", "updatedAt": "2026-10-07T08:15:04Z"
}
```

---

## 2. Bảng endpoint

| # | Method | Path | Auth | Trả về |
|---|---|---|---|---|
| 5.1 | GET | `/api/v1/categories` | — | `200` list `PublicCategory` + phân trang |
| 5.2 | GET | `/api/v1/categories/{slug}` | — | `200` `PublicCategory` |
| 5.3 | GET | `/api/v1/admin/categories` | ADMIN | `200` list `AdminCategory` + phân trang |
| 5.4 | POST | `/api/v1/admin/categories` | ADMIN | `201` `AdminCategory` |
| 5.5 | GET | `/api/v1/admin/categories/{categoryId}` | ADMIN | `200` `AdminCategory` |
| 5.6 | PATCH | `/api/v1/admin/categories/{categoryId}` | ADMIN | `200` `AdminCategory` |
| 5.7 | DELETE | `/api/v1/admin/categories/{categoryId}` | ADMIN | `204` (không body) |

### Chi tiết

**5.1 `GET /categories`** — chỉ danh mục `isVisible = true`, `meta.total` đếm đúng số đó. Catalogue rỗng / ẩn hết trả `data: []` (**không** `null`, **không** lỗi).
Lỗi: `VALIDATION_ERROR` 400 (`page`/`pageSize`) · `RATE_LIMITED` 429.

**5.2 `GET /categories/{slug}`** — `slug` khớp mẫu URL-safe (`^[a-z0-9]+(-[a-z0-9]+)*$`, ≤ 140).
Lỗi: `CATEGORY_NOT_FOUND` 404 · `RATE_LIMITED` 429.
> **Ẩn / đã xoá / chưa từng tồn tại → cùng `404 CATEGORY_NOT_FOUND`.** Bắt buộc để không xác nhận danh mục operator chọn không công bố.

**5.3 `GET /admin/categories`** — toàn bộ danh mục kể cả ẩn.
Lỗi: `VALIDATION_ERROR` 400 · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `RATE_LIMITED` 429.

**5.4 `POST /admin/categories`** — body `{ name*, slug*, description?, position? }`. `position` default `0`, có thể âm. Danh mục mới **luôn đang hiển thị**; muốn ẩn thì tạo rồi `PATCH isVisible:false`. `slug` **không** sinh từ `name`; giá trị lưu là giá trị operator gõ, đã trim.
Lỗi: `VALIDATION_ERROR` 400 (name rỗng/>120; slug rỗng/>140/sai mẫu; description >2000) · `MALFORMED_REQUEST` 400 · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `CATEGORY_NAME_TAKEN` 409 · `CATEGORY_SLUG_TAKEN` 409 · `RATE_LIMITED` 429.

**5.5 `GET /admin/categories/{categoryId}`** — `categoryId` là UUID.
Lỗi: `VALIDATION_ERROR` 400 · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `CATEGORY_NOT_FOUND` 404 · `RATE_LIMITED` 429.

**5.6 `PATCH /admin/categories/{categoryId}`** — partial; mọi member tuỳ chọn; bỏ trống = giữ nguyên. Gửi lại đúng giá trị hiện tại **thành công** (không tự trùng chính nó). Đổi `isVisible` đi qua chuyển trạng thái Hide/Show; ẩn danh mục đã ẩn (hoặc hiện đã hiện) là **no-op**, không lỗi. Va chạm để lại **cả hai** danh mục nguyên vẹn.
Lỗi: `VALIDATION_ERROR` 400 · `MALFORMED_REQUEST` 400 · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `CATEGORY_NOT_FOUND` 404 · `CATEGORY_NAME_TAKEN` 409 · `CATEGORY_SLUG_TAKEN` 409 · `RATE_LIMITED` 429.

**5.7 `DELETE /admin/categories/{categoryId}`** — hard delete, `204` không body. Xoá lại danh mục đã xoá cũng `404 CATEGORY_NOT_FOUND`.
Lỗi: `VALIDATION_ERROR` 400 · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `CATEGORY_NOT_FOUND` 404 · `RATE_LIMITED` 429.

### Kiểu request quản trị

`CreateCategoryRequest` (4 member; `name`, `slug` bắt buộc):
```json
{ "name": "Điêu khắc", "slug": "diau-khac", "description": "Tác phẩm điêu khắc", "position": 1 }
```

`UpdateCategoryRequest` (5 member, **tất cả tuỳ chọn**; bỏ trống = giữ nguyên):
```json
{ "name": "...", "slug": "...", "description": "...", "position": 5, "isVisible": false }
```

---

## 3. Bảng giá trị / trạng thái

| Trường | Giá trị | Ý nghĩa / FE |
|---|---|---|
| `isVisible` | `true` / `false` | Khách thấy / operator đang ẩn. Chỉ có ở hình dạng admin |
| `position` | số nguyên bất kỳ (kể cả `0`, âm) | **Thứ tự ưu tiên**, không phải dãy số có lỗ. Trùng vị trí tie-break theo `createdAt` rồi `id` |
| `slug` | `^[a-z0-9]+(-[a-z0-9]+)*$`, ≤ 140 | Đoạn link công khai; do operator viết |
| `name` | ≤ 120 ký tự (đếm **rune**) | Duy nhất, bỏ qua hoa/thường + khoảng trắng hai đầu |
| `description` | ≤ 2000 ký tự | Có thể rỗng (hợp lệ) |

---

## 4. Trước → sau

Feature 005 là module danh mục đầu tiên — **toàn bộ 7 endpoint là MỚI**. Không endpoint nào bị đổi hay xoá.

---

## 5. Bảng mã lỗi

**Dùng chung** (xem guide `002`): `VALIDATION_ERROR` 400, `MALFORMED_REQUEST` 400, `UNAUTHENTICATED` 401, `FORBIDDEN` 403, `RATE_LIMITED` 429, `INTERNAL_ERROR` 500.

**Riêng module category** (3 mã):

| Code | HTTP | FE nên làm |
|---|---|---|
| `CATEGORY_NOT_FOUND` | 404 | Công khai: hiện "không tìm thấy" chung. Admin: id/slug không tồn tại hoặc đã xoá |
| `CATEGORY_NAME_TAKEN` | 409 | Lỗi cạnh ô **name** (`details[].field = "name"`) |
| `CATEGORY_SLUG_TAKEN` | 409 | Lỗi cạnh ô **slug** (`details[].field = "slug"`) |

---

## 6. Checklist FE

- [ ] Trang khách chỉ gọi `/categories*` (không token); trang admin gọi `/admin/categories*`.
- [ ] Dùng `slug` để dựng link công khai; `id` để định danh trong màn quản trị.
- [ ] **Cảnh báo operator khi sửa `slug`** rằng link đã lưu có thể hỏng.
- [ ] Hiển thị `409` đúng ô (name/slug) dựa vào `error.details[].field`.
- [ ] Xử lý `404` công khai bằng một thông báo chung, không suy đoán danh mục có tồn tại.
- [ ] Form tạo luôn gửi `name` + `slug`; `position` để mặc định `0` nếu chưa chọn.
- [ ] Đọc `meta.total` thiếu là `0`; xử lý `data: []`.
- [ ] Toggle hiển thị dùng `PATCH isVisible`; no-op là bình thường, không báo lỗi.
- [ ] `MALFORMED_REQUEST` khi gửi member lạ — chỉ gửi đúng member trong contract.

---

## 7. Changelog & đối chiếu

**2026-10-07** — Backfill guide lần đầu (hiến pháp v1.7.0).

Đối chiếu với nguồn (có đếm):

- endpoint trong §2: **7** — khớp `docs/api-reference.md` §5, `specs/005-category-catalog/contracts/openapi.yaml`, và bảng tổng hợp §6.
- `PublicCategory`: **4** member; `AdminCategory`: **8** member; `CreateCategoryRequest`: **4** (2 bắt buộc); `UpdateCategoryRequest`: **5** (tất cả optional) — khớp `internal/modules/category/presentation/dto/dto.go`.
- mã lỗi riêng module: **3** — khớp `internal/modules/category/domain/constant/codes.go` và `contracts/error-codes.md`.
- enum/giá trị trong §3: **5** trường đặc thù.
- HTTP status khẳng định: `200`, `201`, `204`, `400`, `401`, `403`, `404`, `409`, `429`, `500`.
