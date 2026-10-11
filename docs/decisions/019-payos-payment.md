# ADR 019 - Payment: tích hợp PayOS bằng thư viện chuẩn, hai hợp đồng liên module, `orderCode` ngẫu nhiên, áp-một-lần hai tầng, và "ghi nhận bất thường, không bao giờ hoàn tiền"

- **Status**: Accepted
- **Decision Date**: 2026-10-11
- **Decision Maker**: Mavil
- **Feature**: `specs/011-order-payment` (module 08 Payment, module cuối của V1.0)
- **Liên quan**: [ADR 015](015-shop-first-version-plan-and-payos.md) §5 (kế hoạch V1.0, chọn PayOS, không phí
  ship, không hoàn tiền), [ADR 018](018-order-confirmation-and-editing.md) (`order_version`, hạn thanh toán
  60 phút, mailer dùng chung), [ADR 017](017-order-checkout.md) (đơn, `WithTx`, bước `PAID` giao nhưng để
  module 08 điều khiển), [ADR 014](014-inventory-hold-and-availability.md) (giữ chỗ/khả dụng module 05)

## 1. Bối cảnh

Module 07 (feature `009` + `010`) đã dựng vòng đời đơn tới `PAYMENT_PENDING` và **giao** bước
`PAYMENT_PENDING → PAID` nhưng **chưa** gọi nó qua HTTP: ADR 017 chốt "bước `PAID` do **module 08 Payment**
điều khiển". Feature `010` để lại đúng hai seam cho module 08: `order_version` (so nội dung cũ/mới) và
`payment_expires_at` (hạn thanh toán 60 phút); và ADR 018 ghi rõ "**việc huỷ payment session khi sửa/… phải
được đáp**" khi module 08 ra đời.

Module 08 phải giao phần **trả tiền online** mà không vi phạm bốn ràng buộc:

1. **Constitution I** cấm module này đọc bảng của module 07 (và ngược lại) — giao tiếp chỉ qua
   `internal/contracts`.
2. **Constitution II** buộc thay đổi tiền/tồn kho và trạng thái thanh toán nằm trong **một** transaction,
   và callback của cổng phải **idempotent**.
3. **Constitution III** buộc vòng đời thanh toán là **state machine** tường minh.
4. **ADR 015 §5 D4** chốt PayOS và **"không thêm dependency"** — nhưng PayOS định danh bằng một số nguyên
   `orderCode` (tối đa 53-bit), còn đơn của shop là UUID, và xác nhận qua **webhook có chữ ký HMAC_SHA256**.

Ngoài ra, V1.0 **không có hoàn tiền** (ADR 015 §5 D5): khi một xác nhận đã xác minh **không áp được** vào
đơn (đơn đã sửa, đã huỷ, hết hạn, hoặc sai số tiền), hệ thống phải **không** làm hỏng đơn và **không** tự
hoàn tiền — nhưng cũng **không** được im lặng bỏ qua, vì tiền đã tới.

Năm quyết định dưới đây định hình lược đồ, ranh giới liên module, thuật toán chữ ký và hành vi khi bất
thường, nên chúng được ghi thành ADR chứ không chỉ nằm trong `research.md`.

## 2. Quyết định

### 2.1 Tích hợp PayOS bằng thư viện chuẩn — không thêm dependency

Client PayOS nằm ở `internal/modules/payment/infrastructure/implement/payos`, viết bằng **`net/http` +
`crypto/hmac` + `crypto/sha256` + `encoding/json`**, cài đặt port `Provider` của module với ba thao tác:

| Thao tác | Method + path | Dùng |
|---|---|---|
| Tạo liên kết | `POST /v2/payment-requests` | Bắt đầu thanh toán |
| Đọc liên kết | `GET /v2/payment-requests/{id}` | **Để dành** cho operator đối soát thủ công; feature này **không** gọi |
| Huỷ liên kết | `POST /v2/payment-requests/{id}/cancel` | Huỷ khi đơn bị huỷ |

Xác thực bằng header `x-client-id` + `x-api-key` (từ biến môi trường). Chữ ký tạo liên kết là **HMAC_SHA256**
với checksum key trên đúng các trường **sắp alphabet**: `amount`, `cancelUrl`, `description`, `orderCode`,
`returnUrl`. Chữ ký webhook là HMAC_SHA256 trên các trường của object `data` **sắp alphabet**,
`key=value&...`, null/undefined thành chuỗi rỗng. Cả hai thuật toán được test bằng **vector mẫu trong tài
liệu PayOS** — chứng minh bản cài đặt stdlib, không giả định.

Khi `PAYOS_*` **rỗng**, module dùng `LogProvider`: trả một liên kết giả và **từ chối mọi webhook** (không có
checksum key để kiểm). Nhờ vậy mọi đường **không phải provider** vẫn chạy được mà không cần credential.

### 2.2 Hai hợp đồng liên module, mỗi chiều một cái

Module 08 và module 07 nói chuyện **chỉ** qua `internal/contracts`, mỗi chiều một interface với adapter ở
composition root:

- **`OrderPayment`** (module 07 cài đặt, module 08 tiêu thụ): `PaymentTarget(ctx, orderID)` trả các sự thật
  thanh toán của đơn (chủ, tổng tiền + currency, `Version`, `PaymentExpiresAt`, cờ `AwaitingPayment`), và
  `MarkPaid(ctx, orderID, sourceReference, expectedVersion)` điều khiển bước đã trả.
- **`PaymentCancellation`** (module 08 cài đặt, module 07 tiêu thụ): `CancelForOrder(ctx, orderID)` để module
  07 báo cho module 08 rằng một đơn đã bị huỷ, nên yêu cầu thanh toán ở cổng cũng bị huỷ.

`OrderPaymentTarget` chỉ mang **kiểu cục bộ của package contract** (`AwaitingPayment` là **cờ**, không phải
kiểu trạng thái của module 07), đúng như `contracts.ProductSummary` mang `OnSale` thay vì kiểu trạng thái
của module 04 (`docs/system-design/contract-purity.md`).

**Bước `MarkPaid` là version-aware**: một đơn đã `PAID` (hoặc xa hơn) là **no-op thành công** — giữ nguyên
hành vi đã giao và test của feature `010`, để một callback lặp không áp hai lần ở tầng đơn; nhưng một đơn
**không còn chờ thanh toán**, **đã huỷ/hết hạn**, hoặc có `Version` **khác** `expectedVersion` thì **bị từ
chối** (sentinel `ErrOrderNotPayable` / `ErrOrderStale`), nên một xác nhận thuộc nội dung cũ **không bao
giờ** được áp.

**Vì sao cần version-aware**: một đơn `PAYMENT_PENDING` có thể bị khách **sửa** (về `PENDING`) rồi artist
**xác nhận lại** — đơn lại `PAYMENT_PENDING` ở **cùng trạng thái nhưng phiên bản mới**. Chỉ **phiên bản**
mới phân biệt được liên kết cũ với nội dung hiện tại; trạng thái không đủ.

**Vì sao cần `PaymentCancellation`**: FR-015 buộc huỷ đơn chưa trả cũng huỷ yêu cầu ở cổng, và dự án **không
có event bus**; cách hợp hiến duy nhất để một module hành động theo sự kiện của module khác là một hợp đồng
với adapter ở composition root. Điểm gọi đặt trên use case `Cancel` — đường mà cả khách huỷ lẫn sweeper hết
hạn đi qua — nên **một** điểm gọi phủ cả hai; gọi **sau khi transaction của đơn commit**, đúng như module 07
đã gọi `Notifier` sau commit (feature `010`). Gọi là **nỗ lực tối đa**: port `Cancellations` nil-tolerant,
adapter nil là no-op, lỗi cổng được log và **không** chặn việc huỷ đơn.

### 2.3 `orderCode` là số ngẫu nhiên 53-bit, chốt bằng unique index

Mỗi yêu cầu thanh toán sinh một **số nguyên dương ngẫu nhiên 53-bit**, lưu cạnh `order_id` và **duy nhất**
bằng `payments_order_code_key`. Webhook mang `orderCode`, nên shop tìm được thanh toán **mà không tin gì từ
client**. Va chạm được **thử lại** bằng mã mới (tối đa 3 lần); `ErrOrderCodeCollision` không bao giờ lộ ra
client.

Không gửi UUID của đơn (PayOS buộc số nguyên) và không dùng mã tuần tự (lộ khối lượng và dễ đoán).

### 2.4 Áp-một-lần **hai tầng**, và bước đã trả nằm trong một transaction

Webhook, sau khi xác minh chữ ký và tìm được thanh toán theo `orderCode`:

1. Nếu thanh toán đã `PAID` → **no-op thành công** (tầng 1: chính bản ghi thanh toán short-circuit).
2. So **số tiền**; lệch → ghi `UNMATCHED`, không áp.
3. Gọi `OrderPayment.MarkPaid(orderID, sourceReference, payment.order_version)`, với `sourceReference` là
   tham chiếu **ổn định** của thanh toán (ví dụ `payos:<order_code>`), nên việc bán theo **từng dòng** của
   module 05 cũng idempotent (tầng 2).
4. Thành công thì đánh dấu thanh toán `PAID` + lưu `provider_reference`, **cùng một transaction** với bước
   đơn sang `PAID` và việc bán hàng.

Gọi provider **trước** khi ghi dòng (transaction không bọc HTTP), nên transaction thanh toán luôn ngắn. Cửa
sổ giữa phản hồi tạo liên kết và lúc dòng commit là **có thật**: nếu bước ghi hỏng, một liên kết sống ở
PayOS không có dòng thanh toán; webhook sau đó trả `PAYMENT_NOT_FOUND`, module **chỉ log warning**, và
operator đối soát ngoài hệ thống (khôi phục tự động đã hoãn — `specs/011-order-payment/deferred.md`).

### 2.5 Ghi nhận bất thường, **không bao giờ** hoàn tiền

Một xác nhận đã xác minh nhưng **không áp được** — sai số tiền, `order_version` lệch, hoặc đơn không còn chờ
thanh toán — trở thành một bản ghi **`UNMATCHED` kèm lý do**; đơn **giữ nguyên**, **không** hoàn tiền
(ADR 015 §5 D5). Webhook **vẫn trả `2xx`** (từ chối sẽ khiến cổng thử lại mãi). Operator đọc
`GET /admin/payments?status=UNMATCHED` — **hàng đợi đối soát**, **chỉ đọc**; V1.0 **không** có thao tác "áp
tay" qua HTTP và **không** hoàn tiền trong hệ thống.

**Vòng đời thanh toán** (Constitution III) là **bốn** trạng thái, mỗi trạng thái đều tới được:

```
PENDING ──apply──> PAID            (cuối)
   │
   ├──> CANCELLED                 (đơn bị huỷ / liên kết bị huỷ)
   └──> UNMATCHED                 (xác nhận đã xác minh nhưng không áp được)
```

`CREATED`/`FAILED`/`EXPIRED` **không** tồn tại vì không tới được: tạo liên kết và lên sóng là **một** bước
nguyên tử (cổng lỗi thì không ghi gì), và hết hạn là đường huỷ của đơn nên trùng nghĩa `CANCELLED`. Trạng
thái chỉ đổi qua phương thức chuyển của entity; `status` là **check constraint** ở tầng lưu trữ
(`payments_status_ck`), nên ghi thẳng giá trị ngoài tập là bất khả.

## 3. Hệ quả

| Hệ quả | Chi tiết |
|---|---|
| Tích cực | **Không thêm dependency**: chỉ thư viện chuẩn, đúng ADR 015 §5 D4; bề mặt nhỏ (tạo/huỷ liên kết + xác minh webhook) |
| Tích cực | **Crypto được chứng minh**, không giả định: hai chữ ký test bằng **vector mẫu của tài liệu PayOS** |
| Tích cực | **Ranh giới module sạch**: hai hợp đồng, mỗi chiều một cái, kiểu contract-local, không module nào đọc bảng của module kia |
| Tích cực | **Xác nhận cũ không áp được**: `order_version` phân biệt nội dung cũ/mới ngay cả khi trạng thái đơn giống hệt — thứ trạng thái không làm được |
| Tích cực | **Áp đúng một lần, hai tầng**: bản ghi thanh toán short-circuit + `source_reference` của module 05; một transaction gộp ghi nhận + trạng thái đơn + bán hàng (Constitution II) |
| Tích cực | **Huỷ phủ cả hai đường**: một điểm gọi trên `Cancel` phủ `CancelMine` lẫn sweeper; best-effort nên cổng lỗi không chặn huỷ đơn |
| Tích cực | **Bất thường không bị nuốt**: tiền tới mà không áp được thì thành bản ghi `UNMATCHED` cho operator, thay vì mất tích trong log |
| Tiêu cực | **Tự viết client**: phải tự theo dõi khi PayOS đổi API/chữ ký; SDK chính thức sẽ tự cập nhật (đổi lại: thêm dependency, đã bị ADR 015 §5 D4 loại) |
| Tiêu cực | **Cửa sổ link mồ côi** là có thật: phản hồi tạo liên kết thành công nhưng dòng chưa commit thì một liên kết sống không có bản ghi; phải đối soát tay |
| Tiêu cực | **Hàng đợi `UNMATCHED` chỉ đọc**: V1.0 không có "áp tay"; operator xử lý ngoài hệ thống, nên không có đường tự động đóng một khoản lệch |
| Tiêu cực | **Hai trục trạng thái**: đơn một trục (`PAID`), thanh toán một trục riêng (`PENDING/PAID/CANCELLED/UNMATCHED`); client phải đọc endpoint thanh toán riêng, không gộp vào response đơn |

**Xem lại quyết định khi**: PayOS đổi API/chữ ký khiến tự viết client đắt hơn dùng SDK; hoặc khi module 16
(Admin) cần một hành động "áp tay/đối soát" qua HTTP; hoặc khi có nhu cầu thật về **hoàn tiền** (khi đó
`UNMATCHED` cần một đường ra chính thức); hoặc khi có nhu cầu **đa tiền tệ** / **thanh toán định kỳ**.

## 4. Cách tiếp cận bị loại

| Phương án | Vì sao không chọn |
|---|---|
| Dùng SDK `payos-lib-golang` | ADR 015 §5 D4 chốt "không thêm dependency"; bề mặt cần dùng nhỏ, tự viết rẻ hơn cái giá của một dependency |
| Module 08 đọc bảng `orders` trực tiếp | Cấm bởi Constitution I; mọi tương tác với đơn phải qua `internal/contracts` |
| Chỉ so **trạng thái** đơn để phát hiện xác nhận cũ | Sai: sau khi sửa rồi xác nhận lại, đơn lại `PAYMENT_PENDING` ở **cùng trạng thái** — chỉ `order_version` phát hiện được liên kết cũ |
| Bỏ no-op cho đơn đã `PAID` ở `MarkPaid` | Phá tính idempotent đã giao và test của feature `010` mà không được gì (webhook đã short-circuit trên thanh toán `PAID`) |
| Tạo liên kết và ghi dòng **trong cùng** transaction, gọi provider giữa transaction | Giữ một transaction DB qua một HTTP call; rollback vẫn để lại liên kết sống ở cổng y hệt, mà còn giam connection |
| Ghi dòng trước khi gọi provider | Vẫn để lại link/dòng lệch nhau khi hỏng, và thêm trạng thái trung gian không cần thiết |
| Dùng `orderCode` tuần tự | Lộ khối lượng đơn và dễ đoán; số ngẫu nhiên + unique index là rẻ và kín |
| Gửi UUID đơn làm `orderCode` | Bất khả: PayOS buộc số nguyên 53-bit |
| Một trục trạng thái thanh toán gộp vào `orders` | Cho đơn hai trục trạng thái và ghép module 07 vào provider; feature `009` chốt đơn giữ **một** trục |
| Dùng chung một hợp đồng cho cả hai chiều | Pillar: mỗi module cài đặt phần của mình; hai interface một chiều phản ánh đúng ai cung cấp cái gì |
| Tự động hoàn tiền khi không áp được | ADR 015 §5 D5: V1.0 không hoàn tiền; và hoàn tiền tự động là nghiệp vụ chưa có |
| Tự động khôi phục link mồ côi ngay trong webhook | Không có dòng thanh toán thì không có `order_version`/số tiền đã lưu để đối chiếu an toàn; đã hoãn |
| Thêm `CREATED`/`FAILED`/`EXPIRED` | Không giá trị nào tới được — trọng lượng chết (Constitution III, VII) |
| Endpoint `POST /payments` cấp cao thay vì dưới đơn | Thanh toán luôn thuộc một đơn; đặt dưới `/orders/{orderId}/payment` nói đúng quan hệ |
| Gộp trường thanh toán vào response của đơn | Clarification của feature 011 chốt **đường thanh toán riêng**; trộn vào đơn làm bẩn cả hai hợp đồng |
| Event bus cho sự kiện huỷ đơn | Constitution VII: không dựng máy móc khi chưa đo được nhu cầu; một hợp đồng đủ |

## 5. Bổ sung (2026-10-11)

Module 08 là **module cuối của V1.0**. Sau nó, toàn bộ `00`–`08` đã được giao (xem
`docs/modules.md`). Các việc cố ý hoãn của feature này — hoàn tiền, đa tiền tệ, thanh toán định kỳ, thanh
toán end-to-end với provider thật, đăng ký URL webhook một lần (`POST /confirm-webhook`), khôi phục tự
động link mồ côi, và đường "áp tay" cho `UNMATCHED` — nằm ở `specs/011-order-payment/deferred.md`.
