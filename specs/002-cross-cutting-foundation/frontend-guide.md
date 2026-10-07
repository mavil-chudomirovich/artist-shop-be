# Frontend Integration Guide — Cross-Cutting Foundation

**Feature**: `002-cross-cutting-foundation`
**Trạng thái**: Hoàn thiện (73/73 task). Backfill sau amendment `frontend-guide` (hiến pháp v1.7.0 §Governance).
**Đối tượng đọc**: Lập trình viên frontend.
**Mục tiêu**: FE nắm được quy ước chung mà **mọi** lời gọi API đều tuân theo, và biết hai endpoint vận hành. Không cần đọc code backend hay Swagger.

> Đây là guide **ngắn** của feature nền tảng. Nó cố ý trỏ về
> [`docs/api-reference.md`](../../docs/api-reference.md) §1 làm nguồn authoritative thay vì chép lại —
> `docs/api-reference.md` **thắng** khi có mâu thuẫn.

---

## 0. Tóm tắt nhanh (TL;DR)

- **Base path**: `/api/v1`. Base URL dev: `http://localhost:8080`.
- **Mọi response thành công** bọc trong `{ "data": ..., "meta": {...} }`.
- **Mọi response lỗi** bọc trong `{ "error": { "code", "message", "details"?, "requestId" } }`.
- **Luôn gửi** `Content-Type: application/json` khi có body, và nên gửi `X-Request-Id` (UUID) để gom log.
- **FE phải phân nhánh theo `error.code`**, không bao giờ theo `error.message`.
- Hai endpoint vận hành `/healthz`, `/readyz` **không** dùng envelope — đây là endpoint của hạ tầng, FE app không gọi.

---

## 1. Quy ước chung (FE cần thuộc)

### 1.1 Response envelope

```json
{
  "data": { "...": "payload" },
  "meta": { "requestId": "5c9f1a1e-...", "timestamp": "2026-09-18T08:15:04Z" }
}
```

Phân trang bổ sung `meta.page`, `meta.pageSize`, `meta.total`. **`total` vắng mặt khi bằng 0**
(`omitempty` của tầng envelope) — FE đọc thiếu `total` là **0**, không phải "không có thông tin".
Đây là hành vi dùng chung, ảnh hưởng mọi endpoint phân trang.

### 1.2 Error envelope

```json
{
  "error": {
    "code": "AUTH_INVALID_CREDENTIALS",
    "message": "Invalid email or password",
    "details": [{ "field": "password", "issue": "too short" }],
    "requestId": "5c9f1a1e-..."
  }
}
```

`details` chỉ có khi lỗi theo field. Lỗi `5xx` không lộ chi tiết nội bộ.

### 1.3 Correlation ID

Gửi `X-Request-Id: <uuid>` (không hợp lệ sẽ bị thay UUID mới). Response **luôn** trả lại header này,
giá trị cũng nằm trong `meta.requestId` / `error.requestId`. Khi báo lỗi cho đội backend, gửi kèm giá trị này.

### 1.4 Xác thực

`Authorization: Bearer <accessToken>`. Access token JWT HS256 sống **15 phút**; refresh token opaque
sống **45 ngày**, xoay vòng mỗi lần dùng. Xem module auth (`specs/001-user-auth/frontend-guide.md`) cho luồng token.

### 1.5 Rate limit

| Phạm vi | Mặc định |
|---|---|
| Toàn bộ `/api/v1` (nhóm read/write/auth) | 20 req/s, burst 40 |
| Nhóm *flow* auth (register/verify/resend/forgot/reset) | 5/phút / IP |
| `POST /auth/login` | 10/phút / IP |
| `POST /users/me/avatar` | 10/giờ / IP |
| Ghi địa chỉ (`POST`/`PATCH`/`DELETE /users/me/addresses*`) | 30/phút / IP |

Khi vượt: `429 RATE_LIMITED` + header `Retry-After` (giây). Trạng thái phía server **không đổi** → an toàn để thử lại sau khi chờ.

### 1.6 Pipeline & lỗi tầng vận chuyển

Mọi request đi qua: `RealIP → Correlation → RequestLogger → Recovery → CORS → BodyLimit → AllowedContentTypes → Authentication → rate limit → handler`.

| Vi phạm | HTTP | Code |
|---|---|---|
| Body vượt `MAX_BODY_BYTES` (mặc định 4 MiB) | 413 | `PAYLOAD_TOO_LARGE` |
| Body vượt trần riêng của route | 413 | mã của **route đó** (ví dụ avatar: `USER_AVATAR_TOO_LARGE`) |
| `Content-Type` không khớp loại body của route | 415 | `UNSUPPORTED_MEDIA_TYPE` |
| Vượt rate limit | 429 | `RATE_LIMITED` (kèm `Retry-After`) |

> `PAYLOAD_TOO_LARGE` nghĩa là **request quá lớn vì lý do không liên quan tới nội dung**. Route nào có
> luật riêng về nội dung thì trả mã của luật đó, để FE biết chính xác cần sửa gì. Xem ADR-008, ADR-010.

---

## 2. Endpoint vận hành

Hai endpoint này **không** dùng envelope và **không** nằm dưới `/api/v1`. Chúng phục vụ orchestrator/load
balancer, không phải FE app.

| Method | Path | Auth | Mô tả |
|---|---|---|---|
| GET | `/healthz` | — | Liveness; `200` khi process còn sống |
| GET | `/readyz` | — | Readiness; `200` khi db + migration + redis đều ok, ngược lại `503` |
| GET | `/swagger/*` | — | Swagger UI, chỉ tồn tại khi `SWAGGER_ENABLED=true` (mặc định tắt) |

`GET /healthz` → `{ "status": "alive" }`

`GET /readyz` →
```json
{ "status": "ready", "checks": { "database": "ok", "migrations": "ok", "cache": "ok" } }
```

---

## 3. Trước → sau

Feature nền tảng **thêm mới** toàn bộ quy ước và endpoint: envelope, error format, correlation ID,
rate limit, `/healthz`, `/readyz` (và `/swagger/*`). Không có endpoint nào bị đổi hay xoá. Tất cả đều là **mới**.

---

## 4. Bảng mã lỗi dùng chung (`httpx`)

FE phải xử lý các mã này ở tầng HTTP client dùng chung:

| Code | HTTP | Nghĩa | FE nên làm |
|---|---|---|---|
| `VALIDATION_ERROR` | 400 | Dữ liệu không hợp lệ; xem `details[].field` | Hiện lỗi cạnh đúng ô nhập |
| `MALFORMED_REQUEST` | 400 | Body không parse được hoặc có field lạ | Lỗi phía FE — kiểm tra serializer |
| `UNAUTHENTICATED` | 401 | Thiếu / token không hợp lệ | Xoá phiên, chuyển login |
| `FORBIDDEN` | 403 | Đã xác thực nhưng thiếu quyền | Ẩn/khóa hành động |
| `NOT_FOUND` | 404 | Route không tồn tại | Lỗi lập trình |
| `METHOD_NOT_ALLOWED` | 405 | Sai HTTP method | Lỗi lập trình |
| `CONFLICT` | 409 | Xung đột trạng thái | Tải lại rồi thử lại |
| `PAYLOAD_TOO_LARGE` | 413 | Body quá lớn | Giảm kích thước request |
| `UNSUPPORTED_MEDIA_TYPE` | 415 | `Content-Type` sai | Sửa header |
| `RATE_LIMITED` | 429 | Quá hạn mức | Chờ `Retry-After` rồi thử lại |
| `INTERNAL_ERROR` | 500 | Lỗi ngoài dự kiến | Thông báo chung, kèm `requestId` |
| `SERVICE_UNAVAILABLE` | 503 | Chưa sẵn sàng phục vụ | Thông báo "thử lại sau" |

---

## 5. Checklist FE

- [ ] HTTP client dùng chung: parse `data`/`meta`, parse `error`, đọc `X-Request-Id`.
- [ ] Phân nhánh theo `error.code`; hiển thị `details[].field` cạnh ô nhập cho `VALIDATION_ERROR`.
- [ ] Xử lý `429` bằng cách tôn trọng `Retry-After`.
- [ ] Xử lý `401` bằng cách xoá phiên + điều hướng login (xem guide auth).
- [ ] Đọc `meta.total` thiếu là `0` ở mọi màn hình phân trang.
- [ ] Gắn `X-Request-Id` (nếu có) vào báo cáo lỗi.

---

## 6. Changelog & đối chiếu

**2026-10-07** — Backfill guide lần đầu (hiến pháp v1.7.0).

Đối chiếu với nguồn (có đếm):

- endpoint vận hành: **2** (`/healthz`, `/readyz`) + **1** Swagger UI (`/swagger/*`) — khớp §2 `docs/api-reference.md`.
- mã lỗi dùng chung trong bảng §4: **12** — khớp `docs/api-reference.md` §1.4.
- hạng mục rate limit trong §1.5: **5** — khớp `docs/api-reference.md` §1.5.
- route trong bảng tổng hợp §2 `docs/api-reference.md`: **2** vận hành + Swagger; không có route business nào thuộc feature này.
- HTTP status xuất hiện: `200`, `413`, `415`, `429`, `500`, `503` — đều nằm trong tập handler có thể trả.
