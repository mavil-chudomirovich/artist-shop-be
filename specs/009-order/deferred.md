# Deferred — 009-order

Việc phát hiện khi xây Order Checkout nhưng **không phải việc của feature này**.
Không mục nào ở đây chặn việc đóng feature này; không mục nào là task của feature này.

File này tồn tại vì một ô `- [ ]` trong `tasks.md` mang một ý nghĩa khác.
`- [ ]` nghĩa là *feature này chưa xong*. Việc thuộc về feature khác, hoặc thuộc
chính hạ tầng test, không phải phần chưa xong của feature này; ghi nó như vậy hoặc
giữ một feature đã đóng trông như còn mở, hoặc ép điều kiện thoát bị làm giả.

## Quy tắc cho file này

- Mỗi mục nêu rõ **nó là gì**, **vì sao ngoài phạm vi**, và **điều gì sẽ gỡ được** nó.
  Một mục chỉ nêu triệu chứng thì chưa phải một mục.
- Mục nào làm mất hiệu lực bằng chứng của một task đã tick `[X]` **phải nói rõ và nêu
  số task đó**. Nếu không, `[X]` sẽ bị đọc là không điều kiện trong khi thực tế không phải.
- Nếu một mục hoá ra thuộc feature này, nó quay lại `tasks.md` thành task thật.
  **File này không phải chỗ đỗ.**

## Ba món nợ feature này đã trả

Feature 009 **đóng** ba mục mà các feature trước để lại cho module Order — chúng **không**
còn deferred:

- **Feature 007 `deferred.md` D1** — hợp đồng **đặt chỗ** liên module (`InventoryReservation`):
  module 07 là consumer đầu tiên, publish `internal/contracts/inventory.go` và wire adapter ở module 05.
- **Feature 007 `deferred.md` D2** — không có luồng huỷ đơn đã trả tiền; **chuyển nhượng** không đổi
  tồn kho: module 07 giao `POST /admin/orders/{orderId}/transfer`.
- **Feature 006 & 007 `deferred.md` D3** — dòng đơn làm gì khi sản phẩm bị xoá cứng: module 07
  **chụp** sản phẩm vào `order_items` (`product_id` là tham chiếu thông tin, không FK).

## Các mục

### D1 — Xác nhận thanh toán và nhà cung cấp PayOS chưa tồn tại

- **Vấn đề**: bước chuyển `PENDING_PAYMENT → PAID` đã được giao và **test trực tiếp** (unit + tích
  hợp, `MarkPaid(orderID, sourceRef)` gọi `ApplySale` từng dòng), nhưng **không** có endpoint HTTP
  nào phát ra nó, và **không** có nhà cung cấp thanh toán nào xác nhận một payment thật. Vì vậy ship,
  complete và transfer (đều cần đơn `PAID`) chỉ chạy được qua đường dùng trực tiếp trong test, không
  qua HTTP.
- **Vì sao ngoài phạm vi**: payment — cổng PayOS, webhook công khai, chữ ký `checksumKey`, bảng map
  `order_code ↔ order_id`, và việc **điều khiển** bước `PAID` — là **module 08 Payment**, chưa tồn
  tại. Spec 009 (mục *Assumptions*, clarification 2026-10-09) đã chốt: order đã có **một** trục trạng
  thái (`paid` nghĩa là đã xác nhận thanh toán), module 08 giữ trạng thái thanh toán riêng trong bảng
  của nó, và việc liên kết đơn ↔ bản ghi thanh toán là việc của 08. Dựng một endpoint `pay` không có
  nguồn sự thật là ghi một thao tác bán shop không hề làm (research D13).
- **Gỡ bằng cách nào**: **module 08 Payment** gọi `MarkPaid` (use case đã có) với một **định danh sự
  kiện ổn định**; module 07 không đổi. Nghĩa vụ kèm theo: `sourceReference` của mỗi dòng là định danh
  sự kiện **+** định danh sản phẩm, vì `inventory_transactions.source_reference` là duy nhất toàn cục
  và một đơn đã trả tiền tạo **N** dòng chuyển kho (research D5). Khi 08 ra đời, `/orders` và
  `/admin/orders` cập nhật **cùng lúc** để nêu webhook/nhánh `PAID` (Constitution VIII).
- **Ảnh hưởng tới task đã tick**: **có, một phần**. Các task bước `PAID` (T030, T032, T035) tick `[X]`
  trên cơ sở **test trực tiếp**, đúng như chúng hứa (giao **năng lực** và test nó); chúng **không**
  hứa một bề mặt HTTP, nên evidence còn nguyên. Nhưng T043/T044 (ship/complete) và T047/T048
  (transfer) chỉ chạy được end to end khi đã có một đơn `PAID`, mà điều đó chỉ đạt được qua đường
  dùng trực tiếp — không qua HTTP — cho tới khi module 08 tồn tại. Không task nào bị suy yếu.

### D2 — Phí ship và tracking giao hàng vẫn hoãn

- **Vấn đề**: `docs/modules/07-order.md` để việc giao hàng cho module 09, và ADR 015 §5 chốt đơn
  **chỉ gồm tiền hàng**. Feature 009 **không** giao phí ship lẫn tracking.
- **Vì sao ngoài phạm vi**: spec khẳng định rõ (mục *Scope boundary*, *Assumptions*): tổng đơn là
  tổng của các dòng và **không** có phí ship; khách **trả phí ship khi nhận hàng**, đơn vị vận chuyển
  là bên ngoài (ViettelPost). Thêm một cột phí ship bây giờ là dựng trước data model và luồng nghiệp
  vụ của module 09 (Constitution VII, YAGNI). Đổi lại, tách `09` sau có thể phải sửa data model đơn —
  đánh đổi đã chấp nhận ở ADR 015.
- **Gỡ bằng cách nào**: **module 09 Shipping** (V1.2) thêm đơn vị vận chuyển, cơ chế tạo vận
  đơn/tracking, và một quyết định về việc phí ship vào đơn hay tính riêng. Khi đó đơn có thể cần một
  cột phí ship (và tổng đơn đổi nghĩa) — một thay đổi contract của order phải vào spec của module 09
  trước.
- **Ảnh hưởng tới task đã tick**: không. Không task nào của feature này hứa phí ship hay tracking;
  FR-008 và ADR 015 §5 biến việc không có phí ship thành một yêu cầu rõ ràng.

### D3 — Hoàn tiền, tách đơn, mã giảm giá vẫn ngoài MVP

- **Vấn đề**: `docs/modules/07-order.md` hoãn "tách đơn (split), đơn định kỳ, hoàn tiền từng phần phức
  tạp" và "mã giảm giá/flash sale". Feature 009 **không** giao mục nào; đặc biệt **không** có luồng
  hoàn tiền (không có endpoint refund, không có nhánh hoàn kho cho đơn đã trả tiền).
- **Vì sao ngoài phạm vi**: spec khẳng định **không** có refund ở MVP (ADR 015 §5) và **không** có mã
  giảm giá (mục *Scope boundary*). Mỗi thứ là một thực thể/luồng mới: refund cần một contract với cổng
  thanh toán và một đường hoàn kho (mà module 05 mới **nhả giữ chỗ**, chưa có "restore" vật lý — xem
  `specs/007-inventory-tracking/deferred.md` D2); tách đơn đổi khoá của `order_items`; mã giảm giá là
  một thực thể khuyến mãi và một luật giá mới. Dựng chúng bây giờ là dựng trước data model
  (Constitution VII).
- **Gỡ bằng cách nào**: mỗi mục là một feature riêng khi có nhu cầu cụ thể. Refund là việc lớn nhất:
  cần một contract hoàn tiền với module 08, một quyết định về hoàn kho (module 05 thêm use case
  "restore"), và một trạng thái/bản ghi hoàn tiền. Tách đơn cần một cạnh chia dòng; mã giảm giá cần
  một quyết định áp mã ở giỏ hay ở checkout.
- **Ảnh hưởng tới task đã tick**: không. Không task nào của feature này hứa ba thứ trên; chúng được
  biến thành "không làm" bằng một yêu cầu rõ ràng ở spec.

### D4 — Bằng chứng "wire `AccountLookup`" của T028 chỉ được thoả khi US5 wire nó (T046/T047)

- **Vấn đề**: task **T028** (US1) tick `[X]` với mô tả *"wire the `CartCheckout`, `ProductCatalog`,
  `InventoryAvailability`, `InventoryReservation`, `CustomerLookupService` **and `AccountLookup`**
  adapters into the order service"*. Nhưng `AccountLookup` (tra tài khoản nhận chuyển nhượng) **không
  có** consumer nào cho tới **US5**: `checkout.go` chỉ khai trường `Accounts` mà **không** dùng nó;
  đường dùng duy nhất là `Transfer` (`transfer.go`, gọi `s.Accounts.UserIDByEmail`). Việc T028 khai
  "đã wire `AccountLookup`" **đúng về mặt dây nối** (adapter `authaccount` được tạo ở T007 và gán vào
  `Service`), nhưng nó **chỉ được chứng minh là đúng** khi T046 (use case `Transfer`) và T047
  (endpoint) wire và test nó ở US5.
- **Vì sao ngoài phạm vi**: đây là một phát hiện về **thứ tự bằng chứng**, không phải việc chưa làm.
  Không có hành vi nào bị bỏ dở: adapter có thật, đúng, và được test ở US5. Ghi lại để một `[X]` ở
  T028 không bị đọc là "đã chứng minh `AccountLookup` hoạt động" khi tại thời điểm US1 nó chưa có
  đường dùng nào.
- **Gỡ bằng cách nào**: không cần gỡ — điều kiện đã thoả ở **T046/T047**. Nếu muốn tránh lặp lại, một
  task "wire X" cho một hợp đồng **chưa có consumer** nên được đọc là "dây nối", và bằng chứng hành vi
  thuộc task đầu tiên **dùng** hợp đồng đó.
- **Ảnh hưởng tới task đã tick**: **T028**. Evidence của T028 (rằng `AccountLookup` được wire) chỉ
  được thoả trọn vẹn khi **T046/T047** (US5) dùng và test nó; ở thời điểm US1, trường `Accounts` được
  khai mà chưa ai gọi.
