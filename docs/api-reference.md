# API Reference

Tài liệu API chính thức của `artist-shop-be`. Đây là **nguồn tra cứu duy nhất** cho
toàn bộ endpoint đang tồn tại: khi thêm, sửa hoặc xoá endpoint, file này **phải**
được cập nhật trong cùng thay đổi đó (hiến pháp v1.4.0, mục *API Documentation*).

| Mục | Nội dung |
|-----|----------|
| Base URL (dev) | `http://localhost:8080` (Docker: `make up` → `API_PORT`) |
| Base path | `/api/v1` |
| Định dạng request | `application/json` — bắt buộc khi có body |
| Định dạng response | `application/json; charset=utf-8` |
| Xác thực | `Authorization: Bearer <accessToken>` |
| Chuẩn hoá error | `httpx` — `internal/share/httpx/errors.go` |
| Nguồn code | `internal/modules/*/presentation/http/router.go` |

> **Quan hệ với `specs/*/contracts/openapi.yaml`**: file OpenAPI là artifact
> machine-readable **theo từng feature**, sinh ra ở bước `/speckit.plan`. File này
> là bản **authoritative, xuyên module**. Khi hai bên lệch nhau, **file này thắng**
> và `openapi.yaml` của feature phải được sửa cho khớp.

---

## 1. Quy ước chung

### 1.1 Middleware pipeline

Mọi request đi qua đúng thứ tự này (`internal/share/httpserver/routes.go`):

`RealIP` → `Correlation` → `RequestLogger` → `Recovery` → `CORS` → `BodyLimit`
→ `JSONContentType` → `Authentication` → *(rate limit, chỉ dưới `/api/v1`)* → handler

Hệ quả trực tiếp:

| Điều kiện vi phạm | HTTP | Error code |
|---|------|-----------|
| Body vượt `MAX_BODY_BYTES` (mặc định 4 MiB) hoặc vượt trần riêng của route (avatar: 2 MB) | 413 | `PAYLOAD_TOO_LARGE` |
| `Content-Type` không khớp với loại body mà route khai báo (route JSON nhận `multipart/form-data`, route avatar nhận `text/plain`) | 415 | `UNSUPPORTED_MEDIA_TYPE` |
| Vượt rate limit | 429 + header `Retry-After: 1` | `RATE_LIMITED` |
| Origin không nằm trong `CORS_ALLOWED_ORIGINS` | CORS bị chặn ở preflight | — |

### 1.2 Correlation ID

- Gửi kèm `X-Request-Id: <uuid>` để gom log theo một luồng; header không phải
  UUID hợp lệ sẽ bị thay bằng UUID mới.
- Response **luôn** trả lại `X-Request-Id`, và giá trị này cũng nằm trong
  `meta.requestId` / `error.requestId`. Khi báo lỗi, gửi kèm giá trị này.

### 1.3 Response envelope

**Thành công**

```json
{
  "data": { "...": "payload của endpoint" },
  "meta": {
    "requestId": "5c9f1a1e-6f0e-4a0e-9c3b-2f6a1b8c7d90",
    "timestamp": "2026-09-18T08:15:04Z"
  }
}
```

Với endpoint phân trang, `meta` bổ sung `page`, `pageSize`, `total`.

**Lỗi**

```json
{
  "error": {
    "code": "AUTH_INVALID_CREDENTIALS",
    "message": "Invalid email or password",
    "details": [{ "field": "password", "issue": "too short" }],
    "requestId": "5c9f1a1e-6f0e-4a0e-9c3b-2f6a1b8c7d90"
  }
}
```

`details` chỉ xuất hiện khi có lỗi theo field. Lỗi 5xx không bao giờ để lộ chi tiết
nội bộ ra ngoài.

### 1.4 Error code

**Dùng chung** (`httpx`):

| Code | HTTP | Nghĩa |
|------|------|-------|
| `VALIDATION_ERROR` | 400 | Dữ liệu không hợp lệ |
| `MALFORMED_REQUEST` | 400 | Body không parse được, hoặc có field lạ (decoder dùng `DisallowUnknownFields`) |
| `UNAUTHENTICATED` | 401 | Thiếu token / token không hợp lệ |
| `FORBIDDEN` | 403 | Đã xác thực nhưng thiếu quyền (bị ghi `audit_logs` với action `AUTH_PRIVILEGE_DENIED`) |
| `NOT_FOUND` | 404 | Không tồn tại route |
| `METHOD_NOT_ALLOWED` | 405 | Sai HTTP method |
| `CONFLICT` | 409 | Xung đột trạng thái hiện tại |
| `PAYLOAD_TOO_LARGE` | 413 | Body quá lớn |
| `UNSUPPORTED_MEDIA_TYPE` | 415 | `Content-Type` không khớp loại body của route |
| `RATE_LIMITED` | 429 | Quá hạn mức |
| `INTERNAL_ERROR` | 500 | Lỗi ngoài dự kiến |
| `SERVICE_UNAVAILABLE` | 503 | Chưa sẵn sàng phục vụ |

**Riêng module auth** (`internal/modules/auth/domain/constant/codes.go`):

| Code | HTTP | Nghĩa |
|------|------|-------|
| `AUTH_WEAK_PASSWORD` | 400 | Mật khẩu không đạt chính sách |
| `AUTH_INVALID_CREDENTIALS` | 401 | Sai email hoặc mật khẩu (không tiết lộ cái nào sai) |
| `AUTH_ACCOUNT_PENDING` | 403 | Chưa xác nhận email |
| `AUTH_ACCOUNT_DISABLED` | 403 | Tài khoản bị vô hiệu hoá |
| `AUTH_OTP_INVALID` | 400 | Mã xác nhận sai |
| `AUTH_OTP_EXPIRED` | 400 | Mã xác nhận hết hạn |
| `AUTH_OTP_TOO_MANY_ATTEMPTS` | 429 | Sai quá số lần cho phép |
| `AUTH_RESEND_COOLDOWN` | 429 | Chưa đủ thời gian chờ giữa 2 lần gửi lại |
| `AUTH_LOGIN_LOCKED` | 429 | Đã khoá tạm sau nhiều lần đăng nhập sai |
| `AUTH_TOKEN_INVALID` | 401 | Token không hợp lệ |
| `AUTH_TOKEN_EXPIRED` | 401 | Token đã hết hạn |
| `AUTH_REFRESH_REUSED` | 401 | Refresh token đã được dùng (rotation phát hiện replay) |
| `AUTH_RESET_INVALID` | 400 | Token đặt lại mật khẩu sai hoặc hết hạn |

### 1.5 Rate limit

| Phạm vi | Mặc định | Biến môi trường |
|---|---|---|
| Nhóm *flow* của auth (đăng ký, xác nhận, gửi lại, quên/đặt lại mật khẩu) | 5 req/phút / IP | `AUTH_FLOW_RATE_PER_MINUTE` |
| `POST /auth/login` | 10 req/phút / IP | `AUTH_LOGIN_RATE_PER_MINUTE` |
| Toàn bộ `/api/v1`, theo nhóm `read` / `write` / `auth` | 20 req/s, burst 40 | `RATE_LIMIT_RPS`, `RATE_LIMIT_BURST` |

Ngoài ra: đăng nhập sai liên tiếp **10 lần** sẽ khoá tài khoản 15 phút
(`AUTH_LOGIN_MAX_FAILURES`, `AUTH_LOGIN_LOCKOUT_TTL`); nhập sai OTP **3 lần** sẽ
khoá mã 60 giây (`OTP_MAX_ATTEMPTS`, `OTP_BLOCK_TTL`).

### 1.6 Vòng đời token

- Access token: JWT HS256, **15 phút** (`ACCESS_TOKEN_TTL`).
- Refresh token: opaque, xoay vòng mỗi lần dùng, **45 ngày** (`REFRESH_TOKEN_TTL`).
- Dùng lại refresh token đã rotate ⇒ `AUTH_REFRESH_REUSED` **và** toàn bộ phiên của
  user bị thu hồi (phát hiện replay), sự kiện được ghi `audit_logs` với action
  `AUTH_REFRESH_REUSED`.
- `POST /auth/logout` chỉ thu hồi **phiên của thiết bị đó**; các thiết bị khác vẫn
  dùng được cho tới khi chính refresh token đã bị thu hồi được dùng lại.

---

## 2. Foundation (`internal/share`)

### `GET /healthz`

Liveness. Luôn trả `200` khi process còn sống. Không cần xác thực.

```json
{ "status": "alive" }
```

### `GET /readyz`

Readiness. Chỉ `200` khi PostgreSQL ping được, schema đã migrate (`version > 0`) và
Redis ping được; nếu không trả `503`. Không cần xác thực.

```json
{
  "status": "ready",
  "checks": { "database": "ok", "migrations": "ok", "cache": "ok" }
}
```

Không dùng envelope `data`/`meta` (đây là endpoint vận hành, độc lập contract).

---

## 3. Module 01 — Auth (`/api/v1/auth`)

Hạ tầng chung: `register`, `verify-email`, `resend-verification`,
`password/forgot`, `password/reset` nằm trong nhóm rate limit *flow*; `login` có
hạn mức riêng.

Email được normalize (trim + lowercase) trước khi xử lý. Endpoint *register* và
*resend* **không** tiết lộ email đã tồn tại hay không (luôn trả `202` với thông
báo chung), chỉ `verify-email` mới phản ánh trạng thái tài khoản.

### 3.1 `POST /register`

Tạo tài khoản ở trạng thái `pending` và gửi OTP 6 chữ số qua email. **Không** dùng
token vì thông báo trả về là chung.

| | |
|---|---|
| Auth | Không |
| Rate limit | 5/phút (`AUTH_FLOW_RATE_PER_MINUTE`) |
| Trả về | `202 Accepted` |

Request

```json
{ "email": "user@example.com", "password": "Str0ng!Pass" }
```

`password` phải đạt chính sách mật khẩu; mật khẩu được băm **Argon2id**.

Response `202`

```json
{
  "data": { "message": "If the email is eligible, a confirmation code has been sent." },
  "meta": { "requestId": "...", "timestamp": "..." }
}
```

Lỗi: `AUTH_WEAK_PASSWORD` 400 · `RATE_LIMITED` 429

> Nếu `.env` để `SMTP_HOST` rỗng, email được ghi ra log (log sender). Bật Mailpit
> bằng `make up-tools` rồi đặt `SMTP_HOST=mailpit`, `SMTP_PORT=1025` để xem OTP
> trên `http://localhost:8025`.

### 3.2 `POST /verify-email`

Xác nhận email bằng OTP → chuyển tài khoản sang `active`.

| | |
|---|---|
| Auth | Không |
| Rate limit | 5/phút (flow) |
| Trả về | `200 OK` |

Request `{ "email": "user@example.com", "otp": "123456" }`

Response `200`

```json
{ "data": { "message": "Email confirmed." }, "meta": { "requestId": "...", "timestamp": "..." } }
```

Lỗi: `AUTH_OTP_INVALID` 400 · `AUTH_OTP_EXPIRED` 400 · `AUTH_OTP_TOO_MANY_ATTEMPTS` 429 · `AUTH_ACCOUNT_DISABLED` 403 · `RATE_LIMITED` 429

### 3.3 `POST /resend-verification`

Gửi lại OTP khi mã hết hạn hoặc người dùng không nhận được.

| | |
|---|---|
| Auth | Không |
| Rate limit | 5/phút (flow) |
| Trả về | `202 Accepted` |

Request `{ "email": "user@example.com" }` → response giống hệt `POST /register`.

Lỗi: `AUTH_RESEND_COOLDOWN` 429 · `AUTH_OTP_TOO_MANY_ATTEMPTS` 429 · `RATE_LIMITED` 429

### 3.4 `POST /login`

Đăng nhập, cấp cặp access + refresh token.

| | |
|---|---|
| Auth | Không |
| Rate limit | 10/phút (`AUTH_LOGIN_RATE_PER_MINUTE`) |
| Trả về | `200 OK` |

Request `{ "email": "user@example.com", "password": "Str0ng!Pass" }`

Response `200`

```json
{
  "data": {
    "accessToken": "<jwt>",
    "refreshToken": "<opaque>",
    "expiresIn": 900
  },
  "meta": { "requestId": "...", "timestamp": "..." }
}
```

`expiresIn` là số **giây**. IP và User-Agent được ghi lại để phục vụ rate limit và
điều tra sự cố.

Lỗi: `AUTH_INVALID_CREDENTIALS` 401 · `AUTH_ACCOUNT_PENDING` 403 · `AUTH_ACCOUNT_DISABLED` 403 · `AUTH_LOGIN_LOCKED` 429

```bash
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@example.com","password":"Str0ng!Pass"}'
```

### 3.5 `POST /refresh`

Xoay vòng phiên: refresh token cũ bị thu hồi, cặp token mới được cấp.

| | |
|---|---|
| Auth | Không (dùng refresh token trong body) |
| Trả về | `200 OK` |

Request `{ "refreshToken": "<opaque>" }` → response giống `POST /login`.

Lỗi: `AUTH_TOKEN_INVALID` 401 · `AUTH_TOKEN_EXPIRED` 401 · `AUTH_REFRESH_REUSED` 401 (kèm thu hồi toàn bộ phiên của user)

### 3.6 `POST /logout`

Thu hồi phiên của thiết bị hiện tại. Thao tác **idempotent**: refresh token lạ vẫn
trả `204`.

| | |
|---|---|
| Auth | Không (dùng refresh token trong body) |
| Trả về | `204 No Content` — **không có body** |

Request `{ "refreshToken": "<opaque>" }`

Lỗi: `MALFORMED_REQUEST` 400

### 3.7 `POST /password/forgot`

Gửi link/token đặt lại mật khẩu. Phản hồi luôn chung, không tiết lộ email có tồn tại.

| | |
|---|---|
| Auth | Không |
| Rate limit | 5/phút (flow) |
| Trả về | `202 Accepted` |

Request `{ "email": "user@example.com" }`

```json
{ "data": { "message": "If the email is registered, a reset link has been sent." }, "meta": { "...": "..." } }
```

Lỗi: `AUTH_RESEND_COOLDOWN` 429 · `RATE_LIMITED` 429

### 3.8 `POST /password/reset`

Hoàn tất đặt lại mật khẩu bằng token đã nhận. Đặt lại xong sẽ đăng nhập luôn (trả
cặp token).

| | |
|---|---|
| Auth | Không |
| Rate limit | 5/phút (flow) |
| Trả về | `200 OK` |

Request

```json
{ "token": "<reset token>", "newPassword": "Str0ng!Pass" }
```

Response giống `POST /login` (`accessToken`, `refreshToken`, `expiresIn`).

Lỗi: `AUTH_RESET_INVALID` 400 · `AUTH_WEAK_PASSWORD` 400 · `RATE_LIMITED` 429

### 3.9 `GET /me`

Thông tin tài khoản của access token hiện tại. **Yêu cầu token hợp lệ.**

| | |
|---|---|
| Auth | Bearer access token |
| Trả về | `200 OK` |

Response

```json
{
  "data": {
    "id": "0f5c6e0c-1a44-4a1e-9b3d-9a1b2c3d4e5f",
    "email": "user@example.com",
    "role": "CUSTOMER"
  },
  "meta": { "requestId": "...", "timestamp": "..." }
}
```

`role` là `CUSTOMER` hoặc `ADMIN` (`internal/share/access`). Lỗi: `UNAUTHENTICATED` 401 (thiếu token) · `AUTH_TOKEN_INVALID` 401 · `AUTH_TOKEN_EXPIRED` 401

### 3.10 `POST /password/change`

Đổi mật khẩu khi đã đăng nhập. Mọi phiên của tài khoản bị thu hồi, kể cả phiên
đang dùng, rồi cấp lại cặp token mới — vì vậy body **bắt buộc** có
`refreshToken` hiện tại và access token cũ bị blacklist.

| | |
|---|---|
| Auth | Bearer access token |
| Trả về | `200 OK` |

Request

```json
{
  "currentPassword": "Str0ng!Pass",
  "newPassword": "Even5tronger!",
  "refreshToken": "<opaque hiện tại>"
}
```

Response giống `POST /login`.

Lỗi: `UNAUTHENTICATED` 401 · `AUTH_INVALID_CREDENTIALS` 401 · `AUTH_WEAK_PASSWORD` 400 · `VALIDATION_ERROR` 400 (thiếu `refreshToken`) · `AUTH_TOKEN_INVALID` 401 (refresh token không thuộc phiên nào) · `AUTH_REFRESH_REUSED` 401 (refresh token đã thu hồi hoặc thuộc tài khoản khác)

```bash
curl -X POST http://localhost:8080/api/v1/auth/password/change \
  -H "Authorization: Bearer $ACCESS" -H 'Content-Type: application/json' \
  -d '{"currentPassword":"Str0ng!Pass","newPassword":"Even5tronger!","refreshToken":"'$REFRESH'"}'
```

### 3.11 `GET /admin/probe`

Endpoint kiểm tra RBAC: chỉ `ADMIN` mới truy cập được. Khách không đăng nhập bị
`401`; `CUSTOMER` đã đăng nhập bị `403` **và** bị ghi vào `audit_logs`
(`AUTH_PRIVILEGE_DENIED`).

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Trả về | `200 OK` |

Response `{ "data": { "message": "admin" }, "meta": { "requestId": "...", "timestamp": "..." } }`

Lỗi: `UNAUTHENTICATED` 401 · `FORBIDDEN` 403

---

## 4. Module 02 - User (`/api/v1`)

Module hồ sơ khách hàng. Feature `003-user-profile` đang triển khai dần theo từng
user story; ở giai đoạn hiện tại mới có nhóm `/divisions` (dữ liệu hành chính tham
chiếu, dùng chung cho module User, Order, Shipping và Commission).

### 4.1 `GET /divisions/provinces`

Danh sách tỉnh/thành phố từ dataset hành chính nhúng sẵn trong binary (ADR-002),
sắp xếp theo tên, trùng tên thì theo mã.

| | |
|---|---|
| Auth | Bearer access token |
| Rate limit | Toàn cục (không có limit riêng cho endpoint này) |
| Trả về | `200 OK` |

Response:

```json
{
  "data": [
    { "code": "01", "name": "Hà Nội" },
    { "code": "79", "name": "Thành phố Hồ Chí Minh" }
  ],
  "meta": { "requestId": "...", "timestamp": "..." }
}
```

Lỗi: `UNAUTHENTICATED` 401

Ghi chú:

- Cố ý **không** phân trang: kết quả là dữ liệu tham chiếu bị chặn bởi bản thân dataset
  (~35 tỉnh), nhỏ hơn mọi `pageSize` mà client hợp lý có thể yêu cầu. Xem
  `specs/003-user-profile/plan.md` phần Complexity Tracking. Danh sách **địa chỉ** của
  khách hàng thì có phân trang (FR-018).
- Endpoint này không kiểm tra ADMIN: dữ liệu tham chiếu, không phải dữ liệu khách hàng.

### 4.2 `GET /divisions/provinces/{provinceCode}/wards`

Danh sách phường/xã của một tỉnh. `provinceCode` là mã tỉnh trong dataset (ví dụ `01`).

| | |
|---|---|
| Auth | Bearer access token |
| Rate limit | Toàn cục (không có limit riêng cho endpoint này) |
| Trả về | `200 OK` |

Response:

```json
{
  "data": [
    { "code": "0001", "name": "Phường Hoàng Kiết", "provinceCode": "01" }
  ],
  "meta": { "requestId": "...", "timestamp": "..." }
}
```

Lỗi: `UNAUTHENTICATED` 401 · `VALIDATION_ERROR` 400 · `USER_UNKNOWN_PROVINCE` 400

Ghi chú:

- Mã tỉnh không tồn tại trả `400 USER_UNKNOWN_PROVINCE`, **không** phải `404`: tham số
  đường dẫn sai là lỗi input, khớp với `contracts/error-codes.md` và với các endpoint
  địa chỉ dùng cùng mã lỗi này.
- Endpoint cố ý không phân trang, cùng lý do như 4.1.

---

## 5. Bảng tổng hợp

| Method | Path | Auth | Mô tả |
|---|---|---|---|
| GET | `/healthz` | — | Liveness |
| GET | `/readyz` | — | Readiness (db + schema + redis) |
| POST | `/api/v1/auth/register` | — | Đăng ký, gửi OTP |
| POST | `/api/v1/auth/verify-email` | — | Xác nhận OTP |
| POST | `/api/v1/auth/resend-verification` | — | Gửi lại OTP |
| POST | `/api/v1/auth/login` | — | Đăng nhập, cấp token |
| POST | `/api/v1/auth/refresh` | — | Xoay vòng refresh token |
| POST | `/api/v1/auth/logout` | — | Thu hồi phiên hiện tại (204) |
| POST | `/api/v1/auth/password/forgot` | — | Gửi token đặt lại mật khẩu |
| POST | `/api/v1/auth/password/reset` | — | Đặt lại mật khẩu + cấp token |
| GET | `/api/v1/auth/me` | Bearer | Thông tin tài khoản hiện tại |
| POST | `/api/v1/auth/password/change` | Bearer | Đổi mật khẩu, thu hồi mọi phiên |
| GET | `/api/v1/auth/admin/probe` | ADMIN | Kiểm tra RBAC |
| GET | `/api/v1/divisions/provinces` | Bearer | Danh sách tỉnh/thành phố |
| GET | `/api/v1/divisions/provinces/{provinceCode}/wards` | Bearer | Danh sách phường/xã của một tỉnh |

---

## 6. Quy tắc cập nhật

Khi thêm endpoint mới (module mới hoặc tính năng mới trong module cũ), thay đổi
`plan.md`, hoặc sửa/xoá endpoint, **phải** làm trong cùng một thay đổi:

1. Thêm mục cho endpoint vào mục module tương ứng, theo đúng 6 phần mà các mục hiện
   có dùng: bảng thông tin · Request · Response · Lỗi · ghi chú.
2. Cập nhật bảng tổng hợp ở mục 5.
3. Thêm dòng vào Change log ở mục 7.
4. Nếu là endpoint mới: thêm `openapi.yaml` trong `specs/<feature>/contracts/` cho
   khớp, hoặc ghi rõ trong change log rằng chưa có OpenAPI và lý do.
5. Nếu phát sinh error code mới: thêm vào bảng ở mục 1.4 (và vào
   `specs/<feature>/contracts/<module>-error-codes.md` của feature đó).

## 7. Change log

| Ngày | Thay đổi | Nguồn |
|---|---|---|
| 2026-10-06 | Thêm nhóm `/api/v1/divisions/*` (module 02 User): `GET /divisions/provinces` và `GET /divisions/provinces/{provinceCode}/wards`, đọc dataset hành chính nhúng sẵn (ADR-002). Hai endpoint cố ý không phân trang và yêu cầu Bearer token. | `internal/modules/user/presentation/http/router.go` |
| 2026-10-06 | `POST /password/change` thu hồi **toàn bộ** phiên của tài khoản và bắt buộc có `refreshToken`; thêm `VALIDATION_ERROR` 400 và `AUTH_TOKEN_INVALID` 401 vào danh sách lỗi của endpoint này. | `internal/modules/auth/application/implement/password.go` |
| 2026-10-06 | `POST /refresh` phát hiện replay thì thu hồi **toàn bộ** phiên của user và ghi `audit_logs` (`AUTH_REFRESH_REUSED`), khớp với mục 1.6. | `internal/modules/auth/application/implement/session.go` |
| 2026-10-06 | Gỡ `AUTH_EMAIL_TAKEN` khỏi bảng error code và khỏi `/register`: endpoint này luôn trả `202` chung nên không được tiết lộ email đã tồn tại (FR-011). | `internal/modules/auth/domain/constant/codes.go` |
| 2026-10-06 | Gỡ `AUTH_RESEND_COOLDOWN`/`AUTH_OTP_TOO_MANY_ATTEMPTS` khỏi `/register` và `AUTH_RESEND_COOLDOWN` khỏi `/password/forgot` (cơ chế cooldown chỉ áp dụng cho OTP qua `/resend-verification`). | `internal/modules/auth/application/implement/register.go` |
| 2026-10-06 | `POST /logout` chỉ trả `204` (idempotent); bỏ hai dòng lỗi 401 không xảy ra. | `internal/modules/auth/application/implement/session.go` |
| 2026-10-06 | `GET /me` và `POST /password/change` trả `AUTH_TOKEN_INVALID`/`AUTH_TOKEN_EXPIRED` thay vì `UNAUTHENTICATED` khi token sai hoặc hết hạn. | `internal/share/middleware/auth.go` |
| 2026-10-06 | `POST /verify-email` trả `AUTH_ACCOUNT_DISABLED` 403 cho tài khoản bị vô hiệu hoá (không kích hoạt lại được). | `internal/modules/auth/application/implement/verify_email.go` |
| 2026-10-06 | Sửa action audit của `403`: `auth.privilege_denied` → `AUTH_PRIVILEGE_DENIED` cho khớp với các action khác của module. | `internal/modules/auth/domain/constant/audit.go` |
| 2026-10-05 | Tạo tài liệu, ghi lại toàn bộ endpoint của foundation (2) và module 01 auth (11) | `internal/share/httpserver/routes.go`, `internal/modules/auth/presentation/http/router.go` |
