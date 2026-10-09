# Deferred — 008-cart

Việc phát hiện khi xây Cart nhưng **không phải việc của feature này**.
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

## Các mục

### D1 — Checkout biến giỏ thành đơn chưa tồn tại

- **Vấn đề**: `docs/modules/06-cart.md` mô tả bước "khách chuyển sang thanh toán", và spec
  (FR-008, FR-011) giao cho **checkout** việc đối chiếu lại giá và tồn khả dụng trước khi tạo đơn.
  Feature này **không** giao bước đó: không có endpoint nào tạo đơn từ giỏ, và không có nơi nào
  chạy hai lần đối chiếu lại.
- **Vì sao ngoài phạm vi**: Order (07) chưa tồn tại — chưa có thực thể đơn, chưa có state machine
  `PENDING_PAYMENT → PAID → SHIPPED → COMPLETED`, nên không có gì để tạo. Đối chiếu lại giá và tồn
  là **nghĩa vụ của bên tạo đơn**, không phải của giỏ: giỏ trung thực với giá khách đã thấy và chỉ
  *kiểm tra* tồn, không giữ chỗ (FR-011). Dựng checkout bây giờ là dựng trước data model và luồng
  nghiệp vụ của module sau (Hiến pháp VII, YAGNI).
- **Gỡ bằng cách nào**: khi **module 07 Order được specify**, nó thêm endpoint tạo đơn từ giỏ, gọi
  hợp đồng đặt chỗ của module 05 (D2 dưới, và `specs/007-inventory-tracking/deferred.md` D1), và
  **đối chiếu lại giá** của từng dòng trước khi thu tiền. Nếu giá lệch giá snapshot, checkout quyết
  định (báo khách hoặc tính giá mới) — đó là một quyết định của spec Order, không của giỏ.
- **Ảnh hưởng tới task đã tick**: không. Không task nào của feature này hứa tạo đơn; FR-008/FR-011
  ghi rõ việc đối chiếu lại thuộc checkout. Evidence của các task giỏ còn nguyên.

### D2 — Giữ chỗ tồn kho khi khách trả tiền chưa được wire

- **Vấn đề**: `docs/modules/06-cart.md` nói giỏ kiểm tra tồn "tại thời điểm thao tác/thanh toán".
  Feature này **không** giữ chỗ hàng: giỏ chỉ *kiểm tra* khả dụng và không bao giờ gọi đường giữ
  chỗ của module 05 (FR-011). Năng lực giữ chỗ (`Reserve`/`Release`/`ExpireHolds`/`ApplySale`) **đã
  được giao và test** trong feature 007, nhưng **nguồn sự kiện** — một đơn, một lần thanh toán —
  chưa tồn tại, nên chưa ai gọi nó.
- **Vì sao ngoài phạm vi**: giữ chỗ là **năng lực của module 05, được gọi lúc thanh toán**, không
  phải khi thêm vào giỏ. Cho giỏ giữ chỗ sẽ để một khách chặn hàng của người khác vô thời hạn mà
  không có đơn nào (`specs/008-cart/research.md` D12). Hợp đồng liên module để bên tương lai gọi
  (reservation contract) vẫn **mở** ở module 05 (`specs/007-inventory-tracking/deferred.md` D1);
  publish nó bây giờ khi chưa có consumer là đoán nhu cầu (Hiến pháp VII).
- **Gỡ bằng cách nào**: khi **module 07 Order / 08 Payment** tồn tại, chúng gọi use case giữ chỗ của
  module 05 qua hợp đồng sẽ được thêm lúc Order được specify, với một `sourceReference` ổn định để
  chống xử lý trùng. Một lần đọc khả dụng **khác** (`InventoryAvailability`) đã được feature này
  publish cho giỏ; nó không thay thế và không đóng D1 của module 05.
- **Ảnh hưởng tới task đã tick**: không. FR-011 nói rõ giỏ **MUST NOT** giữ chỗ; không task nào của
  feature này hứa gọi đường reserve. Evidence còn nguyên.

### D3 — Giỏ vãng lai, giỏ lưu/later, mã giảm giá, upsell vẫn hoãn

- **Vấn đề**: `docs/modules/06-cart.md` hoãn "Giỏ cho khách vãng lai (chưa đăng nhập)", "Lưu giỏ,
  mã giảm giá, upsell". Feature này **không** giao cả bốn.
- **Vì sao ngoài phạm vi**: spec khẳng định rõ ở mục *Scope boundary* và *Assumptions* — đăng nhập
  là điều kiện để dùng giỏ (một giỏ thuộc một tài khoản, `carts.user_id` unique + FK tới `users`);
  không có mã giảm giá, upsell hay giỏ lưu ở MVP. Mỗi thứ là một thực thể/luồng mới: giỏ vãng lai
  cần một danh tính tạm và một đường gộp vào tài khoản lúc đăng nhập; giỏ lưu cần thêm trạng thái
  và một giỏ thứ hai mỗi tài khoản (đụng thẳng unique index một-giỏ-một-tài-khoản); mã giảm giá là
  một thực thể khuyến mãi và một luật giá mới; upsell là một truy vấn gợi ý. Dựng chúng bây giờ là
  dựng trước data model (Hiến pháp VII, YAGNI).
- **Gỡ bằng cách nào**: mỗi mục là một feature riêng khi có nhu cầu cụ thể. Giỏ vãng lai và giỏ lưu
  là hai thay đổi data model (danh tính tạm; nhiều giỏ mỗi tài khoản kèm cờ "đang dùng"); mã giảm
  giá là một module/thực thể khuyến mãi và một quyết định về việc áp mã ở giỏ hay ở checkout;
  upsell là một đường đọc gợi ý không đổi lược đồ.
- **Ảnh hưởng tới task đã tick**: không. Không task nào của feature này hứa bốn tính năng trên;
  spec biến việc không làm chúng thành một yêu cầu rõ ràng (mục *Scope boundary*).
