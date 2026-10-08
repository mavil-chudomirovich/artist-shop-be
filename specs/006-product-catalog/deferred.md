# Deferred — 006-product-catalog

Việc phát hiện khi xây Product Catalogue nhưng **không phải việc của feature này**.
Không mục nào ở đây chặn việc đóng feature này; không mục nào là task của feature này.

## Quy tắc cho file này

- Mỗi mục nêu rõ **nó là gì**, **vì sao ngoài phạm vi**, và **điều gì sẽ gỡ được** nó.
- Mục nào làm mất hiệu lực bằng chứng của một task đã tick `[X]` **phải nói rõ và nêu số
  task đó**.
- Nếu một mục hoá ra thuộc feature này, nó quay lại `tasks.md` thành task thật.

## Các mục

### D1 — Trạng thái bán tự chuyển khi hết/hồi kho

- **Vấn đề**: `docs/modules/04-product.md` mô tả luồng *"trạng thái tự chuyển khi hết/hồi kho
  (phối hợp inventory)"*. Feature này **không** làm được: không có thực thể kho nào trong hệ
  thống đã giao, và theo quyết định đã chốt (FR-038) sản phẩm **không** mang cột tồn kho. Nên
  `OUT_OF_STOCK` hiện chỉ do operator đặt tay.
- **Vì sao ngoài phạm vi**: tồn kho là module 05. Thêm cột tồn kho bây giờ là dựng trước data
  model của module sau (Constitution VII), và kéo theo nghĩa vụ `inventory_transactions` của
  Constitution II — mọi thay đổi kho phải ghi một bản ghi.
- **Gỡ bằng cách nào**: module 05 Inventory thêm thực thể kho và bảng `inventory_transactions`,
  rồi chuyển `ACTIVE ↔ OUT_OF_STOCK` theo số lượng trong cùng một transaction. State machine của
  module 04 đã có sẵn hai cạnh đó (FR-021), nên module 05 chỉ cần gọi transition đã tồn tại
  thay vì tự viết lại luật.
- **Ảnh hưởng tới task đã tick**: không. FR-038 nói rõ feature này không theo dõi tồn kho, nên
  không task nào của feature này bị suy yếu.

### D2 — Nội dung của combo set không hiện cho khách

- **Vấn đề**: một set là một sản phẩm có tên, giá và ảnh riêng, và khách **không** thấy bên
  trong nó gồm những gì. Shape công khai không liệt kê thành viên (research D18).
- **Vì sao ngoài phạm vi**: spec mô tả set với khách là *"một món duy nhất với tên, giá và ảnh
  riêng"* (US5) và **không** yêu cầu shape công khai liệt kê nội dung. Thêm trường đó là plan tự
  phát minh một contract mà spec không mô tả.
- **Gỡ bằng cách nào**: một feature riêng sửa FR-005 để shape công khai của set mang danh sách
  thành viên (định danh, tên, slug). Dữ liệu đã có sẵn trong `product_set_items`; chỉ thiếu phần
  đọc và một dòng trong contract.
- **Ảnh hưởng tới task đã tick**: không.

### D3 — Xoá cứng sản phẩm, và nghĩa vụ mà nó để lại cho module Order

- **Vấn đề**: xoá sản phẩm là **hard delete** (research D13). `docs/modules/04-product.md` ghi
  *"xóa mềm"* trong phạm vi MVP, nên đây là một lệch có chủ ý so với câu chữ của module doc.
- **Vì sao ngoài phạm vi**: hiện **không có gì đọc** một sản phẩm đã xoá — chưa có đơn hàng
  trong hệ thống. Xoá mềm bây giờ là thêm một trạng thái vô hình vào mọi đường đọc mà không có
  ai cần nó. Module 03 đã chọn hard delete với đúng lý do này.
- **Gỡ bằng cách nào**: module 07 Order, khi dòng đơn hàng phải đọc lại sản phẩm nó tham chiếu,
  phải chọn giữa soft delete (thêm `deleted_at`) hoặc chụp lại dữ liệu sản phẩm vào dòng đơn.
  Đó là quyết định thiết kế của module Order, không phải của module này.
- **Ảnh hưởng tới task đã tick**: không.

### D4 — Trần 10 ảnh và luật "chỉ sản phẩm `is_set` mới được làm set" nằm ở tầng ứng dụng

- **Vấn đề**: hai luật này **không** có ràng buộc tương ứng ở tầng lưu trữ. Trần số ảnh được giữ
  đúng nhờ transaction khoá dòng sản phẩm (research D6), và luật `is_set` chỉ được kiểm trong use
  case.
- **Vì sao ngoài phạm vi**: không luật nào diễn đạt được bằng ràng buộc trên ba bảng này — một
  con số không phải thuộc tính của dòng, và luật thứ hai là thuộc tính của dòng **tham chiếu** ở
  bảng khác. FR-033 liệt kê ba luật phải xuống tầng lưu trữ (slug, giá, tham chiếu danh mục) và
  **không** có hai luật này. Transaction đã làm trần ảnh không còn race.
- **Gỡ bằng cách nào**: nếu sau này cần bảo đảm ở tầng lưu trữ, dùng trigger cho trần ảnh và một
  trigger kiểm `is_set` cho `product_set_items`. Chỉ nên làm khi có bằng chứng cần — trigger là
  business logic trong schema, vô hình với domain và với test.
- **Ảnh hưởng tới task đã tick**: không, vì FR-020 chỉ đòi kiểm **trước khi lưu trữ**, và
  transaction thoả điều đó.

### D5 — Index thứ tự không phủ bộ lọc hiển thị của đường đọc công khai

- **Vấn đề**: `products_ordering_idx` là `(position, created_at, id)`. Nó cho thứ tự mà không
  phải sort, nhưng không phủ điều kiện hiển thị (`sell_state` + `category_id = ANY(...)`). Ở quy
  mô hiện tại planner quét bảng dù có index.
- **Vì sao ngoài phạm vi**: đúng về mặt chức năng — FR-009 và FR-006 đều được thoả. Thêm cột chỉ
  là suy đoán khi chưa đo. Cùng loại với D8 của feature 005.
- **Gỡ bằng cách nào**: khi catalogue đủ lớn để planner thật sự chọn index, thay bằng một index
  phủ (`sell_state, category_id, position, created_at, id`) hoặc partial index, kèm đo lại.
- **Ảnh hưởng tới task đã tick**: không.

### D6 — `meta.total` vắng mặt khi bằng 0

- **Vấn đề**: tầng envelope dùng `omitempty` cho `Total` (`internal/share/httpx/response.go:21`),
  nên danh sách rỗng trả `meta` **không có** `total`. Đây là bất nhất quán **có sẵn** của tầng
  dùng chung — feature 005 đã ghi nó là D9 — và nó ảnh hưởng cả hai danh sách của feature này.
- **Vì sao ngoài phạm vi**: sửa nó là thay đổi tầng dùng chung ảnh hưởng module 02 và 03, cần
  feature riêng. Contract của feature này đã ghi rõ "thiếu = 0".
- **Gỡ bằng cách nào**: đổi `Total` sang `*int64` (hoặc tách một kiểu meta riêng cho endpoint
  phân trang) để `total` **luôn** có mặt khi phân trang, còn response không phân trang vẫn không
  mang `page`/`pageSize`/`total`.
- **Ảnh hưởng tới task đã tick**: không. Đây là bất nhất quán của tầng dùng chung, không phải do
  feature này tạo ra; 005 đã ghi cùng mục.

### D7 — Biến thể, đa ngôn ngữ, tìm kiếm nâng cao và sắp xếp lại ảnh

- **Vấn đề**: bốn thứ nằm trong "Không (hoãn)" của `docs/modules/04-product.md` hoặc bị YAGNI
  loại: biến thể phức tạp (size/màu), đa ngôn ngữ nội dung sản phẩm, bộ lọc/thuộc tính nâng cao,
  và sắp xếp lại thứ tự ảnh (thứ tự hiện là thứ tự thêm vào, research D11).
- **Vì sao ngoài phạm vi**: module doc tự hoãn hai mục đầu; mục thứ ba cần một chỉ mục tìm kiếm
  chứ không chỉ vài cột lọc, tức là feature riêng; mục thứ tư không được spec yêu cầu — FR-016
  đòi ảnh **có thứ tự**, không đòi thứ tự **sửa được**.
- **Gỡ bằng cách nào**: mỗi mục là một feature riêng khi có nhu cầu cụ thể. Sắp xếp lại ảnh là
  việc nhỏ nhất — thêm một endpoint đổi `position` và một partial unique index trên
  `(product_id, position)` để thứ tự không trùng.
- **Ảnh hưởng tới task đã tick**: không.

### D8 — Contract của feature này khai một đường dẫn thuộc module 03

- **Vấn đề**: `contracts/openapi.yaml` của feature này khai lại `DELETE /api/v1/admin/categories/{id}`
  để ghi nhận `409 CATEGORY_IN_USE` mới. Cùng một path do hai file OpenAPI mô tả.
- **Vì sao ngoài phạm vi**: feature này **đổi** một câu trả lời của module 03, nên nếu không khai
  thì contract máy đọc được của chính feature này thiếu mất thay đổi nó gây ra. Nhưng việc gộp
  hai file OpenAPI thành một nguồn duy nhất là thay đổi quy ước toàn dự án, không phải việc của
  một feature module.
- **Gỡ bằng cách nào**: nếu dự án muốn một file OpenAPI hợp nhất, gộp theo module và sinh
  `docs/api-reference.md` từ đó. Hiện `docs/api-reference.md` là nguồn thẩm quyền
  (Constitution VIII), và hai file feature chỉ chồng lấn ở đúng một path này.
- **Ảnh hưởng tới task đã tick**: không.

### D9 — Alias `MediaStore`/`MediaReference` còn lại trong module 02

- **Vấn đề**: khi tách capability media sang `internal/share/media`, `internal/modules/user/application/interface/ports.go`
  vẫn còn `type MediaStore = media.Store` và `type MediaReference = media.Reference`. Đó là **alias**, không
  phải định nghĩa thứ hai: chỉ có một định nghĩa duy nhất, ở `share/media`, và comment tại chỗ nói rõ điều đó.
  Nhưng production của module 02 (`profile.go`) đã dùng thẳng `media.Store`, nên alias **chỉ còn test dùng**.
- **Vì sao ngoài phạm vi**: T003 cấm sửa `avatar_test.go` để giữ bằng chứng "đây là move chứ không phải
  rewrite" — 3 file test (kể cả `http_integration_test.go`) tham chiếu tên cũ. Giữ alias đổi lấy bằng chứng
  đó là một đánh đổi có ý thức của orchestrator, không phải sót.
- **Gỡ bằng cách nào**: xoá hai alias và đổi ~12 tham chiếu kiểu trong `avatar_test.go` (2 file) và
  `http_integration_test.go`. Thuần đổi tên kiểu, không đổi assertion. Việc nhỏ, làm khi có dịp chạm module 02.
- **Ảnh hưởng tới task đã tick**: không. Không task nào của feature này phụ thuộc alias.

### D10 — `normalized_slug` bằng `slug` với sản phẩm, nên cột đó dư

- **Vấn đề**: luật slug đã bắt buộc chữ thường và đã trim, nên khoá gập `FoldKey(slug)` **luôn bằng** `slug`.
  Cột `normalized_slug`, shape check và unique index trên nó không sai, nhưng **không bao giờ khác** giá trị
  của `slug`. Điều này đúng với cả module 03 (feature 005 cũng có `normalized_slug` như vậy).
- **Vì sao ngoài phạm vi**: đây là pattern **có sẵn** từ feature 005, và sửa nó là thay đổi cả module 03.
  Với `name` thì khoá gập thật sự cần (tên không bắt buộc chữ thường); với `slug` thì không.
- **Gỡ bằng cách nào**: một feature riêng bỏ `normalized_slug` ở **cả hai** module và chuyển unique index
  sang chính `slug`, kèm ghi chú rằng luật shape đã bảo đảm chữ thường. Cần làm cùng lúc hai module để
  hai bảng không lệch quy ước.
- **Ảnh hưởng tới task đã tick**: không. T026 khẳng định unique index từ chối slug trùng — vẫn đúng, chỉ là
  index đang nằm trên cột có giá trị bằng `slug`.

### D11 — `preorderExpectedAt: null` khi cập nhật nghĩa là "giữ nguyên"

- **Vấn đề**: gửi `null` và **không gửi** trường `preorderExpectedAt` đều được hiểu là "giữ nguyên ngày cũ".
  Vì vậy client **không thể** xoá ngày dự kiến mà vẫn giữ nhãn pre-order; muốn xoá thì gửi `isPreorder: false`
  (xoá cả hai).
- **Vì sao ngoài phạm vi**: spec và `quickstart.md` không yêu cầu thao tác "xoá ngày, giữ nhãn". Thêm cờ
  hiện diện cho một trường là thay đổi contract mà spec không mô tả.
- **Gỡ bằng cách nào**: nếu product owner cần, thêm một cặp cờ hiện diện (`hasPreorderExpectedAt`) hoặc một
  quy ước rõ ràng rằng `null` = xoá, rồi sửa `contracts/openapi.yaml` và `frontend-guide.md` cùng lúc.
- **Ảnh hưởng tới task đã tick**: không.

### D12 — Module 03 và 04 không có annotation Swagger, nên `/swagger` chỉ phủ module 01/02

- **Vấn đề**: ADR 011 dựng Swagger từ annotation trong code, phục vụ tại `/swagger`. Module 01 (auth) và
  module 02 (user) có `@Summary`/`@Router`; module 03 (category) và module 04 (product) **không có**, nên
  giao diện `/swagger` không liệt kê endpoint của hai module này.
- **Vì sao ngoài phạm vi**: đây là **khoảng trống cấp dự án**, không phải phần chưa xong của feature này.
  Feature 005 (module 03) đã giao mà không thêm annotation, và `specs/005-category-catalog/deferred.md`
  không ghi lại điều đó (đó là một thiếu sót của 005, ghi ở đây để không mất). Definition of Done của
  constitution chỉ đòi `docs/api-reference.md` — đã cập nhật — và `make swagger-check` vẫn **pass** vì nó
  chỉ so `docs/swagger` với annotation hiện có, không đòi annotation phải tồn tại.
- **Gỡ bằng cách nào**: một feature riêng thêm annotation cho **cả** module 03 và 04 (và mọi module sau),
  rồi chạy `make swagger`. Làm lẻ một module sẽ khiến `/swagger` lệch quy ước giữa các module.
- **Ảnh hưởng tới task đã tick**: không. T067 giao `docs/api-reference.md` theo Constitution VIII và
  `make check` (gồm `swagger-check`) xanh — không task nào của feature này hứa annotation Swagger.
