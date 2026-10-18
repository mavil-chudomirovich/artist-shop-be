# ADR 023 - Đơn hàng: hai bước giao có ảnh bằng chứng, đổi tên `SHIPPED` → `SHIPPING` và thêm `DELIVERED`, hoàn tất do khách hoặc sweeper 120 giờ, feedback cấp đơn, và luật ảnh dependency-free trong `domain`

- **Status**: Accepted
- **Decision Date**: 2026-10-18
- **Decision Maker**: Mavil
- **Feature**: `specs/019-order-delivery` (module 07 Order)
- **Supersedes**: các cạnh **sau `PAID`** của state machine trong
  [ADR 018](018-order-confirmation-and-editing.md) (feature `010-order-confirmation`): tên trạng thái
  `SHIPPED`, việc thiếu trạng thái "giao thành công", và việc **operator** đánh dấu `COMPLETED`; đồng
  thời **thay thế tập "trạng thái đã kiếm tiền"** mà [ADR 022](022-admin-dashboard-read-model.md) §2.5
  nêu (`PAID`/`SHIPPED`/`COMPLETED`). ADR 018 và ADR 022 **không** bị sửa; cái đầu vẫn đúng cho các cạnh
  trước `PAID`, cái sau vẫn đúng cho mọi quyết định khác của dashboard.
- **Liên quan**: [ADR 017](017-order-checkout.md) (checkout, snapshot dòng),
  [ADR 014](014-inventory-hold-and-availability.md) (giữ chỗ/khả dụng module 05),
  [ADR 013](013-product-visibility-media-and-hard-delete.md) (ảnh sản phẩm, luật media per-module),
  [ADR 010](010-avatar-upload-refusal-and-startup-guard.md) (trần upload theo route + guard khởi động),
  [ADR 015](015-shop-first-version-plan-and-payos.md) §5 (không phí ship, không hoàn tiền)

## 1. Bối cảnh

Sau feature `010`, vòng đời một đơn **sau khi trả tiền** rất ngắn: admin `ship` (`PAID → SHIPPED`) rồi
`complete` (`SHIPPED → COMPLETED`), hai cú nhấp chuột, không bằng chứng và không có bước "khách đã nhận
hàng". Ba vấn đề:

1. **Không có bằng chứng giao hàng.** Shop tranh là một artist gửi hàng vật lý; cần **ảnh** làm bằng
   chứng bàn giao cho bên vận chuyển và bằng chứng giao tới khách, để xử lý tranh chấp.
2. **`SHIPPED` mơ hồ.** Một trạng thái tên "đã giao" gộp hai thời điểm khác nhau: "đã giao cho bên vận
   chuyển" (đang trên đường) và "khách đã nhận". Không có chỗ cho khoảng ở giữa.
3. **Operator hoàn tất thay khách.** Đơn chỉ được đánh `COMPLETED` khi admin nhấp, nên **không** phản
   ánh ý chí của khách và dễ kẹt. Kế hoạch đơn hàng muốn **khách xác nhận đã nhận**, hoặc đơn **tự hoàn
   tất sau một khoảng** nếu khách im lặng.

Ngoài ra, phản hồi của khách về đơn chỉ có chỗ ở module 13 Review (V2.0, chưa tồn tại), trong khi shop
muốn một kênh **feedback cấp đơn** đơn giản ngay bây giờ.

Ràng buộc định hình cách làm:

- **Constitution I**: `presentation → application → domain`, `infrastructure → domain`; `domain` chỉ
  import stdlib, `share/access` và `github.com/google/uuid`. Một module **không** import nội bộ module
  khác. Giao tiếp liên module qua `internal/contracts`.
- **Constitution II/III**: tiền là integer minor units + currency, không float; trạng thái là state
  machine tường minh, đổi **chỉ** qua hàm transition; ràng buộc quan trọng phải có ở **tầng lưu trữ**.
- **Constitution V**: upload phải kiểm định theo **chữ ký nội dung** (không tin tên tệp/kiểu client khai),
  trần kích thước và số lượng, **trước khi** lưu trữ.
- **Constitution VIII**: thay đổi enum là **breaking** với client → phải có frontend guide và change log.

## 2. Quyết định

### 2.1 Bảy trạng thái: `SHIPPED` đổi tên `SHIPPING`, thêm `DELIVERED`

Đơn có **bảy** trạng thái: `PENDING`, `PAYMENT_PENDING`, `PAID`, `SHIPPING` (đang giao, **đổi tên từ
`SHIPPED`**), `DELIVERED` (giao thành công, **mới**), `COMPLETED`, `CANCELLED`. Các cạnh **sau `PAID`**:

```
PAID ──ship──► SHIPPING ──deliver──► DELIVERED ──khách xác nhận / tự động 120h──► COMPLETED
```

Migration `00018_order_delivery.sql` **drop** `orders_status_ck`, **backfill** `UPDATE orders SET status =
'SHIPPING' WHERE status = 'SHIPPED'`, rồi **add** check bảy giá trị; `Down` làm ngược lại (backfill
`SHIPPING → SHIPPED` và khôi phục check sáu giá trị). Backfill chạy **trước** khi siết check, nên một
database đang giữ đơn của 010 migrate sạch, không mất bản ghi (FR-001, FR-002, FR-024).

### 2.2 Hai bước giao, mỗi bước có ảnh bằng chứng tuỳ chọn

Operator `ship` (`PAID → SHIPPING`) khi bàn giao cho bên vận chuyển, rồi `deliver` (`SHIPPING →
DELIVERED`) khi hàng tới tay khách; `deliver` ghi `delivered_at`. Mỗi bước nhận **0–5 ảnh** — ảnh là
**tuỳ chọn**, một bước chuyển vẫn thành công với 0 ảnh.

Ảnh lưu ở bảng `order_delivery_photos(id, order_id FK ON DELETE CASCADE, stage CHECK
('SHIPPING','DELIVERED'), public_id, url, width, height, position, created_at)`; **nhị phân không vào
database** (Constitution, Media) — chỉ tham chiếu tới **media provider dùng chung** (`internal/share/media`).
Order **application** import **chỉ interface `Store`** (alias `MediaStore`), đúng như module 04 import nó
cho ảnh sản phẩm.

### 2.3 Luật ảnh là hàm **dependency-free trong `domain` của order** (không trích ra `share/media`)

Order tự kiểm ảnh bằng hàm **chỉ dùng stdlib** (`DetectDeliveryImage`/`ValidateDeliveryPhoto`), trả về
sentry của **order domain** (`ErrImageTypeUnsupported`, `ErrImageTooLarge`), đúng cách module 02
(`ValidateAvatarUpload`) và module 04 (`ValidatePictureUpload`) đã làm. **Không** trích luật này ra
`internal/share/media`.

Lý do: `internal/share/media` **không** dependency-free (nó import `net/http`, `share/config`,
`share/logging`, …). Nếu `domain` gọi helper ở đó, `domain` sẽ import một gói vi phạm allowlist của
Constitution I. Trích sang gói **mới** stdlib-only cũng bị loại vì thêm một nhà thứ ba cho một hàm 15
dòng mà avatar và ảnh sản phẩm đã giữ cục bộ; pattern per-module này ổn định. Định dạng quyết định bằng
**chữ ký byte** (JPEG/PNG/WebP), trần **2 MB/ảnh** và **5 ảnh/step** — cả hai được kiểm **trước khi** gọi
media provider.

### 2.4 Ảnh upload **ngoài** transaction, hàng ghi **trong** transaction

`StartShippingByAdmin`/`DeliverByAdmin` theo thứ tự của module 04: (1) kiểm từng payload trên byte của
nó; (2) transaction thứ nhất khóa đơn, kiểm trạng thái + đếm ảnh theo stage, và từ chối yêu cầu sai/quá
trần **trước khi** upload byte nào; (3) upload từng ảnh qua `MediaStore` **ngoài** transaction; (4)
transaction thứ hai đếm lại dưới khóa, áp bước DOMAIN, chèn hàng `order_delivery_photos`, chạm
`updated_at` và audit. Nếu bước (4) hỏng, giải phóng asset vừa lưu (best-effort); xấu nhất còn lại là
một asset mồ côi, **không bao giờ** là một đơn trỏ vào ảnh thiếu.

Không giữ khóa DB qua một lời gọi provider là điều module 04 đã chốt; trần 5 ảnh là một **đếm** nên phải
kiểm lại dưới khóa để nhiều nhất 5 (research D5).

### 2.5 Hoàn tất là việc của **khách** hoặc **sweeper 120 giờ**, không phải operator

`ConfirmReceived` (khách, `DELIVERED → COMPLETED`) khóa đơn, chạy bước DOMAIN `Complete`, ghi
`completed_at = now` và `completed_by = <khách>`, audit `ORDER_COMPLETED` (tác nhân là khách), thông báo.

`AutoCompleteDelivered` (sweep) chọn các đơn `DELIVERED` có `delivered_at <= now −
DeliveryCompletionWindow` (cũ nhất trước, index `orders_completion_idx (status, delivered_at)`) và chạy
**cùng** đường hoàn tất với **không tác nhân**, bỏ qua đơn đã hoàn tất (idempotent). Cửa sổ là hằng
**domain** `DeliveryCompletionWindow = 5 × 24h = 120h` (một khoảng thời gian trôi qua, **không** phải
ranh giới ngày dương lịch) tính từ mốc `delivered_at`; cutoff tính bằng **clock inject** và truyền vào
query, đúng như sweep hết hạn truyền `now`.

**Đúng một lần**: cả hai đường chạy **cùng** bước `DELIVERED → COMPLETED` dưới khóa dòng, nên bên commit
sau thấy đơn không còn `DELIVERED` và bị từ chối. Route `POST /admin/orders/{orderId}/complete` **bị
xoá** — operator không còn hoàn tất thay khách (FR-011 đến FR-014).

### 2.6 Feedback cấp đơn, một lần, chỉ sau khi giao

`POST /orders/{orderId}/feedback` lưu **một** hàng `order_feedback(id, order_id FK ON DELETE CASCADE,
author_id, content, created_at)` cho chủ đơn khi đơn `DELIVERED`/`COMPLETED`. `content` **1–2000 ký tự**
(sau trim) — ép bởi `ValidateFeedbackContent` **và** bởi check
`length(btrim(content)) BETWEEN 1 AND 2000`. **Unique index `(order_id)`** ép "một feedback mỗi đơn" ở
**tầng lưu trữ**; lần thứ hai bị từ chối. Artist được thông báo qua port `Notifier` hiện có.

Feedback là **cấp đơn, chỉ văn bản**; đánh giá sản phẩm có điểm số/kiểm duyệt là module 13 (V2.0).

### 2.7 Sweep hoàn tất là **sweep thứ hai** trên worker order hiện có

FR-012 cần một đơn `DELIVERED` tự hoàn tất sau 120 giờ. Worker order đã tick cho **hết hạn**; thêm
**một capability thứ hai** (`AutoCompleteDelivered`) trên **cùng** ticker là hình dạng nhỏ nhất: một
ticker, hai sweep idempotent, một lỗi của sweep này không chặn sweep kia.

### 2.8 Mở rộng **tập trạng thái đã kiếm tiền** của read-model/statistics

Các query thống kê doanh thu của operator (`internal/modules/order/infrastructure/implement/postgres/order.go`)
gate trên `status IN (...)`. Tập đó là **trạng thái đã kiếm tiền** (tiền đã thu). Sau khi `SHIPPED` đổi
tên `SHIPPING` và tách thêm `DELIVERED`, tập ba-giá-trị `('PAID','SHIPPED','COMPLETED')` **nêu ở ADR 022
§2.5** trở thành `('PAID','SHIPPING','DELIVERED','COMPLETED')` — nếu không, doanh thu **âm thầm mất**
mọi đơn đang giao hoặc đã giao. Dashboard module 16 (`§11 api-reference`) và mọi chỗ đọc enum cũng đổi
theo `recentOrders[].status` (bảy giá trị).

### 2.9 Trần upload theo route **cao hơn** cho route ảnh giao hàng

Mỗi ảnh trần **2 MB**; một lần gửi tối đa **5 ảnh** ⇒ trần route `5 × 2 MB + 64 KB` đệm
`multipart` = **10 551 296 byte**, lớn hơn trần route avatar (`2 162 688`). Pipeline bọc body **trước
routing** nên **không thể** nâng ở route; vì vậy **mặc định `MAX_BODY_BYTES` được nâng** lên **12 MiB**
(`12582912`) và composition root thêm **guard khởi động** cho trần ảnh giao hàng
(`RequireDeliveryEvidenceUploadCeiling`), mirror `RequireAvatarUploadCeiling` của module 02. Cả hai guard
chỉ chạy khi media được cấu hình. Số lấy từ **chính hàm route dùng**
(`DeliveryEvidenceUploadCeiling`), nên guard không trôi khỏi giới hạn thật (mirror ADR 010).

## 3. Hệ quả

| Hệ quả | Chi tiết |
|---|---|
| Tích cực | **Có bằng chứng giao hàng**: ảnh bàn giao và ảnh giao tới khách, lưu qua media store, trả trong chi tiết đơn |
| Tích cực | **Từ vựng khớp thực tế**: `SHIPPING` là "đang giao", `DELIVERED` là "khách đã nhận" — hai thời điểm khác nhau, hai trạng thái |
| Tích cực | **Hoàn tất không còn kẹt**: khách xác nhận sớm, hoặc sweeper 120 giờ hoàn tất **đúng một lần**; hai đường đua nhau vẫn chỉ một `COMPLETED` |
| Tích cực | **Ràng buộc ở tầng lưu trữ**: check bảy giá trị, `order_id` FK cascade, unique `(order_id)` feedback, check độ dài nội dung |
| Tích cực | **`domain` dependency-free**: luật ảnh stdlib-only trong order `domain`; media chỉ vào **application** qua interface `Store` |
| Tích cực | **Không chặn DB qua provider**: ảnh upload ngoài transaction, hàng ghi trong transaction |
| Tiêu cực | **Breaking contract**: `SHIPPED` → `SHIPPING` là đổi enum client-visible; bắt buộc frontend guide và phát hành đồng bộ (chưa deploy nên gói gọn) |
| Tiêu cực | **Trần `MAX_BODY_BYTES` mặc định tăng 4 MiB → 12 MiB**: body không liên quan cũng được nới; bù lại còn rate limit và các kiểm theo field |
| Tiêu cực | **Order thêm phụ thuộc media** (application) và hai bảng mới; mỗi bước giao có thể sinh asset mồ côi khi transaction thứ hai hỏng (chấp nhận, có giải phóng best-effort) |
| Tiêu cực | **`order_delivery_photos` đếm 5/step bằng code** (không unique index); bảo vệ bằng khóa dòng, không phải một ràng buộc schema |
| Tiêu cực | **Ca giao thất bại chưa có trạng thái**: đơn có thể dừng ở `SHIPPING`/`DELIVERED`; trả hàng/hoàn tiền chưa có (deferred) |

**Xem lại quyết định khi**: có nhu cầu thật về **giao thất bại/trả hàng/hoàn tiền** (cần trạng thái
`FAILED`/`RETURNED` và luồng hoàn tiền); chốt **phạm vi chuyển nhượng** theo `SHIPPING`/`DELIVERED`; có
**module 13 Review** thay feedback cấp đơn; hoặc media provider đổi cách trả `width`/`height`.

## 4. Cách tiếp cận bị loại

| Phương án | Vì sao không chọn |
|---|---|
| Giữ `SHIPPED` (= "đang giao") và chỉ thêm `DELIVERED` | Người dùng chọn **đổi tên**; một trạng thái tên "đã giao" mà nghĩa "đang giao" trong khi có `DELIVERED` riêng là từ vựng sai |
| Dùng `SHIPPED` nghĩa "đã giao" và chèn `SHIPPING` mới | Giữ lại một trạng thái tên không khớp bước nào và nhân đôi khái niệm |
| Thêm trạng thái `FAILED`/`RETURNED` ngay | Cần domain trả hàng/hoàn tiền chưa có; **hoãn** (Constitution VII, YAGNI) |
| Trích luật ảnh sang `internal/share/media` | `share/media` **không** dependency-free ⇒ `domain` import nó vi phạm Constitution I (cổng analyze đã bắt lỗi này) |
| Tạo gói `internal/share/imaging` stdlib-only | Thêm nhà thứ ba cho một hàm 15 dòng mà avatar và ảnh sản phẩm đã giữ cục bộ; nghi thức không cần thiết |
| Import module 04 để dùng lại luật ảnh | Một module không được import nội bộ module khác (Constitution I) |
| Bỏ ảnh ở bước giao | Ảnh bằng chứng là **mục đích** của feature |
| Một endpoint `/delivery` với trường `stage` | Một stage là một transition và có thể bị từ chối; hai endpoint nói đúng sự thật đó |
| Body JSON với ảnh base64 | Phình payload ~33% và lệch pattern multipart của avatar/ảnh sản phẩm |
| Upload ảnh **trong** transaction | Giữ khóa dòng order qua lời gọi mạng; provider chậm biến thành vấn đề database |
| Upload **sau** khi transition commit | Một transition đã commit lại phụ thuộc một lời gọi provider có thể hỏng ⇒ đơn không bằng chứng, không lỗi |
| Một cột JSON ảnh trên `orders` | Schema, không JSON, là nơi chứa số lượng và FK; phá pattern media-reference |
| Bảng riêng cho mỗi bước giao | Nhân đôi schema và mapper vô ích; một bảng + `stage` giữ một hình dạng, một trần |
| Trạng thái `AUTO_COMPLETED` riêng | Cách hoàn tất là **sự kiện**, không phải trạng thái (lý luận của ADR 018 §2.1) |
| Hardcode cửa sổ 120 giờ trong SQL | Cửa sổ sẽ nằm hai nơi và test không lay chuyển được cùng clock |
| Operator vẫn được `complete` | Người dùng yêu cầu **khách** xác nhận hoặc tự động; operator hoàn tất thay khách là hành vi feature này xoá |
| Feedback gắn bảng/mã riêng cho đánh giá sản phẩm | Review có điểm số/kiểm duyệt là module 13 (V2.0) |
| Feedback cấp sản phẩm hoặc cho phép gửi nhiều lần | Người dùng chọn **cấp đơn, một lần**; unique `(order_id)` là ràng buộc lưu trữ |
| Cho gửi feedback **trước** khi giao | Cần đã nhận hàng (người dùng chốt) |
| Thêm worker thứ ba cho sweep hoàn tất | YAGNI: một ticker, hai sweep idempotent là đủ |
| Job định kỳ ở database | Thêm hạ tầng dự án không dùng |
| Giữ trần `MAX_BODY_BYTES` 4 MiB | Pipeline chặn mọi lần gửi 5 ảnh **trước** khi route kịp áp luật của nó, và lý do không chỉ ra biến cần sửa (ADR 010) |
| Thêm knob cấu hình cho trần từng ảnh | Trần 2 MB đã là hằng domain dùng chung với avatar/ảnh sản phẩm; một knob nữa chỉ tạo nguồn lệch |

## 5. Bổ sung (2026-10-18)

Các mục cố ý hoãn của feature 019 — **giao thất bại/trả hàng**, **phạm vi chuyển nhượng** theo trạng
thái giao hàng mới, **phí ship/tracking hãng vận chuyển** (module 09), **đánh giá sản phẩm** (module 13),
**thông báo in-app** (module 12) — nằm ở
[`specs/019-order-delivery/deferred.md`](../../specs/019-order-delivery/deferred.md). Bàn giao frontend
nằm ở [`specs/019-order-delivery/frontend-guide.md`](../../specs/019-order-delivery/frontend-guide.md).
