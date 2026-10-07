# Frontend Integration Guide — Sửa lỗi tồn đọng (delta)

**Feature**: `004-fix-pending-defects`
**Trạng thái**: 31/32 task; **T028b chưa xong** — bị chặn bởi cấu hình mail relay (`525 5.7.1`), ngoài phạm vi code. Backfill sau amendment `frontend-guide` (hiến pháp v1.7.0 §Governance).
**Đối tượng đọc**: Lập trình viên frontend.
**Mục tiêu**: FE nắm **đúng những gì đổi** ở hành vi thấy được từ client. Đây là guide dạng **delta**: không có endpoint mới, chỉ có thay đổi status/mã lỗi trên endpoint đã có.

> Nguồn: [`specs/004-fix-pending-defects/contracts/contract-changes.md`](contracts/contract-changes.md) và change log [`docs/api-reference.md`](../../docs/api-reference.md) §8 (các dòng 2026-10-06). Khi lệch, `docs/api-reference.md` thắng.

---

## 0. Tóm tắt nhanh (TL;DR)

- **Không thêm endpoint, không đổi hình dạng request/response.** Chỉ đổi **status và mã lỗi** ở ba endpoint đã tồn tại.
- Avatar quá 2 MB: **luôn** `413 USER_AVATAR_TOO_LARGE` (trước đây đôi khi là `413 PAYLOAD_TOO_LARGE`).
- Đăng ký khi gửi thư thất bại: `503 SERVICE_UNAVAILABLE` thay cho `500 INTERNAL_ERROR`.
- Gửi lại mã xác minh ngay sau một lần gửi **hỏng**: **được chấp nhận** (không còn `AUTH_RESEND_COOLDOWN`).
- Hai thay đổi còn lại (phân loại lỗi provider, từ chối khởi động) **không phải API** — chỉ dành cho vận hành.

---

## 1. Trước → sau

### 1.1 `POST /api/v1/users/me/avatar` — lý do từ chối ảnh quá lớn

Request **không đổi**: vẫn `multipart/form-data`, một part `file`.

| Tình huống | Trước | Sau |
|---|---|---|
| Ảnh > 2 MB, client **khai** `Content-Length` | `413 PAYLOAD_TOO_LARGE` | `413 USER_AVATAR_TOO_LARGE` |
| Ảnh > 2 MB, client **không** khai độ dài | `413 USER_AVATAR_TOO_LARGE` | `413 USER_AVATAR_TOO_LARGE` (không đổi) |
| Ảnh > 2 MB, khai **thiếu** độ dài | `413 USER_AVATAR_TOO_LARGE` | `413 USER_AVATAR_TOO_LARGE` (không đổi) |
| Ảnh đúng 2 MB | `200` | `200` (không đổi) |
| Không phải JPEG/PNG/WebP (theo byte) | `400 USER_AVATAR_TYPE_UNSUPPORTED` | không đổi |
| Body không phải multipart, thiếu part `file` | `400 VALIDATION_ERROR` | không đổi |
| Media service lỗi/chưa cấu hình | `503 USER_MEDIA_UNAVAILABLE` | không đổi |

**Tác động FE**: **thu hẹp** — giờ chỉ còn **một** mã cho ảnh quá lớn, FE không phải xử lý hai nhánh.
`PAYLOAD_TOO_LARGE` **vẫn** là mã của mọi route không phải avatar.

### 1.2 `POST /api/v1/auth/register` — khi không gửi được thư

Request **không đổi**.

| Kết quả | Trước | Sau |
|---|---|---|
| Gửi thư thành công | `202` (body chung) | `202` (không đổi) |
| **Không gửi được thư** | `500 INTERNAL_ERROR` | `503 SERVICE_UNAVAILABLE` |

Body nhánh lỗi mới:
```json
{
  "error": {
    "code": "SERVICE_UNAVAILABLE",
    "message": "The confirmation email could not be sent. Nothing was delivered to your address; request a new confirmation code and try again.",
    "requestId": "..."
  }
}
```

**Tác động FE**: `503` mời khách "chờ và thử lại"; `500` khiến khách tưởng mình sai nên thử lại y hệt (không bao giờ thành công). FE nên, khi gặp `503` ở đây, mời khách **xin mã mới** (`resend-verification`) ngay.
> **Bảo đảm chống dò email**: cả nhánh "email đã có tài khoản `pending`" lẫn "email chưa có" đều trả **cùng** status/mã/câu khi gửi hỏng — FE không thể và không nên cố phân biệt. Tài khoản `pending` được **giữ lại**.

### 1.3 `POST /api/v1/auth/resend-verification` — cooldown sau lần gửi hỏng

Request **không đổi**. Thay đổi hành vi:

| Tình huống | Trước | Sau |
|---|---|---|
| Xin mã mới trong 60s sau một lần gửi **hỏng** | `429 AUTH_RESEND_COOLDOWN` | **được chấp nhận**, cấp mã mới |
| Xin mã mới trong 60s sau một lần gửi **thành công** | `AUTH_RESEND_COOLDOWN` | không đổi |
| Xin mã mới sau khi hết cooldown | được chấp nhận | không đổi |
| Gọi lặp quá hạn mức *flow* | bị từ chối | không đổi (`RATE_LIMITED`) |

**Tác động FE**: sau `503` ở `register`, có thể gọi `resend-verification` **ngay** thay vì phải đếm ngược.
> **Khoảng trống đã biết**: nếu chính lần gửi lại này thất bại, endpoint vẫn trả **`500`** (chưa map thành `503`). Xem `docs/api-reference.md` §3.3 và `deferred.md`.

---

## 2. Không phải endpoint (chỉ ảnh hưởng vận hành)

- **Phân loại lỗi provider** (`TRANSIENT` / `UNREACHABLE` / `CONFIGURATION` / `REFUSED` / `UNKNOWN`): chỉ dùng cho log vận hành và metadata audit, **không** xuất hiện trong response.
- **Từ chối khởi động**: nếu media đã cấu hình mà `MAX_BODY_BYTES` thấp hơn trần route avatar (`2 162 688`), API **từ chối khởi động** và in tên biến + cả hai giá trị. FE không thấy gì; đây là tín hiệu cho operator.

---

## 3. Bảng mã lỗi liên quan

| Code | HTTP | Nơi | Ghi chú thay đổi |
|---|---|---|---|
| `USER_AVATAR_TOO_LARGE` | 413 | avatar | Nay là **câu trả lời duy nhất** cho ảnh quá lớn |
| `SERVICE_UNAVAILABLE` | 503 | register | **Mới** ở nhánh gửi thư thất bại (thay `500`) |
| `AUTH_RESEND_COOLDOWN` | 429 | resend-verification | **Không** còn áp sau lần gửi hỏng |
| `PAYLOAD_TOO_LARGE` | 413 | mọi route khác | Không đổi; **không** còn ở route avatar |

Các mã khác của hai endpoint này (xem guide `001` và `003`) **không đổi**.

---

## 4. Checklist FE

- [ ] Bỏ nhánh xử lý `PAYLOAD_TOO_LARGE` **riêng cho avatar**; dùng `USER_AVATAR_TOO_LARGE` cho mọi trường hợp ảnh quá lớn.
- [ ] Ở `register`, xử lý `503 SERVICE_UNAVAILABLE` bằng "mời xin mã mới", không phải lỗi chung.
- [ ] Sau `503` của `register`, cho phép gọi `resend-verification` ngay (không tự khoá nút theo cooldown 60s).
- [ ] Vẫn tôn trọng `RATE_LIMITED` (429) cho nhóm *flow*.
- [ ] Không cố suy ra email đã tồn tại từ status của `register`.

---

## 5. Changelog & đối chiếu

**2026-10-07** — Backfill guide lần đầu (hiến pháp v1.7.0).

Đối chiếu với nguồn (có đếm):

- endpoint bị ảnh hưởng: **3** (`POST /users/me/avatar`, `POST /auth/register`, `POST /auth/resend-verification`) — khớp `contracts/contract-changes.md` §1–§3.
- endpoint mới: **0** — feature không thêm endpoint.
- bảng trước→sau: **3** (mục 1.1, 1.2, 1.3).
- mã lỗi bị chạm: **4** (`USER_AVATAR_TOO_LARGE`, `SERVICE_UNAVAILABLE`, `AUTH_RESEND_COOLDOWN`, `PAYLOAD_TOO_LARGE`) — khớp `docs/api-reference.md` §8 các dòng 2026-10-06.
- **T028b** (kết cục gửi thư thật) chưa xác minh do mail relay trả `525 5.7.1`; không ảnh hưởng hình dạng API mà FE thấy.
