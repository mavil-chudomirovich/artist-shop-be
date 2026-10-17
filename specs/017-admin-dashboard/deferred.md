# Deferred — Admin Dashboard Overview (017)

Việc phát hiện khi xây **Admin Dashboard Overview** (module 16 Admin) nhưng **không phải việc của
feature này**. Không mục nào ở đây chặn việc đóng feature; không mục nào là task của feature.

File này tồn tại vì một ô `- [ ]` trong `tasks.md` mang một ý nghĩa khác. `- [ ]` nghĩa là *feature này
chưa xong*. Việc thuộc về feature khác, hoặc thuộc chính hạ tầng test, không phải phần chưa xong của
feature này; ghi nó như vậy hoặc giữ một feature đã đóng trông như còn mở, hoặc ép điều kiện thoát bị làm giả.

## Các mục

### D1 — Doanh thu quy theo `orders.created_at`, chưa theo thời điểm thanh toán

- **Vấn đề**: Dashboard cộng doanh thu theo `orders.created_at` (FR-010) vì bảng `orders` **chưa có**
  mốc thời điểm thanh toán. Một đơn tạo ngày 31/10 nhưng được trả tiền ngày 02/11 được tính vào doanh
  thu **tháng 10**; theo định nghĩa kế toán/thanh toán thì nó thuộc **tháng 11**. Sai lệch này xuất hiện
  ở cả `revenue.currentMonthAmount`, `revenue.lastMonthAmount` và `revenueTrend`.
- **Vì sao ngoài phạm vi**: Feature 017 là **read-only** và spec ghi rõ trong *Assumptions* rằng doanh
  thu quy theo `created_at` cho tới khi có `paid_at`; thêm một mốc `paid_at`/payment-event là thay đổi
  **schema và state machine của module 07 Order/08 Payment** — không phải việc của một feature dashboard
  chỉ đọc.
- **Gỡ bằng cách nào**: Thêm `paid_at` (hoặc một bảng payment-event sở hữu bởi module 08) do module
  order/payment ghi khi đơn chuyển `PAID`; đổi read contract `OrderStatistics` và các câu SQL của order
  sang lọc/cộng theo mốc đó. Nằm ở một feature của module 07/08, không ở module 16.
- **Ảnh hưởng tới task đã tick**: Không. Các task đã tick (T013, T034, T037) chứng minh doanh thu đúng
  **theo định nghĩa `created_at`** của spec; chúng không tuyên bố quy theo thời điểm thanh toán.

### D2 — Khoảng trống snapshot danh mục cho dữ liệu cũ

- **Vấn đề**: Backfill trong `migrations/00017_order_item_category_snapshot.sql` chỉ điền snapshot cho
  những dòng đơn mà **variant/product/danh mục hiện tại còn resolve được**, và nó dùng **danh mục hiện
  tại** của sản phẩm — không có dữ liệu lịch sử để biết danh mục lúc bán. Hai hệ quả: (a) dòng cũ có
  sản phẩm đã xoá hoặc danh mục đã xoá ở lại `category_id = null` và rơi vào bucket `"Khác"`; (b) dòng
  cũ có sản phẩm **đã đổi danh mục** sau khi bán mang danh mục **hiện tại**, không phải danh mục lúc
  bán — nên doanh thu lịch sử của chúng có thể đã nằm sai bucket trước lần bán đầu tiên sau feature này.
  Các dòng **mới** (bán sau feature) luôn snapshot đúng tại checkout/edit nên không bị ảnh hưởng.
- **Vì sao ngoài phạm vi**: Không tồn tại nguồn dữ liệu lịch sử nào để backfill chính xác hơn —
  `order_items` chưa từng lưu danh mục, và `products.category_id` chỉ giữ giá trị **hiện tại**. Không
  feature nào trong repo có thể tái tạo quá khứ; "sửa" nó cần một nguồn ngoài (log, sao lưu) không có.
- **Gỡ bằng cách nào**: Chấp nhận là giới hạn một lần (ghi ở đây và ở
  [`docs/modules/16-admin.md`](../../docs/modules/16-admin.md)); nếu cần chính xác lịch sử, khôi phục từ
  sao lưu/bản ghi ngoài hệ thống. Từ feature 017 trở đi, snapshot tại checkout/edit đã giữ đúng lịch sử,
  nên vấn đề **không tái diễn** cho dữ liệu mới. Có thể thêm một công cụ đối soát một lần nếu có sao lưu.
- **Ảnh hưởng tới task đã tick**: Không làm mất hiệu lực bằng chứng của **T052** (T052 chứng minh backfill
  điền được dòng resolve được, check both-or-none từ chối snapshot nửa vời, và Down/Up khả nghịch — đúng
  như nó tuyên bố); nó chỉ nêu rõ **T052 không** chứng minh danh mục lịch sử của dữ liệu cũ là chính
  xác. Tương tự với T047 (snapshot dữ liệu **mới** không đổi khi danh mục đổi) — không bị suy yếu.

### D3 — Nợ join product → `categories` xuyên module (có trước feature này)

- **Vấn đề**: `internal/modules/product/infrastructure/implement/postgres/variant.go`
  (`FindFactsByIDs`) đã `JOIN categories` — một module (04 Product) đọc bảng của module khác (03
  Category) — từ trước feature 017. Feature 017 **mở rộng** projection đó bằng `category_id`/`category_name`
  để phục vụ snapshot danh mục khi checkout (T009), nhưng **không** sửa ranh giới.
- **Vì sao ngoài phạm vi**: Sửa nợ này đòi hỏi một hợp đồng đọc `CategoryQuery` (do module 03 sở hữu) và
  một refactor read của product — thay đổi kiến trúc của module 04, không phải việc của một feature
  dashboard. Feature 017 ghi nhận và **không** copy join này vào module admin (admin không đọc bảng nào).
  Plan của feature đã ghi nợ này ở Complexity Tracking.
- **Gỡ bằng cách nào**: Thêm một hợp đồng đọc danh mục trong `internal/contracts` do module 03 cấp adapter,
  rồi thay `JOIN categories` trong `FindFactsByIDs` bằng lời gọi hợp đồng (hoặc giữ một projection category
  thuộc module 04 nếu module 04 được phép đọc — cần một ADR). Nằm ở feature của module 03/04.
- **Ảnh hưởng tới task đã tick**: Không. **T009** chỉ mở rộng projection hiện có và test của nó
  (`catalog_test.go`) chứng minh mapping đúng; nó không tuyên bố ranh giới module đã sạch.

### D4 — Index/cache khi volume tăng

- **Vấn đề**: Dashboard `GROUP BY` trên `order_items`/`orders` theo cửa sổ tháng và không có index riêng
  cho danh mục; đồng thời không cache. Ở khối lượng MVP (mục tiêu SC-006: 10.000 order / 1.000 variant
  dưới 2 giây) điều này đủ. Với dữ liệu lớn hơn nhiều, truy vấn category/trend có thể chậm dần và bảng
  `order_items` tăng theo số dòng bán.
- **Vì sao ngoài phạm vi**: Spec ghi rõ *Assumptions* "Không cache … nếu volume tăng, có thể thêm read
  model/cache bằng feature riêng" và Constitution VII (YAGNI) cấm thêm cache/pre-aggregation khi chưa đo
  được nhu cầu. Tối ưu trước khi có số đo là thêm phức tạp không cần thiết.
- **Gỡ bằng cách nào**: Trước tiên **đo** (kết quả SC-006 ở `overview_integration_test.go`); nếu vượt mục
  tiêu, thêm index phù hợp (ví dụ index hỗ trợ `orders(status, created_at)` cho revenue, hoặc một index
  trên `order_items(order_id, category_id)`), rồi một read model/bảng tổng hợp hoặc cache TTL ngắn nếu
  index chưa đủ. Nằm ở một feature riêng của module 16/07.
- **Ảnh hưởng tới task đã tick**: Không.

### D5 — Hỗ trợ đa tiền tệ

- **Vấn đề**: Toàn bộ dashboard giả định **VND**: các read của order lọc `currency = 'VND'`, và read
  contract set `Currency: "VND"` cho mọi amount (kể cả zero-fill của trend). Đơn có currency khác **không**
  được cộng vào amount VND (đúng theo spec) nhưng hiện **không** có cách hiển thị doanh thu theo currency
  khác; trộn currency sẽ làm một tổng mất nghĩa.
- **Vì sao ngoài phạm vi**: Spec ghi rõ *Assumptions* "MVP chỉ VND"; mở rộng cách biểu diễn tiền (nhiều
  currency, tỷ giá, tổng theo từng currency) là một thay đổi xuyên module (order/product/cart/payment và
  mọi hợp đồng đọc tiền), không phải việc của một feature dashboard.
- **Gỡ bằng cách nào**: Một feature riêng mở rộng cách biểu diễn tiền — hoặc trả doanh thu **tách theo
  từng currency**, hoặc thêm quy đổi tỷ giá có mốc thời gian. Phải qua một hợp đồng đọc tiền thống nhất,
  không cộng trộn trong module 16.
- **Ảnh hưởng tới task đã tick**: Không. Các task đã tick (T013, T034) chứng minh doanh thu chỉ cộng VND
  đúng như spec; chúng không tuyên bố hỗ trợ đa currency.
