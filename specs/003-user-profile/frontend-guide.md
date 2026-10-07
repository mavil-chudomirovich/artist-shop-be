# Frontend Integration Guide — Hồ sơ Khách hàng

**Feature**: `003-user-profile`
**Trạng thái**: Hoàn thiện (64/64 task). Backfill sau amendment `frontend-guide` (hiến pháp v1.7.0 §Governance).
**Đối tượng đọc**: Lập trình viên frontend (web).
**Mục tiêu**: FE triển khai hồ sơ, avatar, sổ địa chỉ giao hàng và dữ liệu hành chính chỉ với tài liệu này — không cần đọc code backend hay Swagger.

> Nguồn authoritative là [`docs/api-reference.md`](../../docs/api-reference.md) §4. Guide này **phải khớp** với file đó; khi lệch, `docs/api-reference.md` thắng.

---

## 0. Tóm tắt nhanh (TL;DR)

- **12 endpoint**: hồ sơ (`GET`/`PATCH /users/me`), avatar (`POST`/`DELETE /users/me/avatar`), sổ địa chỉ (5 route `/users/me/addresses*`), tra cứu admin (`GET /users/{userId}`), dữ liệu hành chính (`/divisions/*`).
- **Chủ tài khoản luôn lấy từ session** — không route tự phục vụ nào nhận id chủ sở hữu từ client. Truy cập chéo tài khoản là bất khả thi *theo cấu trúc*.
- **`avatar` là `null` khi chưa có ảnh** — không có cờ `hasAvatar`. Đừng dựa vào `avatar.url` để suy ra có ảnh.
- **Số điện thoại luôn chuẩn hoá 10 chữ số bắt đầu `0`**, hoặc `null` khi chưa đặt.
- **`PATCH /users/me`: bỏ trống = giữ nguyên, chuỗi rỗng = xoá.** Ngược lại, **`PATCH .../addresses/{id}` từ chối chuỗi rỗng.**
- **`PATCH`/`DELETE`/đặt-mặc-định địa chỉ của người khác trả cùng `404 USER_ADDRESS_NOT_FOUND`** — không dò được địa chỉ của tài khoản khác.
- **Xoá địa chỉ là ẩn mềm** (`204`), dòng dữ liệu được giữ cho đơn hàng cũ.

---

## 1. Quy ước

- Base path `/api/v1`; mọi route `/users` cần `Bearer` access token (trừ `GET /divisions/*` chỉ cần Bearer, không cần ADMIN).
- Rate limit riêng: avatar **10/giờ/IP**; ghi địa chỉ (`POST`/`PATCH`/`DELETE /users/me/addresses*`) **30/phút/IP**. Các route đọc không có limit riêng (chịu limit toàn cục).
- Trần avatar **2 MB** nằm trên route (không phải trần chung); mọi cách gửi ảnh quá trần đều trả `413 USER_AVATAR_TOO_LARGE`.
- Toàn bộ route `/divisions/*` **cố ý không phân trang** (dataset ~35 tỉnh).

### Hình dạng hồ sơ (`Profile`)

```json
{
  "id": "0f5c6e0c-1a44-4a1e-9b3d-9a1b2c3d4e5f",
  "email": "an.nguyen@example.com",
  "role": "CUSTOMER",
  "displayName": "Nguyễn Thị An",
  "phone": "0912345678",
  "avatar": { "publicId": "...", "url": "https://...", "width": 512, "height": 512 }
}
```

- `avatar`: `null` khi chưa có ảnh. Khi có: `{ publicId, url, width, height }`.
- `phone`: chuỗi 10 chữ số bắt đầu `0`, hoặc `null`.
- `displayName`: chuỗi, **có thể rỗng**, **không bao giờ `null`**.
- `role`: `CUSTOMER` hoặc `ADMIN`.

### Hình dạng địa chỉ (`Address`)

```json
{
  "id": "...", "recipientName": "Nguyễn Thị An", "recipientPhone": "0912345678",
  "provinceCode": "01", "provinceName": "Hà Nội", "wardCode": "00004", "wardName": "Ba Đình",
  "streetAddress": "12 Ngõ 129 Dịch Vọng", "isDefault": true, "divisionNeedsReview": false
}
```

- `divisionNeedsReview` **luôn có mặt** (kể cả `false`) trên địa chỉ của **chính khách**; vắng mặt trên tra cứu admin (`4.10`).
- Danh sách rỗng trả `data: []` (không phải `null`).

---

## 2. Bảng endpoint

| # | Method | Path | Auth | Trả về |
|---|---|---|---|---|
| 4.1 | GET | `/api/v1/users/me` | Bearer | `200` Profile |
| 4.2 | PATCH | `/api/v1/users/me` | Bearer | `200` Profile |
| 4.3 | POST | `/api/v1/users/me/avatar` | Bearer | `200` Profile (multipart) |
| 4.4 | DELETE | `/api/v1/users/me/avatar` | Bearer | `200` Profile |
| 4.5 | GET | `/api/v1/users/me/addresses` | Bearer | `200` list + phân trang |
| 4.6 | POST | `/api/v1/users/me/addresses` | Bearer | `201` Address |
| 4.7 | PATCH | `/api/v1/users/me/addresses/{addressId}` | Bearer | `200` Address |
| 4.8 | DELETE | `/api/v1/users/me/addresses/{addressId}` | Bearer | `204` (không body) |
| 4.9 | POST | `/api/v1/users/me/addresses/{addressId}/default` | Bearer | `200` Address |
| 4.10 | GET | `/api/v1/users/{userId}` | Bearer ADMIN | `200` (kèm `addresses`) |
| 4.11 | GET | `/api/v1/divisions/provinces` | Bearer | `200` list |
| 4.12 | GET | `/api/v1/divisions/provinces/{provinceCode}/wards` | Bearer | `200` list |

### Chi tiết

**4.1 `GET /users/me`** — đọc hồ sơ. Không gọi media service (đọc cột đã lưu) nên vẫn chạy khi media hỏng/chưa cấu hình. Lỗi: `UNAUTHENTICATED` 401 · `RATE_LIMITED` 429.

**4.2 `PATCH /users/me`** — body `{ "displayName"?, "phone"? }`. Omit = giữ nguyên; `""`/`null` = xoá. Body `{}` = no-op trả `200`. `displayName` ≤ 120 **ký tự (rune)**. `phone` nhận `+84 912 345 678`, `0912.345.678`... và chuẩn hoá về `0912345678`.
Lỗi: `VALIDATION_ERROR` 400 (`displayName`) · `USER_INVALID_PHONE` 400 · `MALFORMED_REQUEST` 400 · `UNAUTHENTICATED` 401 · `RATE_LIMITED` 429.

**4.3 `POST /users/me/avatar`** — `multipart/form-data`, đúng **một** part `file`. Loại ảnh nhận diện theo **byte** (JPEG/PNG/WebP), không tin tên file/`Content-Type`. Trần 2 MB.
Lỗi: `VALIDATION_ERROR` 400 (thiếu `file`/sai loại body) · `USER_AVATAR_TYPE_UNSUPPORTED` 400 · `USER_AVATAR_TOO_LARGE` 413 · `USER_MEDIA_UNAVAILABLE` 503 · `MALFORMED_REQUEST` 400 · `UNAUTHENTICATED` 401 · `RATE_LIMITED` 429.
> **`PAYLOAD_TOO_LARGE` không xuất hiện ở endpoint này** — mọi cách gửi quá trần đều là `413 USER_AVATAR_TOO_LARGE` (ADR-010).

**4.4 `DELETE /users/me/avatar`** — idempotent, không body, trả `200` Profile với `avatar: null`. Vẫn `200` khi thiếu cấu hình media. Lỗi: `UNAUTHENTICATED` 401 · `RATE_LIMITED` 429.

**4.5 `GET /users/me/addresses`** — query `page` (default `1`, min `1`), `pageSize` (default `20`, `1..100`). Sắp xếp: mặc định trước, rồi cập nhật gần nhất. Lỗi: `VALIDATION_ERROR` 400 (`page`/`pageSize`) · `UNAUTHENTICATED` 401 · `RATE_LIMITED` 429.

**4.6 `POST /users/me/addresses`** — body `{ recipientName, recipientPhone, provinceCode, wardCode, streetAddress, provinceName?, wardName? }`. Địa chỉ **đầu tiên tự động thành mặc định**.
Lỗi: `VALIDATION_ERROR` 400 (thiếu member / quá 120 / 255 ký tự) · `USER_INVALID_PHONE` 400 · `USER_UNKNOWN_PROVINCE` 400 · `USER_UNKNOWN_WARD` 400 · `USER_WARD_PROVINCE_MISMATCH` 400 · `MALFORMED_REQUEST` 400 · `UNAUTHENTICATED` 401 · `RATE_LIMITED` 429.

**4.7 `PATCH /users/me/addresses/{addressId}`** — partial; **cờ mặc định giữ nguyên** (dùng `4.9`). Chuỗi rỗng bị **từ chối** (`recipientName`/`streetAddress` → `VALIDATION_ERROR`; `recipientPhone` → `USER_INVALID_PHONE`).
Lỗi: `VALIDATION_ERROR` 400 · `USER_INVALID_PHONE` 400 · `USER_UNKNOWN_PROVINCE` 400 · `USER_UNKNOWN_WARD` 400 · `USER_WARD_PROVINCE_MISMATCH` 400 · `USER_ADDRESS_NOT_FOUND` 404 · `MALFORMED_REQUEST` 400 · `UNAUTHENTICATED` 401 · `RATE_LIMITED` 429.

**4.8 `DELETE /users/me/addresses/{addressId}`** — ẩn mềm, `204` không body (ADR-004). Ẩn địa chỉ mặc định duy nhất → tài khoản không còn mặc định; địa chỉ tạo tiếp theo thành mặc định.
Lỗi: `VALIDATION_ERROR` 400 (không phải UUID) · `USER_ADDRESS_NOT_FOUND` 404 · `UNAUTHENTICATED` 401 · `RATE_LIMITED` 429.

**4.9 `POST /users/me/addresses/{addressId}/default`** — không body → `200` Address với `isDefault: true`. Xoá mặc định cũ + đặt mới trong **một** transaction.
Lỗi: `VALIDATION_ERROR` 400 · `USER_ADDRESS_NOT_FOUND` 404 · `UNAUTHENTICATED` 401 · `RATE_LIMITED` 429.

**4.10 `GET /users/{userId}`** — **ADMIN**, chỉ đọc, `userId` là **đối tượng** (không có tiền tố `/admin`). Trả Profile **kèm `addresses`** (không phân trang, trần 100, **không có** `divisionNeedsReview`). Mỗi lần đọc thành công ghi audit `USER_PROFILE_VIEWED_BY_ADMIN`.
Lỗi: `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `VALIDATION_ERROR` 400 (`userId` không phải UUID) · `USER_NOT_FOUND` 404 · `RATE_LIMITED` 429.

**4.11 `GET /divisions/provinces`** — `[{ "code": "01", "name": "Hà Nội" }, ...]`, sắp theo tên. Lỗi: `UNAUTHENTICATED` 401.

**4.12 `GET /divisions/provinces/{provinceCode}/wards`** — `[{ "code": "00004", "name": "Ba Đình", "provinceCode": "01" }, ...]`. Mã phường 5 chữ số, tên **không kèm tiền tố** `Phường`/`Xã`. Mã tỉnh sai → `400 USER_UNKNOWN_PROVINCE` (**không** phải 404).
Lỗi: `UNAUTHENTICATED` 401 · `VALIDATION_ERROR` 400 · `USER_UNKNOWN_PROVINCE` 400.

---

## 3. Bảng giá trị / enum

| Trường | Giá trị | Ý nghĩa |
|---|---|---|
| `role` | `CUSTOMER`, `ADMIN` | Vai trò hệ thống |
| `avatar` | object hoặc `null` | `null` = chưa có ảnh (tín hiệu duy nhất) |
| `phone`, `recipientPhone` | 10 chữ số bắt đầu `0`, hoặc `null` | Đã chuẩn hoá |
| `divisionNeedsReview` | `true`/`false` | `true` = mã đã lưu không còn trong dataset; tên đã chụp vẫn trả |

---

## 4. Trước → sau

Feature 003 thêm nhóm `/users/*` và `/divisions/*` — **toàn bộ 12 endpoint là MỚI**. Không endpoint nào bị đổi hay xoá.
> Riêng hành vi `413` của avatar về sau được **sửa** bởi feature 004 (trước đó có thể trả `PAYLOAD_TOO_LARGE`); xem `specs/004-fix-pending-defects/frontend-guide.md`.

---

## 5. Bảng mã lỗi

**Dùng chung** (xem guide `002`).

**Riêng module user** (9 mã):

| Code | HTTP | FE nên làm |
|---|---|---|
| `USER_NOT_FOUND` | 404 | Chỉ ở tra cứu admin; báo "không tìm thấy khách" |
| `USER_ADDRESS_NOT_FOUND` | 404 | Báo địa chỉ không tồn tại (cũng dùng khi là của người khác / đã ẩn) |
| `USER_INVALID_PHONE` | 400 | Lỗi cạnh ô SĐT; hiển thị định dạng mong đợi |
| `USER_UNKNOWN_PROVINCE` | 400 | Tỉnh không có trong dataset |
| `USER_UNKNOWN_WARD` | 400 | Phường không có trong dataset |
| `USER_WARD_PROVINCE_MISMATCH` | 400 | Phường không thuộc tỉnh đã chọn |
| `USER_AVATAR_TYPE_UNSUPPORTED` | 400 | Ảnh không phải JPEG/PNG/WebP |
| `USER_AVATAR_TOO_LARGE` | 413 | Ảnh > 2 MB; yêu cầu nén |
| `USER_MEDIA_UNAVAILABLE` | 503 | Thử lại; hồ sơ không bị đổi |

---

## 6. Checklist FE

- [ ] Không bao giờ gửi id chủ tài khoản lên; luôn dựa vào token.
- [ ] `avatar === null` là tín hiệu duy nhất "chưa có ảnh".
- [ ] `PATCH /users/me`: phân biệt **bỏ trống** (giữ) vs **`""`** (xoá).
- [ ] `PATCH .../addresses/{id}`: **không** gửi chuỗi rỗng để xoá member; muốn bỏ thì gọi `DELETE`.
- [ ] Form địa chỉ nạp tỉnh/phường từ `/divisions/*`; gửi `provinceCode`/`wardCode` (không gửi tên, hoặc gửi tên là tuỳ chọn).
- [ ] Hiển thị cảnh báo khi `divisionNeedsReview === true` (dữ liệu hành chính có thể đã đổi).
- [ ] Avatar: kiểm tra loại/kích thước phía client trước khi upload; xử lý `429` (10/giờ) và `503`.
- [ ] Danh sách địa chỉ/phân trang: xử lý `data: []` và `meta.total` thiếu = 0.
- [ ] `GET /users/{userId}` chỉ gọi ở khu vực admin.

---

## 7. Changelog & đối chiếu

**2026-10-07** — Backfill guide lần đầu (hiến pháp v1.7.0).

Đối chiếu với nguồn (có đếm):

- endpoint trong §2: **12** — khớp `docs/api-reference.md` §4, `specs/003-user-profile/contracts/openapi.yaml`, và bảng tổng hợp §6.
- mã lỗi riêng module user: **9** — khớp bảng §1.4 `docs/api-reference.md` và `contracts/error-codes.md`.
- trường `Profile`: **6** (id, email, role, displayName, phone, avatar); `avatar`: **4** (publicId, url, width, height); `Address`: **10** (9 + `divisionNeedsReview`) — khớp DTO code `internal/modules/user/presentation/dto`.
- route `/users` trong bảng tổng hợp `docs/api-reference.md` §6: **11** (`me` ×9 + `{userId}` + 2 divisions = 12 tổng).
- HTTP status khẳng định: `200`, `201`, `204`, `400`, `401`, `403`, `404`, `413`, `429`, `503`.
