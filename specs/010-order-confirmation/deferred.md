# Deferred — Order Confirmation & Editing

Việc phát hiện khi xây **Order Confirmation & Editing** (`specs/010-order-confirmation`) nhưng **không
phải việc của feature này**. Không mục nào ở đây chặn việc đóng feature; không mục nào là task của nó.

File này tồn tại vì một ô `- [ ]` trong `tasks.md` mang một ý nghĩa khác. `- [ ]` nghĩa là *feature này
chưa xong*. Việc thuộc về feature khác, hoặc thuộc chính hạ tầng test, không phải phần chưa xong của
feature này; ghi nó như vậy hoặc giữ một feature đã đóng trông như còn mở, hoặc ép điều kiện thoát bị
làm giả.

> **Feature này thay thế state machine của `009-order`**: `PENDING_PAYMENT` **đổi tên** thành
> `PAYMENT_PENDING` và thêm `PENDING`; `CONFIRMED` gộp vào `PAYMENT_PENDING`; `REJECTED`/`EXPIRED` dùng
> lại `CANCELLED`. `specs/009-order/frontend-guide.md` và `specs/009-order/contracts/openapi.yaml` đã bị
> thay thế (guide mới: `specs/010-order-confirmation/frontend-guide.md`).

## Quy tắc cho file này

- Mỗi mục nêu rõ **nó là gì**, **vì sao ngoài phạm vi**, và **điều gì sẽ gỡ được** nó. Một mục chỉ nêu
  triệu chứng thì chưa phải một mục.
- Mục nào làm mất hiệu lực bằng chứng của một task đã tick `[X]` **phải nói rõ và nêu số task đó**.
- Nếu một mục hoá ra thuộc feature này, nó quay lại `tasks.md` thành task thật. **File này không phải
  chỗ đỗ.**
- Báo cáo mọi mục trong báo cáo cuối của skill.

## Các mục

### D1 — Payment attempt/session, IPN và callback đến muộn

- **Vấn đề**: Feature này định nghĩa và test bước `PAYMENT_PENDING → PAID` (`MarkPaid`), mở hạn thanh
  toán 60 phút, và tăng `order_version` để đối chiếu, nhưng **không** có payment attempt/session, không
  có IPN/webhook, và không có bước xác nhận thanh toán thật nào qua HTTP. Một đơn không thể chuyển
  `PAID` trong hệ thống đang chạy.
- **Vì sao ngoài phạm vi**: Nhà cung cấp thanh toán (PayOS) và mọi thứ về vòng đời phiên thanh toán là
  **module 08 Payment**, chưa tồn tại (ADR 015 §5 chốt PayOS). Feature `010` chỉ giao **seam** (`order_version`,
  `payment_expires_at`, `confirmed_at`) để module 08 nối vào; gọi provider hoặc xử lý webhook ở đây là
  thiết kế theo yêu cầu đoán mò (Constitution VII).
- **Gỡ bằng cách nào**: **Module 08** (feature riêng) gọi `MarkPaid` với `sourceReference` ổn định, đối
  chiếu `order_version` để từ chối callback thuộc nội dung cũ, và xử lý IPN idempotent theo Constitution
  II.
- **Ảnh hưởng tới task đã tick**: **T021** (thêm `ConfirmByAdmin`/`MarkPaid`), **T040** (sweeper
  `PAYMENT_PENDING`), **T041** (test hết hạn) — các task này đã giao và test **trực tiếp**, nhưng đường
  **qua HTTP** tới `PAID` chưa có, nên bằng chứng end-to-end của chúng phụ thuộc module 08.

### D2 — Huỷ phiên thanh toán của provider khi sửa đơn

- **Vấn đề**: Sửa một đơn đang `PAYMENT_PENDING` (hoặc huỷ nó) chỉ **tăng `order_version`** và **nhả giữ
  chỗ**; nó **không** huỷ payment link/session đã tạo ở nhà cung cấp. Một khách có thể còn một link
  thanh toán cũ (cho nội dung cũ) vẫn trả tiền được ở phía provider.
- **Vì sao ngoài phạm vi**: Huỷ session/payment link của PayOS là thao tác provider — thuộc **module 08
  Payment**, chưa tồn tại. Feature này chỉ có thể vô hiệu xác nhận/phiên cũ **ở phía mình** bằng
  `order_version` (research D7); việc gọi provider nằm ngoài ranh giới của nó.
- **Gỡ bằng cách nào**: **Module 08** giữ ánh xạ `order_id ↔ payment session` và huỷ session khi một sửa
  hoặc huỷ làm `order_version` đổi; callback đến muộn bị từ chối bằng cách so `order_version` (D1).
- **Ảnh hưởng tới task đã tick**: **T030** (use case `EditMine`), **T032** (test sửa đơn nhả hàng) — các
  task này giải quyết phần **order** (phiên bản + nhả hàng) đúng như thiết kế; phần **provider** còn lại
  là của module 08.

### D3 — Thông báo in-app và các email còn lại

- **Vấn đề**: Feature này chỉ gửi **email** — artist khi đơn cần xác nhận, khách mỗi khi trạng thái đổi —
  và chỉ một phần email đã có nội dung tối thiểu. **Không** có thông báo in-app, không có trung tâm
  thông báo, và không có các email khác của hệ thống.
- **Vì sao ngoài phạm vi**: Kênh thông báo là **module 12 Notification**, chưa tồn tại (V1.2). Spec
  `010` khẳng định thông báo **chỉ qua email** và in-app thuộc module 12 (spec Assumptions).
- **Gỡ bằng cách nào**: **Module 12** dựng kênh in-app (và có thể hợp nhất hai mailer — xem D5), rồi
  consumer của các trạng thái đơn chuyển sang đó.
- **Ảnh hưởng tới task đã tick**: **T036** (test notify), **T037** (gửi thông báo), **T038** (test HTTP
  notify) — đã giao **email**; in-app chưa có nên không nằm trong bằng chứng của chúng.

### D4 — Comment trong code của module order còn nói "awaiting payment" (ngoài phạm vi task tài liệu)

- **Vấn đề**: `internal/modules/order/presentation/http/handler.go` còn hai comment lỗi thời so với hành
  vi đã giao: comment của `CancelMine` và comment của method `CancelMine` trong interface `Service` vẫn
  nói đơn "still awaiting payment", trong khi từ feature 010 huỷ được **cả** `PENDING` lẫn
  `PAYMENT_PENDING`.
- **Vì sao ngoài phạm vi**: Phạm vi lần này **chỉ sửa tài liệu** (`docs/**`, `specs/**`); mọi tệp dưới
  `internal/**` bị loại trừ tường minh. Đây là lệch giữa **comment** và code, không phải lệch hành vi.
- **Gỡ bằng cách nào**: Sửa hai comment trong `internal/modules/order/presentation/http/handler.go` cho
  khớp hành vi (`PENDING` hoặc `PAYMENT_PENDING`); thuộc thay đổi code của module order, không phải thay
  đổi tài liệu này.
- **Ảnh hưởng tới task đã tick**: không. `[X]` của các task tài liệu (`T042`, `T043`) vẫn đúng vì tài
  liệu **docs** phản ánh đúng code; chỉ comment **trong code** còn cũ.

### D5 — `docs/modules/05-inventory.md` còn xếp phần "đơn hàng điều khiển" vào chưa giao

- **Vấn đề**: Mục *Phần chưa giao* của `docs/modules/05-inventory.md` vẫn nói nửa tự động do đơn hàng
  điều khiển **chưa giao** vì "Order (07) và Payment (08) chưa tồn tại", trong khi module 07 đã tồn tại
  và điều khiển reserve/release/consume từ feature 009 (và thêm confirm/release-on-edit ở feature 010);
  chỉ Payment (08) là còn thiếu.
- **Vì sao ngoài phạm vi**: Task `T044` của lần này giới hạn `docs/modules/05-inventory.md` ở việc cập
  nhật **cửa sổ giữ chỗ 15 → 60 phút**; viết lại bảng "Phần chưa giao" là một sửa tài liệu rộng hơn,
  thuộc về lần cập nhật module 05 khi module 07/08 hoàn tất.
- **Gỡ bằng cách nào**: Cập nhật bảng *Phần chưa giao* của `docs/modules/05-inventory.md` để phản ánh
  module 07 đã wire nửa đơn hàng và chỉ module 08 còn thiếu; đối chiếu `specs/007-inventory-tracking/deferred.md` D1.
- **Ảnh hưởng tới task đã tick**: không. `T044` vẫn đúng phần nó được giao (cửa sổ 60 phút); mục D5 là
  phần tài liệu module 05 **không** thuộc phạm vi lần này.
