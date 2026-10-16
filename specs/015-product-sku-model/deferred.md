# Deferred — Product SKU Model (015)

Việc phát hiện khi xây Product SKU Model nhưng **không phải việc của feature này**.
Không mục nào ở đây chặn việc đóng feature 015; không mục nào là task của feature 015.

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

### D-01 — Biên đọc product → inventory cho trạng thái còn hàng theo variant

- **Vấn đề**: để hiển thị `inStock` trên trang chi tiết sản phẩm (FR-003/FR-005), module 04 phải
  hỏi module 05 tồn khả dụng của từng variant. Đây là một **cross-module read mới** theo chiều
  ngược với tín hiệu `ACTIVE ↔ OUT_OF_STOCK` sẵn có (inventory → product). Trang chi tiết giờ phụ
  thuộc vào một lời gọi inventory (bulk, một lần cho cả trang) trong luồng đọc công khai.
- **Vì sao ngoài phạm vi**: spec (Assumptions) và research D9/D10 **cố ý** chọn cách này — module 04
  không được đọc bảng tồn kho (Constitution I), nên một hợp đồng đọc là bắt buộc; và nhu cầu thật là
  hiển thị trạng thái, không phải tối ưu nó. Tối ưu (cache, gộp vào một câu SQL, hoặc một read-model)
  là việc riêng, có đo lường.
- **Gỡ bằng cách nào**: một feature tối ưu đọc (ví dụ cache ngắn hạn theo variant, hoặc một
  read-model cập nhật theo sự kiện kho) — chỉ khi chi phí đo được. Nếu không, giữ nguyên.
- **Ảnh hưởng tới task đã tick**: không. T033/T046 giao đúng một read bulk một lần/trang; không có
  bằng chứng nào nói nó nhanh hay chậm.

### D-02 — Biến thể nhiều chiều (màu × size) và ma trận tổ hợp

- **Vấn đề**: mỗi sản phẩm có một danh sách variant **phẳng** (một tên, một SKU, một giá, một ảnh,
  một thứ tự). Không biểu diễn được một sản phẩm vừa chọn màu vừa chọn size, và không có bảng tổ hợp
  (mỗi tổ hợp một SKU/giá/tồn riêng).
- **Vì sao ngoài phạm vi**: đây là **data model khác**, không phải cách diễn đạt khác — cần một ma
  trận tổ hợp và cách tính giá/tồn theo tổ hợp. Spec 015 (mục *Scope boundary*) xếp đa chiều ngoài
  phạm vi; nó từng là mục hoãn của 014 (D-02) và được **mang tiếp** sang đây.
- **Gỡ bằng cách nào**: một feature mới với entity tổ hợp (combination) và SKU matrix, xây trên nền
  sản phẩm + variant của 015. Không phải một mở rộng nhỏ.
- **Ảnh hưởng tới task đã tick**: không.

### D-03 — Bất biến "mỗi sản phẩm ≥1 variant" chỉ ở tầng ứng dụng

- **Vấn đề**: "mỗi sản phẩm phải có ≥1 variant" là bất biến liên dòng, **không** diễn đạt được bằng
  một ràng buộc một dòng (unique index/check). Hiện **ứng dụng** giữ nó: tạo sản phẩm bắt buộc kèm
  ≥1 variant; gỡ variant cuối bị từ chối `409 VARIANT_LAST_REMOVAL`. Một đường ghi bỏ qua ứng dụng
  (SQL tay, một adapter tương lai) có thể để lại sản phẩm không variant.
- **Vì sao ngoài phạm vi**: `data-model.md` đã ghi rõ FR-023 "where enforceable"; module 04 vốn từ
  chối trigger khi chưa đo được nhu cầu (Constitution VII). Ràng buộc này đòi một cơ chế đếm ở tầng
  lưu trữ (trigger, hoặc một ràng buộc bắc cầu) — thay đổi schema không cần cho feature.
- **Gỡ bằng cách nào**: một trigger `BEFORE DELETE` trên `product_variants` (từ chối khi là còn lại
  duy nhất) hoặc một kiểm tra lưu trữ tương đương, nếu một đường ghi ngoài ứng dụng xuất hiện.
- **Ảnh hưởng tới task đã tick**: suy yếu một phần bằng chứng của **T037/T040** (các use case giữ
  luật ở ứng dụng); các test hiện có chứng minh luật đúng ở đường ứng dụng, **không** chứng minh
  nó đúng với mọi đường ghi vào bảng.

### D-04 — Cửa sổ migration: sản phẩm từng là lựa chọn của một nhóm

- **Vấn đề**: migration `00014` **drop** mọi cấu trúc của feature 014. Nếu một DB thật đã chạy
  `00013` và có các sản phẩm là **lựa chọn** của một variant group, các nhóm biến mất và những sản
  phẩm đó trở thành **sản phẩm độc lập** (không mất dữ liệu sản phẩm, nhưng mất nhóm, nhãn, và bộ ảnh
  của nhóm). Mỗi sản phẩm như vậy cũng nhận một variant mặc định (`sku = code = slug`).
- **Vì sao ngoài phạm vi**: research D11 ghi giả định **014 chưa từng được áp lên DB sống** (đã
  commit nhưng chưa push), nên đây không phải một cửa sổ thật cần chuyển dữ liệu nhóm. `DROP ...
  IF EXISTS` làm cả DB đã chạy `00013` lẫn chưa chạy đều tới mô hình mới.
- **Gỡ bằng cách nào**: nếu 014 **đã** chạy ở đâu đó, cần một migration bổ sung **trước** `00014` để
  biến mỗi nhóm + lựa chọn thành một sản phẩm cha + variant (chuyển tên nhóm thành tên sản phẩm, các
  lựa chọn thành variant), thay vì chỉ drop. Là một feature/migration riêng.
- **Ảnh hưởng tới task đã tick**: suy yếu một phần bằng chứng của **T003/T047** (migration/backfill):
  chúng chứng minh mỗi sản phẩm cũ có code và một variant mặc định, **không** chứng minh dữ liệu nhóm
  của 014 được chuyển đổi — vì giả định là 014 chưa từng chạy.

### D-05 — Mã not-found của variant trong giỏ không thống nhất với `VARIANT_NOT_FOUND`

- **Vấn đề**: `internal/contracts/error-codes.md` của 015 định nghĩa `VARIANT_NOT_FOUND` 404 cho "một
  `variantId` lạ", và inventory dùng mã này. Nhưng **module cart** vẫn trả **`PRODUCT_NOT_FOUND`** 404
  cho một `variantId` không tồn tại (mã cũ đã lập, chỉ đổi khoá từ sản phẩm sang variant —
  `cart/presentation/http/errors.go`). Hai bề mặt trả hai mã khác nhau cho cùng tình huống "variant
  không tồn tại".
- **Vì sao ngoài phạm vi**: cart **cố ý** tái sử dụng `PRODUCT_NOT_FOUND` cho cả "variant không tồn
  tại" lẫn "dòng không thuộc giỏ này" để route không xác nhận nội dung giỏ của khách khác (quyết định
  của 008, giữ nguyên). Spec 015 chỉ buộc cart **khoá theo variant**, không buộc đổi mã.
- **Gỡ bằng cách nào**: nếu muốn thống nhất, đổi cart sang `VARIANT_NOT_FOUND` cho variant lạ — nhưng
  phải xem lại lý do "một câu trả lời cho cả hai tình huống" của cart, và là một thay đổi bề mặt
  client. Cân nhắc ở một feature riêng.
- **Ảnh hưởng tới task đã tick**: không (T023/T034 giữ nguyên mã cart như 008).

### D-06 — Hành động audit ảnh variant tái dùng `PRODUCT_VARIANT_UPDATED`

- **Vấn đề**: thêm, thay và gỡ ảnh của một variant đều ghi `audit_logs` với action
  `PRODUCT_VARIANT_UPDATED` (không có action riêng cho ảnh variant). Đường ảnh của **sản phẩm** có
  action riêng (`PRODUCT_IMAGE_ADDED`/`_REMOVED`/`_PRIMARY_SET`), nên audit ảnh variant kém chi tiết
  hơn.
- **Vì sao ngoài phạm vi**: variant có **một** ảnh (không có gallery, không có ảnh chính), nên một
  action "cập nhật variant" bao trùm là đủ để truy vết; thêm ba action nữa cho một ảnh là bề mặt
  audit thừa chưa đo được nhu cầu (Constitution VI/VII).
- **Gỡ bằng cách nào**: thêm `PRODUCT_VARIANT_IMAGE_ADDED`/`_REMOVED` nếu cần phân biệt thao tác ảnh
  variant trong audit; là thay đổi nhỏ nhưng đổi contract action (một breaking change của audit).
- **Ảnh hưởng tới task đã tick**: không. T040 giao ảnh variant qua nhà media; audit action chỉ là
  lựa chọn chi tiết.

### D-07 — Combo set và pre-order bị bỏ

- **Vấn đề**: các cờ `is_set`/`memberProductIds` (combo set) và `is_preorder`/`preorderExpectedAt`
  (đặt trước) đã bị **bỏ** cùng các bảng/cột của chúng; không còn đường bán combo set hay gắn nhãn
  pre-order.
- **Vì sao ngoài phạm vi**: spec 015 (Clarifications và *Scope boundary*) chốt bỏ cả hai ở phiên bản
  này; chúng làm phức tạp mô hình và không khớp "mỗi sản phẩm có nhiều SKU".
- **Gỡ bằng cách nào**: một feature riêng dựng lại combo set (một nhóm bán chung có giá riêng) và
  pre-order (nhãn + ngày dự kiến) trên mô hình sản phẩm + variant, khi có nhu cầu thật.
- **Ảnh hưởng tới task đã tick**: không. Chúng đã bị bỏ có chủ đích (T007, T026).
