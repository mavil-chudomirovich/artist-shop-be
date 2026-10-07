# Frontend Integration Guide — Xác thực & Phiên (Auth)

**Feature**: `001-user-auth`
**Trạng thái**: Hoàn thiện (70/70 task). Backfill sau amendment `frontend-guide` (hiến pháp v1.7.0 §Governance).
**Đối tượng đọc**: Lập trình viên frontend (web).
**Mục tiêu**: FE triển khai đăng ký, đăng nhập, quên/đổi mật khẩu và quản lý phiên chỉ với tài liệu này — không cần đọc code backend hay Swagger.

> Nguồn authoritative là [`docs/api-reference.md`](../../docs/api-reference.md) §1 và §3. Guide này là bản bàn giao cho FE và **phải khớp** với file đó; khi lệch, `docs/api-reference.md` thắng.

---

## 0. Tóm tắt nhanh (TL;DR)

- **11 endpoint** dưới `/api/v1/auth/*`. Tất cả response bọc envelope `{data, meta}` / `{error}` (xem guide `002`).
- **`register` và `resend-verification` luôn trả `202` chung** — FE **không thể** biết email đã tồn tại. Đừng thiết kế UI dựa vào "email đã được đăng ký".
- **`login`/`refresh`/`password/reset`/`password/change` trả cặp token** `{accessToken, refreshToken, expiresIn}`. `expiresIn` tính bằng **giây**.
- **Refresh token xoay vòng**: mỗi lần `refresh` cặp cũ bị thu hồi. Dùng lại token cũ ⇒ `AUTH_REFRESH_REUSED` **và** toàn bộ phiên của user bị thu hồi.
- **Đổi mật khẩu thu hồi mọi phiên** (kể cả phiên đang dùng) rồi cấp cặp token mới — body bắt buộc có `refreshToken` hiện tại.
- **`admin/probe`** là endpoint kiểm tra RBAC, không phải API nghiệp vụ.

---

## 1. Quy ước

- Base path `/api/v1`, `Content-Type: application/json`.
- Email được **normalize (trim + lowercase)** trước khi xử lý.
- `role` nhận đúng **2 giá trị**: `CUSTOMER`, `ADMIN`.
- Rate limit: nhóm *flow* (register, verify-email, resend-verification, password/forgot, password/reset) **5/phút/IP**; `login` **10/phút/IP**.
- Sau **10 lần** đăng nhập sai liên tiếp → khoá tài khoản **15 phút** (`AUTH_LOGIN_LOCKED`).
- Nhập sai OTP **3 lần** → khoá mã **60 giây** (`AUTH_OTP_TOO_MANY_ATTEMPTS`).

### Vòng đời token

| Token | Kiểu | Hạn |
|---|---|---|
| `accessToken` | JWT HS256 | **15 phút** |
| `refreshToken` | opaque, xoay vòng mỗi lần dùng | **45 ngày** |

- `logout` chỉ thu hồi **phiên của thiết bị hiện tại**.
- Dùng lại refresh token đã rotate ⇒ `AUTH_REFRESH_REUSED` + thu hồi **toàn bộ** phiên user.

---

## 2. Bảng endpoint

| # | Method | Path | Auth | Trả về |
|---|---|---|---|---|
| 3.1 | POST | `/api/v1/auth/register` | — | `202` (hoặc `503`) |
| 3.2 | POST | `/api/v1/auth/verify-email` | — | `200` |
| 3.3 | POST | `/api/v1/auth/resend-verification` | — | `202` |
| 3.4 | POST | `/api/v1/auth/login` | — | `200` + token |
| 3.5 | POST | `/api/v1/auth/refresh` | — (refresh trong body) | `200` + token |
| 3.6 | POST | `/api/v1/auth/logout` | — (refresh trong body) | `204` (không body) |
| 3.7 | POST | `/api/v1/auth/password/forgot` | — | `202` |
| 3.8 | POST | `/api/v1/auth/password/reset` | — | `200` + token |
| 3.9 | GET | `/api/v1/auth/me` | Bearer | `200` |
| 3.10 | POST | `/api/v1/auth/password/change` | Bearer | `200` + token |
| 3.11 | GET | `/api/v1/auth/admin/probe` | Bearer ADMIN | `200` |

### Kiểu dữ liệu chính

`Identity` (dùng ở `/me`):
```json
{ "id": "<uuid>", "email": "user@example.com", "role": "CUSTOMER" }
```

`TokenPair` (mọi endpoint cấp token):
```json
{ "accessToken": "<jwt>", "refreshToken": "<opaque>", "expiresIn": 900 }
```

`GenericMessage` (`data.message`): dùng cho register/verify/resend/forgot/admin-probe.

### Chi tiết từng endpoint

**3.1 `POST /register`** — body `{ "email", "password" }` (password ≥ 8, đạt chính sách).
`202` chung (không tiết lộ email tồn tại):
```json
{ "data": { "message": "If the email is eligible, a confirmation code has been sent." }, "meta": { "...": "..." } }
```
**Nhánh `503`** khi không gửi được thư xác nhận (mã dùng chung `SERVICE_UNAVAILABLE`, không phải `500`):
```json
{ "error": { "code": "SERVICE_UNAVAILABLE", "message": "The confirmation email could not be sent...", "requestId": "..." } }
```
Lỗi: `AUTH_WEAK_PASSWORD` 400 · `RATE_LIMITED` 429 · `SERVICE_UNAVAILABLE` 503.
> Tài khoản `pending` **vẫn được tạo** khi gửi hỏng; FE nên mời khách thử `resend-verification` ngay (cooldown đã được gỡ cho lần hỏng).

**3.2 `POST /verify-email`** — `{ "email", "otp" }` (otp 6 chữ số) → `200 { "data": { "message": "Email confirmed." } }`.
Lỗi: `AUTH_OTP_INVALID` 400 · `AUTH_OTP_EXPIRED` 400 · `AUTH_OTP_TOO_MANY_ATTEMPTS` 429 · `AUTH_ACCOUNT_DISABLED` 403 · `RATE_LIMITED` 429.

**3.3 `POST /resend-verification`** — `{ "email" }` → `202` giống `register`.
Lỗi: `AUTH_RESEND_COOLDOWN` 429 · `AUTH_OTP_TOO_MANY_ATTEMPTS` 429 · `RATE_LIMITED` 429.
> Nếu chính lần gửi lại này thất bại, endpoint hiện trả **`500`** (chưa map thành `503` như `register`) — **khoảng trống đã biết**, xem `docs/api-reference.md` §3.3.

**3.4 `POST /login`** — `{ "email", "password" }` → `200` + `TokenPair`. `expiresIn` = giây.
Lỗi: `AUTH_INVALID_CREDENTIALS` 401 · `AUTH_ACCOUNT_PENDING` 403 · `AUTH_ACCOUNT_DISABLED` 403 · `AUTH_LOGIN_LOCKED` 429.

**3.5 `POST /refresh`** — `{ "refreshToken" }` → `200` + `TokenPair`.
Lỗi: `AUTH_TOKEN_INVALID` 401 · `AUTH_TOKEN_EXPIRED` 401 · `AUTH_REFRESH_REUSED` 401 (thu hồi toàn bộ phiên user).

**3.6 `POST /logout`** — `{ "refreshToken" }` → `204` không body. **Idempotent**: token lạ vẫn `204`.
Lỗi: `MALFORMED_REQUEST` 400.

**3.7 `POST /password/forgot`** — `{ "email" }` → `202` chung.
Lỗi: `AUTH_RESEND_COOLDOWN` 429 · `RATE_LIMITED` 429.

**3.8 `POST /password/reset`** — `{ "token", "newPassword" }` → `200` + `TokenPair` (đăng nhập luôn).
Lỗi: `AUTH_RESET_INVALID` 400 · `AUTH_WEAK_PASSWORD` 400 · `RATE_LIMITED` 429.

**3.9 `GET /me`** — Bearer → `200` `Identity`.
Lỗi: `UNAUTHENTICATED` 401 · `AUTH_TOKEN_INVALID` 401 · `AUTH_TOKEN_EXPIRED` 401.

**3.10 `POST /password/change`** — Bearer, body `{ "currentPassword", "newPassword", "refreshToken" }` → `200` + `TokenPair`.
Thu hồi **mọi** phiên (kể cả phiên hiện tại) và cấp lại cặp mới. Lỗi: `UNAUTHENTICATED` 401 · `AUTH_INVALID_CREDENTIALS` 401 · `AUTH_WEAK_PASSWORD` 400 · `VALIDATION_ERROR` 400 (thiếu `refreshToken`) · `AUTH_TOKEN_INVALID` 401 · `AUTH_REFRESH_REUSED` 401.

**3.11 `GET /admin/probe`** — Bearer ADMIN → `200`.
Lỗi: `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 (ghi audit `AUTH_PRIVILEGE_DENIED`).

---

## 3. Bảng giá trị / enum

| Trường | Giá trị | Ý nghĩa |
|---|---|---|
| `role` | `CUSTOMER` | Người dùng thường |
| `role` | `ADMIN` | Quản trị viên |

Không có enum nào khác. Trạng thái tài khoản (`pending`/`active`/`disabled`) **không** xuất hiện như trường trong response; nó biểu hiện qua lỗi (`AUTH_ACCOUNT_PENDING`, `AUTH_ACCOUNT_DISABLED`).

---

## 4. Trước → sau

Feature 001 là module auth đầu tiên — **toàn bộ 11 endpoint là MỚI**. Không có endpoint nào bị đổi hay xoá.

---

## 5. Bảng mã lỗi

**Dùng chung** (xem guide `002`): `VALIDATION_ERROR` 400, `MALFORMED_REQUEST` 400, `UNAUTHENTICATED` 401, `FORBIDDEN` 403, `RATE_LIMITED` 429, `INTERNAL_ERROR` 500, `SERVICE_UNAVAILABLE` 503.

**Riêng module auth** (13 mã):

| Code | HTTP | FE nên làm |
|---|---|---|
| `AUTH_WEAK_PASSWORD` | 400 | Chỉ lỗi cạnh ô mật khẩu; hiển thị chính sách |
| `AUTH_INVALID_CREDENTIALS` | 401 | Câu chung "Sai email hoặc mật khẩu" — **không** nói cái nào sai |
| `AUTH_ACCOUNT_PENDING` | 403 | Chuyển sang màn nhập OTP |
| `AUTH_ACCOUNT_DISABLED` | 403 | Thông báo tài khoản bị vô hiệu hoá |
| `AUTH_OTP_INVALID` | 400 | Lỗi cạnh ô OTP |
| `AUTH_OTP_EXPIRED` | 400 | Mời gọi lại `resend-verification` |
| `AUTH_OTP_TOO_MANY_ATTEMPTS` | 429 | Khoá nhập OTP 60 giây |
| `AUTH_RESEND_COOLDOWN` | 429 | Đếm ngược trước khi cho gửi lại |
| `AUTH_LOGIN_LOCKED` | 429 | Khoá đăng nhập 15 phút |
| `AUTH_TOKEN_INVALID` | 401 | Xoá phiên, điều hướng login |
| `AUTH_TOKEN_EXPIRED` | 401 | Thử `refresh`; nếu fail thì login lại |
| `AUTH_REFRESH_REUSED` | 401 | Xoá phiên ngay; buộc đăng nhập lại |
| `AUTH_RESET_INVALID` | 400 | Link/token đặt lại không hợp lệ hoặc hết hạn |

---

## 6. Checklist FE

- [ ] Lưu `accessToken` + `refreshToken`; coi `expiresIn` là **giây**.
- [ ] Interceptor: gặp 401 do token hết hạn → gọi `POST /refresh` (một lần, có khoá) rồi phát lại request.
- [ ] Gặp `AUTH_REFRESH_REUSED` → dừng mọi refresh, xoá phiên, đăng nhập lại.
- [ ] `register`/`resend`/`forgot`: luôn hiện thông báo chung, **không** xác nhận email tồn tại.
- [ ] Xử lý nhánh `503` của `register` bằng "thử gửi lại mã" (không phải báo lỗi chung chung).
- [ ] `password/change`: gửi kèm `refreshToken` hiện tại, sau khi thành công cập nhật cặp token mới.
- [ ] Tôn trọng `Retry-After` cho mọi `429`.
- [ ] Không log token / mật khẩu.

---

## 7. Changelog & đối chiếu

**2026-10-07** — Backfill guide lần đầu (hiến pháp v1.7.0).

Đối chiếu với nguồn (có đếm):

- endpoint trong §2: **11** — khớp `docs/api-reference.md` §3 và `specs/001-user-auth/contracts/openapi.yaml`.
- mã lỗi riêng module auth trong §5: **13** — khớp bảng §1.4 `docs/api-reference.md` và `contracts/auth-error-codes.md`.
- enum trong §3: **1** (`role`) × **2** giá trị (`CUSTOMER`, `ADMIN`) — khớp `internal/share/access`.
- route trong bảng tổng hợp `docs/api-reference.md` §6 thuộc `/auth`: **11**.
- HTTP status khẳng định: `200`, `202`, `204`, `400`, `401`, `403`, `429`, `500`, `503` — đều là status handler có thể trả.
