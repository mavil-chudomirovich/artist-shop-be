# Deferred — Product Variants (014)

Việc phát hiện khi xây Product Variants nhưng **không phải việc của feature này**.
Không mục nào ở đây chặn việc đóng feature 014; không mục nào là task của feature 014.

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
- Báo cáo mọi mục trong báo cáo cuối của skill. Hoãn trong im lặng thì vô hiệu mục đích
  của việc ghi lại.

## Các mục

### D-01 — Không hiển thị khả dụng theo từng lựa chọn trên trang nhóm

- **Vấn đề**: `GET /product-groups/{slug}` và selector `variantGroup` trong product detail
  chỉ trả `label`, `price`, `imageUrl`, `isPreorder` của mỗi lựa chọn — không có số lượng
  còn lại hay cờ còn-hàng. Khách chỉ biết một lựa chọn không mua được khi thêm nó vào giỏ,
  đúng như hôm nay với một sản phẩm thường.
- **Vì sao ngoài phạm vi**: module 04 không đọc được tồn kho (Constitution I), nên hiển thị
  khả dụng cần một **cross-module read mới** (một hợp đồng đọc bulk trong `internal/contracts`
  do module 05 cài đặt). Spec (Assumptions) và research D11 **cố ý** để việc này ngoài phạm
  vi: feature này "adds no new cross-module read".
- **Gỡ bằng cách nào**: thêm một hợp đồng đọc khả dụng theo danh sách product id (module 05
  cài đặt adapter ở composition root), gọi nó trong `GetPublicGroupBySlug` và trong read
  selector của product detail, rồi thêm trường vào DTO công khai. Vì đổi bề mặt client, việc
  này phải là một feature riêng (có spec/plan riêng), không phải một task nối thêm của 014.
- **Ảnh hưởng tới task đã tick**: không. FR-002 chỉ yêu cầu label/giá/ảnh của lựa chọn, và
  feature chủ động không giao khả dụng từng lựa chọn.

### D-02 — Biến thể nhiều chiều (màu × size) và ma trận tổ hợp

- **Vấn đề**: mỗi nhóm có **đúng một** chiều chọn (`option_name`), nên không biểu diễn được
  một sản phẩm vừa chọn màu vừa chọn size, và không có bảng tổ hợp (mỗi tổ hợp một giá/tồn
  riêng).
- **Vì sao ngoài phạm vi**: đây là **data model khác**, không phải cách diễn đạt khác — cần
  một ma trận tổ hợp và cách tính giá/tồn theo tổ hợp (SKU matrix). Spec Clarifications chốt
  một chiều duy nhất và xếp đa chiều vào mục "Ngoài phạm vi".
- **Gỡ bằng cách nào**: một feature mới với entity tổ hợp (combination) và SKU matrix, xây
  trên nền nhóm + lựa chọn của 014. Không phải một mở rộng nhỏ của 014.
- **Ảnh hưởng tới task đã tick**: không.

### D-03 — Sắp xếp lựa chọn ngoài `position` của sản phẩm

- **Vấn đề**: thứ tự lựa chọn trong nhóm dùng chính `position` của **sản phẩm**, mà
  `position` cũng là thứ tự của sản phẩm trong danh mục. Một lựa chọn muốn đứng khác trong
  nhóm so với khi đứng riêng trong danh mục thì không có cách nào — một trường đang phục vụ
  hai mục đích.
- **Vì sao ngoài phạm vi**: spec không yêu cầu một editor thứ tự riêng, và research D5 chọn
  tái dùng `position` để tránh một cột + một endpoint mới cho v1.
- **Gỡ bằng cách nào**: thêm một cột `variant_option_position` (hoặc một bảng thứ tự theo
  nhóm) + một endpoint reorder, và cho read lựa chọn sắp theo cột đó rồi mới tới `position`.
  Một feature nhỏ, đứng riêng.
- **Ảnh hưởng tới task đã tick**: không (D5 ghi rõ đây là lựa chọn có chủ đích, không phải
  thiếu sót).

### D-04 — Trần 10 ảnh của gallery nhóm không khoá dòng

- **Vấn đề**: `reserveGroupImage` và `insertGroupImage` trong
  `internal/modules/product/application/implement/groups.go` đếm ảnh rồi chèn trong một
  transaction, nhưng **không** lấy row lock của nhóm — port nhóm để lộ `CountImages` chứ
  không lộ một lock. Hai upload **đồng thời** lúc gallery đang 9 (hoặc 10) ảnh có thể cùng
  đọc `count = 9` rồi cùng chèn, đưa gallery vượt trần 10. Đường ảnh sản phẩm tránh được
  điều này bằng `LockProduct` (`pictures.go`).
- **Vì sao ngoài phạm vi**: chặn race này cần thêm một method lock vào `VariantGroupRepository`
  (hoặc một constraint/trigger đếm) — tức đổi port và adapter. FR-018 yêu cầu đúng **luật**
  10 ảnh như sản phẩm, và luật đó đúng khi tuần tự; tính đúng **dưới đồng thời** là debt hạ
  tầng đã chấp nhận, không phải một hành vi mới của feature.
- **Gỡ bằng cách nào**: thêm `LockGroup(ctx, id)` vào port nhóm (mô phỏng `LockProduct`) và
  gọi nó trong `reserveGroupImage`/`insertGroupImage` trước khi `CountImages`; hoặc một cơ
  chế đếm ở tầng lưu trữ (partial unique / check). Nên nằm cùng feature sở hữu bảng ảnh nhóm.
- **Ảnh hưởng tới task đã tick**: suy yếu bằng chứng của **T027** — task giao các use case
  ảnh nhóm "reusing the module's media + UnitOfWork picture pattern", nhưng bảo đảm trần 10
  ảnh chỉ đúng khi tuần tự. Các test hiện có (T025, T032) là single-threaded nên không phủ
  race này.

### D-05 — Test double `memoryProducts` lệch adapter với một pre-order đã nghỉ (chỉ test)

- **Vấn đề**: `memoryProducts` trong
  `internal/modules/product/application/implement/catalogue_test.go` (cả vòng standalone của
  `ListCatalogue`, `ListVisible` và `hasVisibleOption`) dùng predicate
  `SellState != Active && !IsPreorder ⇒ ẩn`. Adapter thật (`product.go`) dùng
  `sell_state = 'ACTIVE' OR (is_preorder AND sell_state <> 'DISCONTINUED')`. Với một sản phẩm
  **pre-order đã nghỉ** (`SellState = DISCONTINUED` **và** `IsPreorder = true`), test double
  coi là hiển thị còn DB thật coi là ẩn — ngược nhau. (Fake nhóm `memoryVariantGroups.optionVisible`
  thì đúng.)
- **Vì sao ngoài phạm vi**: đây là **test double**, không phải code sản phẩm. Không test nào
  hiện seed một pre-order đã nghỉ, nên không kết quả test nào đang sai; adapter thật đã được
  phủ bởi test tích hợp (T012, T036). Sửa nó là sửa harness test, không phải việc của feature.
- **Gỡ bằng cách nào**: đổi predicate của `memoryProducts` thành
  `SellState == Active || (IsPreorder && SellState != Discontinued)` ở cả ba chỗ, để fake
  khớp adapter ở mọi trạng thái.
- **Ảnh hưởng tới task đã tick**: suy yếu (ở đúng một trạng thái) bằng chứng của **T018,
  T019, T035** — các unit test chạy qua `memoryProducts`. Những trạng thái chúng thực sự dùng
  (Active, Discontinued **không** pre-order, ẩn) không bị ảnh hưởng.

### D-06 — Không có slug registry dùng chung cho hai namespace

- **Vấn đề**: nhóm và sản phẩm có **hai không gian tên slug riêng**, nên cùng một giá trị có
  thể tồn tại một lần ở mỗi loại. Nếu sau này muốn một giá trị slug chỉ thuộc **một** đối
  tượng trên toàn hệ (một namespace dùng chung), hiện không có cơ chế chống trùng xuyên bảng.
- **Vì sao ngoài phạm vi**: spec (Clarifications Q1) và research D6 **cố ý** chọn hai namespace
  và hai route riêng; một slug registry dùng chung là máy móc chưa đo được nhu cầu
  (Constitution VII).
- **Gỡ bằng cách nào**: thêm một bảng `slug_registry(kind, slug)` với unique trên `slug`, hoặc
  một check xuyên bảng ở tầng ứng dụng + ràng buộc lưu trữ. Một feature riêng, chỉ khi có nhu
  cầu thật.
- **Ảnh hưởng tới task đã tick**: không.
