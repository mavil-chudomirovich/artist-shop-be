# Module 08 — Payment

- **Trạng thái Spec Kit**: Hoàn tất (feature `011-order-payment` implement)
- **Spec**: [`specs/011-order-payment/spec.md`](../../specs/011-order-payment/spec.md)
- **Plan**: [`specs/011-order-payment/plan.md`](../../specs/011-order-payment/plan.md)
- **Tasks**: [`specs/011-order-payment/tasks.md`](../../specs/011-order-payment/tasks.md)
- **Contract**: [`specs/011-order-payment/contracts/openapi.yaml`](../../specs/011-order-payment/contracts/openapi.yaml),
  [`error-codes.md`](../../specs/011-order-payment/contracts/error-codes.md)
- **Frontend guide**: [`specs/011-order-payment/frontend-guide.md`](../../specs/011-order-payment/frontend-guide.md)
- **Deferred**: [`specs/011-order-payment/deferred.md`](../../specs/011-order-payment/deferred.md)
- **Code**: `internal/modules/payment/{domain,application,infrastructure,presentation}`,
  `migrations/00012_payment.sql`, `internal/contracts/{order.go,payment.go}`
- **Ưu tiên / Giai đoạn**: V1.0 (module **cuối** của V1.0)
- **Phụ thuộc**: order

## Mục đích

Cho khách **trả tiền online** cho một đơn **đang chờ thanh toán** qua cổng **PayOS**, và **tin cậy xác
nhận của cổng đúng một lần** — an toàn với tiền và tồn kho. Cổng báo kết quả qua **webhook có chữ ký**;
hệ thống xác minh, đối chiếu đơn + số tiền, rồi áp kết quả **một lần**: đơn chuyển `PAID` và hàng giữ
chỗ thành **bán**. Xác nhận **không áp được** thì **không** ghi nhận vào đơn — nó thành bản ghi
`UNMATCHED` để operator **đối soát**.

> Phiên bản MoMo/VietQR trong tài liệu cũ **đã bị thay** bởi PayOS (ADR 015 §5, ghi lại ở
> [decisions/019](../decisions/019-payos-payment.md)). Module **không** dùng SDK chính thức: tích hợp
> viết bằng **thư viện chuẩn** (`net/http`, `crypto/hmac`, `crypto/sha256`), **không thêm dependency**.

## Phạm vi MVP

Có:
- **Bắt đầu thanh toán** (`POST /orders/{orderId}/payment`): tạo yêu cầu PayOS cho **đúng tổng tiền
  hàng** và trả về **liên kết/QR**; một yêu cầu còn hiệu lực thì **dùng lại**, không tạo yêu cầu thứ hai.
- **Tin cậy xác nhận đúng một lần** (`POST /payments/payos/webhook`, **công khai**): xác minh chữ ký,
  đối chiếu orderCode/đơn/số tiền, áp kết quả một lần — ghi nhận thanh toán, đơn sang `PAID`, hàng giữ
  chỗ thành bán, **tất cả trong một transaction**.
- **Xác nhận cũ/không khớp không bị áp**: sai số tiền, nội dung đơn cũ (khác `order_version`), hoặc đơn
  không còn chờ thanh toán → bản ghi `UNMATCHED` kèm lý do, **đơn giữ nguyên**, **không** hoàn tiền.
- **Huỷ đơn chưa trả thì huỷ yêu cầu ở cổng** (nỗ lực tối đa, **không** chặn việc huỷ đơn; nhả hàng là của
  module 07).
- **Nhìn thấy & đối soát**: khách đọc thanh toán của **chính** đơn mình (`GET /orders/{orderId}/payment`);
  operator đọc **mọi** bản ghi và lọc `status=UNMATCHED` (`GET /admin/payments`).
- Trạng thái thanh toán riêng của module, **không** gộp vào response của đơn.

Không (hoãn — xem [`deferred.md`](../../specs/011-order-payment/deferred.md)):
- **Hoàn tiền**; đơn **đã trả tiền không huỷ** (được chuyển nhượng như module 07).
- **Đa tiền tệ**, **thanh toán định kỳ**.
- **Giao diện quản trị đầy đủ** (module 16); **thông báo in-app** (module 12).

## Thực thể dữ liệu

- `payments`: một lần thanh toán cho một đơn — `order_id` và `user_id` (**tham chiếu lỏng, không foreign
  key**: thanh toán và đơn ở hai module khác nhau), `order_code` (**số ngẫu nhiên 53-bit dương**,
  `UNIQUE` — khoá PayOS dùng để định danh), `amount` (`CHECK (> 0)`) + `currency` (`CHECK (~ '^[A-Z]{3}$')`,
  VND), `description` (mô tả ngắn PayOS hiển thị cho người trả), `order_version` (**phiên bản nội dung đơn
  lúc tạo**, `CHECK (>= 1)`), `status` (`CHECK` một trong **bốn**), `provider` (`payos`),
  `provider_reference` (**tham chiếu giao dịch của cổng**, NULL tới khi xác nhận — **partial unique index**
  biến nó thành khoá chống trùng), `payment_link_id`, `checkout_url`, `qr_code`, `unmatched_reason`
  (NULL trừ khi `UNMATCHED`), `created_at`/`updated_at`.
- `payment_events`: **một dòng mỗi quyết định** của module về một thanh toán — `payment_id` (FK →
  `payments(id)` **ON DELETE CASCADE**), `action` (`CHECK` một trong `PAYMENT_CREATED`, `PAYMENT_APPLIED`,
  `PAYMENT_CANCELLED`, `PAYMENT_UNMATCHED`), `actor_id` (tham chiếu lỏng; NULL cho webhook của cổng vì
  không có tài khoản tác nhân), `detail` (lý do ngắn hoặc tham chiếu của cổng), `created_at`.

**Ràng buộc ở tầng lưu trữ** (Constraint II): `payments_status_ck` (bốn giá trị), `payments_currency_ck`,
`payments_amount_ck`, `payments_order_version_ck`, `payments_unmatched_ck`
(`(status = 'UNMATCHED') = (unmatched_reason IS NOT NULL)`), `payments_order_code_key` (unique),
`payments_provider_reference_key` (partial unique), `payments_live_order_key` (partial unique trên
`order_id` khi `status = 'PENDING'` — tối đa một thanh toán còn hiệu lực mỗi đơn), cộng hai index cho đường đọc (`payments_order_idx` cho
khách, `payments_status_idx` cho hàng đợi đối soát). Chi tiết đầy đủ ở
[`specs/011-order-payment/data-model.md`](../../specs/011-order-payment/data-model.md).

## Luồng nghiệp vụ chính

1. **Bắt đầu** (`POST /orders/{orderId}/payment`): đọc đơn **qua hợp đồng** `OrderPayment` (không đọc
   bảng của module 07); từ chối nếu không phải của khách (trả như không tìm thấy) hoặc không chờ thanh
   toán; nếu đã có thanh toán còn hiệu lực thì **dùng lại** và trả **chính liên kết đó**; nếu chưa, sinh
   `order_code`, gọi PayOS **trước** transaction (để transaction ngắn, không bọc HTTP), rồi ghi dòng
   `PENDING` + sự kiện `PAYMENT_CREATED` trong **một** transaction. Cổng lỗi → `422`, **không** ghi gì,
   đơn nguyên vẹn.
2. **Webhook** (`POST /payments/payos/webhook`): xác minh **chữ ký** trên đúng byte nhận được; tìm thanh
   toán theo `order_code` (không thấy → `404`, **chỉ log warning**, không ghi gì); nếu đã `PAID` thì
   **no-op**; so **số tiền**; gọi `OrderPayment.MarkPaid` với `order_version` đã lưu; khi thành công, đánh
   dấu `PAID` + lưu `provider_reference` + sự kiện `PAYMENT_APPLIED`, **cùng một transaction** với bước
   đơn sang `PAID` và bán hàng (Constitution II).
3. **Không áp được** → ghi `UNMATCHED` kèm lý do (sai số tiền / phiên bản đơn lệch / đơn không còn chờ
   thanh toán) + sự kiện `PAYMENT_UNMATCHED`; **đơn không đổi**, **không** hoàn tiền; webhook vẫn trả `2xx`.
4. **Huỷ đơn** (module 07 gọi qua hợp đồng `PaymentCancellation`, **sau khi transaction của đơn commit**):
   huỷ yêu cầu ở PayOS (nỗ lực tối đa), đánh dấu `CANCELLED` + sự kiện `PAYMENT_CANCELLED`. Lỗi cổng được
   ghi log và **không** làm hỏng việc huỷ đơn; đơn không có thanh toán còn hiệu lực là no-op.
5. **Đọc** (`GET /orders/{orderId}/payment` của khách; `GET /admin/payments` của operator, lọc được theo
   `status`).

## Trạng thái (state machine)

```
PENDING ──apply──► PAID            (cuối)
   │
   ├──► CANCELLED                 (đơn bị huỷ / liên kết bị huỷ)
   └──► UNMATCHED                 (xác nhận đã xác minh nhưng không áp được)
```

Các cạnh **hợp lệ duy nhất**: `PENDING → PAID` (áp một xác nhận hợp lệ); `PENDING → CANCELLED` (đơn bị
huỷ); bất kỳ → `UNMATCHED` (ghi lại một xác nhận không áp được). `PAID`/`CANCELLED`/`UNMATCHED` đều **cuối**
và không có đường lùi; một xác nhận lặp trên thanh toán đã `PAID` là **no-op thành công**. Trạng thái chỉ
đổi qua phương thức chuyển của entity; `status` là **check constraint** ở tầng lưu trữ nên không ghi thẳng
giá trị ngoài tập được (Constitution III). Tập cố ý nhỏ — **không** có `CREATED`/`FAILED`/`EXPIRED` vì
không giá trị nào trong số đó là trạng thái quan sát được (tạo liên kết và lên sóng là một bước nguyên tử;
cổng lỗi thì không ghi gì; hết hạn là đường huỷ của đơn và trùng nghĩa `CANCELLED`).

## PayOS — tích hợp và chữ ký

- **Tạo liên kết**: `POST {PAYOS_BASE_URL}/v2/payment-requests` với header `x-client-id` + `x-api-key`;
  body gồm `orderCode`, `amount`, `description`, `cancelUrl`, `returnUrl` và `signature`. Chữ ký là
  **HMAC_SHA256** với **checksum key** trên đúng các trường **sắp theo alphabet**:
  `amount=$amount&cancelUrl=$cancelUrl&description=$description&orderCode=$orderCode&returnUrl=$returnUrl`.
  Mô tả là chuỗi ngắn, ổn định, suy từ `orderCode` (`DH%07d`) vì tài liệu PayOS ghi giới hạn 9 ký tự cho
  tài khoản ngân hàng không liên kết PayOS.
- **Webhook**: body `{code, desc, success, data, signature}`; chữ ký = **HMAC_SHA256** với checksum key
  trên **các trường của `data` sắp theo alphabet**, `key=value&...`, giá trị null/undefined thành chuỗi
  rỗng. Xác minh trên **đúng byte nhận được**, trước khi tin bất cứ thứ gì. Cả hai thuật toán được kiểm
  thử bằng **vector mẫu trong tài liệu PayOS** (không gọi provider thật).
- **Huỷ liên kết**: `POST {PAYOS_BASE_URL}/v2/payment-requests/{id}/cancel` với `{reason}`.
- **Khi chưa cấu hình** (`PAYOS_*` rỗng): adapter dùng `LogProvider` — trả một liên kết giả và **từ chối
  mọi webhook** (không có checksum key để kiểm), nên mọi đường **không phải provider** vẫn chạy mà không
  cần credential (FR-019, research D11).

## Ánh xạ `orderCode` và áp-một-lần hai tầng

- **`orderCode`**: PayOS định danh bằng **số nguyên** `orderCode`, còn đơn của shop là UUID. Mỗi lần tạo
  liên kết sinh một **số ngẫu nhiên 53-bit dương**, lưu cạnh `order_id` và **unique** bằng index; vì vậy
  webhook tìm được thanh toán **mà không tin gì từ client**. Va chạm được **thử lại** với mã mới (tối đa
  3 lần), không bao giờ lộ ra client.
- **Áp-một-lần hai tầng**: (1) thanh toán tự **short-circuit** khi đã `PAID` (gửi lặp là no-op); (2) bước
  bán hàng của module 05 nhận một `source_reference` **ổn định** nên bán theo từng dòng cũng idempotent.
  Việc ghi nhận thanh toán, bước đơn sang `PAID` và việc bán hàng **commit cùng nhau** (Constitution II).

## Huỷ và hàng đợi đối soát

- **Huỷ**: khi một đơn đang chờ thanh toán bị huỷ (khách huỷ hoặc sweeper hết hạn), module 07 gọi
  `CancelForOrder` **sau khi transaction của đơn commit** (qua hợp đồng `PaymentCancellation`). Việc gọi
  nằm trên use case `Cancel` — đường mà cả `CancelMine` lẫn sweeper đi qua — nên **một** điểm gọi phủ cả
  hai. Gọi là **nỗ lực tối đa**: lỗi cổng được log, **không** chặn việc huỷ đơn.
- **Đối soát**: mọi bản ghi `UNMATCHED` là việc của operator. `GET /admin/payments?status=UNMATCHED` là
  **hàng đợi đối soát**, **chỉ đọc** — V1.0 **không** có thao tác "áp tay" qua HTTP; operator xử lý
  **ngoài hệ thống** (không hoàn tiền). Link mồ côi (liên kết sống ở PayOS nhưng không có dòng thanh toán,
  do cửa sổ giữa phản hồi tạo link và lúc ghi dòng) làm webhook trả `PAYMENT_NOT_FOUND` và **chỉ log
  warning**; khôi phục tự động đã hoãn (`deferred.md` D6).

## Yêu cầu chức năng sơ bộ

- Số tiền của yêu cầu **bằng đúng tổng đơn**, là **số nguyên đơn vị nhỏ nhất + currency**, **không** phí
  ship (FR-004, ADR 015 §5).
- Chỉ chấp nhận xác nhận **xác minh được nguồn gốc**; xác nhận đã xác minh phải **khớp** đơn và số tiền.
- Áp kết quả, đổi trạng thái đơn và bán hàng trong **một transaction**, ghi audit; gửi lặp **không** tạo
  hiệu ứng thứ hai.
- Chủ sở hữu lấy từ **session**, không bao giờ từ request; đơn của khách khác trả cùng
  `PAYMENT_ORDER_NOT_FOUND`.
- Điểm nhận xác nhận **công khai** nhưng **xác minh chữ ký**; credential của cổng lấy từ biến môi trường,
  **không** commit.
- **Không** hoàn tiền; đơn đã trả tiền **không** huỷ (được chuyển nhượng).

## Tiêu chí hoàn thành

- [x] Khách mở thanh toán cho đơn `PAYMENT_PENDING` → yêu cầu đúng tổng đơn + liên kết; mở lần hai **dùng
  lại** cùng liên kết, không thêm dòng (SC-001) — chứng minh trên PostgreSQL thật.
- [x] Webhook hợp lệ áp **đúng một lần**: đơn `PAID`, hàng bán một lần; gửi lặp không đổi gì; hai xác nhận
  đồng thời bán **một lần** (SC-002) — chứng minh trên PostgreSQL thật.
- [x] Xác nhận sai số tiền / nội dung đơn cũ / đơn không còn chờ thanh toán **không bao giờ** được áp:
  bản ghi `UNMATCHED` kèm lý do, đơn giữ nguyên (SC-003) — chứng minh trên PostgreSQL thật.
- [x] Huỷ đơn chưa trả huỷ yêu cầu ở cổng và nhả hàng; lỗi cổng không chặn việc huỷ (SC-004).
- [x] Khách thấy thanh toán của đơn mình, không thấy của người khác; operator thấy mọi bản ghi và
  `status=UNMATCHED` chỉ trả hàng đợi đối soát (SC-005).
- [x] Không bí mật nào bị commit; webhook từ chối mọi yêu cầu không xác minh được (SC-006).
- [x] Cổng lỗi khi tạo yêu cầu không để đơn ở trạng thái sai (SC-007).

Bốn endpoint của module nằm ở [api-reference.md](../api-reference.md) mục 10. Guide bàn giao cho frontend
là [`frontend-guide.md`](../../specs/011-order-payment/frontend-guide.md).

> **Module này là module cuối của V1.0.** Sau nó, toàn bộ `00`–`08` đã được giao.

## Ghi chú / câu hỏi mở

- Câu hỏi mở cũ *"Chữ ký/xác thực webhook MoMo theo cơ chế nào?"* **đã có câu trả lời**: nhà cung cấp là
  **PayOS**, xác thực bằng **chữ ký HMAC_SHA256** trên `data` của webhook (ADR 015 §5, ADR 019).
- Câu hỏi mở cũ *"Chính sách hoàn tiền/hủy sau thanh toán?"* **đã chốt**: V1.0 **không hoàn tiền**; đơn đã
  trả tiền **không huỷ** mà **chuyển nhượng** (module 07, ADR 015 §5). Các khoản không áp được đưa vào
  hàng đợi đối soát cho operator.
- Quyết định dài hạn ở [decisions/019](../decisions/019-payos-payment.md) (PayOS, hai hợp đồng, `orderCode`,
  áp-một-lần, không hoàn tiền) và [decisions/015](../decisions/015-shop-first-version-plan-and-payos.md)
  (kế hoạch V1.0, chọn PayOS).

## Tham chiếu

- [development/payment-testing.md](../development/payment-testing.md) — chạy thử thanh toán thật (ngrok +
  `confirm-webhook`)
- [`specs/011-order-payment/deferred.md`](../../specs/011-order-payment/deferred.md) — các điểm cố ý hoãn
- [decisions/019](../decisions/019-payos-payment.md) — PayOS qua thư viện chuẩn, hai hợp đồng, `orderCode`,
  áp-một-lần, "ghi nhận bất thường, không bao giờ hoàn tiền"
- [decisions/015](../decisions/015-shop-first-version-plan-and-payos.md) — kế hoạch V1.0 và PayOS
- [decisions/018](../decisions/018-order-confirmation-and-editing.md) — `order_version` (seam mà module 08
  dùng) và state machine đơn
- `docs/product/backend-spec.md` (Payment System)
- `.specify/memory/constitution.md` (Transactional Integrity, Security)
