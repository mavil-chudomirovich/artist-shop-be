# Frontend Integration Guide — Online Order Payment

**Feature**: `011-order-payment` (module 08 Payment, module **cuối** của V1.0)
**Trạng thái**: **Bốn** endpoint của module 08 đã được ghi vào `docs/api-reference.md` §10.
**Đối tượng đọc**: Lập trình viên frontend (khách đã đăng nhập trả tiền cho đơn, và bàn đối soát của operator).
**Mục tiêu**: FE triển khai thanh toán chỉ với tài liệu này — không cần đọc code backend hay Swagger.

> **Đây là bàn giao cho frontend team.** Nguồn authoritative là
> [`docs/api-reference.md`](../../docs/api-reference.md) §10. Guide này **phải khớp** với file đó; khi lệch,
> `docs/api-reference.md` thắng. Mọi tên trường dưới đây là **đúng** `json:"…"` mà DTO phát ra. Hợp đồng máy
> đọc được là [`contracts/openapi.yaml`](contracts/openapi.yaml).
>
> Đây là **lần đầu tiên** khách trả được tiền cho đơn: các feature `009`/`010` mới chỉ đưa đơn tới
> `PAYMENT_PENDING`, chưa có bề mặt thanh toán nào. Guide này không thay thế guide của `010` — nó **bổ
> sung**: đọc guide `010` cho trạng thái đơn, đọc guide này cho thanh toán.

---

## 0. Tóm tắt nhanh (TL;DR)

- **4 endpoint**, tất cả **MỚI**: **2** của **KHÁCH đã đăng nhập** (`POST`/`GET /orders/{orderId}/payment`),
  **1** của **ADMIN** (`GET /admin/payments`), **1 công khai** (`POST /payments/payos/webhook`, PayOS gọi —
  FE **không** gọi endpoint này).
- **Chủ sở hữu lấy từ session**, không bao giờ từ body/query; **không** route nào nêu id chủ. Đơn của khách
  khác trả **cùng** `404 PAYMENT_ORDER_NOT_FOUND`.
- **Khách bắt đầu thanh toán** cho một đơn `PAYMENT_PENDING` → nhận **`checkoutUrl`** và **`qrCode`** để
  trả. Mở lần hai → **dùng lại** đúng liên kết đó, **không** tạo yêu cầu mới.
- **Trạng thái thanh toán là trục riêng**, **không** nằm trong response của đơn. Đọc qua
  `GET /orders/{orderId}/payment`.
- **Bốn** giá trị `PaymentStatus`: `PENDING`, `PAID`, `CANCELLED`, `UNMATCHED`.
- Sau khi PayOS xác nhận, đơn chuyển **`PAID`** (bên module 07) — FE thấy điều đó qua `GET /orders/{id}`;
  thanh toán thành `PAID`.
- **Xác nhận không áp được** (sai tiền / nội dung đơn cũ / đơn đã huỷ-hết hạn) → thanh toán **`UNMATCHED`**
  kèm `unmatchedReason`; đơn **không** đổi. Operator xử lý qua `GET /admin/payments?status=UNMATCHED`.
- **V1.0 KHÔNG hoàn tiền**: không có API hoàn tiền; đơn đã trả tiền **không** huỷ (chuyển nhượng, module 07).
- `checkoutUrl`, `qrCode`, `unmatchedReason` **luôn có mặt** trong JSON; khi không áp dụng là **chuỗi rỗng**
  `""`, **không** phải `null`.
- Module **không** có rate limit riêng; chỉ chịu hạn mức toàn cục.

---

## 1. Quy ước

- Base path `/api/v1`; envelope `{data, meta}` / `{error}` như các guide trước.
- Route khách yêu cầu **Bearer** (thiếu/sai token → `401 UNAUTHENTICATED`); route admin yêu cầu role `ADMIN`
  (`CUSTOMER` → `403 FORBIDDEN`). Webhook **công khai**, **không** có Bearer: chữ ký PayOS là xác thực.
- Tác nhân (khách) lấy từ **session**, không bao giờ từ body. Route khách **không** có body; nếu có, member
  lạ → `MALFORMED_REQUEST`.
- `orderId` là UUID. Không phải UUID → `400 VALIDATION_ERROR` với `details[].field = "orderId"`.
- `page` mặc định `1` (tối thiểu `1`); `pageSize` mặc định `20`, khoảng `1..100`. Ngoài khoảng →
  `400 VALIDATION_ERROR` với `details[].field` là `"page"` hoặc `"pageSize"`.
- Tiền là **số nguyên đơn vị nhỏ nhất + currency**, không bao giờ là số thực. Số tiền = **tổng đơn, không
  phí ship**.
- Danh sách thanh toán của operator **mới nhất trước** (`createdAt`, rồi `id`).

### Kiểu dữ liệu *(mọi trường JSON của **mọi** DTO client thấy)*

`MoneyResponse` (2 member) — tiền:
```json
{ "amount": 300000, "currency": "VND" }
```
- `amount`: số nguyên, đơn vị nhỏ nhất của `currency`. Số tiền thanh toán = tổng đơn.
- `currency`: ba chữ in hoa (hiện luôn `VND`).

`PaymentResponse` (11 member) — một thanh toán, hình dạng **khách**; cả `POST` và `GET` trả về nó:
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
- `id`: UUID thanh toán.
- `orderId`: UUID đơn mà thanh toán thuộc về.
- `amount`: `MoneyResponse` — tiền của thanh toán (bằng tổng đơn).
- `status`: một trong **4** giá trị (§3).
- `provider`: tên nhà cung cấp; hiện luôn `payos`.
- `orderCode`: số nguyên định danh của PayOS — **chỉ để nhận dạng**, FE **không** cần tự xử lý.
- `checkoutUrl`: URL khách mở để trả. Có giá trị khi thanh toán còn hiệu lực; **`""`** khi không.
- `qrCode`: payload VietQR do PayOS trả, để render QR. Có giá trị khi còn hiệu lực; **`""`** khi không.
- `unmatchedReason`: vì sao một xác nhận không áp được. **Chỉ** có giá trị khi `status = "UNMATCHED"`; các
  trạng thái khác là `""`.
- `createdAt` / `updatedAt`: mốc ISO 8601.

`AdminPaymentResponse` (13 member) — hình dạng **quản trị** = `PaymentResponse` (11) **+** 2:
```json
{
  "id": "0f5c6e0c-1a44-4a1e-9b3d-9a1b2c3d4e5f",
  "orderId": "b2f1c0d4-5a6e-4b7c-8d9e-0f1a2b3c4d5e",
  "amount": { "amount": 300000, "currency": "VND" },
  "status": "UNMATCHED",
  "provider": "payos",
  "orderCode": 123456789012345,
  "checkoutUrl": "",
  "qrCode": "",
  "unmatchedReason": "payment is for order version 1, order is at 2",
  "createdAt": "2026-10-11T08:15:04Z",
  "updatedAt": "2026-10-11T08:20:00Z",
  "userId": "11111111-1111-1111-1111-111111111111",
  "providerReference": ""
}
```
- `userId`: tài khoản đã khởi tạo thanh toán.
- `providerReference`: tham chiếu giao dịch của PayOS **khi đã xác nhận**; `""` khi chưa.

> **Luôn đủ mặt**: cả ba trường `checkoutUrl`, `qrCode`, `unmatchedReason` (và `providerReference` ở dạng
> quản trị) là chuỗi **không** `omitempty`, nên JSON luôn có chúng. Đừng phân biệt "vắng mặt" với "rỗng";
> hãy coi `""` là *không có giá trị*.

**Request body**: hai route khách **không** có body; `GET /admin/payments` chỉ có query. Webhook nhận
envelope PayOS `{code, desc, success, data, signature}` — **không phải** DTO client, FE không gửi.

---

## 2. Bảng endpoint

| # | Method | Path | Auth | Trạng thái | Trả về |
|---|---|---|---|---|---|
| 10.1 | POST | `/api/v1/orders/{orderId}/payment` | Khách | **MỚI** | `200` `PaymentResponse` |
| 10.2 | GET | `/api/v1/orders/{orderId}/payment` | Khách | **MỚI** | `200` `PaymentResponse` |
| 10.3 | GET | `/api/v1/admin/payments` | ADMIN | **MỚI** | `200` `[AdminPaymentResponse]` (phân trang) |
| 10.4 | POST | `/api/v1/payments/payos/webhook` | — | **MỚI** | `200` (PayOS gọi; FE **không** gọi) |

### Chi tiết endpoint **MỚI**

**10.1 `POST /orders/{orderId}/payment`** — **MỚI**. Khách bắt đầu (hoặc **dùng lại**) thanh toán cho đơn
`PAYMENT_PENDING` của mình. Trả `200` `PaymentResponse` với `status: "PENDING"` và, khi còn hiệu lực,
`checkoutUrl` + `qrCode`. Không body.
Lỗi: `VALIDATION_ERROR` 400 (`orderId`) · `UNAUTHENTICATED` 401 · `PAYMENT_ORDER_NOT_FOUND` 404 ·
`PAYMENT_ORDER_NOT_PAYABLE` 409 · `PAYMENT_ALREADY_LIVE` 409 · `PAYMENT_PROVIDER_ERROR` 422 ·
`RATE_LIMITED` 429 · `INTERNAL_ERROR` 500.

**10.2 `GET /orders/{orderId}/payment`** — **MỚI**. Đọc thanh toán **mới nhất** của đơn mình, ở **bất kỳ**
trạng thái (kể cả `PAID`/`CANCELLED`/`UNMATCHED`). Trả `200` `PaymentResponse`.
Lỗi: `VALIDATION_ERROR` 400 (`orderId`) · `UNAUTHENTICATED` 401 · `PAYMENT_ORDER_NOT_FOUND` 404 ·
`PAYMENT_NOT_FOUND` 404 · `RATE_LIMITED` 429 · `INTERNAL_ERROR` 500.

**10.3 `GET /admin/payments`** — **MỚI**. Operator xem mọi bản ghi thanh toán, mới nhất trước, phân trang.
Query `status` **tuỳ chọn** lọc một trong bốn trạng thái; `status=UNMATCHED` = **hàng đợi đối soát**.
Trả `200` `[AdminPaymentResponse]` + `meta` phân trang.
Lỗi: `VALIDATION_ERROR` 400 (`page`/`pageSize`/`status`) · `UNAUTHENTICATED` 401 · `FORBIDDEN` 403 ·
`RATE_LIMITED` 429 · `INTERNAL_ERROR` 500.

**10.4 `POST /payments/payos/webhook`** — **MỚI**, công khai. PayOS gọi khi có kết quả thanh toán; chữ ký
được xác minh trước khi tin. FE **không** gọi endpoint này, nhưng nên biết nó tồn tại: đây là mắt xích đưa
đơn sang `PAID`. Sự kiện đã xử lý trả `200` để PayOS ngừng thử lại.

---

## 3. Bảng giá trị enum `PaymentStatus`

`status` là một trong **4** giá trị:

| Giá trị | Ý nghĩa | Vào bằng | Ra được không |
|---|---|---|---|
| `PENDING` | Liên kết còn hiệu lực, khách có thể trả | Khách bắt đầu thanh toán (`10.1`) | → `PAID`, `CANCELLED`, `UNMATCHED` |
| `PAID` | Xác nhận hợp lệ đã áp; **đơn đã `PAID`**, hàng giữ chỗ thành **bán** | Webhook PayOS hợp lệ + khớp (`10.4`) | **Không** (cuối) |
| `CANCELLED` | Đơn bị huỷ nên yêu cầu ở cổng cũng bị huỷ | Huỷ đơn `PAYMENT_PENDING` (module 07, `9.5`) | **Không** (cuối) |
| `UNMATCHED` | Xác nhận không áp được, chờ operator **đối soát** | Webhook hợp lệ nhưng sai tiền / nội dung đơn cũ / đơn không còn chờ thanh toán | **Không** (cuối) |

> `UNMATCHED` là **trạng thái riêng của thanh toán**, **không** phải của đơn: khi nó xảy ra, `status` của
> đơn **không** đổi. FE **không** được coi `UNMATCHED` là đã trả tiền hay đã huỷ.

---

## 4. Trước → sau

Đây là **lần đầu tiên** khách có thể trả tiền: `009`/`010` dựng đơn và bước duyệt, nhưng **không** có bề
mặt thanh toán nào; đơn có thể tới `PAYMENT_PENDING` rồi **không có cách trả**.

### Endpoint **MỚI** (4)

| # | Method | Path | Trước (009/010) | Sau (011) |
|---|---|---|---|---|
| 10.1 | POST | `/api/v1/orders/{orderId}/payment` | *chưa tồn tại* — đơn `PAYMENT_PENDING` không có cách trả | `200` `PaymentResponse` (bắt đầu/dùng lại thanh toán, trả link/QR) |
| 10.2 | GET | `/api/v1/orders/{orderId}/payment` | *chưa tồn tại* — trạng thái thanh toán không có bề mặt đọc | `200` `PaymentResponse` (đọc thanh toán của chính đơn mình) |
| 10.3 | GET | `/api/v1/admin/payments` | *chưa tồn tại* | `200` `[AdminPaymentResponse]` (mọi bản ghi; `UNMATCHED` = hàng đợi đối soát) |
| 10.4 | POST | `/api/v1/payments/payos/webhook` | *chưa tồn tại* — chưa có module nào gọi bước đã trả | `200` (PayOS xác nhận → đơn `PAID`) |

### Không phải endpoint nhưng **ĐỔI bề mặt client** (2)

| Bề mặt | Trước (009/010) | Sau (011) |
|---|---|---|
| Đường tới `PAID` | Đơn có thể tới `PAYMENT_PENDING` nhưng **không** có cách trả; `PAID` bất khả đạt qua HTTP | Khách trả qua `10.1`; webhook `10.4` đưa đơn sang **`PAID`** — giờ có thật |
| Truy cập thêm | — | `PaymentStatus` (**4** giá trị) và một **đường đọc thanh toán riêng**; trạng thái thanh toán **không** gộp vào response của đơn |

**Tổng: 4 endpoint mới, 0 đổi, 0 xoá; 1 đường nghiệp vụ mới có thật (`PAID`), 1 enum mới.** Không endpoint
nào của module khác bị đổi.

> **Bất biến của guide `010` vẫn đúng**: `POST /orders` tạo `PENDING` và **không** giữ hàng; artist `confirm`
> giữ hàng và mở hạn 60 phút; đơn `PAYMENT_PENDING` tự huỷ sau 60 phút; khách sửa được đơn trước khi trả.
> Guide này chỉ **thêm** phần thanh toán: sau khi có yêu cầu thanh toán, khách trả qua `10.1`.

---

## 5. Bảng mã lỗi

**Riêng module payment** (**6** mã, tất cả **MỚI** trong feature 011):

| Code | HTTP | FE nên làm |
|---|---|---|
| `PAYMENT_ORDER_NOT_FOUND` | 404 | Đơn không tồn tại **hoặc** của khách khác (cùng một câu). Hiện "không tìm thấy đơn", tải lại danh sách. `details` không có |
| `PAYMENT_ORDER_NOT_PAYABLE` | 409 | Đơn **không** ở trạng thái chờ thanh toán (đã trả, đã huỷ, hoặc còn chờ artist). Tải lại đơn; **chỉ** hiện nút trả khi đơn `PAYMENT_PENDING` |
| `PAYMENT_NOT_FOUND` | 404 | (Đọc) đơn của khách **chưa có thanh toán nào**, hoặc (webhook) orderCode lạ. Với `10.2`: hiện "đơn chưa có thanh toán", mời khách bắt đầu (`10.1`). Không thử lại |
| `PAYMENT_ALREADY_LIVE` | 409 | Đã có thanh toán còn hiệu lực nhưng link **không còn trả được** (huỷ/hết hạn ở cổng). Không có link mới; báo khách liên hệ shop để đối soát. **Không** tự thử lại |
| `PAYMENT_PROVIDER_ERROR` | 422 | Cổng lỗi khi tạo yêu cầu; **đơn không đổi**. Cho khách **thử lại** sau giây lát |
| ▲ `PAYMENT_WEBHOOK_INVALID_SIGNATURE` | 401 | Không phải FE gọi (webhook). FE không cần xử lý riêng |

**Dùng chung / tái sử dụng**: `VALIDATION_ERROR` 400 (kèm `details[].field`), `MALFORMED_REQUEST` 400,
`UNAUTHENTICATED` 401, `FORBIDDEN` 403, `RATE_LIMITED` 429, `INTERNAL_ERROR` 500.

`PAYMENT_WEBHOOK_INVALID_SIGNATURE` **không** phải `UNAUTHENTICATED`: nó là chữ ký webhook sai, không phải
phiên thiếu. Chỉ webhook thấy nó; FE người dùng không gọi webhook.

---

## 6. Luồng thanh toán (nhắc cho FE)

1. **Chờ artist xác nhận.** Đơn `PENDING` chưa trả tiền được (chưa giữ hàng). FE hiện trạng thái đơn; nút
   trả chỉ bật khi đơn `PAYMENT_PENDING`.
2. **Bắt đầu thanh toán.** Gọi `POST /orders/{id}/payment`; nhận `PaymentResponse` với `status: "PENDING"`,
   `checkoutUrl`, `qrCode`. Mở `checkoutUrl` (hoặc render `qrCode`). Nếu khách bấm lần nữa, endpoint **dùng
   lại** cùng liên kết — FE có thể gọi lại an toàn, kết quả không đổi.
3. **Khách trả ở PayOS.** Sau khi trả, PayOS redirect về `PAYMENT_RETURN_URL` (hoặc `PAYMENT_CANCEL_URL` khi
   huỷ). FE **không** tin redirect là dấu hiệu thành công.
4. **Xác nhận.** PayOS gọi webhook `10.4`; hệ thống xác minh, đối chiếu và áp **một lần**: thanh toán thành
   `PAID`, đơn (`GET /orders/{id}`) thành `PAID`, hàng giữ chỗ thành bán.
5. **Vòng lặp chờ của FE.** FE **không** có webhook; hãy **poll** `GET /orders/{id}/payment` (hoặc đọc lại
   đơn) tới khi `status` khác `PENDING`. Đừng giả định tức thời — webhook có độ trễ.
6. **Không áp được.** Nếu tiền tới nhưng không khớp, thanh toán thành `UNMATCHED` với `unmatchedReason`;
   **đơn không đổi**. FE hiện thông báo cho khách rằng giao dịch đang được đối soát; **không** tự đánh dấu
   đã trả. Operator xử lý qua `10.3`.
7. **Huỷ đơn.** Nếu đơn `PAYMENT_PENDING` bị huỷ hoặc hết hạn, yêu cầu ở cổng cũng bị huỷ; thanh toán thành
   `CANCELLED`. FE thấy điều đó qua `10.2`.
8. **Không hoàn tiền.** V1.0 **không** có API hoàn tiền. Đơn đã `PAID` **không** huỷ được (chuyển nhượng,
   guide `010`). Đừng dựng nút hoàn tiền.

---

## 7. Checklist FE

- [ ] Bật nút **thanh toán** chỉ khi đơn ở `PAYMENT_PENDING`.
- [ ] Gọi `POST /orders/{id}/payment`; dùng `checkoutUrl` (hoặc render `qrCode`) để đưa khách đi trả.
- [ ] Coi việc gọi `10.1` lặp là **an toàn** — nó dùng lại cùng liên kết, không tạo yêu cầu thứ hai.
- [ ] Xử lý `409 PAYMENT_ORDER_NOT_PAYABLE` bằng cách tải lại đơn (có thể artist chưa xác nhận / đã huỷ).
- [ ] Xử lý `422 PAYMENT_PROVIDER_ERROR` bằng cách mời khách **thử lại** (đơn không đổi).
- [ ] Sau khi khách quay lại từ PayOS, **poll** `GET /orders/{id}/payment` tới khi `status != "PENDING"`;
      **không** tin redirect.
- [ ] Hiển thị `status` theo bảng §3; **không** coi `UNMATCHED` là đã trả hay đã huỷ.
- [ ] Hiển thị `unmatchedReason` khi `status = "UNMATCHED"` và thông báo đang đối soát.
- [ ] Hiển thị tiền bằng `amount.amount` (số nguyên) + `amount.currency`; **không** quy đổi/làm tròn.
- [ ] `checkoutUrl`/`qrCode`/`unmatchedReason` có thể là `""`; coi `""` là *không có giá trị*, không phải
      lỗi.
- [ ] Trang admin: gọi `GET /admin/payments?status=UNMATCHED` cho màn hình đối soát; chỉ gọi khi token có
      role `ADMIN`, xử lý `403 FORBIDDEN` riêng.
- [ ] **Không** dựng UI hoàn tiền (V1.0 không có) và **không** gửi `userId`/chủ sở hữu ở body/query.
- [ ] **Không** gọi webhook `10.4` — đó là PayOS gọi backend.

---

## 8. Changelog & đối chiếu

**2026-10-11** — Viết guide cho feature 011 (module 08 Payment, module cuối của V1.0); bàn giao cho frontend
team. Đây là lần đầu tiên khách trả được tiền.

Đối chiếu với implementation, **có đếm** — mọi con số dưới đây được đếm lại từ chính nguồn, không chép từ
tài liệu khác.

**Endpoint và route** — **4**, đối chiếu hai chiều:

| Hướng | Nguồn | Kết quả |
|---|---|---|
| Guide → code | §2 liệt kê **4** mục | `internal/modules/payment/presentation/http/router.go` khai đúng **4** route: `Router` (`POST /`, `GET /`), `WebhookRouter` (`POST /`), `AdminRouter` (`GET /`) |
| Code → guide | router **4 route** | cả 4 đều có mặt ở §2 (2 khách, 1 admin, 1 webhook) |

Cùng khớp `specs/011-order-payment/contracts/openapi.yaml` (**4** path: `/orders/{orderId}/payment` với `post`
+ `get`, `/admin/payments` với `get`, `/payments/payos/webhook` với `post`). Mount ở `cmd/api/main.go`:
`/orders/{orderId}/payment`, `/payments/payos/webhook`, `/admin/payments`. **4 mới, 0 đổi, 0 xoá.**

**Trường JSON** — `internal/modules/payment/presentation/dto/dto.go` có **15** tag `json:"…"` trên **3** kiểu,
thành **14** tên member phân biệt:

| Kiểu | Số member | Member |
|---|---|---|
| `MoneyResponse` | 2 | `amount`, `currency` |
| `PaymentResponse` | 11 | `id`, `orderId`, `amount`, `status`, `provider`, `orderCode`, `checkoutUrl`, `qrCode`, `unmatchedReason`, `createdAt`, `updatedAt` |
| `AdminPaymentResponse` | 2 (+11 nhúng) | `userId`, `providerReference` (+ `PaymentResponse`) |

**14/14** tên phân biệt đều có mặt trong guide §1. Tổng **15** tag → **14** tên vì `amount` xuất hiện ở cả
`MoneyResponse` và `PaymentResponse`. Gồm đủ ba trường được yêu cầu: **`checkoutUrl`**, **`qrCode`**,
**`unmatchedReason`**. Không tag nào ngoài guide.

**Giá trị enum** — **4**: `PaymentStatus` = `PENDING`, `PAID`, `CANCELLED`, `UNMATCHED` (khớp
`internal/modules/payment/domain/constant/codes.go` `IsValidStatus`, `contracts/openapi.yaml` `PaymentStatus`,
và cột `payments_status_ck` trong `migrations/00012_payment.sql`). §3 liệt kê đủ **4**.

**Mã lỗi** — **6** mã riêng của module (`PAYMENT_ORDER_NOT_FOUND`, `PAYMENT_ORDER_NOT_PAYABLE`,
`PAYMENT_NOT_FOUND`, `PAYMENT_ALREADY_LIVE`, `PAYMENT_PROVIDER_ERROR`, `PAYMENT_WEBHOOK_INVALID_SIGNATURE` —
khớp `domain/constant/codes.go` và `contracts/error-codes.md`; **cả 6 mới** trong 011) cộng **6** mã **tái
sử dụng** (`VALIDATION_ERROR`, `MALFORMED_REQUEST`, `UNAUTHENTICATED`, `FORBIDDEN`, `RATE_LIMITED`,
`INTERNAL_ERROR`).

**HTTP status khẳng định** — **9**: `200`, `400`, `401`, `403`, `404`, `409`, `422`, `429`, `500`. Mỗi status
là một câu trả lời thật: `200` do cả bốn handler; `400` (`VALIDATION_ERROR`/`MALFORMED_REQUEST`); `401`
(`UNAUTHENTICATED` cho route khách, `PAYMENT_WEBHOOK_INVALID_SIGNATURE` cho webhook); `403` (`FORBIDDEN` do
role guard admin); `404` (`PAYMENT_ORDER_NOT_FOUND`, `PAYMENT_NOT_FOUND`); `409`
(`PAYMENT_ORDER_NOT_PAYABLE`, `PAYMENT_ALREADY_LIVE`); `422` (`PAYMENT_PROVIDER_ERROR`); `429`
(`RATE_LIMITED`); `500` (`INTERNAL_ERROR` mặc định của `mapError`). Module **không** dùng `201`, `204`,
`405`, `413`, `503` nên các status đó vắng mặt có chủ ý.
