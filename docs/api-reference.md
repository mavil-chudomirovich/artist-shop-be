# API Reference

Tài liệu API chính thức của `artist-shop-be`. Đây là **nguồn tra cứu duy nhất** cho
toàn bộ endpoint đang tồn tại: khi thêm, sửa hoặc xoá endpoint, file này **phải**
được cập nhật trong cùng thay đổi đó (hiến pháp v1.6.0, mục *API Documentation*).

| Mục | Nội dung |
|-----|----------|
| Base URL (dev) | `http://localhost:8080` (Docker: `make up` → `API_PORT`) |
| Base path | `/api/v1` |
| Định dạng request | `application/json` — bắt buộc khi có body |
| Định dạng response | `application/json; charset=utf-8` |
| Xác thực | `Authorization: Bearer <accessToken>` |
| Chuẩn hoá error | `httpx` — `internal/share/httpx/errors.go` |
| Nguồn code | `internal/modules/*/presentation/http/router.go` |

> **Quan hệ với `specs/*/contracts/openapi.yaml` và `docs/swagger/`**: file OpenAPI
> trong `specs/*/contracts/` là artifact machine-readable **theo từng feature**, sinh ra
> ở bước `/speckit.plan`. `docs/swagger/` là spec machine-readable **xuyên module**, sinh
> từ annotation trong code bằng `make swagger`. File này là bản **authoritative** cho cả
> hai. Khi hai bên lệch nhau, **file này thắng** và artifact kia phải được sửa/sinh lại
> cho khớp (xem `make swagger-check`).

---

## 1. Quy ước chung

### 1.1 Middleware pipeline

Mọi request đi qua đúng thứ tự này (`internal/share/httpserver/routes.go`):

`RealIP` → `Correlation` → `RequestLogger` → `Recovery` → `CORS` → `BodyLimit`
→ `AllowedContentTypes(JSON, multipart)` → `Authentication` → *(rate limit, chỉ dưới
`/api/v1`)* → handler

`AllowedContentTypes` ở tầng pipeline chỉ kiểm tra `Content-Type` có nằm trong danh sách
media type được phép hay không. Kiểm tra **JSON thuần** nằm ở từng route, áp bằng
`JSONContentType`, vì một kiểm tra JSON toàn cục sẽ trả `415` cho mọi request
`multipart/form-data`. Xem `docs/decisions/008-per-route-content-type-and-body-ceiling.md`.

Hệ quả trực tiếp:

| Điều kiện vi phạm | HTTP | Error code |
|---|------|-----------|
| Body vượt `MAX_BODY_BYTES` (mặc định 4 MiB) | 413 | `PAYLOAD_TOO_LARGE` |
| Body vượt **trần riêng của route**, ở route nào cần trần riêng | 413 | Mã của **route đó**, không phải `PAYLOAD_TOO_LARGE` — xem `4.3` |
| `Content-Type` không khớp với loại body mà route khai báo (route JSON nhận `multipart/form-data`, route avatar nhận `text/plain`) | 415 | `UNSUPPORTED_MEDIA_TYPE` |
| Vượt rate limit | 429 + header `Retry-After: 1` | `RATE_LIMITED` |
| Origin không nằm trong `CORS_ALLOWED_ORIGINS` | CORS bị chặn ở preflight | — |

`PAYLOAD_TOO_LARGE` nghĩa đúng như câu chữ của nó: **request quá lớn vì một lý do không
liên quan tới nội dung request**. Route nào có luật riêng về nội dung — ví dụ avatar bị
giới hạn ở mức ảnh — thì trả mã của luật đó, để client biết chính xác cần sửa gì. Trước
đây route avatar trả `PAYLOAD_TOO_LARGE` khi client khai `Content-Length`, và trả
`USER_AVATAR_TOO_LARGE` khi client không khai: cùng một lỗi, hai mã, tuỳ cách client gửi.
Xem [ADR 010](decisions/010-avatar-upload-refusal-and-startup-guard.md).

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

Với endpoint phân trang, `meta` bổ sung `page`, `pageSize`, `total`. Lưu ý: `total` **vắng mặt khi bằng 0** (tầng envelope dùng `omitempty`), nên client phải đọc `total` thiếu là **0**, không phải "không có thông tin". Đây là hành vi có sẵn của tầng dùng chung, ảnh hưởng mọi endpoint phân trang.

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

**Riêng module user** (`internal/modules/user/domain/constant/codes.go`):

| Code | HTTP | Nghĩa |
|------|------|-------|
| `USER_NOT_FOUND` | 404 | Không có tài khoản nào mang identifier đó (chỉ dùng ở endpoint tra cứu của admin) |
| `USER_ADDRESS_NOT_FOUND` | 404 | Địa chỉ không tồn tại **đối với tài khoản đang gọi** |
| `USER_INVALID_PHONE` | 400 | Không phải số di động Việt Nam hợp lệ (10 chữ số, bắt đầu bằng `0`) |
| `USER_UNKNOWN_PROVINCE` | 400 | Mã tỉnh không có trong dataset hành chính |
| `USER_UNKNOWN_WARD` | 400 | Mã phường/xã không có trong dataset hành chính |
| `USER_WARD_PROVINCE_MISMATCH` | 400 | Phường/xã không thuộc tỉnh đã chọn |
| `USER_AVATAR_TYPE_UNSUPPORTED` | 400 | Byte tải lên không phải JPEG, PNG hoặc WebP |
| `USER_AVATAR_TOO_LARGE` | 413 | Ảnh vượt trần 2 MB |
| `USER_MEDIA_UNAVAILABLE` | 503 | Dịch vụ media từ chối hoặc không lưu được; **hồ sơ không bị đổi** nên thử lại được |

Các **mã lỗi** `USER_*` chỉ xuất hiện ở `/api/v1/users/*` và `/api/v1/divisions/*`. Những
tình huống dưới đây cố ý **không** sinh mã riêng của module (xem
`specs/003-user-profile/contracts/error-codes.md`): thiếu hoặc sai phiên →
`UNAUTHENTICATED`; `CUSTOMER` gọi endpoint của admin → `FORBIDDEN` 403; tham số
phân trang sai → `VALIDATION_ERROR` 400; một member của địa chỉ sai cấu trúc (ví dụ
`recipientName` rỗng khi `PATCH`) → `VALIDATION_ERROR` 400 với
`error.details[].field` chỉ đúng tên member.

> Action `audit_logs` của module dùng cùng tiền tố `USER_` nhưng **không** phải mã
> lỗi: `USER_PROFILE_UPDATED`, `USER_AVATAR_SET`, `USER_AVATAR_REMOVED`,
> `USER_ADDRESS_CREATED`, `USER_ADDRESS_UPDATED`, `USER_ADDRESS_DELETED`,
> `USER_ADDRESS_DEFAULT_SET`, `USER_PROFILE_VIEWED_BY_ADMIN` — xem
> `internal/modules/user/domain/constant/audit.go`. Không có `USER_PROFILE_VIEWED_BY_ADMIN`
> cho lần đọc trả `404`, vì không có dữ liệu nào rời khỏi tầm kiểm soát của khách.

**Riêng module category** (`internal/modules/category/domain/constant/codes.go`):

| Code | HTTP | Nghĩa |
|------|------|-------|
| `CATEGORY_NOT_FOUND` | 404 | Không có danh mục nào mang định danh/slug đó, hoặc danh mục đang bị ẩn, hoặc đã bị xoá — ba tình huống **cố ý** trả lời giống nhau |
| `CATEGORY_NAME_TAKEN` | 409 | Một danh mục khác đã dùng tên đó (so khớp sau khi trim và bỏ qua hoa/thường) |
| `CATEGORY_SLUG_TAKEN` | 409 | Một danh mục khác đã dùng slug đó |
| `CATEGORY_IN_USE` | 409 | Danh mục còn sản phẩm tham chiếu nên không xoá được. **Mới trở nên khả thi ở feature 006**: module 04 Product thêm `products.category_id` với `ON DELETE RESTRICT`, nên tầng lưu trữ từ chối thay vì xoá thành công |

Những tình huống dưới đây cố ý **không** sinh mã riêng của module (xem
`specs/005-category-catalog/contracts/error-codes.md`): slug không an toàn URL hoặc một
trường vượt độ dài → `VALIDATION_ERROR` 400 với `error.details[].field`; định danh đường
dẫn không phải UUID → `VALIDATION_ERROR` 400; thiếu hoặc sai phiên → `UNAUTHENTICATED` 401;
`CUSTOMER` gọi endpoint quản trị → `FORBIDDEN` 403 (ghi `AUTH_PRIVILEGE_DENIED`);
`page`/`pageSize` ngoài khoảng → `VALIDATION_ERROR` 400; body không parse được hoặc có
member lạ → `MALFORMED_REQUEST` 400.

> Action `audit_logs` của module dùng tiền tố `CATEGORY_` nhưng **không** phải mã lỗi:
> `CATEGORY_CREATED`, `CATEGORY_UPDATED`, `CATEGORY_HIDDEN`, `CATEGORY_SHOWN`,
> `CATEGORY_DELETED` — xem `internal/modules/category/domain/constant/audit.go`. Metadata
> của mỗi dòng chỉ mang `name` và `slug` do operator gõ, **không** mang hai cột khoá chuẩn
> hoá `normalized_name` / `normalized_slug` — hai cột đó không bao giờ xuất hiện trong
> response, audit hay log (`specs/005-category-catalog/data-model.md`).

**Riêng module product** (`internal/modules/product/domain/constant/codes.go`):

| Code | HTTP | Nghĩa |
|------|------|-------|
| `PRODUCT_NOT_FOUND` | 404 | Không có sản phẩm nào mang định danh/slug đó, hoặc sản phẩm bị ẩn — không đang bán và không phải pre-order, đã ngừng bán, hoặc **danh mục của nó bị ẩn** — hoặc đã bị xoá. Trên bề mặt công khai các tình huống này **cố ý** trả lời giống nhau (mục 6) |
| `PRODUCT_SLUG_TAKEN` | 409 | Một sản phẩm khác đã dùng slug đó (so khớp sau khi trim và bỏ qua hoa/thường) |
| `PRODUCT_STATE_TRANSITION_INVALID` | 409 | Chuyển trạng thái bán không được trạng thái hiện tại cho phép; message nêu trạng thái hiện tại |
| `PRODUCT_IMAGE_LIMIT_REACHED` | 409 | Sản phẩm đã có đủ 10 ảnh |
| `PRODUCT_IMAGE_TYPE_UNSUPPORTED` | 400 | Byte tải lên không phải JPEG, PNG hoặc WebP (nhận theo chữ ký nội dung) |
| `PRODUCT_IMAGE_TOO_LARGE` | 413 | Ảnh vượt trần 2 MB |
| `PRODUCT_MEDIA_UNAVAILABLE` | 503 | Dịch vụ media từ chối hoặc không lưu được; **không có gì bị đổi** nên thử lại được |

Những tình huống dưới đây cố ý **không** sinh mã riêng của module product (xem
`specs/006-product-catalog/contracts/error-codes.md`): slug không an toàn URL, một trường vượt
độ dài, giá không dương, `currency` không phải ba chữ in hoa, `categoryId`/`memberProductIds`
không tồn tại → `VALIDATION_ERROR` 400 với `error.details[].field`; định danh đường dẫn không
phải UUID → `VALIDATION_ERROR` 400; thiếu hoặc sai phiên → `UNAUTHENTICATED` 401; `CUSTOMER`
gọi endpoint quản trị → `FORBIDDEN` 403 (ghi `AUTH_PRIVILEGE_DENIED`); `page`/`pageSize` ngoài
khoảng → `VALIDATION_ERROR` 400; body không parse được hoặc có member lạ →
`MALFORMED_REQUEST` 400.

> Action `audit_logs` của module dùng tiền tố `PRODUCT_` nhưng **không** phải mã lỗi:
> `PRODUCT_CREATED`, `PRODUCT_UPDATED`, `PRODUCT_STATE_CHANGED`, `PRODUCT_IMAGE_ADDED`,
> `PRODUCT_IMAGE_REMOVED`, `PRODUCT_IMAGE_PRIMARY_SET`, `PRODUCT_DELETED` — xem
> `internal/modules/product/domain/constant/audit.go`. Metadata của mỗi dòng chỉ mang giá trị
> do operator gõ (`slug` ở create/update, `from`/`to` ở đổi trạng thái, `imageId` ở thao tác
> ảnh), **không** mang cột khoá chuẩn hoá `normalized_slug` — cột đó không bao giờ xuất hiện
> trong response, audit hay log (`specs/006-product-catalog/data-model.md`).

**Riêng module inventory** (`internal/modules/inventory/domain/constant/codes.go`):

| Code | HTTP | Nghĩa |
|------|------|-------|
| `INVENTORY_INSUFFICIENT_STOCK` | 409 | Thao tác sẽ đẩy số lượng **vật lý** của sản phẩm xuống dưới 0, hoặc xuống dưới phần đang được **giữ** cho các đơn đang thanh toán. Không có gì bị đổi |

`INVENTORY_INSUFFICIENT_STOCK` là **conflict** chứ không phải validation: số lượng gửi lên hợp lệ,
chỉ có trạng thái hiện tại của kệ khiến thao tác bất khả thi (không đủ hàng), và bước tiếp theo của
operator khác hẳn — nhập thêm hàng hoặc chờ giữ chỗ được giải quyết, chứ không phải sửa con số họ
gửi.

Những tình huống dưới đây cố ý **không** sinh mã riêng của module inventory (xem
`specs/007-inventory-tracking/contracts/error-codes.md`): sản phẩm không tồn tại → **tái sử dụng**
`PRODUCT_NOT_FOUND` 404 của module 04 (sản phẩm là tài nguyên của module 04; một mã thứ hai
gần giống sẽ buộc client rẽ nhánh trên hai mã cho cùng một tình huống — mã này vì vậy xuất hiện
trên **cả năm** route `/admin/inventory`); số lượng thiếu, không nguyên, âm hoặc bằng 0 khi bắt
buộc dương → `VALIDATION_ERROR` 400 với `error.details[].field`; định danh đường dẫn không phải
UUID → `VALIDATION_ERROR` 400; `page`/`pageSize` ngoài khoảng → `VALIDATION_ERROR` 400; body
không parse được hoặc có member lạ → `MALFORMED_REQUEST` 400; thiếu hoặc sai phiên →
`UNAUTHENTICATED` 401; `CUSTOMER` gọi endpoint quản trị → `FORBIDDEN` 403; quá hạn mức →
`RATE_LIMITED` 429; lỗi ngoài dự kiến → `INTERNAL_ERROR` 500.

> Action `audit_logs` của module dùng tiền tố `INVENTORY_` nhưng **không** phải mã lỗi:
> `INVENTORY_RESTOCKED`, `INVENTORY_DAMAGED`, `INVENTORY_ADJUSTED`, `INVENTORY_SALE_APPLIED` —
> xem `internal/modules/inventory/domain/constant/audit.go`. Metadata của mỗi dòng chỉ mang `kind`
> và `delta` (thao tác thủ công) hoặc `kind` và `sourceReference` (một sự kiện ngoài), và **không**
> mang ghi chú tự do của operator lẫn dữ liệu cá nhân của khách. `INVENTORY_SALE_APPLIED` không có
> bề mặt HTTP: đơn thanh toán do module 07/08 chưa tồn tại nên chưa có sự kiện nào phát ra nó.

### 1.5 Rate limit

| Phạm vi | Mặc định | Biến môi trường |
|---|---|---|
| Nhóm *flow* của auth (đăng ký, xác nhận, gửi lại, quên/đặt lại mật khẩu) | 5 req/phút / IP | `AUTH_FLOW_RATE_PER_MINUTE` |
| `POST /auth/login` | 10 req/phút / IP | `AUTH_LOGIN_RATE_PER_MINUTE` |
| `POST /users/me/avatar` | 10 req/giờ / IP | `USER_AVATAR_UPLOAD_RATE_PER_HOUR` |
| Ghi địa chỉ (`POST`/`PATCH`/`DELETE /users/me/addresses*`) | 30 req/phút / IP | `USER_ADDRESS_WRITE_RATE_PER_MINUTE` |
| Toàn bộ `/api/v1`, theo nhóm `read` / `write` / `auth` | 20 req/s, burst 40 | `RATE_LIMIT_RPS`, `RATE_LIMIT_BURST` |

Hai hạn mức của module user là **hai bucket riêng**, không dùng chung bộ đếm với
nhau và không dùng chung với hạn mức toàn cục: một tài khoản gõ bão nút upload avatar
không thể dùng hết hạn mức ghi địa chỉ. Cả hai vẫn cộng dồn trên hạn mức toàn cục
(`RATE_LIMIT_RPS`), vốn là lưới an toàn thô áp cho mọi route dưới `/api/v1`. Các
endpoint đọc của module user không có hạn mức riêng.

Nhóm `/categories` (công khai) và `/admin/categories` (quản trị) của module category
**không** có hạn mức riêng, chỉ chịu hạn mức toàn cục. Các endpoint ghi là ADMIN-only, nên
bề mặt lạm dụng mà hạn mức riêng của module user tồn tại để chặn — mọi khách đã đăng nhập
đều gọi được — **không tồn tại** ở đây (`specs/005-category-catalog/deferred.md`, D5).

Nhóm `/products` (công khai) và `/admin/products` (quản trị) của module product cũng **không**
có hạn mức riêng, chỉ chịu hạn mức toàn cục. Lý do giống module category: các endpoint ghi là
ADMIN-only, nên bề mặt lạm dụng mà hạn mức riêng tồn tại để chặn — mọi khách đã đăng nhập đều
gọi được — **không tồn tại** (`specs/006-product-catalog/spec.md`, mục *Assumptions*).

Nhóm `/admin/inventory` của module inventory cũng **không** có hạn mức riêng, chỉ chịu hạn mức
toàn cục, cùng lý do: mọi endpoint đều là ADMIN-only nên bề mặt lạm dụng mà hạn mức riêng tồn tại
để chặn **không tồn tại** (`specs/007-inventory-tracking/spec.md`, mục *Assumptions*).

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

### `GET /swagger/*`

Giao diện Swagger UI (OpenAPI 2.0) cho toàn bộ endpoint đã annotate. **Không** nằm dưới
`/api/v1` nên không bị rate limit API. Cần xác thực? Không.

Chỉ tồn tại khi `SWAGGER_ENABLED=true`; mặc định tắt và production phải để tắt. Khi tắt,
route trả `404 NOT_FOUND` như mọi path không tồn tại.

Spec được sinh từ annotation trong code bằng `make swagger` → `docs/swagger/`. Bản này
là **derived**, không phải nguồn authoritative: khi lệch với file này, file này thắng và
`docs/swagger/` phải được sinh lại (xem `make swagger-check`,
[decisions/011](decisions/011-swagger-from-code-annotations.md)).

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
| Trả về | `202 Accepted`, hoặc `503` **khi không gửi được** (xem bên dưới) |

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

Lỗi: `AUTH_WEAK_PASSWORD` 400 · `RATE_LIMITED` 429 · `SERVICE_UNAVAILABLE` 503

#### Nhánh gửi lại được `503`

Khi nhà cung cấp email **từ chối** thư xác nhận, endpoint này trả `503` chứ không phải
`500`: đây là sự cố của dịch vụ, không phải lỗi khách gây ra, và `500` khiến client
tưởng mình sai nên thử lại y hệt — một việc không bao giờ có kết quả.

Response `503`

```json
{
  "error": {
    "code": "SERVICE_UNAVAILABLE",
    "message": "The confirmation email could not be sent. Nothing was delivered to your address; request a new confirmation code and try again.",
    "requestId": "..."
  }
}
```

Mã `SERVICE_UNAVAILABLE` là mã **dùng chung** của `httpx`, được tái sử dụng; endpoint này
không thêm mã lỗi riêng nào của module auth.

Điều mà `503` này hứa thì đúng:

| Điều | Kết quả quan sát được |
|---|---|
| Tài khoản có được tạo không | **Có**, và giữ trạng thái `pending`. Không rollback: khách vẫn xác nhận được ngay khi gửi lại thành công |
| `POST /auth/resend-verification` ngay sau đó | **Được chấp nhận**, không bị `AUTH_RESEND_COOLDOWN`. Một lần gửi hỏng không tiêu tốn cooldown |
| Rate limit nhóm *flow* | Vẫn áp dụng, nên không mở đường gửi không giới hạn |
| Body có lộ chi tiết nhà cung cấp, credential hay địa chỉ người nhận không | Không |
| Có lộ ra email **đã tồn tại** không | **Không** — cả hai nhánh trả cùng một câu, xem bên dưới |

> **Hai nhánh trả lời giống hệt nhau khi không gửi được thư.**
> Một địa chỉ **đã có** tài khoản đang chờ xác minh vẫn thử gửi, và nếu gửi hỏng thì
> nhận **đúng** câu trả lời mà một địa chỉ chưa có nhận — cùng status, cùng mã lỗi, cùng
> câu chữ. Việc này là cần thiết: nếu hai nhánh khác nhau thì `202` nghĩa là *đã đăng ký*
> và `503` nghĩa là *chưa*, và ai đó đoán được email nào đã có tài khoản trong suốt lúc
> nhà cung cấp hỏng. Bước tiếp theo dành cho khách là giống nhau ở cả hai trường hợp —
> yêu cầu một mã xác minh mới — nên che được mà vẫn nói thật.
>
> Một địa chỉ đã **xác minh** vẫn trả `202` và **không** nhận thư, vì gửi mã xác minh cho
> người đã xác minh là hành vi không ai yêu cầu. Nói cách khác, oracle còn lại chỉ thu hẹp
> còn email **đã xác minh** trong lúc hạ tầng mail hỏng. Xem mục *Khoảng trống đã biết*
> ở `3.3`.

Bản ghi `audit_logs` cho lần đăng ký đó dùng action `AUTH_REGISTER_DELIVERY_FAILED` với
outcome `FAILURE`, và metadata `{"classification": ...}` là phân loại **của hệ thống
này**, không phải câu chữ của nhà cung cấp:

| Phân loại | Nghĩa | Có thử lại không |
|---|---|---|
| `TRANSIENT` | Provider báo lỗi tạm thời (SMTP 4xx) | Có, trong hạn mức thử lại |
| `UNREACHABLE` | Không kết nối được provider | Có, trong hạn mức thử lại |
| `CONFIGURATION` | Provider từ chối vì deployment này cấu hình sai (SMTP 5xx) | Không — thử lại không đổi được kết quả |
| `REFUSED` | Provider từ chối chính thư đó | Không |
| `UNKNOWN` | Lỗi không mang phân loại nào hệ thống này nhận biết | Không |

Một dòng log ở mức `error` đi kèm, chỉ chứa `accountId`, `deliveryFailure` và
`deliveryAttempts` — không có mã xác nhận, không có câu chữ của provider, không có địa
chỉ người nhận. Ngân sách thử lại có trần (tối đa 3 lần, tổng thời gian chờ nằm trong
mục tiêu trả lời 2 giây), nên `503` vẫn tới khách kịp thời.

> Nếu `.env` để `SMTP_HOST` rỗng, email được ghi ra log (log sender). Để xem OTP thật qua
> Mailpit thì không cần sửa `.env`: `make up-tools` tự trỏ container vào Mailpit, còn khi
> chạy API trên host thì tạo `.env.local` với `SMTP_HOST=localhost`, `SMTP_PORT=1025`. Đọc
> hộp thư ở `http://localhost:8025` hoặc `make mail`. Xem
> [configuration.md](configuration.md) mục *Phân tầng file env*.

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

Ghi chú cho người đọc tài liệu này:

- **Một lần gửi hỏng ở `POST /register` không tiêu tốn cooldown.** Khách vừa nhận
  `503` ở `3.1` có thể gọi endpoint này ngay lập tức và được chấp nhận; đó là điều mà
  câu trả lời `503` hứa. Ngược lại, sau một lần gửi **thành công**, cooldown vẫn áp dụng
  và trả `AUTH_RESEND_COOLDOWN` như trước.
- **Rate limit nhóm *flow* vẫn áp dụng** ngay cả khi cooldown đã được gỡ, nên việc gỡ
  cooldown không mở ra một đường gửi không giới hạn.
- **Khoảng trống đã biết:** nếu chính lần gửi lại này cũng thất bại, endpoint hiện trả
  `500 INTERNAL_ERROR` thay vì `503 SERVICE_UNAVAILABLE` như `POST /register`. Cơ chế gỡ
  cooldown và rate limit vẫn đúng, chỉ là **mã trả về** chưa được ánh xạ giống nhau. Sửa
  ở đây là một thay đổi hành vi, không phải một sửa tài liệu, nên chưa nằm trong thay
  đổi này.
- **Khoảng trống đã biết:** ở đường thất bại, endpoint này **vẫn** phân biệt được: một
  địa chỉ không có tài khoản, hoặc đã không còn ở trạng thái `pending`, trả `nil` và nhận
  `202` chung; còn một địa chỉ **có** tài khoản `pending` mà bước gửi hỏng thì trả lỗi.
  Nên khi nhà cung cấp hỏng, `202` nghĩa là *không phải tài khoản `pending`*, còn lỗi nghĩa
  là *là*. Ở đường thành công hai trường hợp không phân biệt được; ở đường thất bại thì có.
  Khác với `3.1`, ở đây FR-008a không áp dụng được vì endpoint này **không** hứa sẽ trả
  `503` khi gửi hỏng — nó trả `500`. Đóng khoảng trống này cần sửa cả status của
  resend lẫn hành vi khi địa chỉ đã tồn tại; cả hai đều ngoài thay đổi này.

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

Module hồ sơ khách hàng: hồ sơ, avatar, sổ địa chỉ giao hàng và tra cứu khách hàng
của admin, cùng nhóm dữ liệu tham chiếu `/divisions`.

Quy tắc chung của nhóm `/users`:

- **Chủ tài khoản luôn lấy từ session**, không bao giờ từ body hay query của client.
  Không route tự phục vụ nào nhận tham số định danh chủ sở hữu, nên truy cập chéo
  tài khoản là bất khả thi *theo cấu trúc*, không phải nhờ một kiểm tra có thể quên.
  Ngoại lệ duy nhất là `GET /users/{userId}` (`4.10`), ở đó `userId` là **đối tượng
  của** request chứ không phải tác nhân, và route có guard `ADMIN`.
- `chi` phân giải đoạn tĩnh `/me` trước tham số `/{userId}`, nên nhóm tự phục vụ giữ
  handler riêng và `/{userId}` vẫn trống cho endpoint tra cứu.

### 4.1 `GET /users/me`

Hồ sơ của chính access token đang dùng. Trả về đúng hình dạng hồ sơ mà mọi route
ghi của module đều trả, nên client chỉ cần một kiểu dữ liệu cho cả nhóm.

| | |
|---|---|
| Auth | Bearer access token |
| Rate limit | Toàn cục (không có limit riêng cho endpoint này) |
| Trả về | `200 OK` |

Response

```json
{
  "data": {
    "id": "0f5c6e0c-1a44-4a1e-9b3d-9a1b2c3d4e5f",
    "email": "an.nguyen@example.com",
    "role": "CUSTOMER",
    "displayName": "Nguyễn Thị An",
    "phone": "0912345678",
    "avatar": {
      "publicId": "artist-shop/avatars/8f2c1d4e6a",
      "url": "https://res.cloudinary.com/demo/image/upload/artist-shop/avatars/8f2c1d4e6a.jpg",
      "width": 512,
      "height": 512
    }
  },
  "meta": { "requestId": "...", "timestamp": "..." }
}
```

Lỗi: `UNAUTHENTICATED` 401 · `RATE_LIMITED` 429

Ghi chú:

- Endpoint này **không** gọi dịch vụ media: nó đọc từ các cột đã lưu. Vì vậy hồ sơ vẫn
  đọc được khi dịch vụ media hỏng hoặc khi chưa cấu hình `MEDIA_*` — cũng nghĩa là
  `avatar.url` là ảnh đã lưu, không phải kiểm tra sống của provider.
- `avatar` là `null` khi khách chưa có ảnh. Không có cờ `hasAvatar`: `null` là tín
  hiệu **duy nhất** cho "chưa có ảnh".
- `phone` luôn ở dạng chuẩn hoá **10 chữ số bắt đầu bằng `0`** và là `null` khi chưa
  đặt, bất kể client đã gửi `+84 912 345 678`, `0912.345.678` hay `0912345678`.
- `displayName` là chuỗi, có thể rỗng khi khách chưa từng đặt; nó không bao giờ `null`.
- `role` là `CUSTOMER` hoặc `ADMIN` (`internal/share/access`).

### 4.2 `PATCH /users/me`

Cập nhật tên hiển thị và/hoặc số điện thoại. **Cập nhật một phần**: hai thành viên
độc lập nhau, gửi một thành viên không đụng tới thành viên kia.

| | |
|---|---|
| Auth | Bearer access token |
| Rate limit | Toàn cục (không có limit riêng cho endpoint này) |
| Trả về | `200 OK` — trả lại **hồ sơ sau khi cập nhật** |

Request

```json
{ "displayName": "Nguyễn Thị An", "phone": "+84 912 345 678" }
```

Response `200` giống hệt `GET /users/me`, với `phone` đã chuẩn hoá thành
`"0912345678"`.

Lỗi: `VALIDATION_ERROR` 400 (`displayName` quá 120 ký tự; `details[].field` là
`"displayName"`) · `USER_INVALID_PHONE` 400 · `MALFORMED_REQUEST` 400 (body không
parse được, hoặc có member lạ) · `UNAUTHENTICATED` 401 · `RATE_LIMITED` 429

Ghi chú:

- **Bỏ trống một thành viên = giữ nguyên. Chuỗi rỗng = xoá.**
  `{ "phone": "" }` và `{ "phone": null }` đều xoá số điện thoại; `{ "displayName": "" }`
  xoá tên hiển thị. `displayName` trong contract là chuỗi thuần nên không gửi `null`.
- Một body **không nêu thành viên nào** (`{}`) là thao tác rỗng: trả `200` với hồ sơ
  hiện tại và **không** ghi dòng audit, vì không có gì thay đổi để truy vết.
- Mỗi thay đổi thành công ghi `audit_logs` với action `USER_PROFILE_UPDATED`, thuộc
  tính `changedFields` (`["displayName", "phone"]`) — **không** ghi giá trị, để dữ liệu
  liên hệ không bị nhân bản vào nhật ký.
- Validate ở tầng domain trước khi ghi: tên quá dài hoặc số điện thoại sai ⇒ **không
  cột nào** bị ghi, hồ sơ đã lưu giữ nguyên, kể cả thành viên hợp lệ đi kèm.
  `displayName` được đếm theo **ký tự** (rune) không phải byte, nên tiếng Việt có dấu vẫn
  tính đúng 120 ký tự.

### 4.3 `POST /users/me/avatar`

Tải ảnh đại diện lên, hoặc thay ảnh hiện tại. Body là `multipart/form-data` với đúng
một part `file`.

| | |
|---|---|
| Auth | Bearer access token |
| Rate limit | 10/giờ / IP (`USER_AVATAR_UPLOAD_RATE_PER_HOUR`) |
| Trả về | `200 OK` — trả lại hồ sơ đã gắn avatar mới |

Request

```bash
curl -X POST http://localhost:8080/api/v1/users/me/avatar \
  -H "Authorization: Bearer $ACCESS" \
  -F "file=@avatar.jpg"
```

```json
{
  "data": {
    "id": "0f5c6e0c-1a44-4a1e-9b3d-9a1b2c3d4e5f",
    "email": "an.nguyen@example.com",
    "role": "CUSTOMER",
    "displayName": "Nguyễn Thị An",
    "phone": "0912345678",
    "avatar": {
      "publicId": "artist-shop/avatars/8f2c1d4e6a",
      "url": "https://res.cloudinary.com/demo/image/upload/artist-shop/avatars/8f2c1d4e6a.jpg",
      "width": 512,
      "height": 512
    }
  },
  "meta": { "requestId": "...", "timestamp": "..." }
}
```

Lỗi: `VALIDATION_ERROR` 400 (thiếu part `file`, hoặc body không phải
`multipart/form-data`) · `USER_AVATAR_TYPE_UNSUPPORTED` 400 · `USER_AVATAR_TOO_LARGE`
413 · `USER_MEDIA_UNAVAILABLE` 503 · `MALFORMED_REQUEST` 400 ·
`UNAUTHENTICATED` 401 · `RATE_LIMITED` 429

> **`PAYLOAD_TOO_LARGE` không xuất hiện ở endpoint này.** Trước đây nó xuất hiện: một
> request quá lớn mà client **khai `Content-Length`** bị chặn ở tầng pipeline và nhận
> `413 PAYLOAD_TOO_LARGE`, còn request **không khai** nhận
> `413 USER_AVATAR_TOO_LARGE`. Cùng một lỗi, hai mã khác nhau, chỉ tuỳ client có khai độ
> dài hay không — client không có cách nào biết trước sẽ nhận mã nào. Nay **cả ba**
> dạng gửi (khai độ dài, không khai, khai thiếu) đều trả `413 USER_AVATAR_TOO_LARGE`.
> `PAYLOAD_TOO_LARGE` vẫn là mã của mọi route khác. Xem
> [ADR 010](decisions/010-avatar-upload-refusal-and-startup-guard.md).

Ghi chú:

- Loại ảnh được nhận diện từ **byte tải lên** (chữ ký định dạng), không tin tên file
  cũng không tin `Content-Type` mà client khai. Chỉ nhận JPEG, PNG và WebP.
- **Trần 2 MB nằm trên route này**, không phải trên hạn mức toàn cục:
  - route đặt `BodyLimit(2 MB + 64 KB)` — 64 KB là chỗ dành cho phần đệm của
    `multipart` (header part, boundary, tên field), tức **2 162 688 byte**. Middleware
    này chạy **trước** handler nên một body khai sai độ dài bị chặn sớm mà không bị đệm
    hết vào bộ nhớ. Nó báo đúng mã mà handler sẽ báo, nên request có khai `Content-Length`
    hay không đều cho cùng một câu trả lời;
  - handler còn chặn lần nữa khi đọc part bằng `LimitReader`, vì một `Content-Length`
    không đáng tin. Cả hai đường đều trả cùng một lỗi `413 USER_AVATAR_TOO_LARGE`;
  - `MAX_BODY_BYTES` (mặc định 4 MiB) chỉ là **chặn sớm thô** của cả pipeline, chạy
    trước routing nên route không nâng được; nó phải lớn hơn mọi trần riêng của route,
    nếu không một upload hợp lệ sẽ bị chặn trước khi route kịp áp luật thật. Ràng buộc
    đó **được kiểm tra lúc khởi động**: media có cấu hình mà `MAX_BODY_BYTES` thấp hơn
    2 162 688 thì API **từ chối khởi động** và báo tên biến cùng cả hai giá trị — xem
    `docs/configuration.md` và
    [ADR 010](decisions/010-avatar-upload-refusal-and-startup-guard.md).
- Ảnh lưu ở bề rộng **tối đa 512 px**; provider là nguồn sự thật cho kích thước kết quả
  (ADR-005), dịch vụ này không đụng vào ảnh.
- Thứ tự thao tác được chốt: kiểm tra byte trước → tải lên → dựng tham chiếu → **ghi
  dòng** → mới giải phóng ảnh cũ. Nên ảnh bị từ chối **không** chạm vào dòng dữ liệu
  và avatar hiện tại giữ nguyên; lỗi dọn ảnh cũ không biến một thay đổi đã thành công
  thành request thất bại.
- **Thiếu cấu hình `MEDIA_*` chỉ tắt đúng endpoint này.** Adapter fail-closed: mọi lệnh
  gọi provider mà không có credential trả `503 USER_MEDIA_UNAVAILABLE`, hồ sơ không bị
  đổi nên khách thử lại được. `GET /users/me`, `PATCH /users/me`,
  `DELETE /users/me/avatar` và toàn bộ nhóm `/addresses` vẫn hoạt động bình thường —
  vì chúng không gọi provider (xem `4.4`).
- Mỗi lần gắn/thay ảnh ghi `audit_logs` với action `USER_AVATAR_SET`, kèm kích thước
  và cờ `replaced` — không ghi đường dẫn ảnh.

### 4.4 `DELETE /users/me/avatar`

Gỡ ảnh đại diện. **Idempotent**: khách vốn đã không có ảnh thì không có gì để ghi,
không audit, và không gọi media service.

| | |
|---|---|
| Auth | Bearer access token |
| Rate limit | Toàn cục (không có limit riêng cho endpoint này) |
| Trả về | `200 OK` — trả lại hồ sơ sau khi gỡ |

Request: không có body.

Response `200` giống `GET /users/me`, với `avatar` là `null`.

Lỗi: `UNAUTHENTICATED` 401 · `RATE_LIMITED` 429

Ghi chú:

- Tham chiếu ảnh được xoá khỏi dòng **trước**, tài nguyên ở provider mới được giải phóng
  sau, và lỗi giải phóng bị bỏ qua. Dòng dữ liệu là điều khách và mọi lần đọc sau đó thấy;
  một ảnh mà provider không xoá được chỉ là dung lượng rác, còn báo lỗi sau khi đã ghi
  dòng sẽ nói với khách rằng ảnh vẫn còn trong khi nó đã mất.
- Vì lỗi giải phóng bị bỏ qua, endpoint này **vẫn trả `200`** kể cả khi thiếu cấu hình
  `MEDIA_*`. `503` chỉ xảy ra ở `4.3`.
- Gỡ ảnh thật sự ghi `audit_logs` với action `USER_AVATAR_REMOVED`, kèm kích thước ảnh
  và không ghi đường dẫn. Lần gọi mà không có ảnh để gỡ thì **không** ghi dòng nào.

### 4.5 `GET /users/me/addresses`

Trang địa chỉ giao hàng của chính khách: chỉ địa chỉ chưa ẩn, địa chỉ mặc định đứng
trước, sau đó tới địa chỉ cập nhật gần nhất nhất.

| | |
|---|---|
| Auth | Bearer access token |
| Rate limit | Toàn cục (không có limit riêng cho endpoint này) |
| Trả về | `200 OK` |

Query: `page` (mặc định `1`, tối thiểu `1`), `pageSize` (mặc định `20`, khoảng `1..100`).

Response

```json
{
  "data": [
    {
      "id": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e",
      "recipientName": "Nguyễn Thị An",
      "recipientPhone": "0912345678",
      "provinceCode": "01",
      "provinceName": "Hà Nội",
      "wardCode": "00004",
      "wardName": "Ba Đình",
      "streetAddress": "12 Ngõ 129 Dịch Vọng",
      "isDefault": true,
      "divisionNeedsReview": false
    }
  ],
  "meta": { "requestId": "...", "timestamp": "...", "page": 1, "pageSize": 20, "total": 1 }
}
```

Lỗi: `VALIDATION_ERROR` 400 (`page` / `pageSize` sai định dạng hoặc ngoài khoảng;
`details[].field` là `"page"` hoặc `"pageSize"`) · `UNAUTHENTICATED` 401 ·
`RATE_LIMITED` 429

Ghi chú:

- `divisionNeedsReview` **luôn có mặt**, kể cả khi là `false`: danh sách của chính khách
  biết câu trả lời vì use case kiểm tra từng mã đã lưu với dataset. Bỏ mất `false` đã
  biết sẽ khiến client đọc "vắng mặt" thành "không biết" — trong JavaScript đó là
  `undefined`, không phải `false`.
  `divisionNeedsReview: true` nghĩa là mã tỉnh/phường đã lưu không còn trong dataset
  hiện hành; tên đã chụp vẫn được trả về để khách thấy đúng thứ đã lưu.
- `recipientPhone` cũng được chuẩn hoá 10 chữ số, giống `phone` của hồ sơ.
  - Khi tài khoản chưa có địa chỉ nào, `data` là **`[]`**, không phải `null`. Trường
    `addresses` trong truy vấn của admin (`4.10`) cũng là `[]`: cả hai khớp với
    `type: array` trong contract, nên client không phải xử lý `null`.

### 4.6 `POST /users/me/addresses`

Tạo địa chỉ giao hàng. Địa chỉ đầu tiên của một tài khoản tự động trở thành địa chỉ
mặc định.

| | |
|---|---|
| Auth | Bearer access token |
| Rate limit | 30/phút / IP (`USER_ADDRESS_WRITE_RATE_PER_MINUTE`) |
| Trả về | `201 Created` |

Request

```json
{
  "recipientName": "Nguyễn Thị An",
  "recipientPhone": "0912345678",
  "provinceCode": "01",
  "wardCode": "00004",
  "streetAddress": "12 Ngõ 129 Dịch Vọng"
}
```

Response `201` là một phần tử `data` của `GET /users/me/addresses`, cùng hình dạng.

Lỗi: `VALIDATION_ERROR` 400 (thiếu `recipientName` / `recipientPhone` /
`provinceCode` / `wardCode` / `streetAddress`, hoặc member vượt giới hạn 120 / 255 ký
tự; `details[].field` chỉ đúng tên member) · `USER_INVALID_PHONE` 400 ·
`USER_UNKNOWN_PROVINCE` 400 · `USER_UNKNOWN_WARD` 400 ·
`USER_WARD_PROVINCE_MISMATCH` 400 · `MALFORMED_REQUEST` 400 · `UNAUTHENTICATED` 401 ·
`RATE_LIMITED` 429

Ghi chú:

- `provinceName` và `wardName` là **tuỳ chọn**: client gửi lên thì dùng, không gửi thì
  tên được chụp từ dataset. Tên đã chụp là thứ hiển thị về sau, nên đổi tên đơn vị hành
  chính sau này không xoá được thứ khách đã nhìn thấy.
- Tỉnh và phường phải đến từ dataset hành chính, và phường phải thuộc tỉnh đã chọn —
  cả hai được kiểm tra **trước khi** ghi bất kỳ thứ gì, nên một lần lưu sai không để lại
  dòng nào.
- `recipientPhone` trả `400 USER_INVALID_PHONE` với `details[].field` là
  `"recipientPhone"` — cùng một luật domain bảo vệ hai member khác nhau, nên tên field
  trong `details` mới là thứ client dùng để báo lỗi cạnh đúng ô nhập.
- Ghi `audit_logs` với action `USER_ADDRESS_CREATED`. Khi địa chỉ vừa tạo trở thành
  địa chỉ mặc định đầu tiên của tài khoản, nó ghi **thêm** một dòng
  `USER_ADDRESS_DEFAULT_SET`: trở thành mặc định vốn là một thay đổi mặc định, dù khách
  chỉ xin lưu một địa chỉ. Quyết định "có thành mặc định không" và lệnh insert nằm
  trong **một** transaction.

### 4.7 `PATCH /users/me/addresses/{addressId}`

Sửa một địa chỉ. Mọi member đều tuỳ chọn; cờ mặc định **được giữ nguyên**, dùng
`4.9` để đổi.

| | |
|---|---|
| Auth | Bearer access token |
| Rate limit | 30/phút / IP (`USER_ADDRESS_WRITE_RATE_PER_MINUTE`) |
| Trả về | `200 OK` |

Request

```json
{ "streetAddress": "12 Ngõ 129 Dịch Vọng, căn hộ 5", "wardCode": "00008" }
```

Response `200` là một phần tử `data` của `GET /users/me/addresses`.

Lỗi: `VALIDATION_ERROR` 400 (`addressId` không phải UUID, hoặc member sai cấu trúc;
`details[].field` chỉ đúng tên member) · `USER_INVALID_PHONE` 400 ·
`USER_UNKNOWN_PROVINCE` 400 · `USER_UNKNOWN_WARD` 400 ·
`USER_WARD_PROVINCE_MISMATCH` 400 · `USER_ADDRESS_NOT_FOUND` 404 ·
`MALFORMED_REQUEST` 400 · `UNAUTHENTICATED` 401 · `RATE_LIMITED` 429

Ghi chú:

- Member bị bỏ trống thì giữ nguyên. **Khác `PATCH /users/me`, một chuỗi rỗng ở đây bị
  từ chối chứ không phải "xoá"**: `recipientName` và `streetAddress` rỗng trả
  `400 VALIDATION_ERROR` (`details[].field` là tên member), và `recipientPhone` rỗng trả
  `400 USER_INVALID_PHONE` — vì cột tương ứng trong `addresses` là `NOT NULL` và một
  địa chỉ không có người nhận hoặc không có địa chỉ chi tiết là dữ liệu không dùng
  được. Muốn bỏ một địa chỉ thì dùng `4.8`.
- Toàn bộ thay đổi được validate **trước** khi ghi, và trần độ dài của contract được
  kiểm tra lại cho *mọi* thành viên kể cả thành viên request không nêu — nên một lần
  sửa không thể để lại một giá trị quá dài đang tồn tại, và ngược lại một dòng dữ liệu
  cũ đã quá dài buộc phải được rút ngắn chứ không chỉ chạm vào.
- Tỉnh/phường chỉ được kiểm tra lại với dataset **khi request chạm vào chúng**. Một
  địa chỉ có mã phường đã rút khỏi dataset vẫn phải sửa được các member còn hợp lệ —
  một mã đã nghỉ không được đóng băng cả dòng.
- Tên đơn vị hành chính được chụp lại mỗi khi **mã** của nó dịch chuyển, nên tên không
  bao giờ mô tả một đơn vị khác với mã đang lưu.
- Chỉ ghi `audit_logs` (`USER_ADDRESS_UPDATED`) khi có member **thực sự đổi**; một
  `PATCH` không đổi gì thì không ghi dòng.

### 4.8 `DELETE /users/me/addresses/{addressId}`

Ẩn một địa chỉ. Dòng dữ liệu **được giữ lại** để các đơn hàng cũ vẫn giữ đúng địa chỉ
đã dùng (ADR-004).

| | |
|---|---|
| Auth | Bearer access token |
| Rate limit | 30/phút / IP (`USER_ADDRESS_WRITE_RATE_PER_MINUTE`) |
| Trả về | `204 No Content` — **không có body** |

Request: không có body.

Lỗi: `VALIDATION_ERROR` 400 (`addressId` không phải UUID) ·
`USER_ADDRESS_NOT_FOUND` 404 · `UNAUTHENTICATED` 401 · `RATE_LIMITED` 429

Ghi chú:

- Địa chỉ bị ẩn rời khỏi `4.5` và không thể là địa chỉ mặc định nữa. Ẩn địa chỉ mặc
  định **duy nhất** để tài khoản không còn mặc định nào; địa chỉ tạo tiếp theo sẽ thành
  mặc định.
- Không ghi `USER_ADDRESS_DEFAULT_SET`: không có địa chỉ nào *trở thành* mặc định, nên
  ghi sự kiện đó sẽ mô tả sai điều đã xảy ra. Trường hợp này đã được
  `USER_ADDRESS_DELETED` bao phủ.

### 4.9 `POST /users/me/addresses/{addressId}/default`

Đặt một địa chỉ làm mặc định. Xoá mặc định cũ và đặt mặc định mới trong **một** thao
tác không chia nhỏ được.

| | |
|---|---|
| Auth | Bearer access token |
| Rate limit | 30/phút / IP (`USER_ADDRESS_WRITE_RATE_PER_MINUTE`) |
| Trả về | `200 OK` |

Request: không có body.

Response `200` là một phần tử `data` của `GET /users/me/addresses`, với
`isDefault: true`.

Lỗi: `VALIDATION_ERROR` 400 (`addressId` không phải UUID) ·
`USER_ADDRESS_NOT_FOUND` 404 · `UNAUTHENTICATED` 401 · `RATE_LIMITED` 429

Ghi chú:

- Cả hai lệnh ghi nằm trong **một transaction**, và thứ tự là xoá mặc định cũ **rồi mới**
  đặt mới: đó là điều partial unique index trên `(user_id) WHERE is_default AND
  deleted_at IS NULL` đòi hỏi. Nếu lỗi giữa hai lệnh, tài khoản sẽ mất hẳn địa chỉ mặc
  định.
- Ràng buộc "tối đa một địa chỉ mặc định" nằm ở **tầng lưu trữ**, không chỉ ở tầng ứng
  dụng (ADR-003).

### 4.10 `GET /users/{userId}`

Tra cứu chỉ đọc cho **admin**, phục vụ xử lý đơn hàng và commission.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (không có limit riêng cho endpoint này) |
| Trả về | `200 OK` |

Response

```json
{
  "data": {
    "id": "0f5c6e0c-1a44-4a1e-9b3d-9a1b2c3d4e5f",
    "email": "an.nguyen@example.com",
    "role": "CUSTOMER",
    "displayName": "Nguyễn Thị An",
    "phone": "0912345678",
    "addresses": [
      {
        "id": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e",
        "recipientName": "Nguyễn Thị An",
        "recipientPhone": "0912345678",
        "provinceCode": "01",
        "provinceName": "Hà Nội",
        "wardCode": "00004",
        "wardName": "Ba Đình",
        "streetAddress": "12 Ngõ 129 Dịch Vọng",
        "isDefault": true
      }
    ]
  },
  "meta": { "requestId": "...", "timestamp": "..." }
}
```

Lỗi: `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 (ghi `audit_logs` với action
`AUTH_PRIVILEGE_DENIED`) · `VALIDATION_ERROR` 400 (`userId` không phải UUID) ·
`USER_NOT_FOUND` 404 · `RATE_LIMITED` 429

Ghi chú:

- **Đường dẫn là `GET /api/v1/users/{userId}`, không có tiền tố `/admin`.** Ở đây tài
  khoản trong đường dẫn là **đối tượng** của request chứ không phải tác nhân; tác nhân
  đến từ session và vai trò `ADMIN` do middleware quyết định.
- Module này **không** có đường ghi cho admin. Không phải là từ chối 403 — endpoint đó
  đơn giản là không tồn tại.
- `addresses` **không có** `divisionNeedsReview`, và đó là cố ý chứ không phải bỏ sót:
  DTO hợp đồng liên module mà lookup dựng từ đó không mang cờ này, và trên đường đi này
  không có bước nào kiểm tra mã đã lưu với dataset. Trả `false` sẽ khẳng định mọi mã đều
  còn hiệu lực khi không có gì xác nhận điều đó; vắng mặt nghĩa là "góc nhìn này không
  trả lời được", còn danh sách của chính khách thì biết nên phải nói ra. Tài khoản
  không có địa chỉ trả `[]` chứ không phải `null`.
- Danh sách địa chỉ ở đây **không phân trang** và được đặt trần ở 100 phần tử: hợp
  đồng liên module mang kèm thứ tự *mặc định trước, rồi cập nhật gần nhất nhất*, nên
  module đơn vị có thể dựa vào thứ tự đó để đưa địa chỉ mặc định lên đầu.
- **Mỗi lần đọc thành công đều bị ghi** `audit_logs` với action
  `USER_PROFILE_VIEWED_BY_ADMIN` và `addressCount` — không ghi dữ liệu liên hệ. Một lần
  đọc trả `404` thì **không** ghi: không có dữ liệu nào rời khỏi tầm kiểm soát của khách.
- `403` cho khách đã đăng nhập được ghi bởi **module auth** với action
  `AUTH_PRIVILEGE_DENIED` (`internal/modules/auth/domain/constant/audit.go`), không phải
  action của module user — cùng cơ chế như `GET /auth/admin/probe`.

> **Địa chỉ của người khác trả về cùng một lỗi.** `4.7`, `4.8` và `4.9` đọc địa chỉ
> qua truy vấn có điều kiện sở hữu, nên một `addressId` không tồn tại, một địa chỉ đã ẩn
> và một địa chỉ thuộc tài khoản khác đều cho ra **cùng** `404
> USER_ADDRESS_NOT_FOUND`. Nếu có mã riêng cho trường hợp "tồn tại nhưng của người
> khác", bất kỳ khách đã đăng nhập nào cũng dò được một địa chỉ có thật trong hệ thống.

### 4.11 `GET /divisions/provinces`

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
    { "code": "79", "name": "Hồ Chí Minh" }
  ],
  "meta": { "requestId": "...", "timestamp": "..." }
}
```

Lỗi: `UNAUTHENTICATED` 401

Ghi chú:

- Cố ý **không** phân trang: kết quả là dữ liệu tham chiếu bị chặn bởi bản thân dataset
  (~35 tỉnh), nhỏ hơn mọi `pageSize` mà client hợp lý có thể yêu cầu. Xem
  `specs/003-user-profile/plan.md` phần Complexity Tracking. Danh sách **địa chỉ** của
  khách hàng thì có phân trang (FR-018, xem `4.5`).
- Endpoint này không kiểm tra ADMIN: dữ liệu tham chiếu, không phải dữ liệu khách hàng.

### 4.12 `GET /divisions/provinces/{provinceCode}/wards`

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
    { "code": "00004", "name": "Ba Đình", "provinceCode": "01" }
  ],
  "meta": { "requestId": "...", "timestamp": "..." }
}
```

> Mã phường trong dataset là chuỗi **5 chữ số** và tên là tên đơn vị **không kèm tiền
> tố** `Phường`/`Xã` (`00004` = `Ba Đình`, `00008` = `Ngọc Hà` cùng thuộc `01`). Mọi ví dụ
> trong mục này lấy từ chính dataset.

Lỗi: `UNAUTHENTICATED` 401 · `VALIDATION_ERROR` 400 · `USER_UNKNOWN_PROVINCE` 400

Ghi chú:

- Mã tỉnh không tồn tại trả `400 USER_UNKNOWN_PROVINCE`, **không** phải `404`: tham số
  đường dẫn sai là lỗi input, khớp với `contracts/error-codes.md` và với các endpoint
  địa chỉ dùng cùng mã lỗi này.
- Endpoint cố ý không phân trang, cùng lý do như 4.11.

---

## 5. Module 03 — Category (`/api/v1`)

Danh mục sản phẩm: khách duyệt và đọc danh mục, operator quản trị. **Một bảng** phục vụ
**hai bề mặt** trên **hai hình dạng response** khác nhau — bề mặt công khai
(`/categories`, không cần token, địa chỉ theo **slug**) và bề mặt quản trị
(`/admin/categories`, vai trò `ADMIN`, địa chỉ theo **định danh**).

Bốn điều dễ đọc sai, nói ngay:

- **Một danh mục bị ẩn và một slug chưa từng tồn tại trả lời y hệt nhau**
  (`404 CATEGORY_NOT_FOUND`). Nếu hai câu trả lời khác nhau, endpoint sẽ xác nhận những
  danh mục operator đã chọn **không** công bố. Xem `5.2`.
- **Va chạm tên/slug trả `409`, không phải `400`.** Giá trị gửi lên hợp lệ và chỉ đang bị
  chiếm; hai mã `CATEGORY_NAME_TAKEN` / `CATEGORY_SLUG_TAKEN` nói **field nào** va chạm.
  Xem `5.4` và `5.6`.
- **Hình dạng công khai chỉ có bốn member** `id`, `name`, `slug`, `description`; trạng thái
  hiển thị và thứ tự (`isVisible`, `position`) chỉ có ở hình dạng quản trị.
- Module **không** có hạn mức riêng, chỉ chịu hạn mức toàn cục (§1.5).

Hai danh sách đều phân trang theo quy ước chung: `page` mặc định `1`, `pageSize` mặc định
`20`, khoảng `1..100`.

### 5.1 `GET /categories`

Danh sách danh mục **đang hiển thị**, theo thứ tự operator đã đặt.

| | |
|---|---|
| Auth | Không |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` |

Query: `page` (mặc định `1`, tối thiểu `1`), `pageSize` (mặc định `20`, khoảng `1..100`).

Response `200`

```json
{
  "data": [
    {
      "id": "0f5c6e0c-1a44-4a1e-9b3d-9a1b2c3d4e5f",
      "name": "Điêu khắc",
      "slug": "diau-khac",
      "description": "Tác phẩm điêu khắc"
    }
  ],
  "meta": { "requestId": "...", "timestamp": "...", "page": 1, "pageSize": 20, "total": 1 }
}
```

Lỗi: `VALIDATION_ERROR` 400 (`page` / `pageSize` sai định dạng hoặc ngoài khoảng;
`details[].field` là `"page"` hoặc `"pageSize"`) · `RATE_LIMITED` 429

Ghi chú:

- Chỉ trả danh mục `is_visible = true`; `meta.total` đếm đúng số đó.
- Thứ tự ổn định: `position`, rồi `created_at`, rồi `id` — hai request giống nhau trả
  cùng thứ tự (FR-003).
- Catalogue rỗng hoặc bị ẩn hết trả `data: []` (mảng rỗng), **không** phải `null` và
  **không** phải lỗi.

### 5.2 `GET /categories/{slug}`

Đọc một danh mục đang hiển thị theo slug.

| | |
|---|---|
| Auth | Không |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` |

Response `200`

```json
{
  "data": {
    "id": "0f5c6e0c-1a44-4a1e-9b3d-9a1b2c3d4e5f",
    "name": "Điêu khắc",
    "slug": "diau-khac",
    "description": "Tác phẩm điêu khắc"
  },
  "meta": { "requestId": "...", "timestamp": "..." }
}
```

Lỗi: `CATEGORY_NOT_FOUND` 404 · `RATE_LIMITED` 429

> **Danh mục bị ẩn, danh mục đã xoá và slug chưa từng tồn tại trả lời y hệt nhau** — cùng
> status, cùng mã `CATEGORY_NOT_FOUND`, cùng câu chữ. Đây là bắt buộc, không chỉ tiện: nếu
> một danh mục bị ẩn trả lời khác một slug chưa dùng, endpoint sẽ xác nhận danh mục nào
> operator đã chọn **không** công bố, và bất kỳ ai cũng dò được. Xem
> `specs/005-category-catalog/contracts/error-codes.md`, mục *The one answer that is
> deliberately ambiguous*.

### 5.3 `GET /admin/categories`

Danh sách **toàn bộ** danh mục, kể cả danh mục không hiển thị.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` |

Query: như `5.1`.

Response `200`

```json
{
  "data": [
    {
      "id": "0f5c6e0c-1a44-4a1e-9b3d-9a1b2c3d4e5f",
      "name": "Điêu khắc",
      "slug": "diau-khac",
      "description": "Tác phẩm điêu khắc",
      "position": 1,
      "isVisible": true,
      "createdAt": "2026-10-07T08:15:04Z",
      "updatedAt": "2026-10-07T08:15:04Z"
    }
  ],
  "meta": { "requestId": "...", "timestamp": "...", "page": 1, "pageSize": 20, "total": 1 }
}
```

Lỗi: `VALIDATION_ERROR` 400 (`page` / `pageSize` sai định dạng hoặc ngoài khoảng) ·
`UNAUTHENTICATED` 401 · `FORBIDDEN` 403 (ghi `audit_logs` với action
`AUTH_PRIVILEGE_DENIED`) · `RATE_LIMITED` 429

### 5.4 `POST /admin/categories`

Tạo danh mục. Danh mục mới **luôn đang hiển thị**; muốn ẩn thì tạo rồi gọi `PATCH` với
`isVisible: false`.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `201 Created` |

Request

```json
{ "name": "Điêu khắc", "slug": "diau-khac", "description": "Tác phẩm điêu khắc", "position": 1 }
```

`name` và `slug` bắt buộc; `description` và `position` tuỳ chọn (`position` mặc định `0` và
có thể âm). `slug` do operator viết, **không** bao giờ sinh từ `name`; giá trị lưu là giá
trị operator gõ, đã trim.

Response `201` giống `data` của `5.3`.

Lỗi: `VALIDATION_ERROR` 400 (`name` rỗng hoặc quá 120 ký tự; `slug` rỗng, quá 140 ký tự hoặc
không khớp mẫu URL-safe; `description` quá 2000 ký tự — `details[].field` chỉ đúng member) ·
`MALFORMED_REQUEST` 400 (body không parse được hoặc có member lạ) · `UNAUTHENTICATED` 401 ·
`FORBIDDEN` 403 · `CATEGORY_NAME_TAKEN` 409 · `CATEGORY_SLUG_TAKEN` 409 · `RATE_LIMITED` 429

> **Va chạm là `409`, không phải `400`.** Giá trị gửi lên **hợp lệ**; nó chỉ đang bị một
> danh mục khác chiếm. Đó là tình huống khác một giá trị sai hình dạng, và bước tiếp theo
> của operator cũng khác: đổi sang giá trị khác, chứ không phải sửa giá trị này. Hai mã
> `CATEGORY_NAME_TAKEN` / `CATEGORY_SLUG_TAKEN` cho client biết **field nào** va chạm, kèm
> `details[].field` (`"name"` hoặc `"slug"`). Gộp cả hai vào `400` sẽ khiến hai lời từ chối
> phổ biến nhất của catalogue không phân biệt được
> (`specs/005-category-catalog/contracts/error-codes.md`).
>
> So khớp tên bỏ qua **hoa/thường** (theo Unicode, kể cả chữ có dấu tiếng Việt) và
> **khoảng trắng hai đầu**. Khoảng trắng **bên trong** không được gộp: `"Tranh sơn dầu"` và
> `"Tranh  sơn  dầu"` là hai tên khác nhau (`specs/005-category-catalog/deferred.md`, D3).

### 5.5 `GET /admin/categories/{categoryId}`

Đọc một danh mục theo định danh, kể cả danh mục đang ẩn.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` |

Response `200` giống `data` của `5.3`.

Lỗi: `VALIDATION_ERROR` 400 (`categoryId` không phải UUID) · `UNAUTHENTICATED` 401 ·
`FORBIDDEN` 403 · `CATEGORY_NOT_FOUND` 404 · `RATE_LIMITED` 429

### 5.6 `PATCH /admin/categories/{categoryId}`

Sửa một phần: member bỏ trống giữ nguyên giá trị hiện tại. Gửi lại đúng giá trị danh mục
đang giữ cũng **thành công** — một danh mục không bao giờ trùng với chính nó.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` — trả danh mục sau khi sửa |

Request (mọi member tuỳ chọn)

```json
{
  "name": "Điêu khắc",
  "slug": "diau-khac",
  "description": "Mô tả mới",
  "position": 5,
  "isVisible": false
}
```

Đổi `isVisible` đi qua chuyển trạng thái Hide/Show chứ không gán thẳng; ẩn một danh mục đã
ẩn (hoặc hiện một danh mục đã hiện) là **no-op**, không phải lỗi.

Response `200` giống `data` của `5.3`.

Lỗi: `VALIDATION_ERROR` 400 (giá trị sai hình dạng hoặc quá độ dài, `categoryId` không phải
UUID) · `MALFORMED_REQUEST` 400 · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 ·
`CATEGORY_NOT_FOUND` 404 · `CATEGORY_NAME_TAKEN` 409 · `CATEGORY_SLUG_TAKEN` 409 ·
`RATE_LIMITED` 429. Va chạm để lại **cả hai** danh mục nguyên vẹn.

### 5.7 `DELETE /admin/categories/{categoryId}`

Xoá danh mục. Đây là **hard delete**: dòng biến mất, dòng audit ở lại.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `204 No Content` — **không có body** |

Request: không có body.

Lỗi: `VALIDATION_ERROR` 400 (`categoryId` không phải UUID) · `UNAUTHENTICATED` 401 ·
`FORBIDDEN` 403 · `CATEGORY_NOT_FOUND` 404 · `CATEGORY_IN_USE` 409 · `RATE_LIMITED` 429

Ghi chú:

- Xoá lại một danh mục đã xoá trả cùng `404 CATEGORY_NOT_FOUND`, không phải lỗi.
- **Luật "không xoá cứng danh mục còn sản phẩm" nay đã kiểm chứng được** (feature 006). Tham
  chiếu `products.category_id` với `ON DELETE RESTRICT` do module 04 Product thêm vào; khi còn
  sản phẩm tham chiếu, **tầng lưu trữ** từ chối xoá và module 03 dịch lỗi khoá ngoại đó thành
  `409 CATEGORY_IN_USE`. Trước feature 006, tình huống này không thể xảy ra (không có bảng sản
  phẩm), nên đây là một câu trả lời **mới** của endpoint. Danh mục và sản phẩm đều nguyên vẹn
  sau khi bị từ chối; xoá danh mục không còn sản phẩm vẫn `204` như trước.
  Xem `specs/006-product-catalog/deferred.md` (D8) và
  [decisions/013](decisions/013-product-visibility-media-and-hard-delete.md).

---

## 6. Module 04 — Product (`/api/v1`)

Sản phẩm của shop: món vật lý, combo set và pre-order, cùng ảnh, giá, danh mục và trạng thái
bán. **Một bảng, hai bề mặt, hai hình dạng response**: bề mặt công khai (`/products`, không cần
token, địa chỉ theo **slug**) và bề mặt quản trị (`/admin/products`, vai trò `ADMIN`, địa chỉ
theo **định danh**).

Bốn điều dễ đọc sai, nói ngay:

- **Sản phẩm bị ẩn và một slug chưa từng tồn tại trả lời y hệt nhau** (`404
  PRODUCT_NOT_FOUND`). Gồm cả sản phẩm nằm trong **danh mục bị ẩn**. Nếu các tình huống trả lời
  khác nhau, endpoint sẽ xác nhận sản phẩm/danh mục operator đã chọn **không** công bố. Xem
  `6.2`.
- **Slug là ngoại lệ ở bề mặt công khai**: một sản phẩm **pre-order** được hiện dù chưa bán,
  nhưng **không mua được**. "Hiện" và "mua được" là hai điều kiện khác nhau.
- **Trạng thái bán đi qua endpoint riêng** `POST .../state`; `PATCH` **không** nhận `sellState`.
  Một lần chuyển không hợp lệ trả `409`, không phải `400`.
- Module **không** có hạn mức riêng, chỉ chịu hạn mức toàn cục (§1.5).

Hai danh sách đều phân trang theo quy ước chung: `page` mặc định `1`, `pageSize` mặc định `20`,
khoảng `1..100`. Thứ tự ổn định: `position`, rồi `createdAt`, rồi `id`.

**Hình dạng công khai** `PublicProduct` (6 member) — một phần tử của danh sách:

```json
{
  "id": "0f5c6e0c-1a44-4a1e-9b3d-9a1b2c3d4e5f",
  "name": "Acrylic stand Aki",
  "slug": "acrylic-stand-aki",
  "price": { "amount": 120000, "currency": "VND" },
  "imageUrl": "https://res.cloudinary.com/demo/image/upload/stand.jpg",
  "isPreorder": false
}
```

**Hình dạng quản trị** `AdminProduct` (15 member) — một phần tử của danh sách quản trị:

```json
{
  "id": "0f5c6e0c-1a44-4a1e-9b3d-9a1b2c3d4e5f",
  "name": "Acrylic stand Aki",
  "slug": "acrylic-stand-aki",
  "description": "Acrylic stand 15cm",
  "price": { "amount": 120000, "currency": "VND" },
  "categoryId": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e",
  "position": 10,
  "sellState": "COMING_SOON",
  "isSet": false,
  "isPreorder": false,
  "preorderExpectedAt": null,
  "imageCount": 1,
  "imageUrl": "https://res.cloudinary.com/demo/image/upload/stand.jpg",
  "createdAt": "2026-10-07T08:15:04Z",
  "updatedAt": "2026-10-07T08:15:04Z"
}
```

### 6.1 `GET /products`

Danh sách sản phẩm **khách thấy được**: đang bán (`ACTIVE`) **hoặc** là pre-order đang thông
báo, và **danh mục chưa bị ẩn** (FR-002). Lọc tuỳ chọn theo danh mục.

| | |
|---|---|
| Auth | Không |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` |

Query: `page` (mặc định `1`, tối thiểu `1`), `pageSize` (mặc định `20`, khoảng `1..100`),
`category` (slug danh mục, tuỳ chọn).

Response `200`: `data` là mảng `PublicProduct`; `meta` là khối phân trang chung.

Lỗi: `VALIDATION_ERROR` 400 (`page` / `pageSize` sai định dạng hoặc ngoài khoảng;
`details[].field` là `"page"` hoặc `"pageSize"`) · `RATE_LIMITED` 429

Ghi chú:

- **Ẩn danh mục ẩn luôn sản phẩm bên trong** — nhưng **không** sửa gì trên dòng sản phẩm; bỏ ẩn
  danh mục đưa sản phẩm trở lại y như cũ.
- Lọc `?category=<slug>`: danh mục **ẩn**, **không tồn tại**, hoặc **không có sản phẩm hiển thị**
  đều trả `data: []` — **không** `404` — nên bộ lọc không tiết lộ danh mục ẩn (FR-006).
- `meta.total` đếm đúng số sản phẩm hiển thị; thiếu `total` nghĩa là `0` (§1.3).
- Catalogue rỗng trả `data: []`, **không** phải `null` và **không** phải lỗi (FR-007).

### 6.2 `GET /products/{slug}`

Đọc một sản phẩm khách thấy được theo slug, kèm mô tả và **toàn bộ** ảnh theo thứ tự.

| | |
|---|---|
| Auth | Không |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` |

Response `200` là `data` = `PublicProductDetail` (8 member = `PublicProduct` + `description` +
`images`; mỗi phần tử `images` là `{ id, url, width, height }`).

Lỗi: `PRODUCT_NOT_FOUND` 404 · `RATE_LIMITED` 429

> **Sản phẩm bị ẩn, hết hàng, đã ngừng bán, nằm trong danh mục bị ẩn, đã xoá và slug chưa từng
> tồn tại trả lời y hệt nhau** — cùng status, cùng mã `PRODUCT_NOT_FOUND`, cùng câu chữ. Đây là
> bắt buộc, không chỉ tiện (FR-003): nếu một sản phẩm bị ẩn trả lời khác một slug chưa dùng,
> endpoint sẽ xác nhận sản phẩm/danh mục operator đã chọn **không** công bố. Một **pre-order**
> thì được phục vụ (`200`) và mang `isPreorder: true`.
>
> Hình dạng công khai **không** liệt kê nội dung của một combo set (FR-005, research D18); chỉ
> hình dạng quản trị (`6.5`) mới có `members`.

### 6.3 `GET /admin/products`

Danh sách **toàn bộ** sản phẩm, kể cả sản phẩm không hiển thị.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` |

Query: như `6.1` (không có `category`).

Response `200`: `data` là mảng `AdminProduct`; `meta` là khối phân trang chung.

Lỗi: `VALIDATION_ERROR` 400 (`page` / `pageSize`) · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403
(ghi `audit_logs` với action `AUTH_PRIVILEGE_DENIED`) · `RATE_LIMITED` 429

### 6.4 `POST /admin/products`

Tạo sản phẩm. Sản phẩm **luôn được tạo ở `COMING_SOON`** (FR-010). Tác nhân lấy từ **session**,
không bao giờ từ body.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `201 Created` |

Request (`CreateProductRequest`, `additionalProperties: false`)

```json
{
  "name": "Acrylic stand Aki",
  "slug": "acrylic-stand-aki",
  "description": "Acrylic stand 15cm",
  "price": { "amount": 120000, "currency": "VND" },
  "categoryId": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e",
  "position": 10,
  "isSet": false,
  "memberProductIds": [],
  "isPreorder": false,
  "preorderExpectedAt": null
}
```

`name`, `slug`, `price`, `categoryId`, `position` **bắt buộc**; `description`, `position` có
mặc định (`position` mặc định `0`, có thể âm); `memberProductIds` chỉ được ghi khi `isSet` là
`true`; `preorderExpectedAt` (`YYYY-MM-DD`) chỉ có nghĩa khi `isPreorder` là `true`. `slug` do
operator viết, **không** sinh từ `name`.

Response `201`: `data` = `AdminProductDetail` (17 member = `AdminProduct` + `images` +
`members`).

Lỗi: `VALIDATION_ERROR` 400 (`name` rỗng/quá 120 ký tự; `slug` rỗng, quá 140 ký tự hoặc không
khớp mẫu URL-safe; `description` quá 5000 ký tự; `price.amount` không dương; `price.currency`
không phải ba chữ in hoa; `categoryId`/`memberProductIds` không tồn tại — `details[].field` chỉ
đúng member) · `MALFORMED_REQUEST` 400 (body không parse được hoặc có member lạ) ·
`UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `PRODUCT_SLUG_TAKEN` 409 · `RATE_LIMITED` 429

Ghi chú:

- Sản phẩm mang `isPreorder: true` **hiện ngay** với khách; sản phẩm thường vẫn ẩn cho tới khi
  lên `ACTIVE` (FR-002, FR-040).
- Combo set là **một sản phẩm độc lập** có giá riêng do operator đặt; giá này **không** suy ra
  từ các thành viên (FR-039, research D10).
- Mỗi lần tạo ghi `audit_logs` với action `PRODUCT_CREATED`, metadata `{"slug": ...}`.

### 6.5 `GET /admin/products/{id}`

Đọc một sản phẩm theo định danh, kể cả sản phẩm không hiển thị, kèm ảnh và (với set) thành viên.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` |

Response `200`: `data` = `AdminProductDetail`; `members` chỉ có mặt khi sản phẩm là set, mỗi
phần tử là `{ id, name, slug }`.

Lỗi: `VALIDATION_ERROR` 400 (`id` không phải UUID) · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 ·
`PRODUCT_NOT_FOUND` 404 · `RATE_LIMITED` 429

### 6.6 `PATCH /admin/products/{id}`

Sửa một phần: member bỏ trống giữ nguyên giá trị hiện tại. **Không bao giờ đổi `sellState`**
(research D11).

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` — trả sản phẩm sau khi sửa |

Request (`UpdateProductRequest`, mọi member tuỳ chọn, `additionalProperties: false`)

```json
{
  "name": "Acrylic stand Aki mới",
  "slug": "acrylic-stand-aki-moi",
  "description": "Mô tả mới",
  "price": { "amount": 130000, "currency": "VND" },
  "categoryId": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e",
  "position": 5,
  "isSet": true,
  "memberProductIds": ["<uuid>", "<uuid>"],
  "isPreorder": true,
  "preorderExpectedAt": "2026-12-01"
}
```

Gửi `memberProductIds` **thay toàn bộ** danh sách thành viên (chỉ ghi khi là set); gửi `[]` xoá
hết; đổi `isSet` từ `true` sang `false` cũng xoá thành viên. `isPreorder: false` xoá luôn
`preorderExpectedAt` (FR-040).

Response `200`: `data` = `AdminProductDetail`.

Lỗi: `VALIDATION_ERROR` 400 · `MALFORMED_REQUEST` 400 · `UNAUTHENTICATED` 401 · `FORBIDDEN`
403 · `PRODUCT_NOT_FOUND` 404 · `PRODUCT_SLUG_TAKEN` 409 · `RATE_LIMITED` 429

Ghi chú:

- Gửi lại **đúng slug sản phẩm đang giữ** thành công — một sản phẩm không bao giờ trùng với
  chính nó.
- Ghi `audit_logs` với action `PRODUCT_UPDATED`, metadata `{"slug": ...}`.

### 6.7 `DELETE /admin/products/{id}`

Xoá sản phẩm. Đây là **hard delete** (research D13): dòng biến mất, dòng audit ở lại.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `204 No Content` — **không có body** |

Request: không có body.

Lỗi: `VALIDATION_ERROR` 400 (`id` không phải UUID) · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 ·
`PRODUCT_NOT_FOUND` 404 · `RATE_LIMITED` 429

Ghi chú:

- Ảnh của sản phẩm được giải phóng và các dòng membership bị xoá theo (cascade);
  **thành viên của một set bị xoá không bị đụng** (FR-013, US5).
- Xoá lại sản phẩm đã xoá trả cùng `404 PRODUCT_NOT_FOUND`, không phải lỗi.
- Ghi `audit_logs` với action `PRODUCT_DELETED`.
- **Lệch có chủ ý so với module doc**: `docs/modules/04-product.md` ghi "xóa mềm" trong phạm vi
  MVP, nhưng feature này chọn hard delete vì hiện **không có gì đọc** một sản phẩm đã xoá (chưa
  có đơn hàng). Nghĩa vụ còn lại thuộc module 07 Order, ghi ở
  `specs/006-product-catalog/deferred.md` (D3) và
  [decisions/013](decisions/013-product-visibility-media-and-hard-delete.md).

### 6.8 `POST /admin/products/{id}/state`

Chuyển trạng thái bán. Đây là một **transition**, không phải một field: nó có thể bị từ chối và
câu từ chối nêu trạng thái hiện tại (Constitution III, research D11).

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` — trả sản phẩm ở trạng thái mới |

Request (`ChangeStateRequest`, `additionalProperties: false`)

```json
{ "to": "ACTIVE" }
```

Các cạnh **hợp lệ duy nhất** (FR-022): `COMING_SOON → ACTIVE`; `ACTIVE ↔ OUT_OF_STOCK` (hai
chiều, để hồi kho đưa sản phẩm trở lại bán); mọi trạng thái → `DISCONTINUED`. `DISCONTINUED` là
**cuối** và không có đường ra (FR-026). Lên `ACTIVE` **xoá nhãn pre-order** và ngày dự kiến.

Response `200`: `data` = `AdminProductDetail`.

Lỗi: `VALIDATION_ERROR` 400 (`to` không thuộc bốn trạng thái; `details[].field` là `"to"`) ·
`MALFORMED_REQUEST` 400 · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `PRODUCT_NOT_FOUND` 404 ·
`PRODUCT_STATE_TRANSITION_INVALID` 409 (message nêu trạng thái hiện tại) · `RATE_LIMITED` 429

Ghi chú:

- Một lần chuyển bị từ chối **không đổi gì**: sản phẩm ở nguyên trạng thái cũ (FR-024).
- Ghi `audit_logs` với action `PRODUCT_STATE_CHANGED`, metadata `{"from": ..., "to": ...}`.

### 6.9 `POST /admin/products/{id}/images`

Gắn một ảnh vào sản phẩm. Body là `multipart/form-data` với đúng một part tên `image`.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `201 Created` — trả sản phẩm với ảnh mới |

Request

```bash
curl -X POST http://localhost:8080/api/v1/admin/products/$ID/images \
  -H "Authorization: Bearer $ACCESS" \
  -F "image=@stand.jpg"
```

Response `201`: `data` = `AdminProductDetail`; ảnh mới nằm trong `images` (mỗi phần tử
`{ id, publicId, url, width, height, position, isPrimary }`).

Lỗi: `VALIDATION_ERROR` 400 (thiếu part `image`, hoặc body không phải `multipart/form-data`;
`details[].field` là `"image"`) · `MALFORMED_REQUEST` 400 · `PRODUCT_IMAGE_TYPE_UNSUPPORTED` 400
(byte không phải JPEG/PNG/WebP, theo chữ ký nội dung) · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403
· `PRODUCT_NOT_FOUND` 404 · `PRODUCT_IMAGE_LIMIT_REACHED` 409 · `PRODUCT_IMAGE_TOO_LARGE` 413 ·
`RATE_LIMITED` 429 · `PRODUCT_MEDIA_UNAVAILABLE` 503

Ghi chú:

- Loại ảnh nhận diện từ **byte tải lên** (chữ ký định dạng), không tin tên file cũng không tin
  `Content-Type` client khai; chỉ JPEG, PNG và WebP (FR-019).
- Trần ảnh là **2 MB**, cùng ngưỡng avatar; route đặt trần riêng cho phần đệm `multipart` và
  handler chặn lại khi đọc, nên mọi cách gửi đều trả cùng `413 PRODUCT_IMAGE_TOO_LARGE`
  (FR-019).
- **Ảnh đầu tiên trở thành ảnh chính** (`isPrimary: true`); sản phẩm tối đa **10** ảnh, ảnh thứ
  11 bị từ chối **trước khi** byte nào lên provider, nên không để lại tài nguyên mồ côi
  (FR-020).
- Ảnh được thu nhỏ ở provider (mặc định bề rộng tối đa **1600 px**); provider là nguồn sự thật
  cho kích thước (research D16).
- Thiếu cấu hình `MEDIA_*` chỉ tắt thao tác ảnh: mọi lệnh gọi provider trả `503
  PRODUCT_MEDIA_UNAVAILABLE`, sản phẩm **không** bị đổi nên thử lại được (FR-018).
- Ghi `audit_logs` với action `PRODUCT_IMAGE_ADDED`, metadata `{"imageId": ...}`.

### 6.10 `DELETE /admin/products/{id}/images/{imageId}`

Gỡ một ảnh khỏi sản phẩm và giải phóng tài nguyên đã lưu (FR-021).

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `204 No Content` — **không có body** |

Request: không có body.

Lỗi: `VALIDATION_ERROR` 400 (`id` / `imageId` không phải UUID) · `UNAUTHENTICATED` 401 ·
`FORBIDDEN` 403 · `PRODUCT_NOT_FOUND` 404 · `RATE_LIMITED` 429

Ghi chú:

- Gỡ **ảnh chính** sẽ thăng cấp ảnh kế theo `position`, nên sản phẩm còn ảnh luôn có đúng một
  ảnh chính (FR-021, research D7).
- Một ảnh không thuộc sản phẩm (hoặc sản phẩm không tồn tại) trả **cùng** `404
  PRODUCT_NOT_FOUND`, nên không dò được ảnh của sản phẩm khác.
- Tài nguyên được giải phóng **sau** khi dòng đã mất: hỏng bước giải phóng chỉ để lại tài
  nguyên mồ côi, không làm sản phẩm trỏ vào thứ không còn (research D14).
- Ghi `audit_logs` với action `PRODUCT_IMAGE_REMOVED`, metadata `{"imageId": ...}`.

### 6.11 `POST /admin/products/{id}/images/{imageId}/primary`

Đặt một ảnh làm ảnh chính của sản phẩm (FR-017).

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` — trả sản phẩm với ảnh được chọn làm chính |

Request: không có body.

Response `200`: `data` = `AdminProductDetail`.

Lỗi: `VALIDATION_ERROR` 400 (`id` / `imageId` không phải UUID) · `UNAUTHENTICATED` 401 ·
`FORBIDDEN` 403 · `PRODUCT_NOT_FOUND` 404 (sản phẩm hoặc ảnh không thuộc sản phẩm) ·
`RATE_LIMITED` 429

Ghi chú:

- Thao tác chạy trong một transaction khoá dòng sản phẩm, nên hai lần thăng cấp đồng thời không
  thể để lại hai ảnh chính; partial unique index trên `(product_id) WHERE is_primary` là bảo
  đảm ở tầng lưu trữ (research D7).
- Ghi `audit_logs` với action `PRODUCT_IMAGE_PRIMARY_SET`, metadata `{"imageId": ...}`.

---

## 7. Module 05 — Inventory (`/api/v1`)

Tồn kho của một sản phẩm: số lượng vật lý trên kệ, phần đang được giữ cho các đơn đang thanh toán,
và phần còn lại khả dụng cho khách. **Năm endpoint, tất cả của quản trị viên** dưới
`/admin/inventory`, địa chỉ theo **định danh sản phẩm**. Không có bề mặt tồn kho cho khách: khách
biết còn hàng hay không qua **trạng thái bán** của sản phẩm, mà module này tự chuyển khi khả dụng
cắt qua 0 (xem ghi chú cuối mục và §6.8).

Bốn điều dễ đọc sai, nói ngay:

- **Sản phẩm chưa từng nhập kho trả `0`, không phải `404`.** Một sản phẩm do module 04 tạo ra chưa
  có dòng tồn kho nào, và đọc nó là trạng thái bình thường. Chỉ **định danh không trỏ tới sản phẩm
  nào** mới trả `404 PRODUCT_NOT_FOUND`. Xem `7.1`.
- **Số lượng là số nguyên, không bao giờ âm.** Một thao tác làm số lượng vật lý hoặc khả dụng
  xuống dưới 0 bị từ chối `409 INVENTORY_INSUFFICIENT_STOCK`; một thao tác đưa đúng về 0 **thành
  công**.
- **Điều chỉnh ghi lại phần chênh lệch**, không phải giá trị tuyệt đối. Điều chỉnh về đúng giá trị
  đang lưu **không đổi gì** và vẫn trả `200`.
- **Giữ chỗ không có bề mặt HTTP.** `reserve`/`release`/hết hạn là năng lực nội bộ của luồng thanh
  toán; module 07/08 chưa tồn tại nên không có endpoint nào. Khả dụng = vật lý − giữ chỗ đang hoạt
  động, được tính tại thời điểm hỏi, không lưu sẵn.
- Module **không** có hạn mức riêng, chỉ chịu hạn mức toàn cục (§1.5).

Lịch sử biến động phân trang theo quy ước chung: `page` mặc định `1`, `pageSize` mặc định `20`,
khoảng `1..100`.

### 7.1 `GET /admin/inventory/{productId}`

Đọc số lượng vật lý, số đang giữ và số khả dụng của một sản phẩm.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` |

Response `200`

```json
{
  "data": {
    "physicalQuantity": 10,
    "heldQuantity": 2,
    "availableQuantity": 8
  },
  "meta": { "requestId": "...", "timestamp": "..." }
}
```

`StockView` có **3 member**: `physicalQuantity` (trên kệ), `heldQuantity` (đang giữ cho đơn đang
thanh toán), `availableQuantity` (khác biệt — thứ khách có thể lấy). Cả ba là số nguyên không âm.
Định danh sản phẩm nằm ở đường dẫn, **không** lặp lại trong body.

Lỗi: `VALIDATION_ERROR` 400 (`productId` không phải UUID) · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403
· `PRODUCT_NOT_FOUND` 404 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500

Ghi chú:

- Sản phẩm **chưa từng nhập kho** trả `physicalQuantity: 0`, `heldQuantity: 0`,
  `availableQuantity: 0`.
- Số đang giữ là tổng các giữ chỗ **còn hiệu lực**; giữ chỗ đã hết hạn nhưng chưa được quét không
  còn được tính.

### 7.2 `GET /admin/inventory/{productId}/movements`

Đọc lịch sử biến động kho của một sản phẩm, **cũ nhất trước**.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` |

Query: `page` (mặc định `1`, tối thiểu `1`), `pageSize` (mặc định `20`, khoảng `1..100`).

Response `200`: `data` là mảng `StockMovement`; `meta` là khối phân trang chung.

```json
{
  "data": [
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
  ],
  "meta": { "requestId": "...", "timestamp": "...", "page": 1, "pageSize": 20, "total": 1 }
}
```

`StockMovement` có **9 member**: `id`, `productId`, `kind`, `delta` (có dấu), `resultingQuantity`
(số lượng vật lý sau thay đổi), `sourceReference` (`null` với thao tác thủ công), `actorId` (`null`
với thay đổi do hệ thống), `note` (`null` khi không có), `createdAt`. `kind` là một trong **4** giá
trị `RESTOCK`, `DAMAGE`, `ADJUSTMENT`, `SALE`.

Lỗi: `VALIDATION_ERROR` 400 (`productId` không phải UUID, hoặc `page`/`pageSize` ngoài khoảng;
`details[].field` chỉ đúng member) · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `PRODUCT_NOT_FOUND`
404 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500

Ghi chú:

- Sản phẩm **không có biến động** trả `data: []`, không phải lỗi.
- `sourceReference` và `actorId` loại trừ nhau: một thao tác thủ công mang `actorId`, một sự kiện
  ngoài mang `sourceReference`.

### 7.3 `POST /admin/inventory/{productId}/restock`

Ghi nhận hàng về. Tăng số lượng vật lý thêm một lượng **dương**.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` — tồn kho sau thay đổi |

Request (`QuantityRequest`, `additionalProperties: false`)

```json
{ "quantity": 10, "note": "hàng về kho" }
```

`quantity` bắt buộc, là số nguyên **≥ 1**; `note` tuỳ chọn. Tác nhân lấy từ **session**, không bao
giờ từ body.

Response `200`: `data` là `StockView` (như `7.1`).

Lỗi: `VALIDATION_ERROR` 400 (`quantity` thiếu, không nguyên, âm hoặc bằng 0; `details[].field` là
`"quantity"`) · `MALFORMED_REQUEST` 400 (body không parse được hoặc có member lạ) ·
`UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `PRODUCT_NOT_FOUND` 404 · `RATE_LIMITED` 429 ·
`INTERNAL_ERROR` 500

Ghi chú:

- Mỗi lần restock ghi một dòng ledger `kind: "RESTOCK"` (mang tác nhân và ghi chú) và một dòng
  audit `INVENTORY_RESTOCKED` (mang tác nhân, `kind` và `delta`, **không** mang ghi chú).

### 7.4 `POST /admin/inventory/{productId}/damage`

Ghi nhận hàng hư hỏng. Giảm số lượng vật lý đi một lượng **dương**.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` — tồn kho sau thay đổi |

Request (`QuantityRequest`, `additionalProperties: false`) như `7.3`.

Response `200`: `data` là `StockView`.

Lỗi: `VALIDATION_ERROR` 400 (`quantity` thiếu, không nguyên, âm hoặc bằng 0) ·
`MALFORMED_REQUEST` 400 · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `PRODUCT_NOT_FOUND` 404 ·
`INVENTORY_INSUFFICIENT_STOCK` 409 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500

`409 INVENTORY_INSUFFICIENT_STOCK` xảy ra khi lượng giảm vượt quá số đang có trên kệ, **hoặc** khi
nó để lại trên kệ ít hơn phần đang được giữ cho đơn đang thanh toán — một giữ chỗ không bao giờ bị
phá bởi thay đổi vật lý. Bị từ chối thì **không có gì đổi** và **không** có dòng ledger mới. Ghi
`kind: "DAMAGE"` và audit `INVENTORY_DAMAGED`.

### 7.5 `POST /admin/inventory/{productId}/adjustment`

Điều chỉnh số lượng vật lý về **giá trị đã đếm**, ghi lại **phần chênh lệch có dấu**.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` — tồn kho sau điều chỉnh |

Request (`AdjustmentRequest`, `additionalProperties: false`)

```json
{ "quantity": 7, "note": "kiểm kê" }
```

`quantity` bắt buộc, là số nguyên **≥ 0** (0 là giá trị đếm hợp lệ); `note` tuỳ chọn.

Response `200`: `data` là `StockView`.

Lỗi: `VALIDATION_ERROR` 400 (`quantity` thiếu, không nguyên hoặc âm) · `MALFORMED_REQUEST` 400 ·
`UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `PRODUCT_NOT_FOUND` 404 ·
`INVENTORY_INSUFFICIENT_STOCK` 409 (giá trị đếm thấp hơn phần đang được giữ) · `RATE_LIMITED` 429 ·
`INTERNAL_ERROR` 500

Ghi chú:

- Điều chỉnh về **đúng** giá trị đang lưu trả `200`, **không** ghi dòng ledger nào (không có gì
  đổi), nhưng **vẫn** ghi một dòng audit `INVENTORY_ADJUSTED` cho chính thao tác.
- Khi có chênh lệch, ledger ghi `kind: "ADJUSTMENT"` với `delta` có dấu bằng `đã đếm − đang lưu` và
  `resultingQuantity` bằng giá trị đã đếm.
- Ledger **không bao giờ** chứa `delta: 0`: một thay đổi không đổi gì thì không phải một thay đổi.

### Quan hệ với trạng thái bán của sản phẩm (module 04)

Khi khả dụng của một sản phẩm **cắt qua 0** trong cùng transaction với thay đổi, module này báo
module 04 đổi `sellState`: về 0 → `OUT_OF_STOCK`, từ 0 lên dương → `ACTIVE`. Một thay đổi **không**
cắt qua 0 **không** đụng tới trạng thái bán, nên nhãn `OUT_OF_STOCK` do operator đặt tay được giữ
nguyên. Sản phẩm `COMING_SOON` và `DISCONTINUED` không bao giờ bị thay đổi bởi tồn kho. Đây là
nghĩa vụ D1 mà feature 006 để lại cho module 05; hai cạnh hợp lệ nằm ở §6.8.

---

## 8. Bảng tổng hợp

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
| GET | `/api/v1/users/me` | Bearer | Hồ sơ của chính mình |
| PATCH | `/api/v1/users/me` | Bearer | Cập nhật tên hiển thị / số điện thoại |
| POST | `/api/v1/users/me/avatar` | Bearer | Tải hoặc thay avatar (multipart, trần 2 MB) |
| DELETE | `/api/v1/users/me/avatar` | Bearer | Gỡ avatar |
| GET | `/api/v1/users/me/addresses` | Bearer | Địa chỉ của chính mình (có phân trang) |
| POST | `/api/v1/users/me/addresses` | Bearer | Tạo địa chỉ (201) |
| PATCH | `/api/v1/users/me/addresses/{addressId}` | Bearer | Sửa địa chỉ, giữ cờ mặc định |
| DELETE | `/api/v1/users/me/addresses/{addressId}` | Bearer | Ẩn địa chỉ (204) |
| POST | `/api/v1/users/me/addresses/{addressId}/default` | Bearer | Đặt địa chỉ mặc định |
| GET | `/api/v1/users/{userId}` | ADMIN | Tra cứu khách hàng (chỉ đọc, có audit) |
| GET | `/api/v1/divisions/provinces` | Bearer | Danh sách tỉnh/thành phố |
| GET | `/api/v1/divisions/provinces/{provinceCode}/wards` | Bearer | Danh sách phường/xã của một tỉnh |
| GET | `/api/v1/categories` | — | Danh mục đang hiển thị (có phân trang) |
| GET | `/api/v1/categories/{slug}` | — | Chi tiết danh mục theo slug |
| GET | `/api/v1/admin/categories` | ADMIN | Toàn bộ danh mục, kể cả ẩn (có phân trang) |
| POST | `/api/v1/admin/categories` | ADMIN | Tạo danh mục (201) |
| GET | `/api/v1/admin/categories/{categoryId}` | ADMIN | Chi tiết danh mục, kể cả ẩn |
| PATCH | `/api/v1/admin/categories/{categoryId}` | ADMIN | Sửa một phần danh mục |
| DELETE | `/api/v1/admin/categories/{categoryId}` | ADMIN | Xoá danh mục (204; `409 CATEGORY_IN_USE` nếu còn sản phẩm — feature 006) |
| GET | `/api/v1/products` | — | Sản phẩm khách thấy được (có phân trang, lọc `?category=`) |
| GET | `/api/v1/products/{slug}` | — | Chi tiết sản phẩm theo slug |
| GET | `/api/v1/admin/products` | ADMIN | Toàn bộ sản phẩm, kể cả không hiển thị (có phân trang) |
| POST | `/api/v1/admin/products` | ADMIN | Tạo sản phẩm, luôn ở `COMING_SOON` (201) |
| GET | `/api/v1/admin/products/{id}` | ADMIN | Chi tiết sản phẩm, kể cả không hiển thị |
| PATCH | `/api/v1/admin/products/{id}` | ADMIN | Sửa một phần sản phẩm (không đổi `sellState`) |
| DELETE | `/api/v1/admin/products/{id}` | ADMIN | Xoá cứng sản phẩm (204) |
| POST | `/api/v1/admin/products/{id}/state` | ADMIN | Chuyển trạng thái bán |
| POST | `/api/v1/admin/products/{id}/images` | ADMIN | Gắn ảnh (multipart, ≤ 2 MB, ≤ 10 ảnh) (201) |
| DELETE | `/api/v1/admin/products/{id}/images/{imageId}` | ADMIN | Gỡ ảnh (204) |
| POST | `/api/v1/admin/products/{id}/images/{imageId}/primary` | ADMIN | Đặt ảnh chính |
| GET | `/api/v1/admin/inventory/{productId}` | ADMIN | Tồn kho một sản phẩm (vật lý, đang giữ, khả dụng) |
| GET | `/api/v1/admin/inventory/{productId}/movements` | ADMIN | Lịch sử biến động kho (có phân trang, cũ nhất trước) |
| POST | `/api/v1/admin/inventory/{productId}/restock` | ADMIN | Nhập thêm hàng |
| POST | `/api/v1/admin/inventory/{productId}/damage` | ADMIN | Ghi nhận hư hỏng (`409 INVENTORY_INSUFFICIENT_STOCK` nếu vượt kệ/giữ chỗ) |
| POST | `/api/v1/admin/inventory/{productId}/adjustment` | ADMIN | Điều chỉnh về giá trị đã đếm |

---

## 9. Quy tắc cập nhật

Khi thêm endpoint mới (module mới hoặc tính năng mới trong module cũ), thay đổi
`plan.md`, hoặc sửa/xoá endpoint, **phải** làm trong cùng một thay đổi:

1. Thêm mục cho endpoint vào mục module tương ứng, theo đúng 6 phần mà các mục hiện
   có dùng: bảng thông tin · Request · Response · Lỗi · ghi chú.
2. Cập nhật bảng tổng hợp ở mục 8.
3. Thêm dòng vào Change log ở mục 10.
4. Nếu là endpoint mới: thêm `openapi.yaml` trong `specs/<feature>/contracts/` cho
   khớp, hoặc ghi rõ trong change log rằng chưa có OpenAPI và lý do.
5. Nếu phát sinh error code mới: thêm vào bảng ở mục 1.4 (và vào
   `specs/<feature>/contracts/<module>-error-codes.md` của feature đó).
6. Chạy `make swagger` để sinh lại `docs/swagger/` (annotation của handler phải khớp
   mục vừa thêm). CI chạy `make swagger-check` nên quên bước này là build đỏ.

## 10. Change log

| Ngày | Thay đổi | Nguồn |
|---|---|---|
| 2026-10-09 | Thêm nhóm `/api/v1/admin/inventory` (module 05 Inventory): **năm** route quản trị dưới `/admin/inventory/{productId}` — đọc tồn kho, đọc lịch sử biến động (phân trang), và ba thao tác thủ công `restock`/`damage`/`adjustment` — tất cả yêu cầu vai trò `ADMIN`. Hai hình dạng response: `StockView` **3 member** (`physicalQuantity`, `heldQuantity`, `availableQuantity`) và `StockMovement` **9 member**. Bổ sung mã `INVENTORY_INSUFFICIENT_STOCK` 409 vào mục 1.4 (kèm ghi rõ `PRODUCT_NOT_FOUND` 404 của module 04 được **tái sử dụng** trên cả năm route) và ghi chú module không có hạn mức riêng ở mục 1.5. Nêu rõ: sản phẩm chưa từng nhập kho trả `0` chứ không `404`; điều chỉnh ghi phần chênh lệch và một lần điều chỉnh về đúng giá trị đang lưu không ghi ledger; trạng thái bán của sản phẩm tự chuyển khi khả dụng cắt qua 0 (đóng nghĩa vụ D1 của feature 006). Giữ chỗ có hạn không có bề mặt HTTP. Phần 7 được chèn, bảng tổng hợp/quy tắc/change log dời xuống mục 8/9/10. Xem ADR-014. | `internal/modules/inventory/presentation/http/router.go` |
| 2026-10-08 | Thêm nhóm `/api/v1/products` (module 04 Product): hai route công khai không cần token (`GET /products` có lọc `?category=<slug>`, `GET /products/{slug}`) và chín route quản trị dưới `/admin/products` (danh sách, tạo, đọc, sửa, xoá, đổi trạng thái, thêm/gỡ/đặt ảnh chính), tất cả yêu cầu vai trò `ADMIN`. Hai hình dạng response: công khai 6 member (danh sách) / 8 member (chi tiết, thêm `description` + `images`); quản trị 15 member / 17 member (thêm `images`, `members`). Bổ sung bảy mã `PRODUCT_*` vào mục 1.4 và ghi chú module không có hạn mức riêng ở mục 1.5. Nêu rõ: sản phẩm bị ẩn (kể cả do danh mục bị ẩn) và slug chưa từng tồn tại trả lời y hệt nhau (`404 PRODUCT_NOT_FOUND`); trạng thái bán đi qua endpoint riêng; giá là số nguyên + currency; `preorderExpectedAt` ghi theo `format: date`; xoá cứng. **Đổi một câu trả lời của module 03**: `DELETE /admin/categories/{categoryId}` nay trả `409 CATEGORY_IN_USE` khi còn sản phẩm (tham chiếu `ON DELETE RESTRICT` do feature này thêm), và `CATEGORY_IN_USE` được bổ sung vào bảng mã module category. Phần 6 được chèn, bảng tổng hợp/quy tắc/change log dời xuống mục 7/8/9. Xem ADR-013. | `internal/modules/product/presentation/http/router.go`, `internal/modules/category/presentation/http/errors.go` |
| 2026-10-07 | Thêm `GET /swagger/*` (Swagger UI, gate bởi `SWAGGER_ENABLED`, mặc định tắt) và `make swagger`/`make swagger-check`. Spec sinh từ annotation trong code vào `docs/swagger/`; file này vẫn là nguồn authoritative. Xem ADR-011. | `cmd/api/main.go`, `internal/share/httpserver/routes.go`, `Makefile` |
| 2026-10-07 | Thêm nhóm `/api/v1/categories` (module 03 Category): hai route công khai không cần token (`GET /categories`, `GET /categories/{slug}`) và năm route quản trị dưới `/admin/categories` (danh sách, tạo, đọc, sửa, xoá), tất cả yêu cầu vai trò `ADMIN`. Hình dạng công khai chỉ có bốn member, hình dạng quản trị có thêm `position`, `isVisible`, `createdAt`, `updatedAt`. Bổ sung ba mã `CATEGORY_*` vào mục 1.4 và ghi chú module không có hạn mức riêng ở mục 1.5. Nêu rõ: danh mục bị ẩn và slug chưa từng tồn tại trả lời y hệt nhau; va chạm tên/slug trả `409` kèm field. Phần 5 được chèn và bảng tổng hợp/change log dời xuống mục 6/8. | `internal/modules/category/presentation/http/router.go` |
| 2026-10-06 | `POST /users/me/avatar`: mọi cách gửi ảnh quá trần (khai `Content-Length`, không khai, khai thiếu) đều trả `413 USER_AVATAR_TOO_LARGE`; gỡ `PAYLOAD_TOO_LARGE` khỏi danh sách lỗi của endpoint này vì nó không còn là câu trả lời nào ở đây nữa. Cùng thay đổi đó sửa hàng đầu mục `1.1` (trước đó gộp trần toàn cục và trần riêng của route vào cùng một mã) và bổ sung phần trần 2 162 688 byte trong ghi chú của `4.3`. `PAYLOAD_TOO_LARGE` giữ nguyên trên mọi route không phải avatar. | `internal/modules/user/presentation/http/router.go`, `internal/share/middleware/bodylimit.go` |
| 2026-10-06 | `POST /register`: thêm nhánh gửi lại thất bại — `503 SERVICE_UNAVAILABLE` (mã dùng chung của `httpx`, tái sử dụng, **không** thêm mã riêng của module) kèm câu báo cho khách biết không có gì được gửi tới và có thể xin mã mới. Tài khoản `pending` được giữ, cooldown gửi lại được gỡ (nên lời hứa trong câu trả lời là đúng), rate limit nhóm *flow* vẫn áp dụng. Ghi action audit `AUTH_REGISTER_DELIVERY_FAILED` + outcome `FAILURE` với metadata `classification` thuộc hệ thống này. `POST /resend-verification` ghi rõ khoảng trống đã biết: gửi lại hỏng hiện trả `500` chứ không phải `503`. | `internal/modules/auth/application/implement/email_failure.go`, `internal/modules/auth/application/implement/register.go`, `internal/modules/auth/presentation/http/errors.go` |
| 2026-10-06 | Sửa hai giá trị **ví dụ** đã sai trong mục `/divisions`: tên tỉnh `79` là `Hồ Chí Minh` (không phải `Thành phố Hồ Chí Minh`), và mã phường ví dụ `0001` / `Phường Hoàng Kiết` **không tồn tại** trong dataset. Thay bằng `00004` / `Ba Đình`. Mã phường trong dataset là 5 chữ số và tên không kèm tiền tố `Phường`/`Xã`. | `internal/share/administrative/data/vn-divisions.json` |
| 2026-10-06 | Thêm nhóm `/api/v1/users/*` (module 02 User): hồ sơ (`GET`/`PATCH /me`), avatar (`POST`/`DELETE /me/avatar`), sổ địa chỉ (5 route `/me/addresses*`) và tra cứu khách hàng cho admin (`GET /{userId}`, không có tiền tố `/admin`). Chủ tài khoản luôn lấy từ session; địa chỉ của người khác trả cùng `404 USER_ADDRESS_NOT_FOUND`; `divisionNeedsReview` có mặt trên địa chỉ của chính khách và vắng mặt trên tra cứu của admin. Bổ sung `USER_*` vào mục 1.4 và hai hạn mức của module vào mục 1.5. Sửa phiên bản hiến pháp trích ở đầu file: `v1.4.0` → `v1.6.0`. | `internal/modules/user/presentation/http/router.go` |
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
