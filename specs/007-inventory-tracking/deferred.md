# Deferred — 007-inventory-tracking

Việc phát hiện khi xây Inventory Tracking nhưng **không phải việc của feature này**.
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

### D1 — Hợp đồng đặt chỗ (reservation) liên module chưa được publish

- **Vấn đề**: năng lực giữ chỗ đã được giao và test trực tiếp trong feature này
  (`Service.Reserve`, `Service.Release`, `Service.ExpireHolds`, `Service.ApplySale`), nhưng
  **không** có interface nào trong `internal/contracts` để một module khác gọi nó. Consumer —
  Order (07) và Payment (08) — chưa tồn tại, nên chưa có hình dạng nào để publish.
- **Vì sao ngoài phạm vi**: publish một hợp đồng khi chưa có consumer là thiết kế theo nhu cầu
  đoán mò, đúng thứ Hiến pháp VII (YAGNI) cấm; hợp đồng sẽ đóng băng một interface trước khi nhu
  cầu của Order được biết. Đây cùng lý do module 03 đã nêu cho việc không viết contract sớm
  (`specs/005-category-catalog/deferred.md` D4). Năng lực **đã** được test, nên không có hành vi
  nào bị bỏ dở — chỉ thiếu đường dây cho module tương lai.
- **Gỡ bằng cách nào**: khi **module 07 Order được specify**, thêm một hợp đồng (ví dụ
  `InventoryReservation`) vào `internal/contracts/` với hình dạng theo đúng nhu cầu Order (đặt chỗ
  theo dòng đơn, nhả khi hủy, báo sự kiện thanh toán với một `sourceReference` ổn định), và wire
  adapter ở composition root. Spec ghi rõ nghĩa vụ: bên gọi phải cấp một định danh sự kiện ổn định,
  vì dedupe chỉ theo identity (`spec.md`, mục *Assumptions*).
- **Ảnh hưởng tới task đã tick**: không. Các task hold (T035–T042) và sale (T048–T051) hứa giao
  **năng lực** và test nó trực tiếp, không hứa publish hợp đồng; evidence còn nguyên.

### D2 — Không có luồng huỷ đơn đã thanh toán, và chuyển nhượng không đổi tồn kho

- **Vấn đề**: `docs/modules/05-inventory.md` mô tả luồng "đơn hủy → hoàn kho". Hệ thống **không**
  có luồng huỷ một đơn **đã thanh toán**, nên không có việc hoàn kho vật lý cho đơn đã trả tiền.
  Thay vào đó, một đơn đã thanh toán có thể được **chuyển nhượng** cho một tài khoản khác đã tồn
  tại, và việc chuyển nhượng **không đổi tồn kho**.
- **Vì sao ngoài phạm vi**: clarification đã chốt (session 2026-10-08): câu "đơn huỷ → hoàn kho"
  chỉ còn áp dụng cho đơn **chưa** thanh toán, mà với đơn đó việc hoàn kho chính là **nhả giữ chỗ**
  (không đổi số vật lý). Cả luồng huỷ-đã-trả-tiền lẫn luồng chuyển nhượng đều thuộc **order flow**;
  inventory chỉ sở hữu việc nhả giữ chỗ, đã giao.
- **Gỡ bằng cách nào**: khi có nhu cầu thật về huỷ đơn đã thanh toán (refund/hoàn kho), đó là một
  thay đổi contract của module 07/08 và phải được đưa vào spec của module đó trước; inventory sẽ
  thêm một use case "restore" chỉ khi spec của Order định nghĩa nó. Chuyển nhượng đơn là việc của
  Order, không chạm inventory.
- **Ảnh hưởng tới task đã tick**: không. Spec (FR-017) đã giới hạn release cho đơn **còn giữ chỗ**;
  không task inventory nào hứa hoàn kho cho đơn đã trả tiền.

### D3 — Lịch sử tồn kho không sống ngoài vòng đời sản phẩm

- **Vấn đề**: `stock_levels`, `inventory_transactions` và `stock_holds` đều tham chiếu
  `products(id)` với **`ON DELETE CASCADE`**. Product xoá cứng (feature 006, D3), nên xoá sản phẩm
  xoá luôn tồn kho, giữ chỗ và **lịch sử biến động** của nó.
- **Vì sao ngoài phạm vi**: feature 006 đã chọn hard delete vì **không có gì đọc** sản phẩm đã xoá
  (chưa có đơn hàng), và nó đã **đặt tên module 07 Order là bên quyết định** việc xoá sản phẩm
  (`specs/006-product-catalog/deferred.md` D3). Quyết định lịch sử kho sống ngoài sản phẩm là một
  bản sao lịch sử bền thứ hai không ai cần ở MVP, và nó thuộc cùng câu hỏi đó. Audit trail vẫn giữ
  **sự thật** rằng sản phẩm đã bị xoá.
- **Gỡ bằng cách nào**: module 07 Order quyết định giữa soft delete (thêm `deleted_at`) hoặc chụp
  dữ liệu sản phẩm vào dòng đơn; nếu cần lịch sử kho sống lâu hơn, đổi khóa ngoại thành tham chiếu
  lỏng (bỏ FK, giữ cột) hoặc bảng lịch sử riêng không cascade — làm cùng lúc với quyết định của
  module 07 để hai bên không lệch.
- **Ảnh hưởng tới task đã tick**: không. FR spec ghi rõ "giữ lịch sử tồn kho ngoài vòng đời sản
  phẩm không bắt buộc cho MVP"; D11 của `research.md` (cascade) là lựa chọn có chủ ý và có test
  (T022, quickstart 11d).

### D4 — Ngưỡng cảnh báo tồn kho thấp và kho nhiều địa điểm vẫn hoãn

- **Vấn đề**: `docs/modules/05-inventory.md` để mở câu hỏi "Ngưỡng cảnh báo tồn kho thấp có cần ở
  MVP không?", và hoãn "Quản lý kho nhiều địa điểm" cùng "Dự báo tồn kho, cảnh báo tự động nâng cao".
  Feature này **không** giao cả hai.
- **Vì sao ngoài phạm vi**: spec trả lời thẳng — FR-029 ghi hệ thống **MUST NOT** theo dõi tồn kho ở
  hơn một địa điểm và **MUST NOT** cảnh báo theo ngưỡng; mục *Scope boundary* nói ngưỡng thấp ở lại
  cùng các cảnh báo nâng cao đã hoãn. Một ngưỡng là một cột/luật mới mà không có yêu cầu nào dùng
  nó, và nhiều địa điểm đổi chiều khóa của `stock_levels` (thêm vị trí) — cả hai là dựng trước data
  model (Hiến pháp VII).
- **Gỡ bằng cách nào**: mỗi mục là một feature riêng khi có nhu cầu cụ thể. Ngưỡng thấp là việc nhỏ
  nhất: thêm một cột cấu hình (theo sản phẩm hoặc toàn cục) và một cảnh báo đọc khi một thay đổi
  khiến số lượng xuống dưới nó. Nhiều địa điểm là một feature đổi data model (khóa
  `(product_id, location_id)`) và mọi đường đọc/ghi theo đó.
- **Ảnh hưởng tới task đã tick**: không. Không task nào của feature này hứa ngưỡng hay nhiều địa
  điểm; FR-029 biến việc không làm chúng thành một yêu cầu rõ ràng.
