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
| `AUTH_RESET_INVALID` | 400 | Token đặt lại mật khẩu sai, đã dùng, hết hạn hoặc đã bị một lần reset mới hơn thay thế |
| `AUTH_EMAIL_TAKEN` | 409 | Email đã thuộc một tài khoản — chỉ ở `POST /register` (từ chối sớm) |
| `AUTH_USERNAME_TAKEN` | 409 | Username đã được dùng — ở `POST /register` và `POST /register/verify-otp` |
| `AUTH_REGISTRATION_NOT_FOUND` | 400 | Tham chiếu đăng ký lạ, hết hạn hoặc đã huỷ; dùng chung cho `verify-otp`/`resend-otp`/`cancel` để không tiết lộ sự tồn tại |

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
| `CATEGORY_HAS_PENDING_ORDERS` | 409 | Ẩn danh mục bị từ chối: còn đơn `PENDING` hoặc `PAYMENT_PENDING` giữ một variant của danh mục (feature 016). Thân từ chối nêu từng đơn chặn trong `error.details` — mỗi đơn một phần tử `{"field": "orderId", "issue": "<định danh đơn>"}`; không gì bị đổi |

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
| `PRODUCT_NOT_FOUND` | 404 | Không có sản phẩm nào mang định danh/code đó, hoặc sản phẩm bị ẩn — **không còn variant nào đang bán**, **danh mục của nó bị ẩn**, hoặc đã bị xoá. Trên bề mặt công khai các tình huống này **cố ý** trả lời giống nhau (mục 6) |
| `PRODUCT_CODE_TAKEN` | 409 | Một sản phẩm khác đã dùng mã sản phẩm đó (so khớp sau khi trim và bỏ qua hoa/thường). `code` cũng là đoạn link công khai nên va chạm này bao luôn trường hợp trùng đoạn link |
| `VARIANT_SKU_TAKEN` | 409 | Một variant khác đã dùng SKU đó (so khớp sau khi trim và bỏ qua hoa/thường) |
| `VARIANT_NOT_FOUND` | 404 | Không có variant nào mang định danh đó cho sản phẩm (một `variantId` lạ). Một variant tồn tại nhưng không mua được **không** phải mã này: trên bề mặt công khai nó đơn giản vắng mặt, còn khi thêm vào giỏ nó là từ chối của cart |
| `VARIANT_LAST_REMOVAL` | 409 | Variant là cái cuối cùng của sản phẩm; mỗi sản phẩm phải giữ **ít nhất một** variant |
| `VARIANT_STATE_TRANSITION_INVALID` | 409 | Bước chuyển trạng thái bán không được trạng thái hiện tại của variant cho phép; message nêu trạng thái hiện tại |
| `PRODUCT_HAS_PENDING_ORDERS` | 409 | Tắt cờ hiển thị của sản phẩm (`isVisible: false`) bị từ chối vì còn đơn `PENDING` hoặc `PAYMENT_PENDING` giữ một variant của sản phẩm (feature 016). Thân từ chối nêu từng đơn chặn như mã danh mục trên; không gì bị đổi |
| `VARIANT_HAS_PENDING_ORDERS` | 409 | Chuyển variant khỏi trạng thái đang bán bị từ chối vì còn đơn `PENDING` hoặc `PAYMENT_PENDING` giữ variant đó (feature 016). Thân từ chối nêu từng đơn chặn như trên; không gì bị đổi |
| `PRODUCT_IMAGE_LIMIT_REACHED` | 409 | Sản phẩm đã có đủ 10 ảnh |
| `PRODUCT_IMAGE_TYPE_UNSUPPORTED` | 400 | Byte tải lên không phải JPEG, PNG hoặc WebP (nhận theo chữ ký nội dung) |
| `PRODUCT_IMAGE_TOO_LARGE` | 413 | Ảnh vượt trần 2 MB |
| `PRODUCT_MEDIA_UNAVAILABLE` | 503 | Dịch vụ media từ chối hoặc không lưu được; **không có gì bị đổi** nên thử lại được |

Những tình huống dưới đây cố ý **không** sinh mã riêng của module product (xem
`specs/015-product-sku-model/contracts/error-codes.md`): code không an toàn URL, một trường vượt
độ dài, giá không dương, `currency` không phải ba chữ in hoa, `categoryId` không tồn tại, một
variant thiếu tên/SKU hoặc `to` không thuộc bốn trạng thái bán, `price` của variant không dương
→ `VALIDATION_ERROR` 400 với `error.details[].field`; định danh đường dẫn không phải UUID →
`VALIDATION_ERROR` 400; thiếu hoặc sai phiên → `UNAUTHENTICATED` 401; `CUSTOMER` gọi endpoint
quản trị → `FORBIDDEN` 403 (ghi `AUTH_PRIVILEGE_DENIED`); `page`/`pageSize` ngoài khoảng →
`VALIDATION_ERROR` 400; body không parse được hoặc có member lạ → `MALFORMED_REQUEST` 400.

Giá của variant là **tuỳ chọn**: bỏ trống (hoặc `null`) nghĩa là dùng giá mặc định của sản phẩm,
nên **không** có mã lỗi cho "variant thiếu giá". Và vì tạo sản phẩm **bắt buộc** có ≥1 variant
còn gỡ variant cuối bị từ chối, tình huống "sản phẩm không có variant" không thể phát sinh nên
cũng không có mã riêng.

> Action `audit_logs` của module dùng tiền tố `PRODUCT_` nhưng **không** phải mã lỗi:
> `PRODUCT_CREATED`, `PRODUCT_UPDATED`, `PRODUCT_IMAGE_ADDED`, `PRODUCT_IMAGE_REMOVED`,
> `PRODUCT_IMAGE_PRIMARY_SET`, `PRODUCT_DELETED`, và ba action của variant
> `PRODUCT_VARIANT_ADDED`, `PRODUCT_VARIANT_UPDATED`, `PRODUCT_VARIANT_REMOVED` — xem
> `internal/modules/product/domain/constant/audit.go`. Metadata của mỗi dòng chỉ mang giá trị
> do operator gõ (`code` ở create/update sản phẩm, `imageId` ở thao tác ảnh, `sku` ở thao tác
> variant), **không** mang dữ liệu cá nhân hay giá trị nội bộ nào — sản phẩm **không** còn cột
> khoá chuẩn hoá `normalized_slug` (bị migration `00015` gỡ), và `code` là đoạn link công khai nên
> được trả ra bình thường (`specs/015-product-sku-model/data-model.md`).

**Riêng module inventory** (`internal/modules/inventory/domain/constant/codes.go`):

| Code | HTTP | Nghĩa |
|------|------|-------|
| `INVENTORY_INSUFFICIENT_STOCK` | 409 | Thao tác sẽ đẩy số lượng **vật lý** của **variant** xuống dưới 0, hoặc xuống dưới phần đang được **giữ** cho các đơn đang thanh toán. Không có gì bị đổi |

`INVENTORY_INSUFFICIENT_STOCK` là **conflict** chứ không phải validation: số lượng gửi lên hợp lệ,
chỉ có trạng thái hiện tại của kệ khiến thao tác bất khả thi (không đủ hàng), và bước tiếp theo của
operator khác hẳn — nhập thêm hàng hoặc chờ giữ chỗ được giải quyết, chứ không phải sửa con số họ
gửi.

Những tình huống dưới đây cố ý **không** sinh mã riêng của module inventory (xem
`specs/007-inventory-tracking/contracts/error-codes.md` và
`specs/015-product-sku-model/contracts/error-codes.md`): **variant** không tồn tại → **tái sử dụng**
`VARIANT_NOT_FOUND` 404 (variant là tài nguyên của module 04; một mã thứ hai gần giống sẽ buộc
client rẽ nhánh trên hai mã cho cùng một tình huống — mã này vì vậy xuất hiện trên **cả năm** route
`/admin/inventory/{variantId}`); số lượng thiếu, không nguyên, âm hoặc bằng 0 khi bắt
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

**Riêng module cart** (`internal/modules/cart/domain/constant/codes.go`):

| Code | HTTP | Nghĩa |
|------|------|-------|
| `CART_PRODUCT_NOT_PURCHASABLE` | 409 | **Variant** tồn tại nhưng **không đang bán** — `COMING_SOON`, `OUT_OF_STOCK` hoặc `DISCONTINUED` — nên không thêm được và không giữ được làm dòng. `details[].field = "variantId"`, `issue` nêu trạng thái. Không có gì bị đổi |
| `CART_QUANTITY_EXCEEDS_AVAILABLE` | 409 | Số lượng yêu cầu, hoặc số lượng mà một lần thêm sẽ tạo ra, lớn hơn tồn khả dụng hiện tại của variant. `details[].field = "quantity"`, `issue` nêu tồn khả dụng hiện tại. Không có gì bị đổi |

Hai mã của cart là **conflict** chứ không phải validation: số lượng gửi lên hợp lệ và variant có
thật; thứ khiến yêu cầu bất khả thi là trạng thái của variant/kệ, và bước tiếp theo của khách khác
hẳn — chờ variant mở bán, hoặc giảm số lượng. Điều này cũng giữ "kệ từ chối" tách khỏi "yêu cầu sai".

Những tình huống dưới đây cố ý **không** sinh mã riêng của module cart (xem
`specs/008-cart/contracts/error-codes.md` và `specs/015-product-sku-model/contracts/error-codes.md`):
thêm/đổi/xoá dòng cho một **variant** không tồn tại hoặc đã xoá → **tái sử dụng**
`PRODUCT_NOT_FOUND` 404 của module 04 (giỏ vẫn giữ mã cũ đã lập, chỉ đổi khoá từ sản phẩm sang
variant); một dòng không phải của giỏ đang gọi cũng
trả cùng `PRODUCT_NOT_FOUND` 404, để route không xác nhận nội dung giỏ của khách khác; số lượng
thiếu, bằng 0, âm hoặc không nguyên → `VALIDATION_ERROR` 400 với `error.details[].field`; định danh
đường dẫn không phải UUID → `VALIDATION_ERROR` 400; body không parse được hoặc có member lạ (kể cả
member tên chủ sở hữu) → `MALFORMED_REQUEST` 400; thiếu hoặc sai phiên → `UNAUTHENTICATED` 401; quá
hạn mức → `RATE_LIMITED` 429; lỗi ngoài dự kiến → `INTERNAL_ERROR` 500.

> Module cart **không** ghi `audit_logs`: thao tác giỏ là hành động của chính khách, không phải một
> mutation quản trị, nên không có action `CART_*` (Constitution VI; `specs/008-cart/plan.md`).

**Riêng module order** (`internal/modules/order/domain/constant/codes.go`):

| Code | HTTP | Nghĩa |
|------|------|-------|
| `ORDER_NOT_FOUND` | 404 | Không có đơn nào mang định danh đó, **hoặc** đơn thuộc khách khác. Hai tình huống **cố ý** trả lời giống nhau, để route không xác nhận đơn của khách khác |
| `ORDER_CART_EMPTY` | 409 | Checkout được gọi với giỏ rỗng. Không bao giờ tạo đơn từ hư không |
| `ORDER_ITEM_NOT_PURCHASABLE` | 409 | **Variant** của một dòng không đang bán, hết hàng hoặc đã bị xoá nên không đặt được. `details[].field = "variantId"`, không có gì bị tạo |
| `ORDER_ITEM_PRICE_CHANGED` | 409 | Giá của variant trong một dòng khác giá khách đã thấy lúc thêm vào giỏ. `details[].field = "variantId"`, không có gì bị tạo; khách xem lại giỏ |
| `ORDER_QUANTITY_EXCEEDS_AVAILABLE` | 409 | Một dòng đòi nhiều hơn tồn khả dụng của variant, **hoặc** không giữ được hàng. `details[].field = "variantId"`, `issue` nêu số khả dụng; không có gì bị tạo |
| `ORDER_NO_ADDRESS` | 409 | Khách không có địa chỉ giao, nên đơn không có nơi đi. Không có gì bị tạo |
| `ORDER_STATE_TRANSITION_INVALID` | 409 | Bước chuyển không được trạng thái hiện tại của đơn cho phép — huỷ đơn đã trả tiền, giao đơn chưa trả tiền. Message nêu trạng thái hiện tại |
| `ORDER_NOT_TRANSFERABLE` | 409 | Đơn chưa trả tiền nên không chuyển nhượng được. Chuyển nhượng thay cho huỷ đơn đã trả tiền, chỉ áp dụng cho đơn đã trả tiền |
| `ORDER_TRANSFER_TARGET_NOT_FOUND` | 404 | Không có tài khoản nào mang email mà lệnh chuyển nhượng nêu. Không có gì bị đổi |
| `ORDER_NOT_EDITABLE` | 409 | Đơn không ở trạng thái khách được sửa — đã trả tiền hoặc xa hơn. Chỉ sửa được khi đơn **chờ artist xác nhận** hoặc **chờ thanh toán**. Không có gì bị đổi |
| `ORDER_EMPTY` | 409 | Một lần sửa để đơn **còn 0 dòng**. Một đơn luôn có **≥1 dòng**; khách muốn bỏ hết thì **huỷ đơn**. Không có gì bị đổi |

Mười một mã trên đều là **conflict** (409) hoặc **not-found** (404) chứ không phải validation: request
hợp lệ nhưng trạng thái hiện tại của giỏ/đơn/kệ khiến nó bất khả thi, và bước tiếp theo của khách
khác hẳn — thêm hàng, xoá món, xem lại giỏ, giảm số lượng, thêm địa chỉ, sửa đơn, hoặc đưa đơn qua
một trạng thái hợp lệ (cùng lý luận của module 03–06).

Những tình huống dưới đây cố ý **không** sinh mã riêng của module order (xem
`specs/010-order-confirmation/contracts/error-codes.md`): `addressId` không phải UUID hoặc không phải
địa chỉ của khách, `email` chuyển nhượng thiếu hoặc không phải email, một dòng sửa thiếu `variantId`
hoặc `quantity < 1` → `VALIDATION_ERROR` 400 với `error.details[].field`; định danh đường dẫn không
phải UUID → `VALIDATION_ERROR` 400; body không parse được hoặc có member lạ → `MALFORMED_REQUEST` 400;
thiếu hoặc sai phiên → `UNAUTHENTICATED` 401; `CUSTOMER` gọi endpoint quản trị → `FORBIDDEN` 403 (ghi
`AUTH_PRIVILEGE_DENIED`); `page`/`pageSize` ngoài khoảng, `status` không thuộc sáu trạng thái hoặc
`sort` khác `newest`/`oldest` → `VALIDATION_ERROR` 400; quá hạn mức → `RATE_LIMITED` 429; lỗi ngoài dự
kiến → `INTERNAL_ERROR` 500.

> Action `audit_logs` của module dùng tiền tố `ORDER_` nhưng **không** phải mã lỗi:
> `ORDER_CONFIRMED`, `ORDER_REJECTED`, `ORDER_SHIPPED`, `ORDER_COMPLETED`, `ORDER_TRANSFERRED` — xem
> `internal/modules/order/domain/constant/codes.go`. Metadata của mỗi dòng chỉ nêu định danh đơn,
> **không** mang dữ liệu giao hàng hay dữ liệu cá nhân của khách. Thao tác của khách (checkout, sửa,
> huỷ) và của hệ thống (đã trả tiền, hết hạn) **không** ghi `audit_logs`: chúng được phản ánh trên
> trạng thái, phiên bản và mốc thời gian của đơn (mỗi lần sửa để lại một dòng `order_edit_history`);
> audit ghi các mutation quản trị (Constitution VI).

**Riêng module payment** (`internal/modules/payment/domain/constant/codes.go`):

| Code | HTTP | Nghĩa |
|------|------|-------|
| `PAYMENT_ORDER_NOT_FOUND` | 404 | Không có đơn nào mang định danh đó, **hoặc** đơn thuộc khách khác. Hai tình huống **cố ý** trả lời giống nhau, để route không xác nhận đơn của khách khác |
| `PAYMENT_ORDER_NOT_PAYABLE` | 409 | Đơn **không ở trạng thái chờ thanh toán** — đã trả tiền, đã huỷ, hoặc còn chờ artist xác nhận. Chỉ bắt đầu thanh toán được cho đơn đang chờ thanh toán |
| `PAYMENT_NOT_FOUND` | 404 | Webhook mang `orderCode` không bản ghi thanh toán nào mang (link mồ côi — liên kết sống ở cổng nhưng dòng chưa kịp ghi). Đơn không đổi và **không gì được ghi**; chỉ log warning. Cũng là câu trả lời khi khách đọc thanh toán của một đơn **chưa có thanh toán nào** |
| `PAYMENT_ALREADY_LIVE` | 409 | Đã có thanh toán còn hiệu lực cho đơn nhưng liên kết **không còn trả được** (đã huỷ/hết hạn ở cổng), nên không tạo mới được khi bản ghi cũ còn. Không tạo liên kết thứ hai |
| `PAYMENT_PROVIDER_ERROR` | 422 | Cổng từ chối tạo liên kết hoặc không tới được. **Không gì được tạo, đơn không đổi**; thử lại được |
| `PAYMENT_WEBHOOK_INVALID_SIGNATURE` | 401 | Chữ ký webhook không xác minh, nên sự kiện **không** phải từ cổng. Không gì được áp. Đây là mã **của module** chứ không phải `UNAUTHENTICATED` (vốn nói về phiên) |

Sáu mã trên là **not-found** (404), **conflict** (409), **unprocessable** (422) hoặc **unauthorized** (401)
chứ không phải validation: request hợp lệ nhưng trạng thái của đơn/thanh toán hoặc chữ ký khiến nó không
áp được, và bước tiếp theo khác hẳn — đọc lại đơn, thử lại sau, hay để operator đối soát.

Những tình huống dưới đây cố ý **không** sinh mã riêng của module payment (xem
`specs/011-order-payment/contracts/error-codes.md`): body webhook không parse được → `MALFORMED_REQUEST`
400; `status` ngoài bốn trạng thái, `page`/`pageSize` ngoài khoảng → `VALIDATION_ERROR` 400; thiếu hoặc
sai phiên → `UNAUTHENTICATED` 401; `CUSTOMER` gọi danh sách thanh toán của operator → `FORBIDDEN` 403;
định danh đường dẫn không phải UUID → `VALIDATION_ERROR` 400; quá hạn mức → `RATE_LIMITED` 429; lỗi ngoài
dự kiến → `INTERNAL_ERROR` 500.

> Một xác nhận đã xác minh **sai số tiền**, cho **nội dung đơn cũ**, hoặc cho đơn **không còn chờ thanh
> toán** **không** phải một lỗi trả về: thanh toán thành `UNMATCHED` và webhook vẫn trả `2xx` (từ chối sẽ
> khiến cổng thử lại mãi). Gửi lặp một xác nhận trên thanh toán đã `PAID` là **no-op**.

> Action `audit_logs` của module dùng tiền tố `PAYMENT_` nhưng **không** phải mã lỗi: `PAYMENT_CREATED`,
> `PAYMENT_APPLIED`, `PAYMENT_CANCELLED`, `PAYMENT_UNMATCHED` — xem
> `internal/modules/payment/domain/constant/codes.go` và `payment_events.action`. Metadata của mỗi dòng chỉ
> nêu định danh đơn, **không** mang dữ liệu người trả hay tham chiếu của cổng.

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

Nhóm `/cart` của module cart cũng **không** có hạn mức riêng, chỉ chịu hạn mức toàn cục: giỏ dành
cho khách **đã đăng nhập**, không phải bề mặt lạm dụng mà một hạn mức theo module tồn tại để chặn
(`specs/008-cart/spec.md`, mục *Assumptions*).

Nhóm `/orders` và `/admin/orders` của module order cũng **không** có hạn mức riêng, chỉ chịu hạn mức
toàn cục: checkout và huỷ là hành động của khách **đã đăng nhập**, còn các endpoint của operator là
`ADMIN`-only, nên bề mặt lạm dụng mà một hạn mức theo module tồn tại để chặn **không tồn tại**
(`specs/009-order/spec.md`, mục *Assumptions*).

Nhóm `/orders/{orderId}/payment` (khách) và `/admin/payments` (quản trị) của module payment cũng **không**
có hạn mức riêng, chỉ chịu hạn mức toàn cục. Webhook `/payments/payos/webhook` là **công khai**, nên cũng
chỉ chịu hạn mức toàn cục — nhưng đây là điểm PayOS **gọi tới và thử lại**, nên một sự kiện đã xử lý phải
trả `2xx` nhanh để cổng ngừng thử lại thay vì bị chặn (`specs/011-order-payment/research.md` D10).

Ngoài ra: đăng nhập sai liên tiếp **10 lần** sẽ khoá tài khoản 15 phút
(`AUTH_LOGIN_MAX_FAILURES`, `AUTH_LOGIN_LOCKOUT_TTL`); nhập sai OTP **5 lần** sẽ
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

Hạ tầng chung: `register`, `register/verify-otp`, `register/resend-otp`,
`register/cancel`, `password/forgot`, `password/forgot/verify-otp`, `password/reset`
nằm trong nhóm rate limit *flow*; `login` có hạn mức riêng.

Email được normalize (trim + lowercase) trước khi xử lý. `POST /register` từ chối **sớm** một
email/username đã dùng bằng `409` (thay đổi có chủ đích để biểu mẫu báo ngay); các endpoint
`verify-otp`/`resend-otp`/`cancel` trả **chung một kết quả** `AUTH_REGISTRATION_NOT_FOUND` cho
tham chiếu lạ, hết hạn hoặc đã huỷ, nên không tiết lộ tham chiếu có từng tồn tại hay không.

> **Đăng ký theo cơ chế trì hoãn (deferred).** `POST /register` **không** tạo tài khoản: nó chỉ
> giữ một đăng ký **tạm** trong Redis theo `registrationId` rồi gửi OTP. Tài khoản `active` chỉ
> được tạo khi `POST /register/verify-otp` xác nhận đúng mã. Bỏ dở giữa chừng không để lại gì
> lâu dài. Hai endpoint email-keyed cũ `POST /verify-email` và `POST /resend-verification` đã bị
> **gỡ** (breaking change có chủ đích).

### 3.1 `POST /register`

Mở đầu một lượt đăng ký **chưa** tạo tài khoản: xác thực dữ liệu, từ chối email/username đã
dùng, lưu đăng ký **tạm** trong Redis dưới một `registrationId` khó đoán rồi gửi OTP 6 chữ số
qua email. Tài khoản chỉ được tạo khi OTP được xác nhận (mục 3.2).

| | |
|---|---|
| Auth | Không |
| Rate limit | 5/phút (`AUTH_FLOW_RATE_PER_MINUTE`) |
| Trả về | `202 Accepted`, hoặc `503` **khi không gửi được** (xem bên dưới) |

Request

```json
{ "email": "user@example.com", "username": "artist", "displayName": "Artist", "password": "Str0ng!Pass" }
```

`username` và `displayName` là tùy chọn. `password` phải đạt chính sách mật khẩu và được băm
**Argon2id**; email được normalize (trim + lowercase). **Không** tài khoản nào (kể cả `pending`)
được tạo ở bước này.

Response `202`

```json
{
  "data": {
    "registrationId": "opaque-random-string",
    "status": "PendingVerification",
    "otpExpiresIn": 300,
    "resendAvailableIn": 60
  },
  "meta": { "requestId": "...", "timestamp": "..." }
}
```

`registrationId` là tham chiếu client dùng cho `verify-otp`/`resend-otp`/`cancel`;
`otpExpiresIn` và `resendAvailableIn` là số **giây**.

Lỗi: `VALIDATION_ERROR` 400 (email sai định dạng) · `AUTH_EMAIL_TAKEN` 409 · `AUTH_USERNAME_TAKEN` 409 · `AUTH_WEAK_PASSWORD` 400 · `RATE_LIMITED` 429 · `SERVICE_UNAVAILABLE` 503

Một `email` sai định dạng (ví dụ `user@example`, thiếu `@`/domain) trả `400 VALIDATION_ERROR`
với `error.details[].field` là `"email"`; kiểm định này chạy **trước** khi lưu đăng ký tạm nên
một địa chỉ sai định dạng để lại **không** đăng ký tạm và **không** gửi mã nào (FR-001). Đây là
mã **dùng chung** của `httpx`, endpoint không thêm mã riêng nào của module auth.

Một địa chỉ đã có tài khoản trả `409 AUTH_EMAIL_TAKEN`; một username đã được dùng trả `409
AUTH_USERNAME_TAKEN`. Việc từ chối sớm này để lộ sự tồn tại của tài khoản cho người gọi — đây là
**thay đổi có chủ đích** so với thiết kế che thông tin trước đây, để biểu mẫu đăng ký báo được
"email/tên đăng nhập đã tồn tại". Vì từ chối xảy ra **trước** khi lưu đăng ký tạm, một email đã
có tài khoản không bao giờ tới được nhánh `503` bên dưới.

#### Nhánh gửi OTP thất bại trả `503`

Khi nhà cung cấp email **từ chối** thư xác nhận, endpoint này trả `503` chứ không phải `500`: đây
là sự cố của dịch vụ, không phải lỗi khách gây ra, và `500` khiến client tưởng mình sai nên thử
lại y hệt — một việc không bao giờ có kết quả.

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

Mã `SERVICE_UNAVAILABLE` là mã **dùng chung** của `httpx`, được tái sử dụng; endpoint này không
thêm mã lỗi riêng nào của module auth.

Điều mà `503` này hứa thì đúng:

| Điều | Kết quả quan sát được |
|---|---|
| Đăng ký tạm có được giữ không | **Có**, và không có tài khoản nào được tạo. Khách xác nhận được ngay khi gửi lại thành công |
| `POST /auth/register/resend-otp` ngay sau đó | **Được chấp nhận**, không bị `AUTH_RESEND_COOLDOWN`. Một lần gửi hỏng không tiêu tốn cooldown |
| Rate limit nhóm *flow* | Vẫn áp dụng, nên không mở đường gửi không giới hạn |
| Body có lộ chi tiết nhà cung cấp, credential hay địa chỉ người nhận không | Không |

Bản ghi `audit_logs` cho lần đăng ký đó dùng action `AUTH_REGISTER_DELIVERY_FAILED` với outcome
`FAILURE`, target là `registrationId`, và metadata `{"classification": ...}` là phân loại **của
hệ thống này**, không phải câu chữ của nhà cung cấp:

| Phân loại | Nghĩa | Có thử lại không |
|---|---|---|
| `TRANSIENT` | Provider báo lỗi tạm thời (SMTP 4xx) | Có, trong hạn mức thử lại |
| `UNREACHABLE` | Không kết nối được provider | Có, trong hạn mức thử lại |
| `CONFIGURATION` | Provider từ chối vì deployment này cấu hình sai (SMTP 5xx) | Không — thử lại không đổi được kết quả |
| `REFUSED` | Provider từ chối chính thư đó | Không |
| `UNKNOWN` | Lỗi không mang phân loại nào hệ thống này nhận biết | Không |

Một dòng log ở mức `error` đi kèm, chỉ chứa `registrationId`, `deliveryFailure` và
`deliveryAttempts` — không có mã xác nhận, không có câu chữ của provider, không có địa chỉ người
nhận. Ngân sách thử lại có trần (tối đa 3 lần, tổng thời gian chờ nằm trong mục tiêu trả lời 2
giây), nên `503` vẫn tới khách kịp thời.

> Nếu `.env` để `SMTP_HOST` rỗng, email được ghi ra log (log sender). Để xem OTP thật qua
> Mailpit thì không cần sửa `.env`: `make up-tools` tự trỏ container vào Mailpit, còn khi
> chạy API trên host thì tạo `.env.local` với `SMTP_HOST=localhost`, `SMTP_PORT=1025`. Đọc
> hộp thư ở `http://localhost:8025` hoặc `make mail`. Xem
> [configuration.md](configuration.md) mục *Phân tầng file env*.

### 3.2 `POST /register/verify-otp`

Xác nhận OTP cho đăng ký tạm. Khi mã đúng, chưa hết hạn và chưa vượt số lần thử: **tạo đúng một**
tài khoản `active` vai trò `CUSTOMER`, xoá mã và đánh dấu đăng ký `completed`. **Idempotent**:
gọi lại cùng `registrationId` sau khi đã xác nhận vẫn trả `200` và **không** tạo tài khoản thứ hai.
**Không** cấp phiên — client gọi `/auth/login` sau đó.

| | |
|---|---|
| Auth | Không |
| Rate limit | 5/phút (flow) |
| Trả về | `200 OK` |

Request

```json
{ "registrationId": "opaque-random-string", "otp": "123456" }
```

Response `200`

```json
{ "data": { "message": "Registration confirmed." }, "meta": { "requestId": "...", "timestamp": "..." } }
```

Lỗi: `AUTH_REGISTRATION_NOT_FOUND` 400 · `AUTH_OTP_INVALID` 400 · `AUTH_OTP_EXPIRED` 400 · `AUTH_OTP_TOO_MANY_ATTEMPTS` 429 · `AUTH_USERNAME_TAKEN` 409 · `RATE_LIMITED` 429

- `AUTH_REGISTRATION_NOT_FOUND` 400 cho tham chiếu **lạ, hết hạn hoặc đã huỷ** — phản hồi chung,
  không tiết lộ tham chiếu có từng tồn tại hay không.
- `AUTH_USERNAME_TAKEN` 409 khi một đăng ký khác đã chiếm `username` trong khoảng giữa lúc bắt
  đầu và lúc xác nhận: **không** tài khoản nào được tạo, đăng ký tạm được giữ nguyên.
- Mã sai thì bộ đếm số lần sai tăng; khi chạm `OTP_MAX_ATTEMPTS`, mã bị chặn và trả
  `AUTH_OTP_TOO_MANY_ATTEMPTS` 429 cho tới khi có mã mới (`resend-otp`).

### 3.3 `POST /register/resend-otp`

Gửi lại OTP cho một đăng ký **còn hiệu lực**, sau khoảng chờ giữa hai lần gửi. Mã cũ bị vô hiệu
và bộ đếm số lần sai được reset.

| | |
|---|---|
| Auth | Không |
| Rate limit | 5/phút (flow) |
| Trả về | `202 Accepted`, hoặc `503` **khi không gửi được** |

Request

```json
{ "registrationId": "opaque-random-string" }
```

Response `202`

```json
{ "data": { "message": "A fresh confirmation code has been sent." }, "meta": { "requestId": "...", "timestamp": "..." } }
```

Lỗi: `AUTH_RESEND_COOLDOWN` 429 · `AUTH_REGISTRATION_NOT_FOUND` 400 · `RATE_LIMITED` 429 · `SERVICE_UNAVAILABLE` 503

- `AUTH_RESEND_COOLDOWN` 429 khi còn trong khoảng chờ (`resendAvailableIn` sau lần gửi gần nhất).
- Tham chiếu **lạ, hết hạn hoặc đã huỷ** trả `AUTH_REGISTRATION_NOT_FOUND` 400 chung; đăng ký đã
  `completed` cũng trả chung như vậy, không tiết lộ trạng thái.
- Nếu lần gửi lại này **cũng** thất bại, endpoint trả `503 SERVICE_UNAVAILABLE` như `3.1`; cooldown
  được gỡ nên khách gọi lại được ngay.

### 3.4 `POST /register/cancel`

Huỷ đăng ký **tạm** và vô hiệu tham chiếu lẫn mã của nó. **Idempotent**: một tham chiếu lạ, hết hạn
hoặc đã huỷ cũng trả `200` và không tiết lộ gì. Đăng ký đã xác nhận mà bị huỷ cũng chỉ xoá bản ghi
tạm, **không** đụng tới tài khoản đã tạo.

| | |
|---|---|
| Auth | Không |
| Rate limit | 5/phút (flow) |
| Trả về | `200 OK` |

Request

```json
{ "registrationId": "opaque-random-string" }
```

Response `200`

```json
{ "data": { "message": "Registration cancelled." }, "meta": { "requestId": "...", "timestamp": "..." } }
```

Lỗi: `RATE_LIMITED` 429

### 3.5 `POST /login`

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

### 3.6 `POST /refresh`

Xoay vòng phiên: refresh token cũ bị thu hồi, cặp token mới được cấp.

| | |
|---|---|
| Auth | Không (dùng refresh token trong body) |
| Trả về | `200 OK` |

Request `{ "refreshToken": "<opaque>" }` → response giống `POST /login`.

Lỗi: `AUTH_TOKEN_INVALID` 401 · `AUTH_TOKEN_EXPIRED` 401 · `AUTH_REFRESH_REUSED` 401 (kèm thu hồi toàn bộ phiên của user)

### 3.7 `POST /logout`

Thu hồi phiên của thiết bị hiện tại. Thao tác **idempotent**: refresh token lạ vẫn
trả `204`.

| | |
|---|---|
| Auth | Không (dùng refresh token trong body) |
| Trả về | `204 No Content` — **không có body** |

Request `{ "refreshToken": "<opaque>" }`

Lỗi: `MALFORMED_REQUEST` 400

### 3.8 `POST /password/forgot`

Bắt đầu đặt lại mật khẩu: nếu email có tài khoản, gửi một **mã OTP** đặt lại qua email.
Phản hồi luôn chung, không tiết lộ email có tồn tại. Bước này **cũng là** bước gửi lại:
gọi lại sau khoảng chờ là xin mã mới (không có endpoint resend riêng).

| | |
|---|---|
| Auth | Không |
| Rate limit | 5/phút (flow) |
| Trả về | `202 Accepted`, hoặc `503` **khi không gửi được** |

Request `{ "email": "user@example.com" }`

Response `202`

```json
{ "data": { "message": "If the email is registered, a reset code has been sent." }, "meta": { "...": "..." } }
```

Email chứa **mã 6 chữ số** dùng cho `verify-otp`; email **không** chứa reset token. Mã được
lưu ở dạng **băm không thể đảo ngược** (Argon2id), có hạn riêng và bị thay khi xin mã mới.

Lỗi: `VALIDATION_ERROR` 400 (email sai định dạng) · `AUTH_RESEND_COOLDOWN` 429 · `RATE_LIMITED` 429 · `SERVICE_UNAVAILABLE` 503

- `AUTH_RESEND_COOLDOWN` 429 khi email có tài khoản và còn trong khoảng chờ
  (`OTP_RESEND_COOLDOWN`) kể từ lần gửi gần nhất; nó chặn **cả** lần gửi lại.
- Một địa chỉ **chưa** có tài khoản cũng trả `202` y hệt, **không** gửi gì và **không** để lại
  trạng thái, nên không tiết lộ email có tồn tại (FR-002).
- `SERVICE_UNAVAILABLE` 503 khi nhà cung cấp email **từ chối** thư: **không** gì được gửi tới
  địa chỉ đó, và cooldown được gỡ nên xin lại mã mới được ngay. Bản ghi `audit_logs` dùng action
  `AUTH_PASSWORD_RESET_DELIVERY_FAILED` với outcome `FAILURE` (xem §3.1 cho mô tả `503` dùng chung).

### 3.9 `POST /password/forgot/verify-otp`

Xác minh mã OTP đã nhận. Khi mã đúng, chưa hết hạn và chưa vượt số lần thử: cấp một **reset
token** dùng **một lần**, vô hiệu mã, và trả token kèm hạn của nó. Reset token **không** phải
phiên, không dùng để xác thực bất kỳ request nào khác.

| | |
|---|---|
| Auth | Không |
| Rate limit | 5/phút (flow) |
| Trả về | `200 OK` |

Request

```json
{ "email": "user@example.com", "otp": "123456" }
```

Response `200`

```json
{
  "data": { "resetToken": "opaque-single-use-token", "expiresIn": 1800 },
  "meta": { "requestId": "...", "timestamp": "..." }
}
```

`expiresIn` là số **giây** còn lại của reset token (`PASSWORD_RESET_TTL`, mặc định 30 phút).

Lỗi: `VALIDATION_ERROR` 400 (email sai định dạng) · `AUTH_OTP_INVALID` 400 · `AUTH_OTP_EXPIRED` 400 · `AUTH_OTP_TOO_MANY_ATTEMPTS` 429 · `RATE_LIMITED` 429

- `AUTH_OTP_INVALID` 400 khi mã sai: bộ đếm số lần sai tăng và reset request được giữ cho lần thử sau.
- `AUTH_OTP_EXPIRED` 400 khi mã hết hạn, **chưa** có mã nào đang sống, hoặc địa chỉ không có tài khoản.
- `AUTH_OTP_TOO_MANY_ATTEMPTS` 429 khi chạm `OTP_MAX_ATTEMPTS`: mã bị chặn tới khi có mã mới.
- Các phản hồi trên **không** tiết lộ địa chỉ có được đăng ký hay không (FR-009).
- Mã chỉ dùng **một lần**: sau khi xác minh thành công, nó không dùng lại được.

### 3.10 `POST /password/reset`

Hoàn tất đặt lại mật khẩu bằng reset token. Thay mật khẩu, tiêu thụ token, thu hồi **mọi**
phiên của tài khoản và vô hiệu hoá các access token đã cấp trước đó. **Không** đăng nhập tự
động: trả về thành công, khách quay lại trang đăng nhập và dùng mật khẩu mới.

| | |
|---|---|
| Auth | Không |
| Rate limit | 5/phút (flow) |
| Trả về | `200 OK` |

Request

```json
{ "token": "<reset token>", "newPassword": "Str0ng!Pass" }
```

Response `200`

```json
{ "data": { "message": "Password reset." }, "meta": { "requestId": "...", "timestamp": "..." } }
```

Lỗi: `AUTH_RESET_INVALID` 400 · `AUTH_WEAK_PASSWORD` 400 · `RATE_LIMITED` 429

- `AUTH_RESET_INVALID` 400 khi token sai, đã dùng, hết hạn, hoặc đã bị một lần reset mới hơn
  cho cùng tài khoản thay thế.
- `AUTH_WEAK_PASSWORD` 400 khi mật khẩu mới không đạt chính sách; token **vẫn dùng được** để thử lại.

### 3.11 `GET /me`

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

### 3.12 `POST /password/change`

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

### 3.13 `GET /admin/probe`

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
`CATEGORY_HAS_PENDING_ORDERS` 409 (chỉ khi ẩn — `isVisible: false` — mà còn đơn chờ giữ một
variant của danh mục) · `RATE_LIMITED` 429. Va chạm để lại **cả hai** danh mục nguyên vẹn.

Ẩn bị từ chối thì **không gì đổi**: danh mục giữ nguyên, các dòng giỏ giữ nguyên. Thân từ chối
mang một phần tử `error.details` cho **mỗi** đơn chặn — `{"field": "orderId", "issue": "<định
danh đơn>"}` — để operator xử lý đúng đơn. Hiện danh mục (`isVisible: true`) không bao giờ bị
chặn; ẩn thành công gỡ các variant của danh mục khỏi mọi giỏ trong cùng transaction.

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

Sản phẩm của shop: một **sản phẩm cha** với nội dung dùng chung (tên, mã sản phẩm `code`, giá mặc
định, danh mục, mô tả, bộ ảnh) và một mảng **variant** con — mỗi variant có tên, SKU, giá riêng (tuỳ
chọn), ảnh riêng, thứ tự và **trạng thái bán riêng**; variant là **đơn vị mua** (giỏ, đơn và tồn kho
đều theo variant). **Một tài nguyên sản phẩm, hai bề mặt**: bề mặt công khai (`/products`, không cần
token, địa chỉ theo **`code`** — đoạn link công khai) và bề mặt quản trị (`/admin/products`, vai trò
`ADMIN`, địa chỉ theo **định danh**).

Tám điều dễ đọc sai, nói ngay:

- **Mã sản phẩm là đoạn link công khai.** Sản phẩm **không** còn `slug` riêng: `code` vừa là mã
  operator viết vừa là đoạn link của URL công khai (`/products/{code}`, URL-safe: chữ thường, chữ số,
  dấu gạch đơn; ≤140 ký tự, duy nhất). Danh mục vẫn giữ `slug` riêng của nó (mục 5).

- **Danh mục hiển thị một mục cho mỗi sản phẩm.** Mọi variant của một sản phẩm gộp vào **một** phần
  tử; giá của card là giá **thấp nhất** trong các variant **đang bán**, ảnh là **ảnh chính của gallery**
  sản phẩm, fallback về ảnh của **variant mặc định** (variant có `position` nhỏ nhất). Phần tử không
  còn `type`: chỉ còn một loại.
- **Sản phẩm bị ẩn và một code chưa từng tồn tại trả lời y hệt nhau** (`404 PRODUCT_NOT_FOUND`).
  Gồm cả sản phẩm nằm trong **danh mục bị ẩn** và sản phẩm **không còn variant nào đang bán**. Xem
  `6.2`.
- **Sản phẩm chỉ hiện khi còn ít nhất một variant đang bán.** Một variant hết hàng vẫn được liệt kê
  (đánh dấu hết hàng) trên một sản phẩm đang hiển thị; nếu **mọi** variant đều hết hàng, chưa mở bán
  hoặc đã ngừng bán thì sản phẩm rời danh mục.
- **Trạng thái bán nằm ở variant và đi qua endpoint riêng** `POST .../variants/{variantId}/state`;
  `PATCH` **không** nhận `sellState`. Một lần chuyển không hợp lệ trả `409`, không phải `400`.
- **Mỗi sản phẩm bắt buộc có ≥1 variant.** Tạo sản phẩm phải kèm `variants` (≥1); gỡ variant cuối bị
  từ chối `409 VARIANT_LAST_REMOVAL`.
- **Mã sản phẩm và SKU bắt buộc và duy nhất** (so khớp sau trim và bỏ qua hoa/thường); trùng trả
  `409 PRODUCT_CODE_TAKEN` / `VARIANT_SKU_TAKEN` kèm field.
- Module **không** có hạn mức riêng, chỉ chịu hạn mức toàn cục (§1.5).

Các danh sách đều phân trang theo quy ước chung: `page` mặc định `1`, `pageSize` mặc định `20`,
khoảng `1..100`. Thứ tự ổn định: `position`, rồi `createdAt`, rồi `id`.

**Hình dạng công khai** — một phần tử của danh sách công khai là `PublicProduct` (5 member):

```json
{
  "id": "0f5c6e0c-1a44-4a1e-9b3d-9a1b2c3d4e5f",
  "name": "Acrylic stand Aki",
  "code": "acrylic-stand-aki",
  "price": { "amount": 120000, "currency": "VND" },
  "imageUrl": "https://res.cloudinary.com/demo/image/upload/stand.jpg"
}
```

`price` là **giá thấp nhất** trong các variant đang bán, `imageUrl` là ảnh chính gallery fallback
ảnh variant mặc định, hoặc `null` khi không có ảnh nào.

**Hình dạng quản trị** `AdminProduct` (12 member, thêm `isVisible`) — một phần tử của danh sách quản trị:

```json
{
  "id": "0f5c6e0c-1a44-4a1e-9b3d-9a1b2c3d4e5f",
  "name": "Acrylic stand Aki",
  "code": "acrylic-stand-aki",
  "description": "Acrylic stand 15cm",
  "defaultPrice": { "amount": 120000, "currency": "VND" },
  "categoryId": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e",
  "position": 10,
  "isVisible": true,
  "variantCount": 2,
  "imageUrl": "https://res.cloudinary.com/demo/image/upload/stand.jpg",
  "createdAt": "2026-10-07T08:15:04Z",
  "updatedAt": "2026-10-07T08:15:04Z"
}
```

### 6.1 `GET /products`

Danh sách khách thấy được: **một mục cho mỗi sản phẩm** có **danh mục chưa bị ẩn** **và** còn **ít
nhất một variant đang bán**. Mọi variant của sản phẩm gộp vào mục đó. Lọc tuỳ chọn theo danh mục.

| | |
|---|---|
| Auth | Không |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` |

Query: `page` (mặc định `1`, tối thiểu `1`), `pageSize` (mặc định `20`, khoảng `1..100`),
`category` (slug danh mục, tuỳ chọn).

Response `200`: `data` là mảng `PublicProduct` (5 member); `meta` là khối phân trang chung.

Lỗi: `VALIDATION_ERROR` 400 (`page` / `pageSize` sai định dạng hoặc ngoài khoảng;
`details[].field` là `"page"` hoặc `"pageSize"`) · `RATE_LIMITED` 429

Ghi chú:

- **Một sản phẩm chỉ hiển thị khi còn ít nhất một variant đang bán.** Sản phẩm mà mọi variant đều
  `COMING_SOON`, `OUT_OF_STOCK` hoặc `DISCONTINUED` **không** xuất hiện và **không** được đếm.
- **Giá của card** là giá **thấp nhất** trong các variant **đang bán** (một variant `OUT_OF_STOCK`
  rẻ hơn không được lấy làm giá card). Variant không có giá riêng dùng giá mặc định của sản phẩm.
- **Ảnh của card** là **ảnh chính của gallery** sản phẩm, fallback về ảnh của **variant mặc định**
  (variant `position` nhỏ nhất), và `null` khi không có ảnh nào.
- **Ẩn danh mục ẩn luôn các sản phẩm bên trong** — nhưng **không** sửa gì trên dòng; bỏ ẩn danh mục
  đưa chúng trở lại y như cũ.
- Lọc `?category=<slug>`: danh mục **ẩn**, **không tồn tại**, hoặc **không có mục hiển thị** đều trả
  `data: []` — **không** `404` — nên bộ lọc không tiết lộ danh mục ẩn.
- `meta.total` đếm đúng số **sản phẩm** hiển thị; thiếu `total` nghĩa là `0` (§1.3).
- Catalogue rỗng trả `data: []`, **không** phải `null` và **không** phải lỗi.

### 6.2 `GET /products/{code}`

Đọc một sản phẩm khách thấy được theo `code` (đoạn link công khai): nội dung dùng chung (tên, mô tả,
bộ ảnh) **cộng danh sách variant hiển thị** — mỗi variant mang tên, giá hiệu lực, ảnh, trạng thái bán
và cờ còn hàng.

| | |
|---|---|
| Auth | Không |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` |

Response `200` là `data` = `PublicProductDetail` (8 member = `PublicProduct` + `description` +
`images` + `variants`). Mỗi phần tử `images` là `{ id, url, width, height }`; mỗi phần tử `variants`
là `PublicVariant` (6 member: `id`, `name`, `price`, `imageUrl`, `sellState`, `inStock`). Variant
`COMING_SOON` hoặc `DISCONTINUED` **không** được trả; SKU **không** lộ ra cho khách.

Lỗi: `PRODUCT_NOT_FOUND` 404 · `RATE_LIMITED` 429

> **Sản phẩm bị ẩn, không còn variant nào đang bán, nằm trong danh mục bị ẩn, đã xoá và code chưa
> từng tồn tại trả lời y hệt nhau** — cùng status, cùng mã `PRODUCT_NOT_FOUND`, cùng câu chữ. Đây là
> bắt buộc, không chỉ tiện (FR-008): nếu một sản phẩm bị ẩn trả lời khác một code chưa dùng, endpoint
> sẽ xác nhận sản phẩm/danh mục operator đã chọn **không** công bố.
>
> Một variant `OUT_OF_STOCK` **vẫn** được trả (đánh dấu `sellState: "OUT_OF_STOCK"`, `inStock:
> false`), để khách thấy nó tồn tại nhưng không mua được. Variant mặc định là variant **đầu tiên**
> theo thứ tự `position`, `createdAt`, `id`.

### 6.3 `GET /admin/products`

Danh sách **toàn bộ** sản phẩm, kể cả sản phẩm không hiển thị.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` |

Query: như `6.1` (không có `category`).

Response `200`: `data` là mảng `AdminProduct` (12 member, gồm `code`, `defaultPrice`,
`isVisible`, `variantCount` và `imageUrl` là ảnh chính của gallery); `meta` là khối phân trang chung.

Lỗi: `VALIDATION_ERROR` 400 (`page` / `pageSize`) · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403
(ghi `audit_logs` với action `AUTH_PRIVILEGE_DENIED`) · `RATE_LIMITED` 429

### 6.4 `POST /admin/products`

Tạo sản phẩm **kèm ít nhất một variant**. Sản phẩm **bắt buộc** có `variants` (≥1); một variant
mới **luôn ở `COMING_SOON`**. Tác nhân lấy từ **session**, không bao giờ từ body.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `201 Created` |

Request (`CreateProductRequest`, `additionalProperties: false`)

```json
{
  "name": "Acrylic stand Aki",
  "code": "acrylic-stand-aki",
  "description": "Acrylic stand 15cm",
  "defaultPrice": { "amount": 120000, "currency": "VND" },
  "categoryId": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e",
  "position": 10,
  "variants": [
    { "name": "Aki", "sku": "AKI-STAND-AKI", "price": { "amount": 130000 }, "position": 0 },
    { "name": "Dottore", "sku": "AKI-STAND-DOT", "price": null, "position": 1 }
  ]
}
```

`name`, `code`, `description`, `defaultPrice`, `categoryId`, `variants` **bắt buộc**;
`position` mặc định `0`. `variants` phải có **≥1** phần tử; mỗi phần tử cần `name`, `sku`,
`position`, còn `price` là **tuỳ chọn** (`{ "amount": … }`; bỏ trống hoặc `null` nghĩa là dùng giá
mặc định của sản phẩm — currency luôn là currency của sản phẩm, nên variant **không** nhận
currency). `code` do operator viết (**cũng là đoạn link công khai**, URL-safe), **không** sinh từ
`name`.

Response `201`: `data` = `AdminProductDetail` (14 member = `AdminProduct` + `images` + `variants`);
mỗi phần tử `variants` là `AdminVariant` (8 member: `id`, `name`, `sku`, `price`,
`effectivePrice`, `imageUrl`, `position`, `sellState`), `price` là `null` khi variant dùng giá mặc
định.

Lỗi: `VALIDATION_ERROR` 400 (`name` rỗng/quá 120 ký tự; `code` rỗng, quá 140 ký tự hoặc không khớp
mẫu URL-safe; `description` quá 5000 ký tự; `defaultPrice.amount` không
dương hoặc `currency` không phải ba chữ in hoa; `categoryId` không tồn tại; `variants` rỗng hoặc một
variant thiếu `name`/`sku` hoặc có `price.amount` không dương — `details[].field` chỉ đúng member) ·
`MALFORMED_REQUEST` 400 (body không parse được hoặc có member lạ) · `UNAUTHENTICATED` 401 ·
`FORBIDDEN` 403 · `PRODUCT_CODE_TAKEN` 409 · `VARIANT_SKU_TAKEN` 409 ·
`RATE_LIMITED` 429

Ghi chú:

- Một variant mới **chưa on sale**: nó ở `COMING_SOON`, nên sản phẩm **chưa** xuất hiện trên catalogue
  cho tới khi ít nhất một variant được đưa sang `ACTIVE` (`6.13`).
- Combo set và pre-order đã **bị bỏ** ở phiên bản này; `isSet`/`isPreorder`/`preorderExpectedAt` và
  cơ chế variant group của feature 014 không còn.
- Mỗi lần tạo ghi `audit_logs` với action `PRODUCT_CREATED`, metadata `{"code": ...}`.

### 6.5 `GET /admin/products/{id}`

Đọc một sản phẩm theo định danh, kể cả sản phẩm không hiển thị, kèm **bộ ảnh gallery** và **toàn bộ**
variant (kể cả variant `COMING_SOON`/`DISCONTINUED`) — để operator quản lý một sản phẩm đang ẩn.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` |

Response `200`: `data` = `AdminProductDetail` (14 member = `AdminProduct` + `images` + `variants`);
mỗi phần tử `images` là `{ id, publicId, url, width, height, position, isPrimary }`; mỗi phần tử
`variants` là `AdminVariant` (8 member, gồm `sku`, `effectivePrice` và `sellState`).

Lỗi: `VALIDATION_ERROR` 400 (`id` không phải UUID) · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 ·
`PRODUCT_NOT_FOUND` 404 · `RATE_LIMITED` 429

### 6.6 `PATCH /admin/products/{id}`

Sửa một phần: member bỏ trống giữ nguyên giá trị hiện tại. **Không bao giờ đổi `sellState`** (trạng
thái bán nằm ở variant) và **không** đụng tới variant — variant sửa qua route riêng của nó.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` — trả sản phẩm sau khi sửa |

Request (`UpdateProductRequest`, mọi member tuỳ chọn, `additionalProperties: false`)

```json
{
  "name": "Acrylic stand Aki mới",
  "code": "acrylic-stand-aki-moi",
  "description": "Mô tả mới",
  "defaultPrice": { "amount": 130000, "currency": "VND" },
  "categoryId": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e",
  "position": 5,
  "isVisible": false
}
```

Gửi lại **đúng** `code` sản phẩm đang giữ thành công — một sản phẩm không bao giờ trùng với
chính nó. `description` chuỗi rỗng xoá mô tả.

Response `200`: `data` = `AdminProductDetail`.

Lỗi: `VALIDATION_ERROR` 400 · `MALFORMED_REQUEST` 400 · `UNAUTHENTICATED` 401 · `FORBIDDEN`
403 · `PRODUCT_NOT_FOUND` 404 · `PRODUCT_CODE_TAKEN` 409 · `PRODUCT_HAS_PENDING_ORDERS` 409
(chỉ khi tắt hiển thị — `isVisible: false` — mà còn đơn chờ giữ một variant của sản phẩm) ·
`RATE_LIMITED` 429

Ghi chú:

- **Variant không đổi qua request này**: thêm/sửa/gỡ variant đi qua các route `.../variants*` (§6.8
  trở đi).
- **Tắt hiển thị bị chặn khi còn đơn chờ**: `PRODUCT_HAS_PENDING_ORDERS` 409 khi
  `isVisible: false` mà còn đơn `PENDING`/`PAYMENT_PENDING` giữ một variant của sản phẩm. Thân
  từ chối mang một phần tử `error.details` cho mỗi đơn chặn (`{"field": "orderId", "issue": "<định
  danh đơn>"}`); sản phẩm và giỏ nguyên vẹn. Bật hiển thị (`isVisible: true`) không bao giờ bị
  chặn; tắt hiển thị thành công gỡ các variant của sản phẩm khỏi mọi giỏ trong cùng transaction.
- Ghi `audit_logs` với action `PRODUCT_UPDATED`, metadata `{"code": ...}`.

### 6.7 `DELETE /admin/products/{id}`

Xoá sản phẩm **và các variant của nó**. Đây là **hard delete**: dòng biến mất, dòng audit ở lại.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `204 No Content` — **không có body** |

Request: không có body.

Lỗi: `VALIDATION_ERROR` 400 (`id` không phải UUID) · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 ·
`PRODUCT_NOT_FOUND` 404 · `RATE_LIMITED` 429

Ghi chú:

- **Variant được xoá theo sản phẩm** (FK cascade); các sản phẩm khác không bị đụng.
- Ảnh của sản phẩm được giải phóng và các dòng ảnh bị xoá theo (cascade).
- Xoá lại sản phẩm đã xoá trả cùng `404 PRODUCT_NOT_FOUND`, không phải lỗi.
- Ghi `audit_logs` với action `PRODUCT_DELETED`.
- **Lệch có chủ ý so với module doc**: `docs/modules/04-product.md` ghi "xóa mềm" trong phạm vi
  MVP, nhưng feature này chọn hard delete; nay dòng đơn **chụp** nội dung đã mua (module 07) nên
  đơn không phụ thuộc sản phẩm còn tồn tại. Xem
  [decisions/013](decisions/013-product-visibility-media-and-hard-delete.md).

### 6.8 `POST /admin/products/{id}/variants`

Thêm một variant vào sản phẩm. Variant mới **luôn ở `COMING_SOON`**; trạng thái bán đổi qua `6.13`.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `201 Created` — trả sản phẩm kèm variant mới |

Request (`CreateVariantRequest`, `additionalProperties: false`)

```json
{ "name": "Flins", "sku": "CARD-GENSHIN-FLINS", "price": { "amount": 50000 }, "position": 2 }
```

`name`, `sku`, `position` **bắt buộc**; `price` **tuỳ chọn** (bỏ trống hoặc `null` → dùng giá mặc
định của sản phẩm; không nhận currency). Tác nhân lấy từ **session**, không bao giờ từ body.

Response `201`: `data` = `AdminProductDetail`.

Lỗi: `VALIDATION_ERROR` 400 (`name` rỗng/quá 120 ký tự; `sku` rỗng hoặc quá dài; `price.amount`
không dương — `details[].field` chỉ đúng member) · `MALFORMED_REQUEST` 400 · `UNAUTHENTICATED` 401 ·
`FORBIDDEN` 403 · `PRODUCT_NOT_FOUND` 404 · `VARIANT_SKU_TAKEN` 409 · `RATE_LIMITED` 429

Ghi chú:

- Variant mới **chưa on sale** (`COMING_SOON`), nên sản phẩm chưa xuất hiện trên catalogue cho tới
  khi có một variant `ACTIVE`.
- Ghi `audit_logs` với action `PRODUCT_VARIANT_ADDED`.

### 6.9 `PATCH /admin/products/{id}/variants/{variantId}`

Sửa một phần variant: member bỏ trống giữ nguyên giá trị hiện tại.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` — trả sản phẩm kèm variant đã sửa |

Request (`UpdateVariantRequest`, mọi member tuỳ chọn, `additionalProperties: false`)

```json
{ "name": "Flins (mới)", "sku": "CARD-GENSHIN-FLINS-2", "price": { "amount": 55000 }, "position": 1 }
```

`price` là **ba trạng thái theo sự hiện diện của khoá JSON**: **bỏ qua** giữ nguyên amount hiện tại;
gửi `null` **xoá** amount (variant quay về giá mặc định của sản phẩm); gửi `{ "amount": … }` đặt
amount riêng.

Response `200`: `data` = `AdminProductDetail`.

Lỗi: `VALIDATION_ERROR` 400 · `MALFORMED_REQUEST` 400 · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 ·
`PRODUCT_NOT_FOUND` 404 · `VARIANT_NOT_FOUND` 404 (variant không thuộc sản phẩm này) ·
`VARIANT_SKU_TAKEN` 409 · `RATE_LIMITED` 429

Ghi chú:

- Ảnh variant đổi qua các route riêng (`6.11`, `6.12`); trạng thái bán qua `6.13`.
- Ghi `audit_logs` với action `PRODUCT_VARIANT_UPDATED`.

### 6.10 `DELETE /admin/products/{id}/variants/{variantId}`

Gỡ một variant khỏi sản phẩm. **Bị từ chối khi là variant cuối cùng.**

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `204 No Content` — **không có body** |

Request: không có body.

Lỗi: `VALIDATION_ERROR` 400 (`id` / `variantId` không phải UUID) · `UNAUTHENTICATED` 401 ·
`FORBIDDEN` 403 · `PRODUCT_NOT_FOUND` 404 · `VARIANT_NOT_FOUND` 404 ·
`VARIANT_LAST_REMOVAL` 409 (variant cuối) · `RATE_LIMITED` 429

Ghi chú:

- Ảnh của variant được giải phóng theo. Ghi `audit_logs` với action `PRODUCT_VARIANT_REMOVED`.

### 6.11 `POST /admin/products/{id}/variants/{variantId}/image`

Tải ảnh **riêng** của variant lên (thay ảnh cũ nếu có). Body là `multipart/form-data` với đúng một
part `image`, cùng luật media như ảnh sản phẩm (JPEG/PNG/WebP, trần 2 MB).

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `201 Created` — trả sản phẩm kèm variant đã gắn ảnh |

Request

```bash
curl -X POST http://localhost:8080/api/v1/admin/products/$ID/variants/$VARIANT_ID/image \
  -H "Authorization: Bearer $ACCESS" \
  -F "image=@flins.jpg"
```

Response `201`: `data` = `AdminProductDetail`; `variants[].imageUrl` là ảnh của variant (không phải
ảnh gallery của sản phẩm).

Lỗi: `VALIDATION_ERROR` 400 (thiếu part `image`, hoặc body không phải `multipart/form-data`) ·
`MALFORMED_REQUEST` 400 · `PRODUCT_IMAGE_TYPE_UNSUPPORTED` 400 · `UNAUTHENTICATED` 401 ·
`FORBIDDEN` 403 · `PRODUCT_NOT_FOUND` 404 · `VARIANT_NOT_FOUND` 404 · `PRODUCT_IMAGE_TOO_LARGE` 413 ·
`RATE_LIMITED` 429 · `PRODUCT_MEDIA_UNAVAILABLE` 503

Ghi chú:

- Variant có **tối đa một ảnh**; tải lại **thay** ảnh cũ. Chỉ tham chiếu asset được lưu, không lưu
  byte. Ghi `audit_logs` với action `PRODUCT_VARIANT_UPDATED`.

### 6.12 `DELETE /admin/products/{id}/variants/{variantId}/image`

Gỡ ảnh của variant và giải phóng tài nguyên đã lưu.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `204 No Content` — **không có body** |

Request: không có body.

Lỗi: `VALIDATION_ERROR` 400 (`id` / `variantId` không phải UUID) · `UNAUTHENTICATED` 401 ·
`FORBIDDEN` 403 · `PRODUCT_NOT_FOUND` 404 · `VARIANT_NOT_FOUND` 404 · `RATE_LIMITED` 429

Ghi chú: tài nguyên được giải phóng **sau** khi tham chiếu đã bị xoá; ghi `audit_logs` với action
`PRODUCT_VARIANT_UPDATED`.

### 6.13 `POST /admin/products/{id}/variants/{variantId}/state`

Chuyển trạng thái bán của variant. Đây là một **transition**, không phải một field: nó có thể bị
từ chối và câu từ chối nêu trạng thái hiện tại (Constitution III). Thay cho endpoint cấp sản phẩm
`POST /admin/products/{id}/state` của feature 006 (đã gỡ).

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` — trả sản phẩm kèm variant ở trạng thái mới |

Request (`ChangeVariantStateRequest`, `additionalProperties: false`)

```json
{ "to": "ACTIVE" }
```

Các cạnh **hợp lệ duy nhất**: `COMING_SOON → ACTIVE`; `ACTIVE ↔ OUT_OF_STOCK` (hai chiều, để hồi
kho đưa variant trở lại bán); mọi trạng thái trừ `DISCONTINUED` → `DISCONTINUED`. `DISCONTINUED` là
**cuối** và không có đường ra. Trạng thái bán của variant **độc lập**: đổi một variant không đụng
variant khác.

Response `200`: `data` = `AdminProductDetail`.

Lỗi: `VALIDATION_ERROR` 400 (`to` không thuộc bốn trạng thái; `details[].field` là `"to"`) ·
`MALFORMED_REQUEST` 400 · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `PRODUCT_NOT_FOUND` 404 ·
`VARIANT_NOT_FOUND` 404 · `VARIANT_STATE_TRANSITION_INVALID` 409 (message nêu trạng thái hiện tại) ·
`VARIANT_HAS_PENDING_ORDERS` 409 (mọi bước **rời** trạng thái đang bán khi còn đơn chờ giữ variant) ·
`RATE_LIMITED` 429

Ghi chú:

- Một lần chuyển bị từ chối **không đổi gì**. Module 05 Inventory **tự gọi** hai cạnh
  `ACTIVE ↔ OUT_OF_STOCK` khi khả dụng của variant cắt qua 0 / hồi kho (xem §7).
- **Chuyển rời trạng thái bán bị chặn khi còn đơn chờ**: `ACTIVE → OUT_OF_STOCK`,
  `ACTIVE → DISCONTINUED` và mọi bước rời đang bán khác trả `409 VARIANT_HAS_PENDING_ORDERS`
  khi còn đơn `PENDING`/`PAYMENT_PENDING` giữ variant; thân từ chối nêu từng đơn chặn trong
  `error.details` (`{"field": "orderId", "issue": "<định danh đơn>"}`), variant và giỏ nguyên
  vẹn. Chuyển **về** đang bán không bao giờ bị chặn. Tín hiệu hết hàng của hệ thống (module 05)
  bị chặn thì thành **no-op đã ghi nhận**, không phải lỗi — tồn kho vẫn ghi đúng.
- Ghi `audit_logs` với action `PRODUCT_VARIANT_UPDATED`.

### 6.14 `POST /admin/products/{id}/images`

Gắn một ảnh vào **gallery** của sản phẩm. Body là `multipart/form-data` với đúng một part tên
`image`.

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

### 6.15 `DELETE /admin/products/{id}/images/{imageId}`

Gỡ một ảnh khỏi gallery của sản phẩm và giải phóng tài nguyên đã lưu.

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

### 6.16 `POST /admin/products/{id}/images/{imageId}/primary`

Đặt một ảnh làm **ảnh chính** của gallery sản phẩm.

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

Tồn kho của **một variant**: số lượng vật lý trên kệ, phần đang được giữ cho các đơn đang thanh
toán, và phần còn lại khả dụng cho khách. **Năm endpoint, tất cả của quản trị viên** dưới
`/admin/inventory`, địa chỉ theo **định danh variant**. Không có bề mặt tồn kho cho khách: khách
biết còn hàng hay không qua **trạng thái bán** của variant, mà module này tự chuyển khi khả dụng cắt
qua 0 (xem ghi chú cuối mục và §6.13).

Bốn điều dễ đọc sai, nói ngay:

- **Variant chưa từng nhập kho trả `0`, không phải `404`.** Một variant do module 04 tạo ra chưa có
  dòng tồn kho nào, và đọc nó là trạng thái bình thường. Chỉ **định danh không trỏ tới variant nào**
  mới trả `404 VARIANT_NOT_FOUND`. Xem `7.1`.
- **Số lượng là số nguyên, không bao giờ âm.** Một thao tác làm số lượng vật lý hoặc khả dụng xuống
  dưới 0 bị từ chối `409 INVENTORY_INSUFFICIENT_STOCK`; một thao tác đưa đúng về 0 **thành công**.
- **Điều chỉnh ghi lại phần chênh lệch**, không phải giá trị tuyệt đối. Điều chỉnh về đúng giá trị
  đang lưu **không đổi gì** và vẫn trả `200`.
- **Giữ chỗ không có bề mặt HTTP.** `reserve`/`release`/hết hạn là năng lực nội bộ của luồng thanh
  toán; khả dụng = vật lý − giữ chỗ đang hoạt động, được tính tại thời điểm hỏi, không lưu sẵn.
- Module **không** có hạn mức riêng, chỉ chịu hạn mức toàn cục (§1.5).

Lịch sử biến động phân trang theo quy ước chung: `page` mặc định `1`, `pageSize` mặc định `20`,
khoảng `1..100`.

### 7.1 `GET /admin/inventory/{variantId}`

Đọc số lượng vật lý, số đang giữ và số khả dụng của một variant.

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
Định danh variant nằm ở đường dẫn, **không** lặp lại trong body.

Lỗi: `VALIDATION_ERROR` 400 (`variantId` không phải UUID) · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403
· `VARIANT_NOT_FOUND` 404 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500

Ghi chú:

- Variant **chưa từng nhập kho** trả `physicalQuantity: 0`, `heldQuantity: 0`,
  `availableQuantity: 0`.
- Số đang giữ là tổng các giữ chỗ **còn hiệu lực**; giữ chỗ đã hết hạn nhưng chưa được quét không
  còn được tính.

### 7.2 `GET /admin/inventory/{variantId}/movements`

Đọc lịch sử biến động kho của một variant, **cũ nhất trước**.

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
      "variantId": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e",
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

`StockMovement` có **9 member**: `id`, `variantId`, `kind`, `delta` (có dấu), `resultingQuantity`
(số lượng vật lý sau thay đổi), `sourceReference` (`null` với thao tác thủ công), `actorId` (`null`
với thay đổi do hệ thống), `note` (`null` khi không có), `createdAt`. `kind` là một trong **4** giá
trị `RESTOCK`, `DAMAGE`, `ADJUSTMENT`, `SALE`.

Lỗi: `VALIDATION_ERROR` 400 (`variantId` không phải UUID, hoặc `page`/`pageSize` ngoài khoảng;
`details[].field` chỉ đúng member) · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `VARIANT_NOT_FOUND`
404 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500

Ghi chú:

- Variant **không có biến động** trả `data: []`, không phải lỗi.
- `sourceReference` và `actorId` loại trừ nhau: một thao tác thủ công mang `actorId`, một sự kiện
  ngoài mang `sourceReference`.

### 7.3 `POST /admin/inventory/{variantId}/restock`

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
`UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `VARIANT_NOT_FOUND` 404 · `RATE_LIMITED` 429 ·
`INTERNAL_ERROR` 500

Ghi chú:

- Mỗi lần restock ghi một dòng ledger `kind: "RESTOCK"` (mang tác nhân và ghi chú) và một dòng
  audit `INVENTORY_RESTOCKED` (mang tác nhân, `kind` và `delta`, **không** mang ghi chú).

### 7.4 `POST /admin/inventory/{variantId}/damage`

Ghi nhận hàng hư hỏng. Giảm số lượng vật lý đi một lượng **dương**.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` — tồn kho sau thay đổi |

Request (`QuantityRequest`, `additionalProperties: false`) như `7.3`.

Response `200`: `data` là `StockView`.

Lỗi: `VALIDATION_ERROR` 400 (`quantity` thiếu, không nguyên, âm hoặc bằng 0) ·
`MALFORMED_REQUEST` 400 · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `VARIANT_NOT_FOUND` 404 ·
`INVENTORY_INSUFFICIENT_STOCK` 409 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500

`409 INVENTORY_INSUFFICIENT_STOCK` xảy ra khi lượng giảm vượt quá số đang có trên kệ, **hoặc** khi
nó để lại trên kệ ít hơn phần đang được giữ cho đơn đang thanh toán — một giữ chỗ không bao giờ bị
phá bởi thay đổi vật lý. Bị từ chối thì **không có gì đổi** và **không** có dòng ledger mới. Ghi
`kind: "DAMAGE"` và audit `INVENTORY_DAMAGED`.

### 7.5 `POST /admin/inventory/{variantId}/adjustment`

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
`UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `VARIANT_NOT_FOUND` 404 ·
`INVENTORY_INSUFFICIENT_STOCK` 409 (giá trị đếm thấp hơn phần đang được giữ) · `RATE_LIMITED` 429 ·
`INTERNAL_ERROR` 500

Ghi chú:

- Điều chỉnh về **đúng** giá trị đang lưu trả `200`, **không** ghi dòng ledger nào (không có gì
  đổi), nhưng **vẫn** ghi một dòng audit `INVENTORY_ADJUSTED` cho chính thao tác.
- Khi có chênh lệch, ledger ghi `kind: "ADJUSTMENT"` với `delta` có dấu bằng `đã đếm − đang lưu` và
  `resultingQuantity` bằng giá trị đã đếm.
- Ledger **không bao giờ** chứa `delta: 0`: một thay đổi không đổi gì thì không phải một thay đổi.

### Quan hệ với trạng thái bán của variant (module 04)

Khi khả dụng của một **variant** **cắt qua 0** trong cùng transaction với thay đổi, module này báo
module 04 đổi trạng thái bán của chính variant đó: về 0 → `OUT_OF_STOCK`, từ 0 lên dương → `ACTIVE`.
Một thay đổi **không** cắt qua 0 **không** đụng tới trạng thái bán, nên nhãn do operator đặt tay được
giữ nguyên. Variant `COMING_SOON` và `DISCONTINUED` không bao giờ bị thay đổi bởi tồn kho (hai tín
hiệu tự động là no-op khoan dung với hai trạng thái đó). Trạng thái của mỗi variant **độc lập**; hai
cạnh hợp lệ nằm ở §6.13.

---

## 8. Module 06 — Cart (`/api/v1`)

Giỏ hàng của một khách **đã đăng nhập**: những **variant** khách định mua, số lượng mỗi dòng, và
giá khách được hiển thị lúc thêm. **Bốn endpoint, tất cả của khách đã đăng nhập**, dưới `/cart`. Giỏ
là một **tài nguyên đơn**, định địa chỉ tại `/cart` và **không kèm định danh**: chủ sở hữu là
session, nên client không bao giờ nêu tên một giỏ. Một dòng định địa chỉ bằng **định danh variant**,
vì một giỏ có tối đa một dòng cho mỗi variant (và có thể giữ hai variant của cùng một sản phẩm).

Bốn điều dễ đọc sai, nói ngay:

- **Đọc giỏ đối chiếu lại từng dòng** với trạng thái bán và tồn khả dụng hiện tại của variant, báo
  mỗi dòng có mua được hay không (`buyable`), và kèm `availableQuantity` khi dòng **còn bán nhưng
  thiếu**. Giá hiển thị luôn là giá **snapshot** lúc thêm; checkout mới là nơi đối chiếu lại giá
  trước khi thu tiền.
- **Giỏ không giữ chỗ tồn kho.** Giữ chỗ là việc của module 05 và xảy ra khi khách bắt đầu thanh
  toán; checkout đối chiếu lại khả dụng trước khi tạo đơn.
- **`PRODUCT_NOT_FOUND` 404 của module 04 được tái sử dụng** cho một **variant** không tồn tại/đã xoá
  **và** cho một dòng không phải của giỏ đang gọi — hai tình huống trả lời giống nhau để route
  **không bao giờ** xác nhận nội dung giỏ của khách khác.
- Module **không** có hạn mức riêng, chỉ chịu hạn mức toàn cục (§1.5). Module cũng **không** ghi
  `audit_logs` (§1.4).

Một giỏ rỗng trả `lines: []` và `subtotal: null` — trạng thái bình thường, không phải lỗi, và đọc
một giỏ **không** tạo dòng nào trong cơ sở dữ liệu.

### 8.1 `GET /cart`

Đọc giỏ của khách đang đăng nhập, đối chiếu lại từng dòng.

| | |
|---|---|
| Auth | Bearer access token (khách đã đăng nhập) |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` |

Response `200`

```json
{
  "data": {
    "lines": [
      {
        "productId": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e",
        "productName": "Tranh sơn dầu",
        "variantId": "c3a2b1e5-6f7a-4c8d-9e0f-1a2b3c4d5e6f",
        "variantName": "Bản lớn",
        "quantity": 2,
        "unitPrice": { "amount": 150000, "currency": "VND" },
        "lineTotal": { "amount": 300000, "currency": "VND" },
        "buyable": true
      }
    ],
    "subtotal": { "amount": 300000, "currency": "VND" }
  },
  "meta": { "requestId": "...", "timestamp": "..." }
}
```

`CartResponse` có **2 member**: `lines` (mảng `CartLineResponse`; giỏ rỗng là `[]`) và `subtotal`
(`MoneyResponse`, hoặc `null` khi giỏ rỗng).
`CartLineResponse` có **9 member**: `productId` (UUID), `productName` (`null` khi variant đó đã mất),
`variantId` (UUID — định danh dòng), `variantName` (`null` khi variant đã mất), `quantity` (số nguyên
≥ 1), `unitPrice` (giá snapshot lúc thêm), `lineTotal` (`quantity × unitPrice`), `buyable` (luôn có),
`availableQuantity` (**chỉ có** khi dòng còn bán nhưng thiếu — vắng mặt khi variant ngừng bán hoặc đã
xoá). `MoneyResponse` có **2 member**: `amount` (số nguyên đơn vị nhỏ nhất) và `currency` (ba chữ in
hoa). SKU **không** lộ ra cho khách.

Lỗi: `UNAUTHENTICATED` 401 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500

Ghi chú:

- Giỏ rỗng trả `data.lines: []` cùng `data.subtotal: null`. Một khách chưa từng thêm gì nhận giỏ
  rỗng, **không** phải `404`, và **không** có dòng `carts` nào được tạo.
- Một dòng còn bán và đủ hàng có `buyable: true` và **không** có `availableQuantity`. Một dòng còn
  bán nhưng `availableQuantity < quantity` (kể cả bằng 0) có `buyable: false` và mang
  `availableQuantity`. Một dòng ngừng bán hoặc variant đã xoá có `buyable: false` và **không** có
  `availableQuantity`.
- Đọc **không bao giờ** xoá dòng hay đổi `quantity`/`unitPrice`.

### 8.2 `POST /cart/items`

Thêm một **variant** với số lượng nguyên dương vào giỏ.

| | |
|---|---|
| Auth | Bearer access token (khách đã đăng nhập) |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` — giỏ sau thay đổi |

Request (`AddItemRequest`, `additionalProperties: false`)

```json
{ "variantId": "c3a2b1e5-6f7a-4c8d-9e0f-1a2b3c4d5e6f", "quantity": 2 }
```

`variantId` bắt buộc, UUID; `quantity` bắt buộc, số nguyên **≥ 1**. Thêm một sản phẩm **không kèm
`variantId`** bị từ chối `400 VALIDATION_ERROR` (`details[].field = "variantId"`). Tác nhân lấy từ
**session**, không bao giờ từ body.

Response `200`: `data` là `CartResponse` (như `8.1`).

Lỗi: `VALIDATION_ERROR` 400 (`variantId` thiếu hoặc không phải UUID, hoặc `quantity` thiếu, bằng 0,
âm hoặc không nguyên; `details[].field` chỉ đúng member) · `MALFORMED_REQUEST` 400 (body không parse
được hoặc có member lạ, kể cả member tên chủ sở hữu) · `UNAUTHENTICATED` 401 · `PRODUCT_NOT_FOUND`
404 (variant không tồn tại hoặc đã xoá) · `CART_PRODUCT_NOT_PURCHASABLE` 409 ·
`CART_QUANTITY_EXCEEDS_AVAILABLE` 409 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500

Ghi chú:

- Thêm một **variant** đã có trong giỏ nâng số lượng của dòng đó chứ **không** tạo dòng thứ hai, và
  giữ giá snapshot của lần thêm đầu.
- `409 CART_PRODUCT_NOT_PURCHASABLE` khi variant tồn tại nhưng không đang bán;
  `details[].field = "variantId"`.
- `409 CART_QUANTITY_EXCEEDS_AVAILABLE` khi số lượng **sau khi thêm** vượt tồn khả dụng;
  `details[].field = "quantity"`, `issue` nêu tồn khả dụng hiện tại.
- Bị từ chối thì **không có gì đổi**: giỏ giữ nguyên như trước request.

### 8.3 `PATCH /cart/items/{variantId}`

Đặt số lượng của dòng ứng với một variant về một giá trị mới.

| | |
|---|---|
| Auth | Bearer access token (khách đã đăng nhập) |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` — giỏ sau thay đổi |

Path: `variantId` (UUID).

Request (`QuantityRequest`, `additionalProperties: false`)

```json
{ "quantity": 5 }
```

`quantity` bắt buộc, số nguyên **≥ 1**.

Response `200`: `data` là `CartResponse` (như `8.1`).

Lỗi: `VALIDATION_ERROR` 400 (`variantId` không phải UUID, hoặc `quantity` thiếu, bằng 0, âm hoặc
không nguyên) · `MALFORMED_REQUEST` 400 · `UNAUTHENTICATED` 401 · `PRODUCT_NOT_FOUND` 404 (variant
không tồn tại/đã xoá, **hoặc** không phải một dòng của giỏ này) · `CART_PRODUCT_NOT_PURCHASABLE` 409 ·
`CART_QUANTITY_EXCEEDS_AVAILABLE` 409 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500

Ghi chú:

- Variant nay ngừng bán → `409 CART_PRODUCT_NOT_PURCHASABLE`; số lượng mới vượt tồn khả dụng hiện
  tại → `409 CART_QUANTITY_EXCEEDS_AVAILABLE`; **số lượng cũ được giữ** (không có gì đổi).
- Một `variantId` không phải dòng của giỏ đang gọi trả `404 PRODUCT_NOT_FOUND`, cùng câu trả lời như
  một variant không tồn tại — route không xác nhận nội dung giỏ của khách khác.

### 8.4 `DELETE /cart/items/{variantId}`

Xoá dòng ứng với một variant khỏi giỏ.

| | |
|---|---|
| Auth | Bearer access token (khách đã đăng nhập) |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `204 No Content` |

Path: `variantId` (UUID). Response không có body.

Lỗi: `VALIDATION_ERROR` 400 (`variantId` không phải UUID) · `UNAUTHENTICATED` 401 ·
`PRODUCT_NOT_FOUND` 404 (variant không tồn tại/đã xoá, **hoặc** không phải một dòng của giỏ này) ·
`RATE_LIMITED` 429 · `INTERNAL_ERROR` 500

Ghi chú:

- Xoá một dòng đã xoá trước đó (hoặc không thuộc giỏ này) trả `404 PRODUCT_NOT_FOUND`, không phải
  `204`; đây là cùng một câu trả lời với "không có variant như vậy".

---

## 9. Module 07 — Order (`/api/v1`)

Đơn hàng của shop: biến giỏ thành đơn, artist xác nhận đơn, vòng đời đơn, và **bàn quản trị đơn** của
operator. **Mười hai endpoint**: **năm** của khách **đã đăng nhập** dưới `/orders` — chủ sở hữu là
**session**, nên không route nào nêu định danh chủ — và **bảy** của quản trị dưới `/admin/orders`, vai
trò `ADMIN`, định địa chỉ theo **định danh đơn**.

Một đơn mang **snapshot** của thứ đã mua: mỗi dòng chụp **tên sản phẩm, mã sản phẩm, tên variant,
SKU, đơn giá, currency và số lượng** lúc checkout, cùng địa chỉ giao chụp từ địa chỉ khách chọn; nên
thứ một đơn hiển thị **không** phụ thuộc vào việc sản phẩm/variant hay địa chỉ còn tồn tại hay không.
**Mỗi cặp (mã sản phẩm, SKU) là một dòng đơn** — một sản phẩm mua hai variant cho ra **hai** dòng.
Giao diện khách hiển thị "tên sản phẩm + tên variant"; **SKU và mã sản phẩm được lưu nhưng không trả
ra** ở view của khách. Tiền là **số nguyên đơn vị nhỏ nhất + currency**; tổng đơn là tổng của các
dòng và **không** có phí ship (ADR 015 §5).

Tám điều dễ đọc sai, nói ngay:

- **Đặt hàng không giữ hàng.** Checkout tạo đơn ở `PENDING` (**chờ artist xác nhận**) và **không** giữ
  hàng: tồn khả dụng **không đổi**. Hàng chỉ được giữ khi artist **xác nhận** đơn.
- **Artist xác nhận → giữ hàng toàn bộ (all-or-nothing)** và đơn chuyển sang `PAYMENT_PENDING` (chờ
  thanh toán). Nếu bất kỳ dòng nào không đủ hàng, cả lần xác nhận bị từ chối, đơn giữ nguyên `PENDING`
  và **không** giữ dòng nào (`409 ORDER_QUANTITY_EXCEEDS_AVAILABLE`).
- **Đơn chờ thanh toán tự huỷ sau 60 phút** kể từ lúc xác nhận. Một order quá `payment_expires_at` mà
  vẫn `PAYMENT_PENDING` sẽ tự `CANCELLED` và trả hàng về khả dụng, bởi **một sweeper nền** — khách thấy
  trạng thái trên đơn, **không** nhận lỗi. Đơn còn **chờ artist xác nhận** (`PENDING`) **không** hết
  hạn theo thời gian. Xem `9.1`, `9.5`.
- **Khách sửa được đơn trước khi trả tiền** (`PUT /orders/{orderId}`, `9.4`) khi đơn đang `PENDING` hoặc
  `PAYMENT_PENDING`. Sửa đơn đang `PAYMENT_PENDING` **trả hàng** và đưa đơn **quay lại `PENDING`**
  (artist phải xác nhận lại); sửa đơn `PENDING` giữ nguyên `PENDING`.
- **Trạng thái đơn đi qua endpoint riêng** (`confirm`, `reject`, `cancel`, `ship`, `complete`), **không**
  qua một trường `status` trên `PATCH`. Một bước chuyển có thể bị từ chối và câu từ chối nêu trạng thái
  hiện tại (`409 ORDER_STATE_TRANSITION_INVALID`).
- **Không có endpoint `pay`.** Bước chuyển `PAYMENT_PENDING → PAID` **không** có route HTTP trên đơn; nó do
  **module 08 Payment** điều khiển — nay đã có: webhook PayOS (`10.4`) gọi bước đã trả sau khi xác minh và
  đối chiếu. Khách trả tiền qua `POST /orders/{orderId}/payment` (`10.1`).
- **Chuyển nhượng không phải một trạng thái**: nó chỉ đổi **chủ sở hữu** sang một tài khoản khác đã
  tồn tại (nêu bằng **email**), giữ nguyên dòng, trạng thái và tổng tiền, và **không** đổi tồn kho.
  Một đơn chưa trả tiền **không** chuyển nhượng được.
- Module **không** có hạn mức riêng, chỉ chịu hạn mức toàn cục (§1.5). Thao tác của khách (checkout,
  sửa, huỷ) và của hệ thống (đã trả tiền, hết hạn) **không** ghi `audit_logs`; chỉ
  `confirm`/`reject`/`ship`/`complete`/`transfer` của admin mới ghi (§1.4).

Hai danh sách (`GET /orders`, `GET /admin/orders`) phân trang theo quy ước chung: `page` mặc định `1`,
`pageSize` mặc định `20`, khoảng `1..100`, **mới nhất trước** (`createdAt`, rồi `id`). Danh sách quản
trị nhận thêm hai tham số **tuỳ chọn**: `status` (lọc theo một trong sáu trạng thái) và `sort`
(`newest` mặc định, hoặc `oldest`) — `status=PENDING&sort=oldest` là **hàng đợi xác nhận FIFO** của
artist (`GET /admin/orders`, `9.6`).

**Hình dạng khách** — `OrderSummaryResponse` (danh sách, 5 member) và `OrderResponse` (chi tiết, 7
member = summary + `address` + `lines`). Mỗi phần tử `lines` là `OrderLineResponse` (8 member:
`productId`, `productName`, `variantId`, `variantName`, `quantity`, `unitPrice`, `lineTotal`,
`position`); `productId`/`variantId` là **bản ghi**, không phải tham chiếu sống.

```json
{
  "id": "0f5c6e0c-1a44-4a1e-9b3d-9a1b2c3d4e5f",
  "status": "PENDING",
  "total": { "amount": 300000, "currency": "VND" },
  "itemCount": 2,
  "createdAt": "2026-10-09T08:15:04Z",
  "address": {
    "recipientName": "Nguyễn Thị An",
    "recipientPhone": "0912345678",
    "provinceCode": "01",
    "provinceName": "Hà Nội",
    "wardCode": "00004",
    "wardName": "Ba Đình",
    "streetAddress": "12 Ngõ 129 Dịch Vọng"
  },
  "lines": [
    {
      "productId": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e",
      "productName": "Tranh sơn dầu",
      "variantId": "c3a2b1e5-6f7a-4c8d-9e0f-1a2b3c4d5e6f",
      "variantName": "Bản lớn",
      "quantity": 2,
      "unitPrice": { "amount": 150000, "currency": "VND" },
      "lineTotal": { "amount": 300000, "currency": "VND" },
      "position": 0
    }
  ]
}
```

**Hình dạng quản trị** — `AdminOrderSummaryResponse` (danh sách, 6 member = summary + `userId`) và
`AdminOrderResponse` (chi tiết, 8 member = `OrderResponse` + `userId`). **Khác khách đúng một member
`userId`** (chủ sở hữu đơn).

**Sáu trạng thái đơn** — `status` là một trong sáu giá trị:

| Giá trị | Nghĩa | Vào bằng |
|---|---|---|
| `PENDING` | Chờ artist xác nhận; **chưa** giữ hàng | Checkout tạo đơn |
| `PAYMENT_PENDING` | Chờ thanh toán; hàng đang **giữ** | Artist `confirm` |
| `PAID` | Đã trả tiền; giữ chỗ thành **bán** | Module 08 xác nhận thanh toán (không có HTTP) |
| `SHIPPED` | Đã giao | Admin `ship` |
| `COMPLETED` | Hoàn tất (cuối) | Admin `complete` |
| `CANCELLED` | Đã huỷ/từ chối/hết hạn (cuối) | Khách `cancel`, admin `reject`, hoặc sweeper |

> `PENDING_PAYMENT` của feature 009 **đã được đổi tên** thành `PAYMENT_PENDING`; không còn giá trị enum
> nào tên `PENDING_PAYMENT`. Đơn vẫn **không** có trường trạng thái thanh toán riêng; `PAID` chính là
> đã xác nhận thanh toán.

### 9.1 `POST /orders`

Checkout: biến giỏ của khách đang đăng nhập thành đơn **chờ artist xác nhận** (`PENDING`). Mỗi dòng
được **đối chiếu lại** với trạng thái bán và giá hiệu lực hiện tại của **variant** và với tồn khả
dụng; cả lần checkout bị từ chối nếu bất kỳ dòng nào không đạt. Thành công thì giỏ được **làm rỗng**
và đơn trả về ở trạng thái `PENDING`; **không** có hàng nào bị giữ (tồn khả dụng **không đổi**) — hàng
chỉ được giữ khi artist **xác nhận** (`9.8`), tất cả trong **một transaction**.

| | |
|---|---|
| Auth | Bearer access token (khách đã đăng nhập) |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `201 Created` |

Request (`CheckoutRequest`, `additionalProperties: false`)

```json
{ "addressId": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e" }
```

`addressId` **tuỳ chọn** — bỏ trống hoặc `null` thì dùng **địa chỉ mặc định** của khách. Tác nhân lấy
từ **session**, không bao giờ từ body.

Response `201`: `data` = `OrderResponse` với `status: "PENDING"` (như trên).

Lỗi: `VALIDATION_ERROR` 400 (`addressId` không phải UUID, hoặc không phải một địa chỉ của khách —
`details[].field = "addressId"`) · `MALFORMED_REQUEST` 400 (body không parse được hoặc có member lạ) ·
`UNAUTHENTICATED` 401 · `ORDER_CART_EMPTY` 409 · `ORDER_ITEM_NOT_PURCHASABLE` 409 ·
`ORDER_ITEM_PRICE_CHANGED` 409 · `ORDER_QUANTITY_EXCEEDS_AVAILABLE` 409 · `ORDER_NO_ADDRESS` 409 ·
`RATE_LIMITED` 429 · `INTERNAL_ERROR` 500

Ghi chú:

- `total` **bằng đúng** tổng `quantity × unitPrice` của các dòng, ở đơn vị nhỏ nhất của currency,
  **không** dùng số thực, **không** làm tròn, và **không** cộng phí ship (FR-008, ADR 015 §5).
- Ba mã `ORDER_ITEM_NOT_PURCHASABLE`, `ORDER_ITEM_PRICE_CHANGED`, `ORDER_QUANTITY_EXCEEDS_AVAILABLE`
  nêu món gây lỗi ở `details[].field = "variantId"`; riêng mã số lượng, `issue` nêu số còn khả dụng.
- **Không** giữ hàng ở bước này: số **vật lý** và số **khả dụng** đều **không đổi**. Việc giữ hàng là
  của module 05 và xảy ra ở bước artist xác nhận (`9.8`); đơn `PENDING` mang **không** hạn thanh toán.
- Checkout là **một** thao tác: nếu bất kỳ bước nào hỏng, **không** có đơn, giỏ nguyên vẹn. Hai lần
  checkout **đồng thời** một giỏ chỉ sinh **đúng một** đơn; lần thứ hai nhận `409 ORDER_CART_EMPTY` vì
  giỏ đã rỗng.
- Artist được **thông báo** (email) rằng có một đơn cần xác nhận (gửi là best-effort, lỗi gửi
  **không** làm hỏng checkout).

### 9.2 `GET /orders`

Danh sách đơn của chính khách đang đăng nhập, **mới nhất trước**, phân trang.

| | |
|---|---|
| Auth | Bearer access token (khách đã đăng nhập) |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` |

Query: `page` (mặc định `1`, tối thiểu `1`), `pageSize` (mặc định `20`, khoảng `1..100`).

Response `200`: `data` là mảng `OrderSummaryResponse`; `meta` là khối phân trang chung. Khách chưa có
đơn nào trả `data: []`, **không** phải `null` và **không** phải lỗi.

Lỗi: `VALIDATION_ERROR` 400 (`page` / `pageSize` sai định dạng hoặc ngoài khoảng; `details[].field`
là `"page"` hoặc `"pageSize"`) · `UNAUTHENTICATED` 401 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500

### 9.3 `GET /orders/{orderId}`

Đọc một đơn của chính khách, đầy đủ địa chỉ và các dòng.

| | |
|---|---|
| Auth | Bearer access token (khách đã đăng nhập) |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` |

Path: `orderId` (UUID). Response `200`: `data` = `OrderResponse`.

Lỗi: `VALIDATION_ERROR` 400 (`orderId` không phải UUID) · `UNAUTHENTICATED` 401 ·
`ORDER_NOT_FOUND` 404 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500

> **Đơn không tồn tại, đơn của khách khác và định danh không phải UUID**: định danh sai hình dạng trả
> `400 VALIDATION_ERROR`; còn đơn không tồn tại **hoặc** thuộc khách khác trả **cùng** `404
> ORDER_NOT_FOUND`. Nếu có mã riêng cho "tồn tại nhưng của người khác", bất kỳ khách đã đăng nhập nào
> cũng dò được một đơn có thật trong hệ thống (FR-020).

### 9.4 `PUT /orders/{orderId}`

Sửa một đơn của chính khách khi đơn đang **chờ artist xác nhận** (`PENDING`) hoặc **chờ thanh toán**
(`PAYMENT_PENDING`): thay **toàn bộ** tập dòng (thêm/bớt/đổi số lượng) và, khi nêu, **địa chỉ giao**
(chọn từ địa chỉ đã lưu bằng `addressId`, như checkout). Mỗi dòng được **đối chiếu lại** và **chụp lại
giá hiện tại**; tổng được **tính lại** và **phiên bản nội dung** của đơn tăng. Sửa đơn đang
`PAYMENT_PENDING` **trả hàng** đã giữ và đưa đơn **quay lại `PENDING`** (artist phải xác nhận lại, và
xác nhận/phiên thanh toán cũ mất hiệu lực); sửa đơn `PENDING` giữ nguyên `PENDING`.

| | |
|---|---|
| Auth | Bearer access token (khách đã đăng nhập) |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` — đơn sau khi sửa |

Path: `orderId` (UUID).

Request (`EditOrderRequest`, `additionalProperties: false`)

```json
{
  "addressId": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e",
  "lines": [
    { "variantId": "c3a2b1e5-6f7a-4c8d-9e0f-1a2b3c4d5e6f", "quantity": 3 }
  ]
}
```

`lines` **bắt buộc** và **thay thế toàn bộ** tập dòng; mỗi phần tử là `EditLineRequest` (`variantId`,
`quantity ≥ 1`). `addressId` **tuỳ chọn** — bỏ trống hoặc `null` thì **giữ nguyên** địa chỉ hiện tại của
đơn. Tác nhân lấy từ **session**, không bao giờ từ body.

Response `200`: `data` = `OrderResponse`; `status` là `PENDING` sau khi sửa (một đơn vốn `PAYMENT_PENDING`
cũng về `PENDING`).

Lỗi: `VALIDATION_ERROR` 400 (`addressId` không phải UUID hoặc không phải địa chỉ của khách; `variantId`
không phải UUID; `quantity < 1` — `details[].field` nêu member) · `MALFORMED_REQUEST` 400 (body không
parse được hoặc có member lạ) · `UNAUTHENTICATED` 401 · `ORDER_NOT_FOUND` 404 · `ORDER_NOT_EDITABLE`
409 (đơn đã trả tiền hoặc xa hơn) · `ORDER_EMPTY` 409 (tập dòng rỗng) · `ORDER_ITEM_NOT_PURCHASABLE`
409 · `ORDER_QUANTITY_EXCEEDS_AVAILABLE` 409 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500

Ghi chú:

- Đơn **đã trả tiền** (hoặc xa hơn) **không** sửa được → `409 ORDER_NOT_EDITABLE`; một tập dòng để đơn
  **còn 0 dòng** → `409 ORDER_EMPTY` (khách muốn bỏ hết thì **huỷ đơn**, `9.5`).
- Mỗi dòng sửa được **chụp lại giá hiện tại** (khách chọn lại món), nên một lần đổi giá kể từ lúc đặt
  **không** bị từ chối — khác checkout, nơi giá trong giỏ là giá khách đã thấy.
- Sửa đơn `PAYMENT_PENDING` trả hàng về khả dụng **đúng một lần** và **bỏ** hạn thanh toán; đơn về
  `PENDING`. Mọi lần sửa ghi một dòng **lịch sử sửa** (nội dung trước/sau, người sửa, thời điểm) và
  tăng **phiên bản** đơn (phiên bản là seam nội bộ cho module 08, **không** lộ ra response).
- Mọi lần sửa đều **thành công** dù nội dung y hệt: vẫn tăng phiên bản và ghi lịch sử, không đổi trạng
  thái và không đụng tồn.
- Artist được **thông báo** (email) rằng đơn cần xác nhận lại (gửi best-effort, không làm hỏng sửa).

### 9.5 `POST /orders/{orderId}/cancel`

Huỷ một đơn của chính khách **chưa trả tiền** — cả **chờ artist xác nhận** (`PENDING`) và **chờ thanh
toán** (`PAYMENT_PENDING`) — và trả hàng đã giữ về khả dụng. Đơn **đã trả tiền không huỷ được** — nó
được **chuyển nhượng** (`9.12`).

| | |
|---|---|
| Auth | Bearer access token (khách đã đăng nhập) |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` — đơn sau khi huỷ |

Path: `orderId` (UUID). Request: không có body. Response `200`: `data` = `OrderResponse` với
`status: "CANCELLED"`.

Lỗi: `VALIDATION_ERROR` 400 (`orderId` không phải UUID) · `UNAUTHENTICATED` 401 ·
`ORDER_NOT_FOUND` 404 · `ORDER_STATE_TRANSITION_INVALID` 409 (message nêu trạng thái hiện tại) ·
`RATE_LIMITED` 429 · `INTERNAL_ERROR` 500

Ghi chú:

- Huỷ đơn `PAYMENT_PENDING` trả hàng về khả dụng **không** đổi số **vật lý**, áp dụng **đúng một lần**;
  huỷ đơn `PENDING` **không** có hàng nào để trả (chưa từng giữ). Một lần huỷ thứ hai nhận `409
  ORDER_STATE_TRANSITION_INVALID` nêu `CANCELLED`.
- Đơn `PAYMENT_PENDING` quá hạn tự về `CANCELLED` bởi sweeper (`9.1`); một lần huỷ sau đó cũng nhận
  `409`, cùng câu trả lời như huỷ hai lần.
- Khách được **thông báo** (email) khi đơn đổi trạng thái (gửi best-effort).

### 9.6 `GET /admin/orders`

Danh sách **mọi** đơn, kèm chủ sở hữu, trạng thái và tổng tiền, phân trang. **Mới nhất trước** theo mặc
định; hai tham số **tuỳ chọn** `status` (lọc một trong sáu trạng thái) và `sort` (`newest` mặc định,
hoặc `oldest`) khiến `status=PENDING&sort=oldest` thành **hàng đợi xác nhận FIFO** của artist.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` |

Query: `page`/`pageSize` như `9.2`, cộng `status` và `sort` (cả hai **tuỳ chọn**). Response `200`: `data`
là mảng `AdminOrderSummaryResponse`; `meta` là khối phân trang chung.

Lỗi: `VALIDATION_ERROR` 400 (`page`/`pageSize` ngoài khoảng; `status` không thuộc sáu trạng thái; `sort`
khác `newest`/`oldest` — `details[].field` là `"page"`, `"pageSize"`, `"status"` hoặc `"sort"`) ·
`UNAUTHENTICATED` 401 · `FORBIDDEN` 403 (ghi `audit_logs` với action `AUTH_PRIVILEGE_DENIED`) ·
`RATE_LIMITED` 429 · `INTERNAL_ERROR` 500

### 9.7 `GET /admin/orders/{orderId}`

Đọc một đơn bất kỳ, đầy đủ, kèm chủ sở hữu.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` |

Path: `orderId` (UUID). Response `200`: `data` = `AdminOrderResponse`.

Lỗi: `VALIDATION_ERROR` 400 (`orderId` không phải UUID) · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 ·
`ORDER_NOT_FOUND` 404 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500

### 9.8 `POST /admin/orders/{orderId}/confirm`

Artist **xác nhận** một đơn đang **chờ artist xác nhận** (`PENDING`): hệ thống **giữ toàn bộ** các dòng
trong **một bước all-or-nothing** (nếu bất kỳ dòng nào không giữ được, cả lần xác nhận bị từ chối, đơn
giữ nguyên `PENDING` và **không** giữ dòng nào), đặt mốc xác nhận, **mở hạn thanh toán 60 phút** và đưa
đơn sang **chờ thanh toán** (`PAYMENT_PENDING`). Ghi `audit_logs` action `ORDER_CONFIRMED`, và **thông
báo** khách (email) khi trạng thái đổi.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` — đơn ở trạng thái mới |

Path: `orderId` (UUID). Request: không có body. Response `200`: `data` = `AdminOrderResponse` với
`status: "PAYMENT_PENDING"`.

Lỗi: `VALIDATION_ERROR` 400 (`orderId` không phải UUID) · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 ·
`ORDER_NOT_FOUND` 404 · `ORDER_STATE_TRANSITION_INVALID` 409 (đơn không ở `PENDING`; message nêu trạng
thái hiện tại) · `ORDER_QUANTITY_EXCEEDS_AVAILABLE` 409 (một dòng không giữ được; `details[].field =
"variantId"`) · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500

Ghi chú:

- **Chỉ khi artist xác nhận** hàng mới được giữ: số **vật lý** không đổi, số **khả dụng** giảm.
- Giữ hàng là **all-or-nothing**: hai đơn tranh đơn vị cuối cùng thì **nhiều nhất một** đơn được xác
  nhận; đơn kia bị từ chối và **không** oversell.
- Xác nhận một đơn **không** ở `PENDING` (đã xác nhận, đã huỷ, đã trả tiền) → `409
  ORDER_STATE_TRANSITION_INVALID` nêu trạng thái hiện tại; đơn **nguyên vẹn**.

### 9.9 `POST /admin/orders/{orderId}/reject`

Artist **từ chối** một đơn đang **chờ artist xác nhận** (`PENDING`); đơn chuyển sang **đã huỷ**
(`CANCELLED`). Không có hàng nào đã bị giữ nên **không** cần trả. Ghi `audit_logs` action
`ORDER_REJECTED`, và **thông báo** khách (email) khi trạng thái đổi.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` — đơn sau khi từ chối |

Path: `orderId` (UUID). Request: không có body. Response `200`: `data` = `AdminOrderResponse` với
`status: "CANCELLED"`.

Lỗi: `VALIDATION_ERROR` 400 (`orderId` không phải UUID) · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 ·
`ORDER_NOT_FOUND` 404 · `ORDER_STATE_TRANSITION_INVALID` 409 (đơn không ở `PENDING`; message nêu trạng
thái hiện tại) · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500

Ghi chú: từ chối một đơn không ở `PENDING` → `409 ORDER_STATE_TRANSITION_INVALID` nêu trạng thái hiện
tại; đơn **nguyên vẹn**, tồn kho **không đổi**.

### 9.10 `POST /admin/orders/{orderId}/ship`

Đưa một đơn **đã trả tiền** sang **đã giao** và ghi lại thao tác.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` — đơn ở trạng thái mới |

Path: `orderId` (UUID). Request: không có body. Response `200`: `data` = `AdminOrderResponse` với
`status: "SHIPPED"`.

Lỗi: `VALIDATION_ERROR` 400 (`orderId` không phải UUID) · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 ·
`ORDER_NOT_FOUND` 404 · `ORDER_STATE_TRANSITION_INVALID` 409 (message nêu trạng thái hiện tại) ·
`RATE_LIMITED` 429 · `INTERNAL_ERROR` 500

Ghi chú: một bước chuyển bị từ chối **không đổi gì**; ghi `audit_logs` với action `ORDER_SHIPPED`.

### 9.11 `POST /admin/orders/{orderId}/complete`

Đưa một đơn **đã giao** sang **hoàn tất** (`COMPLETED`, trạng thái cuối) và ghi lại thao tác.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` — đơn ở trạng thái mới |

Path: `orderId` (UUID). Request: không có body. Response `200`: `data` = `AdminOrderResponse` với
`status: "COMPLETED"`.

Lỗi: `VALIDATION_ERROR` 400 (`orderId` không phải UUID) · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 ·
`ORDER_NOT_FOUND` 404 · `ORDER_STATE_TRANSITION_INVALID` 409 (message nêu trạng thái hiện tại) ·
`RATE_LIMITED` 429 · `INTERNAL_ERROR` 500

Ghi chú: `COMPLETED` là **cuối** và không có đường ra; ghi `audit_logs` với action `ORDER_COMPLETED`.

### 9.12 `POST /admin/orders/{orderId}/transfer`

Chuyển một đơn **đã trả tiền** cho một tài khoản khác **đã tồn tại**, nêu bằng **email**. Chỉ **chủ
sở hữu** đổi; dòng, trạng thái và tổng tiền giữ nguyên, và **không** đổi tồn kho.

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` — đơn, nay thuộc tài khoản nhận |

Path: `orderId` (UUID).

Request (`TransferRequest`, `additionalProperties: false`)

```json
{ "email": "recipient@example.com" }
```

`email` **bắt buộc**, là email tài khoản nhận; không tài khoản nào mang email đó thì từ chối.

Response `200`: `data` = `AdminOrderResponse` (với `userId` là chủ mới).

Lỗi: `VALIDATION_ERROR` 400 (`email` thiếu hoặc không phải email —
`details[].field = "email"`) · `MALFORMED_REQUEST` 400 (body không parse được hoặc có member lạ) ·
`UNAUTHENTICATED` 401 · `FORBIDDEN` 403 · `ORDER_NOT_FOUND` 404 (đơn không tồn tại) ·
`ORDER_TRANSFER_TARGET_NOT_FOUND` 404 (không tài khoản nào mang email đó) ·
`ORDER_NOT_TRANSFERABLE` 409 (đơn chưa trả tiền) · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500

Ghi chú:

- Chuyển nhượng **thay cho** huỷ một đơn đã trả tiền (hệ thống không có luồng hoàn tiền); vì vậy nó
  áp dụng **chỉ** cho đơn `PAID`, và một đơn chưa trả tiền nhận `409 ORDER_NOT_TRANSFERABLE`.
- Chuyển nhượng **không** đụng tồn kho: không số **vật lý** lẫn **khả dụng** nào đổi.
- Chuyển nhượng hai lần cho cùng một tài khoản là **idempotent** về hiệu quả (chủ đã là người nhận);
  không có mã lỗi riêng cho việc này.
- Ghi `audit_logs` với action `ORDER_TRANSFERRED`, nêu định danh đơn và administrator.

---

## 10. Module 08 — Payment (`/api/v1`)

Khách trả tiền online cho một đơn **đang chờ thanh toán** qua cổng **PayOS**: hệ thống tạo yêu cầu thanh
toán cho **đúng tổng tiền hàng**, trả về **liên kết thanh toán/QR**, và **tin cậy xác nhận của cổng đúng
một lần**. Cổng báo kết quả qua **webhook có chữ ký**; hệ thống xác minh chữ ký, đối chiếu đơn + số tiền
rồi áp kết quả — đơn chuyển `PAID` và hàng giữ chỗ thành **bán**. Một xác nhận **không áp được** (sai số
tiền, nội dung đơn cũ, đơn đã huỷ/hết hạn) **không** được ghi nhận: bản ghi thanh toán thành `UNMATCHED`
để operator **đối soát**; V1.0 **không có hoàn tiền** (ADR 015 §5, ADR 019).

**Bốn endpoint**: **hai** của khách đã đăng nhập dưới `/orders/{orderId}/payment` (bắt đầu/dùng lại và đọc
thanh toán của **chính** đơn mình), **một** của quản trị (`GET /admin/payments`, vai trò `ADMIN`), và
**một công khai** — webhook PayOS (`POST /payments/payos/webhook`, không cần phiên, **chữ ký là xác thực**).

Module **không** có hạn mức riêng, chỉ chịu hạn mức toàn cục (§1.5); webhook là công khai và PayOS **thử
lại tới khi nhận `2xx`**, nên một sự kiện đã xử lý trả `2xx` nhanh.

Sáu điều dễ đọc sai, nói ngay:

- **Chủ sở hữu lấy từ session**, không bao giờ từ body/query. Đơn của khách khác trả **cùng**
  `404 PAYMENT_ORDER_NOT_FOUND`.
- **Một yêu cầu còn hiệu lực thì dùng lại**: bắt đầu lần thứ hai cho cùng một đơn trả lại **chính liên
  kết/QR đó**, **không** tạo yêu cầu mới.
- **Tiền là tổng đơn, số nguyên đơn vị nhỏ nhất + currency, không phí ship** (ADR 015 §5).
- **Cổng lỗi khi tạo yêu cầu → đơn không đổi** (`422 PAYMENT_PROVIDER_ERROR`, thử lại được).
- **Xác nhận cũ/không khớp không bao giờ bị áp**: bản ghi thành `UNMATCHED` kèm lý do, đơn giữ nguyên,
  **không** hoàn tiền; webhook vẫn trả `2xx`.
- **Huỷ đơn chưa trả → huỷ yêu cầu ở cổng** (nỗ lực tối đa, `9.5`); việc nhả hàng là của module 07, lỗi
  cổng **không** chặn huỷ đơn — không có bề mặt HTTP riêng cho việc huỷ này.
- **Trạng thái thanh toán không nằm trong response của đơn**: đọc qua đường thanh toán riêng.

**Hình dạng khách** — `PaymentResponse` (11 member):

```json
{
  "id": "0f5c6e0c-1a44-4a1e-9b3d-9a1b2c3d4e5f",
  "orderId": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e",
  "amount": { "amount": 300000, "currency": "VND" },
  "status": "PENDING",
  "provider": "payos",
  "orderCode": 123456789012345,
  "checkoutUrl": "https://pay.payos.vn/web/abc123",
  "qrCode": "00020101021238540010A000000727...",
  "unmatchedReason": "",
  "createdAt": "2026-10-11T08:15:04Z",
  "updatedAt": "2026-10-11T08:15:04Z"
}
```

**Hình dạng quản trị** — `AdminPaymentResponse` = `PaymentResponse` **+** `userId` **+**
`providerReference` (13 member). **Khác khách đúng hai member.**

**Bốn trạng thái thanh toán** — `status` là một trong bốn giá trị:

| Giá trị | Nghĩa | Vào bằng |
|---|---|---|
| `PENDING` | Liên kết còn hiệu lực, khách có thể trả; **không** terminal | Bắt đầu thanh toán (`10.1`) |
| `PAID` | Xác nhận hợp lệ đã được áp; hàng giữ chỗ thành **bán** (cuối) | Webhook hợp lệ + khớp (`10.4`) |
| `CANCELLED` | Đơn bị huỷ nên yêu cầu ở cổng cũng bị huỷ (cuối) | Huỷ đơn `PAYMENT_PENDING` (`9.5`) |
| `UNMATCHED` | Xác nhận không áp được, chờ operator **đối soát** (cuối) | Webhook hợp lệ nhưng sai tiền / nội dung đơn cũ / đơn không còn chờ thanh toán |

> `checkoutUrl`, `qrCode` và `unmatchedReason` **luôn** có mặt trong JSON: đây là ba trường `string`
> không dùng `omitempty`, nên khi không áp dụng chúng là **chuỗi rỗng** (`""`), không phải `null` hay vắng
> mặt. Client nên coi `""` là "không có giá trị".

### 10.1 `POST /orders/{orderId}/payment`

Bắt đầu (hoặc **dùng lại**) một thanh toán online cho đơn **của chính khách** đang **chờ thanh toán**
(`PAYMENT_PENDING`). Tạo yêu cầu thanh toán cho **đúng tổng đơn** ở cổng PayOS và trả về liên kết/QR. Nếu
đã có một yêu cầu **còn hiệu lực** cho đơn, nó được **dùng lại** và trả về **chính liên kết đó** — **không**
tạo yêu cầu thứ hai (FR-005).

| | |
|---|---|
| Auth | Bearer access token (khách đã đăng nhập) |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` |

Path: `orderId` (UUID). Request: **không** có body.

Response `200`: `data` = `PaymentResponse` với `status: "PENDING"` và, khi liên kết còn hiệu lực,
`checkoutUrl` + `qrCode`.

Lỗi: `VALIDATION_ERROR` 400 (`orderId` không phải UUID) · `UNAUTHENTICATED` 401 ·
`PAYMENT_ORDER_NOT_FOUND` 404 · `PAYMENT_ORDER_NOT_PAYABLE` 409 · `PAYMENT_ALREADY_LIVE` 409 ·
`PAYMENT_PROVIDER_ERROR` 422 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500

Ghi chú:

- Số tiền = tổng đơn, **không** phí ship; đơn vị nhỏ nhất + currency.
- Yêu cầu còn hiệu lực nhưng liên kết **không còn trả được** (đã huỷ/hết hạn ở cổng) → `409
  PAYMENT_ALREADY_LIVE` để operator đối soát, **không** tạo liên kết thứ hai.
- Cổng lỗi → `422 PAYMENT_PROVIDER_ERROR`, **không** có gì được tạo, đơn **không** đổi; thử lại được.
- Tác nhân lấy từ **session**, không bao giờ từ body.

### 10.2 `GET /orders/{orderId}/payment`

Đọc thanh toán **mới nhất** của đơn **của chính khách**, ở **bất kỳ** trạng thái — đọc được cả `PAID`,
`CANCELLED` và `UNMATCHED`, không chỉ thanh toán còn hiệu lực (FR-017).

| | |
|---|---|
| Auth | Bearer access token (khách đã đăng nhập) |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` |

Path: `orderId` (UUID). Response `200`: `data` = `PaymentResponse`.

Lỗi: `VALIDATION_ERROR` 400 (`orderId` không phải UUID) · `UNAUTHENTICATED` 401 ·
`PAYMENT_ORDER_NOT_FOUND` 404 · `PAYMENT_NOT_FOUND` 404 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500

Ghi chú:

- **Hai tình huống 404 khác nhau**: đơn không tồn tại **hoặc** của khách khác → `PAYMENT_ORDER_NOT_FOUND`;
  đơn của khách nhưng **chưa có thanh toán nào** → `PAYMENT_NOT_FOUND`.
- Đây là **đường đọc riêng** cho thanh toán; trạng thái thanh toán **không** nằm trong response của đơn
  (`9.3`).

### 10.3 `GET /admin/payments`

Danh sách **mọi** bản ghi thanh toán, **mới nhất trước**, phân trang, kèm chủ sở hữu và tham chiếu giao
dịch của cổng. Tham số `status` **tuỳ chọn** lọc theo một trong bốn trạng thái — `status=UNMATCHED` là
**hàng đợi đối soát** (FR-018).

| | |
|---|---|
| Auth | Bearer access token, role `ADMIN` |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` |

Query: `page` (mặc định `1`, tối thiểu `1`), `pageSize` (mặc định `20`, khoảng `1..100`) và `status`
(**tuỳ chọn**, một trong bốn trạng thái). Response `200`: `data` là mảng `AdminPaymentResponse`; `meta`
là khối phân trang chung. Không có thanh toán nào trả `data: []`, **không** phải `null`.

Lỗi: `VALIDATION_ERROR` 400 (`page`/`pageSize` ngoài khoảng; `status` không thuộc bốn trạng thái —
`details[].field` là `"page"`, `"pageSize"` hoặc `"status"`) · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403
(ghi `audit_logs` với action `AUTH_PRIVILEGE_DENIED`) · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500

Ghi chú: danh sách **chỉ để xem** (review-only) — V1.0 **không** có thao tác "áp tay" cho một khoản
`UNMATCHED`; operator xử lý ngoài hệ thống.

### 10.4 `POST /payments/payos/webhook`

Webhook **công khai** của PayOS. Không cần phiên: **chữ ký là xác thực**, xác minh trên **đúng byte nhận
được** trước khi tin bất cứ thứ gì. Sự kiện đã xử lý — áp, đã áp, hoặc ghi `UNMATCHED` — trả `2xx` để
PayOS ngừng thử lại (FR-019).

| | |
|---|---|
| Auth | Không (công khai; xác minh bằng chữ ký PayOS) |
| Rate limit | Toàn cục (module không có hạn mức riêng) |
| Trả về | `200 OK` (sự kiện đã xử lý) |

Request (`PayOSWebhook`): envelope `{code, desc, success, data, signature}`; `data` gồm `orderCode`,
`amount`, `description`, `reference` (tham chiếu giao dịch — khoá chống trùng), `currency`,
`paymentLinkId` và các trường khác. `signature` = **HMAC_SHA256** với checksum key trên **các trường của
`data` sắp theo alphabet**.

Lỗi: `MALFORMED_REQUEST` 400 (body không parse được) · `PAYMENT_WEBHOOK_INVALID_SIGNATURE` 401 ·
`PAYMENT_NOT_FOUND` 404 (`orderCode` không bản ghi nào mang; **không gì được ghi**, chỉ log warning) ·
`RATE_LIMITED` 429 · `INTERNAL_ERROR` 500

Ghi chú:

- Sai số tiền / nội dung đơn cũ (khác `order_version`) / đơn không còn chờ thanh toán → `200`, bản ghi
  thành `UNMATCHED` kèm lý do, **đơn không đổi**.
- Gửi lặp một xác nhận trên thanh toán đã `PAID` → `200`, **không** hiệu ứng thứ hai.
- `PAYMENT_NOT_FOUND` khi không có bản ghi nào mang `orderCode` đó (link mồ côi); **chỉ** log warning, để
  operator đối soát ngoài hệ thống.

---

## 11. Bảng tổng hợp

| Method | Path | Auth | Mô tả |
|---|---|---|---|
| GET | `/healthz` | — | Liveness |
| GET | `/readyz` | — | Readiness (db + schema + redis) |
| POST | `/api/v1/auth/register` | — | Bắt đầu đăng ký: lưu tạm, gửi OTP (202) |
| POST | `/api/v1/auth/register/verify-otp` | — | Xác nhận OTP, tạo tài khoản (200) |
| POST | `/api/v1/auth/register/resend-otp` | — | Gửi lại OTP (202) |
| POST | `/api/v1/auth/register/cancel` | — | Huỷ đăng ký tạm (200) |
| POST | `/api/v1/auth/login` | — | Đăng nhập, cấp token |
| POST | `/api/v1/auth/refresh` | — | Xoay vòng refresh token |
| POST | `/api/v1/auth/logout` | — | Thu hồi phiên hiện tại (204) |
| POST | `/api/v1/auth/password/forgot` | — | Gửi mã OTP đặt lại mật khẩu (202) |
| POST | `/api/v1/auth/password/forgot/verify-otp` | — | Xác minh mã, cấp reset token dùng một lần (200) |
| POST | `/api/v1/auth/password/reset` | — | Đặt lại mật khẩu bằng reset token; thành công, **không** cấp phiên (200) |
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
| PATCH | `/api/v1/admin/categories/{categoryId}` | ADMIN | Sửa một phần danh mục (`409 CATEGORY_HAS_PENDING_ORDERS` khi ẩn mà còn đơn chờ — feature 016) |
| DELETE | `/api/v1/admin/categories/{categoryId}` | ADMIN | Xoá danh mục (204; `409 CATEGORY_IN_USE` nếu còn sản phẩm — feature 006) |
| GET | `/api/v1/products` | — | Catalogue: **một mục mỗi sản phẩm** khách thấy được, giá thấp nhất trong variant đang bán (có phân trang, lọc `?category=`) |
| GET | `/api/v1/products/{code}` | — | Chi tiết sản phẩm theo `code` (đoạn link công khai; kèm danh sách variant hiển thị + trạng thái còn hàng) |
| GET | `/api/v1/admin/products` | ADMIN | Toàn bộ sản phẩm, kể cả không hiển thị (có phân trang) |
| POST | `/api/v1/admin/products` | ADMIN | Tạo sản phẩm **kèm ≥1 variant** (`201`) |
| GET | `/api/v1/admin/products/{id}` | ADMIN | Chi tiết sản phẩm, kể cả không hiển thị, kèm ảnh + mọi variant |
| PATCH | `/api/v1/admin/products/{id}` | ADMIN | Sửa một phần sản phẩm (không đụng variant; `409 PRODUCT_HAS_PENDING_ORDERS` khi tắt hiển thị mà còn đơn chờ — feature 016) |
| DELETE | `/api/v1/admin/products/{id}` | ADMIN | Xoá cứng sản phẩm và các variant (`204`) |
| POST | `/api/v1/admin/products/{id}/variants` | ADMIN | Thêm variant (`201`) |
| PATCH | `/api/v1/admin/products/{id}/variants/{variantId}` | ADMIN | Sửa một phần variant (`price` ba trạng thái) |
| DELETE | `/api/v1/admin/products/{id}/variants/{variantId}` | ADMIN | Gỡ variant (`204`; `409 VARIANT_LAST_REMOVAL` nếu là cái cuối) |
| POST | `/api/v1/admin/products/{id}/variants/{variantId}/image` | ADMIN | Tải ảnh variant (multipart, ≤ 2 MB) (`201`) |
| DELETE | `/api/v1/admin/products/{id}/variants/{variantId}/image` | ADMIN | Gỡ ảnh variant (`204`) |
| POST | `/api/v1/admin/products/{id}/variants/{variantId}/state` | ADMIN | Chuyển trạng thái bán của variant (`409 VARIANT_HAS_PENDING_ORDERS` khi rời trạng thái bán mà còn đơn chờ — feature 016) |
| POST | `/api/v1/admin/products/{id}/images` | ADMIN | Gắn ảnh gallery sản phẩm (multipart, ≤ 2 MB, ≤ 10 ảnh) (`201`) |
| DELETE | `/api/v1/admin/products/{id}/images/{imageId}` | ADMIN | Gỡ ảnh gallery (`204`) |
| POST | `/api/v1/admin/products/{id}/images/{imageId}/primary` | ADMIN | Đặt ảnh chính của gallery |
| GET | `/api/v1/admin/inventory/{variantId}` | ADMIN | Tồn kho một variant (vật lý, đang giữ, khả dụng) |
| GET | `/api/v1/admin/inventory/{variantId}/movements` | ADMIN | Lịch sử biến động kho (có phân trang, cũ nhất trước) |
| POST | `/api/v1/admin/inventory/{variantId}/restock` | ADMIN | Nhập thêm hàng |
| POST | `/api/v1/admin/inventory/{variantId}/damage` | ADMIN | Ghi nhận hư hỏng (`409 INVENTORY_INSUFFICIENT_STOCK` nếu vượt kệ/giữ chỗ) |
| POST | `/api/v1/admin/inventory/{variantId}/adjustment` | ADMIN | Điều chỉnh về giá trị đã đếm |
| GET | `/api/v1/cart` | Bearer | Giỏ của chính mình (đối chiếu lại từng dòng) |
| POST | `/api/v1/cart/items` | Bearer | Thêm **variant** vào giỏ (`409` nếu không bán được hoặc quá tồn) |
| PATCH | `/api/v1/cart/items/{variantId}` | Bearer | Đổi số lượng một dòng |
| DELETE | `/api/v1/cart/items/{variantId}` | Bearer | Xoá một dòng (204) |
| POST | `/api/v1/orders` | Bearer | Checkout: biến giỏ thành đơn chờ artist xác nhận (201; không giữ hàng) |
| GET | `/api/v1/orders` | Bearer | Đơn của chính mình (có phân trang, mới nhất trước) |
| GET | `/api/v1/orders/{orderId}` | Bearer | Chi tiết một đơn của chính mình |
| PUT | `/api/v1/orders/{orderId}` | Bearer | Sửa đơn chưa trả tiền (thay dòng + địa chỉ) |
| POST | `/api/v1/orders/{orderId}/cancel` | Bearer | Huỷ đơn chưa trả tiền, trả hàng đã giữ |
| GET | `/api/v1/admin/orders` | ADMIN | Mọi đơn, kèm chủ sở hữu (có phân trang, lọc `status`, sắp xếp `sort`) |
| GET | `/api/v1/admin/orders/{orderId}` | ADMIN | Chi tiết một đơn bất kỳ, kèm chủ sở hữu |
| POST | `/api/v1/admin/orders/{orderId}/confirm` | ADMIN | Artist xác nhận: giữ toàn bộ hàng, mở hạn thanh toán |
| POST | `/api/v1/admin/orders/{orderId}/reject` | ADMIN | Artist từ chối đơn chờ xác nhận |
| POST | `/api/v1/admin/orders/{orderId}/ship` | ADMIN | Đánh dấu đơn đã trả tiền là đã giao |
| POST | `/api/v1/admin/orders/{orderId}/complete` | ADMIN | Đánh dấu đơn đã giao là hoàn tất |
| POST | `/api/v1/admin/orders/{orderId}/transfer` | ADMIN | Chuyển đơn đã trả tiền cho tài khoản khác |
| POST | `/api/v1/orders/{orderId}/payment` | Bearer | Bắt đầu (hoặc dùng lại) thanh toán online cho đơn chờ thanh toán |
| GET | `/api/v1/orders/{orderId}/payment` | Bearer | Đọc thanh toán của chính mình (đường đọc riêng) |
| GET | `/api/v1/admin/payments` | ADMIN | Mọi bản ghi thanh toán (phân trang, lọc `status`; `UNMATCHED` = hàng đợi đối soát) |
| POST | `/api/v1/payments/payos/webhook` | — | Webhook PayOS (công khai, xác minh chữ ký) |

---

## 12. Quy tắc cập nhật

Khi thêm endpoint mới (module mới hoặc tính năng mới trong module cũ), thay đổi
`plan.md`, hoặc sửa/xoá endpoint, **phải** làm trong cùng một thay đổi:

1. Thêm mục cho endpoint vào mục module tương ứng, theo đúng 6 phần mà các mục hiện
   có dùng: bảng thông tin · Request · Response · Lỗi · ghi chú.
2. Cập nhật bảng tổng hợp ở mục 11.
3. Thêm dòng vào Change log ở mục 13.
4. Nếu là endpoint mới: thêm `openapi.yaml` trong `specs/<feature>/contracts/` cho
   khớp, hoặc ghi rõ trong change log rằng chưa có OpenAPI và lý do.
5. Nếu phát sinh error code mới: thêm vào bảng ở mục 1.4 (và vào
   `specs/<feature>/contracts/<module>-error-codes.md` của feature đó).
6. Chạy `make swagger` để sinh lại `docs/swagger/` (annotation của handler phải khớp
   mục vừa thêm). CI chạy `make swagger-check` nên quên bước này là build đỏ.

## 13. Change log

| Ngày | Thay đổi | Nguồn |
|---|---|---|
| 2026-10-17 | **Ảnh card của danh mục ưu tiên gallery sản phẩm (module 04 Product, feature 015).** Ảnh của entry trong `GET /products` và `imageUrl` tóm tắt trong `GET /products/{code}` nay là **ảnh chính của gallery** sản phẩm, **fallback về ảnh của variant mặc định** (`position` nhỏ nhất), rồi `null` khi không có ảnh nào. Không đổi route/hình dạng DTO. | `internal/modules/product/infrastructure/implement/postgres/product.go` |
| 2026-10-16 | **Chặn ẩn khi còn đơn chờ (feature 016).** Ẩn danh mục (`PATCH /admin/categories/{categoryId}` với `isVisible: false`), tắt cờ hiển thị sản phẩm (`PATCH /admin/products/{id}` với `isVisible: false`) và mọi bước chuyển variant **rời** trạng thái đang bán (`POST .../variants/{variantId}/state`) bị từ chối `409` khi còn đơn `PENDING`/`PAYMENT_PENDING` giữ variant liên quan. **Thêm ba** mã vào mục 1.4: `CATEGORY_HAS_PENDING_ORDERS`, `PRODUCT_HAS_PENDING_ORDERS`, `VARIANT_HAS_PENDING_ORDERS` — thân từ chối mang một phần tử `error.details` cho mỗi đơn chặn (`{"field": "orderId", "issue": "<định danh đơn>"}`); không gì bị đổi. Ẩn thành công gỡ các variant khỏi mọi giỏ trong cùng transaction; hiện/bật hiển thị và chuyển **về** đang bán không bao giờ bị chặn; tín hiệu hết hàng của hệ thống bị chặn thành no-op đã ghi nhận. Không thêm route, không đổi method/path; DTO chỉ thêm member `isVisible` (tuỳ chọn ở request, thêm ở response quản trị). | `specs/016-hide-guard/contracts/error-codes.md`, `internal/modules/category/presentation/http/errors.go`, `internal/modules/product/presentation/http/errors.go` |
| 2026-10-16 | **Mã sản phẩm trở thành đoạn link công khai (module 04 Product, feature 015).** Sản phẩm **không** còn `slug` riêng: `code` vừa là mã sản phẩm operator viết vừa là đoạn link của URL công khai. **Đổi** `GET /products/{slug}` → `GET /products/{code}` (path param `code`). **Đổi** hình dạng công khai: entry và detail mang `code` thay `slug`; body `POST`/`PATCH /admin/products` **bỏ** `slug`, `code` là đoạn link (URL-safe: chữ thường, chữ số, dấu gạch đơn; ≤140 ký tự, duy nhất trên `lower(btrim(code))`). Dòng đơn **bỏ** snapshot `slug` (giữ `product_code` + `sku`). **Gỡ** mã `PRODUCT_SLUG_TAKEN` 409; va chạm dùng `409 PRODUCT_CODE_TAKEN`. Migration `00015_product_code_as_link_segment.sql` gỡ `products.slug`/`normalized_slug` và `order_items.slug`; danh mục vẫn giữ `slug` riêng. Cập nhật mục 1.4, mục 6 và bảng tổng hợp; `docs/swagger/` sinh lại khớp. | `migrations/00015_product_code_as_link_segment.sql`, `internal/modules/product/presentation/http/router.go`, `internal/modules/product/domain/model/code.go` |
| 2026-10-16 | **Tái cấu trúc sản phẩm thành sản phẩm cha + variant (SKU) (module 04, feature 015); thay thế feature 014.** Một `products` cha giữ nội dung dùng chung (tên, mã sản phẩm `code`, giá mặc định, danh mục, mô tả, slug, gallery) và sở hữu mảng **variant** con (`product_variants`: tên, `sku`, giá riêng tuỳ chọn, ảnh riêng, thứ tự, trạng thái bán riêng). **Variant là đơn vị bán**: tồn kho, giỏ và đơn đều theo variant. **Đổi** `GET /products` thành **một mục mỗi sản phẩm** (giá thấp nhất trong variant đang bán, ảnh variant mặc định; **bỏ** `type`). **Đổi** `GET /products/{slug}`: `data` mang thêm `variants[]` (`id`, `name`, `price`, `imageUrl`, `sellState`, `inStock`; SKU không lộ). **Đổi** `POST /admin/products`: **bắt buộc** `variants` (≥1) và `code`; variant mới ở `COMING_SOON`. **Thêm sáu** endpoint variant: `POST`/`PATCH`/`DELETE /admin/products/{id}/variants[/{variantId}]`, `POST`/`DELETE .../variants/{variantId}/image`, `POST .../variants/{variantId}/state`. **Gỡ** endpoint cấp sản phẩm `POST /admin/products/{id}/state` (feature 006) và toàn bộ route variant group của **feature 014** (`GET /product-groups/{slug}`, `GET`/`POST`/`PATCH`/`DELETE /admin/product-groups*` cùng các route ảnh nhóm). **Đổi** giỏ: dòng khoá theo `variantId` (`POST /cart/items` nhận `variantId`, `PATCH`/`DELETE /cart/items/{variantId}`); `CartLineResponse` **9 member** (`productName`/`variantName` thay `name`/`slug`). **Đổi** tồn kho (module 05): `/admin/inventory/*` khoá theo `{variantId}`, `StockMovement.variantId`. **Đổi** đơn (module 07): `OrderLineResponse` **8 member** chụp `productName`/`variantName` (mỗi cặp mã sản phẩm + SKU là một dòng; SKU/mã không trả ra view khách); `EditLineRequest.variantId` và các `details[].field` đổi sang `variantId`. **Thêm năm** mã `PRODUCT_CODE_TAKEN` 409, `VARIANT_SKU_TAKEN` 409, `VARIANT_NOT_FOUND` 404, `VARIANT_LAST_REMOVAL` 409, `VARIANT_STATE_TRANSITION_INVALID` 409; **gỡ** năm mã variant group của feature 014 và `PRODUCT_STATE_TRANSITION_INVALID` của feature 006. Cập nhật mục 1.4 (mã lỗi) và §6/§7/§8/§9. Xem ADR-021. | `internal/modules/product/presentation/http/router.go`, `internal/modules/cart/presentation/http/router.go`, `internal/modules/inventory/presentation/http/router.go`, `internal/modules/order/presentation/http/router.go` |
| 2026-10-15 | **Thêm variant group một chiều vào module 04 Product (feature 014).** Một nhóm gom nhiều **lựa chọn**, mỗi lựa chọn là một **sản phẩm đầy đủ, độc lập** (giá/tồn/trạng thái/ảnh/link riêng), nên **không** đổi bất kỳ hợp đồng liên module nào — khách vẫn thêm một `productId` vào giỏ. **Đổi** `GET /products` thành danh sách **gộp**: mỗi phần tử là `PublicCatalogueEntry` **7 member** (thêm `type` = `"product"` hoặc `"variant-group"`); một sản phẩm đã thuộc nhóm **không** còn xuất hiện riêng lẻ mà được đại diện bởi nhóm; nhóm chỉ hiện khi còn ít nhất một lựa chọn hiển thị, và giá card là giá lựa chọn thấp nhất. **Đổi** `GET /products/{slug}`: khi sản phẩm là lựa chọn, `data` mang thêm bộ chọn `variantGroup` (nhóm + các lựa chọn đang hiển thị); `data` là 8 hoặc 9 member. **Thêm tám** endpoint nhóm: công khai `GET /api/v1/product-groups/{slug}`; quản trị `GET`/`POST /admin/product-groups`, `GET`/`PATCH`/`DELETE /admin/product-groups/{id}`, `POST /{id}/images`, `DELETE /{id}/images/{imageId}`, `POST /{id}/images/{imageId}/primary` (tất cả `ADMIN`). **Đổi** body `POST`/`PATCH /admin/products`: thêm `variantGroupId`+`variantLabel` — với `PATCH` là **ba trạng thái** (bỏ qua = giữ, `null` = gỡ, có giá trị = gắn), gắn/gỡ **không** đụng giá/ảnh/trạng thái/slug. **Thêm năm** mã `VARIANT_GROUP_NOT_FOUND` 404, `VARIANT_GROUP_SLUG_TAKEN` 409, `VARIANT_GROUP_HAS_OPTIONS` 409, `VARIANT_LABEL_TAKEN` 409, `PRODUCT_ALREADY_IN_VARIANT_GROUP` 409 vào mục 1.4, cùng các action audit nhóm. Nhóm và sản phẩm dùng **hai không gian tên slug riêng** (trùng trong cùng một loại vẫn `409`). Gallery nhóm dùng chung luật media và mã `PRODUCT_IMAGE_*`. Ba hình dạng mới: công khai `PublicVariantGroupDetail` **7 member** / `PublicVariantOption` **6 member**; quản trị `AdminVariantGroup` **11 member** / `AdminVariantGroupDetail` **13 member** / `AdminGroupOption` **9 member**. Mục 6 thêm `6.12`–`6.20`; bảng tổng hợp §11 cập nhật. Xem ADR-020. | `internal/modules/product/presentation/http/router.go`, `internal/modules/product/presentation/http/handler.go` |
| 2026-10-12 | **Đổi luồng quên mật khẩu sang xác minh bằng mã OTP (module 01 Auth, feature 013).** `POST /password/forgot` nay gửi một **mã OTP 6 chữ số** qua email thay vì gửi thẳng reset token, và trả lại `429 AUTH_RESEND_COOLDOWN` khi còn trong khoảng chờ (bước này **cũng là** bước gửi lại). **Thêm** endpoint `POST /password/forgot/verify-otp`: xác minh `{ email, otp }`, thành công trả `{ resetToken, expiresIn }` — reset token dùng **một lần**, không phải phiên và không xác thực được request khác. `POST /password/reset` **không** còn trả cặp token (**bỏ đăng nhập tự động**); nay trả `200` kèm message thành công, đồng thời thu hồi mọi phiên và vô hiệu access token cũ. Mã OTP lưu ở Redis (băm Argon2id, TTL riêng, chặn sau `OTP_MAX_ATTEMPTS`, cooldown `OTP_RESEND_COOLDOWN`); reset token giữ ở bảng `password_reset_requests` (`PASSWORD_RESET_TTL`). **Không** thêm mã lỗi mới. Các mục §3.9–3.12 dời xuống §3.10–3.13 (chèn mục verify-otp mới); `docs/swagger/` sinh lại khớp. | `internal/modules/auth/presentation/http/router.go`, `internal/modules/auth/application/implement/password.go` |
| 2026-10-12 | **Đổi luồng đăng ký theo cơ chế trì hoãn (module 01 Auth, feature 012).** `POST /register` **không** còn tạo tài khoản `pending`: nó lưu đăng ký **tạm** trong Redis theo `registrationId` khó đoán rồi gửi OTP, trả `202` kèm `registrationId`, `otpExpiresIn`, `resendAvailableIn`. **Thêm ba** endpoint `POST /register/verify-otp` (xác nhận OTP, tạo **đúng một** tài khoản `active` vai trò `CUSTOMER`, idempotent khi gọi lại, **không** cấp phiên — 200), `POST /register/resend-otp` (thay mã sau cooldown — 202) và `POST /register/cancel` (huỷ đăng ký tạm, idempotent — 200). **Gỡ hai** endpoint `POST /verify-email` và `POST /resend-verification` (**breaking change** có chủ đích; thay bằng `register/verify-otp` và `register/resend-otp`, khoá theo `registrationId` thay vì email). `register` trả `409 AUTH_EMAIL_TAKEN`/`AUTH_USERNAME_TAKEN` sớm (§3.1) và `verify-otp` trả `409 AUTH_USERNAME_TAKEN` khi username bị đăng ký khác chiếm giữa chừng. Tham chiếu lạ/hết hạn/đã huỷ trả chung `AUTH_REGISTRATION_NOT_FOUND` 400. **Đổi mặc định** `OTP_TTL` 15m→5m, `OTP_MAX_ATTEMPTS` 3→5; thêm `PENDING_REGISTRATION_TTL=30m`. Các mục 3.2–3.11 dời xuống 3.2–3.12 (thêm ba mục đăng ký mới); `docs/swagger/` sinh lại khớp. | `internal/modules/auth/presentation/http/router.go`, `internal/modules/auth/application/implement/registration.go` |
| 2026-10-11 | Thêm nhóm thanh toán (module 08 Payment, feature 011): **bốn** endpoint — **hai** của khách đã đăng nhập `POST`/`GET /api/v1/orders/{orderId}/payment` (bắt đầu/dùng lại và đọc thanh toán của chính đơn mình), **một** của quản trị `GET /api/v1/admin/payments` (mọi bản ghi, phân trang, lọc `status`; `status=UNMATCHED` = hàng đợi đối soát), và **một công khai** `POST /api/v1/payments/payos/webhook` (không cần phiên, xác minh chữ ký PayOS). Hai hình dạng response: khách `PaymentResponse` **11 member** (gồm `checkoutUrl`, `qrCode`, `unmatchedReason` — cả ba luôn có mặt, `""` khi không áp dụng); quản trị `AdminPaymentResponse` **13 member** (thêm `userId` + `providerReference`). **Enum mới** `PaymentStatus` — **bốn** giá trị `PENDING`/`PAID`/`CANCELLED`/`UNMATCHED`. Bổ sung **sáu** mã `PAYMENT_*` vào mục 1.4 (`PAYMENT_ORDER_NOT_FOUND` 404, `PAYMENT_ORDER_NOT_PAYABLE` 409, `PAYMENT_NOT_FOUND` 404, `PAYMENT_ALREADY_LIVE` 409, `PAYMENT_PROVIDER_ERROR` 422, `PAYMENT_WEBHOOK_INVALID_SIGNATURE` 401) và ghi chú module không có hạn mức riêng ở mục 1.5. Nêu rõ: chủ sở hữu lấy từ **session**; yêu cầu còn hiệu lực được **dùng lại**; tiền = tổng đơn, **không** phí ship; webhook công khai **xác minh chữ ký**, áp **đúng một lần**; xác nhận cũ/không khớp thành `UNMATCHED` để đối soát, **không** hoàn tiền; huỷ đơn chưa trả huỷ yêu cầu ở cổng. Phần 10 được chèn, bảng tổng hợp/quy tắc/change log dời xuống mục 11/12/13. Xem ADR-019. | `internal/modules/payment/presentation/http/router.go` |
| 2026-10-10 | Bổ sung luồng **xác nhận đơn & chỉnh sửa** cho nhóm `/api/v1/orders` và `/api/v1/admin/orders` (module 07 Order, feature 010): **ba** endpoint mới — `PUT /orders/{orderId}` (khách sửa đơn chưa trả tiền), `POST /admin/orders/{orderId}/confirm` (artist xác nhận: giữ **toàn bộ** hàng all-or-nothing, mở hạn thanh toán) và `POST /admin/orders/{orderId}/reject` (artist từ chối) — nâng module lên **mười hai**: **năm** route của khách dưới `/orders` (thêm `PUT`), **bảy** của admin. **Đổi hành vi**: `POST /orders` nay tạo đơn `PENDING` (**chờ artist xác nhận**) và **không** giữ hàng; `POST /orders/{orderId}/cancel` nay huỷ được cả `PENDING` lẫn `PAYMENT_PENDING`; `GET /admin/orders` nhận thêm `status`/`sort` (tuỳ chọn; `status=PENDING&sort=oldest` = hàng đợi xác nhận FIFO). **Đổi enum**: `PENDING_PAYMENT` **đổi tên** thành `PAYMENT_PENDING`, thêm `PENDING` — thành **sáu** giá trị. Bổ sung hai mã `ORDER_*` (`ORDER_NOT_EDITABLE`, `ORDER_EMPTY`) vào mục 1.4, ghi chú `ORDER_CONFIRMED`/`ORDER_REJECTED` vào nhóm action audit, và hạn **giữ chỗ 15 → 60 phút**. Sửa §9 (enum, endpoint, ghi chú) và bảng tổng hợp §10. Xem ADR-018. | `internal/modules/order/presentation/http/router.go` |
| 2026-10-09 | Thêm nhóm `/api/v1/orders` và `/api/v1/admin/orders` (module 07 Order): **chín** endpoint — **bốn** của khách đã đăng nhập (`POST /orders` checkout, `GET /orders` danh sách, `GET /orders/{orderId}` chi tiết, `POST /orders/{orderId}/cancel` huỷ đơn còn chờ thanh toán) và **năm** của admin (`GET /admin/orders`, `GET /admin/orders/{orderId}`, `POST /admin/orders/{orderId}/ship`, `POST /admin/orders/{orderId}/complete`, `POST /admin/orders/{orderId}/transfer`). Chủ sở hữu lấy từ **session**; đơn của khách khác trả cùng `404 ORDER_NOT_FOUND`. Bốn hình dạng response: khách `OrderSummaryResponse` **5 member** / `OrderResponse` **7 member**; quản trị `AdminOrderSummaryResponse` **6 member** / `AdminOrderResponse` **8 member** (hơn khách đúng `userId`); `OrderLineResponse` **6 member**, `OrderAddressResponse` **7 member**. Bổ sung **chín** mã `ORDER_*` vào mục 1.4 (kèm ghi rõ bốn tình huống cố ý **không** có mã: trả tiền, hết hạn, sản phẩm bị xoá, chuyển nhượng lặp) và ghi chú module không có hạn mức riêng ở mục 1.5. Nêu rõ: checkout **giữ** hàng (vật lý không đổi) và **làm rỗng** giỏ trong một transaction; tiền là số nguyên đơn vị nhỏ nhất, tổng **không** gồm phí ship; đơn chưa trả tiền **tự huỷ** sau 15 phút; trạng thái đi qua endpoint riêng; **không** có endpoint `pay` (module 08 điều khiển); chuyển nhượng chỉ đổi chủ, không đổi tồn kho. Phần 9 được chèn, bảng tổng hợp/quy tắc/change log dời xuống mục 10/11/12. Xem ADR-017. | `internal/modules/order/presentation/http/router.go` |
| 2026-10-09 | Thêm nhóm `/api/v1/cart` (module 06 Cart): **bốn** route của khách đã đăng nhập — đọc giỏ (`GET /cart`), thêm sản phẩm (`POST /cart/items`), đổi số lượng một dòng (`PATCH /cart/items/{productId}`) và xoá một dòng (`DELETE /cart/items/{productId}`). Giỏ là **tài nguyên đơn** định địa chỉ tại `/cart` **không kèm định danh** (chủ sở hữu là session); một dòng định địa chỉ bằng **định danh sản phẩm**. Hai hình dạng response: `CartResponse` **2 member** (`lines`, `subtotal` — `null` khi giỏ rỗng) và `CartLineResponse` **8 member** (`productId`, `name`, `slug`, `quantity`, `unitPrice`, `lineTotal`, `buyable`, `availableQuantity`). Bổ sung hai mã `CART_*` vào mục 1.4 (kèm ghi rõ `PRODUCT_NOT_FOUND` 404 của module 04 được **tái sử dụng** cho sản phẩm không tồn tại lẫn dòng không thuộc giỏ này) và ghi chú module không có hạn mức riêng ở mục 1.5. Nêu rõ: đọc giỏ **đối chiếu lại** trạng thái bán và tồn khả dụng của từng dòng, `availableQuantity` chỉ có khi dòng còn bán nhưng thiếu; giá hiển thị là **snapshot** lúc thêm; giỏ **không** giữ chỗ tồn kho và **không** ghi `audit_logs`. Phần 8 được chèn, bảng tổng hợp/quy tắc/change log dời xuống mục 9/10/11. Xem ADR-016. | `internal/modules/cart/presentation/http/router.go` |
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
