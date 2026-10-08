# ADR 013 - Sản phẩm: hợp đồng hiển thị liên module, nhà chung của media, và hard delete

- **Status**: Accepted
- **Decision Date**: 2026-10-08
- **Decision Maker**: Mavil
- **Feature**: `specs/006-product-catalog`

## 1. Bối cảnh

Module 04 Product giao catalogue sản phẩm, và ba quyết định trong feature này sẽ khiến người đọc
code sau phải dừng lại, vì mỗi quyết định đều có một lựa chọn thay thế trông hợp lý hơn ở bề
mặt:

1. **Vì sao hiển thị sản phẩm phải hỏi module 03 Category qua một interface trong
   `internal/contracts`, thay vì `JOIN` thẳng vào bảng `categories`?** FR-002 làm hiển thị của
   sản phẩm phụ thuộc vào trạng thái hiển thị của danh mục — một dữ kiện module 04 không sở
   hữu.
2. **Vì sao năng lực lưu ảnh chuyển từ module 02 User sang `internal/share/media`?** Module 04
   cũng cần lưu ảnh, mà trước đây chỉ module 02 sở hữu.
3. **Vì sao xoá sản phẩm là hard delete, trong khi `docs/modules/04-product.md` ghi "xóa mềm"?**

Cả ba đều có hệ quả lâu dài — chúng quyết định cách các module được phép nói chuyện với nhau,
nơi ở của một năng lực dùng chung, và dữ liệu nào mất khi operator xoá — nên chúng được ghi
thành ADR thay vì chỉ nằm trong `research.md` của feature.

## 2. Quyết định

### 2.1 Hiển thị đi qua hợp đồng liên module, không `JOIN` vào bảng của module khác

Module 04 định nghĩa một interface trong `internal/contracts` với hai câu hỏi, và module 03
cung cấp adapter ở composition root:

```go
// CategoryQuery trả lời hai dữ kiện về danh mục mà module 04 cần và không
// thể tự đọc: những danh mục khách được thấy, và slug của khách trỏ tới danh
// mục nào.
type CategoryQuery interface {
    VisibleCategoryIDs(ctx context.Context) ([]uuid.UUID, error)
    CategoryIDBySlug(ctx context.Context, slug string) (uuid.UUID, bool, error)
}
```

Use case công khai gọi interface này một lần mỗi request rồi truyền câu trả lời xuống
repository; repository lọc `category_id = ANY($n)` **trong SQL** — ở cả danh sách lẫn đường đọc
chi tiết. Một bộ lọc `JOIN` thẳng vào `categories.is_visible` bị từ chối vì Hiến pháp I cấm một
module đọc bảng của module khác, và một `JOIN` sẽ biến schema của module 03 thành một phần câu
query của module 04.

### 2.2 Năng lực media chuyển sang `internal/share/media`

Port (`MediaStore`/`MediaReference`) và adapter Cloudinary chuyển từ module 02 sang
`internal/share/media`; cả module 02 và module 04 dùng chung gói. Hành vi không đổi: cùng chữ
ký, cùng cách tách validation, cùng cách báo lỗi đã được làm sạch. Port được khai ở đây thay vì
trong `application/interface` của từng module vì Hiến pháp I đồng thời nói "port dịch vụ ngoài
nằm trong `application/interface`" và "mã dùng chung nhiều module nằm trong `internal/share/`";
khi hai luật gặp nhau ở một năng lực dùng chung, chỉ một cách sắp xếp thoả cả hai — một port
chung, một adapter chung. Đây là một cách đọc có chủ ý hai nguyên tắc chồng lấn, được ghi ở
Complexity Tracking của `specs/006-product-catalog/plan.md`.

### 2.3 Xoá sản phẩm là hard delete

Xoá sản phẩm xoá hẳn dòng, cascade sang `product_images` và `product_set_items`; dòng audit ở
lại. Thành viên của một set bị xoá **không** bị đụng. Quyết định này lệch so với câu chữ "xóa
mềm" của `docs/modules/04-product.md`, và cái lệch được ghi lại chứ không che: hiện **không có
gì đọc** một sản phẩm đã xoá vì chưa có đơn hàng trong hệ thống, nên xoá mềm chỉ thêm một trạng
thái vô hình vào mọi đường đọc mà không có ai cần nó. Module 03 đã chọn hard delete với đúng lý
do này.

## 3. Hệ quả

| Hệ quả | Chi tiết |
|---|---|
| Tích cực | Mỗi module vẫn sở hữu bảng của mình: module 04 không biết schema `categories`, module 03 không đọc bảng `products`. Ranh giới module được giữ đúng như Hiến pháp I mô tả |
| Tích cực | Bộ lọc hiển thị chạy **trong SQL**, nên phân trang đúng: lọc trong Go sau khi lấy trang sẽ trả trang ngắn và một `total` đếm những dòng caller không nhận được (FR-006, FR-009) |
| Tích cực | Chỉ có **một** adapter media và **một** port, nên lược đồ ký của Cloudinary, hình dạng multipart và cách phân loại lỗi đã làm sạch không thể lệch giữa hai module |
| Tích cực | Xoá hẳn giữ mọi đường đọc đơn giản: không có `deleted_at` để quên trong `WHERE`, không có "ẩn vì đã xoá" cần phân biệt với "ẩn vì trạng thái" |
| Tiêu cực | **Hiển thị phụ thuộc vào một lời gọi liên module mỗi request công khai.** Câu trả lời là tập hợp danh mục đang hiển thị (tens of rows, không phải thousands) nên chi phí nhỏ, nhưng nó là một phụ thuộc thật: contract và adapter phải tồn tại và được wire ở composition root, và một module 03 chưa wire nghĩa là module 04 không có đường đọc công khai |
| Tiêu cực | **Đường đọc chi tiết trả giá cho một tập hợp nó không thực sự cần.** `VisibleCategoryIDs` trả cả tập trong khi đường đọc một sản phẩm chỉ cần biết danh mục của nó có nằm trong tập không; quy mô nhỏ của catalogue là điều làm đánh đổi này chấp nhận được thay vì cẩu thả (research D1) |
| Tiêu cực | **Hard delete để lại một nghĩa vụ cho module 07 Order.** Khi dòng đơn phải đọc lại sản phẩm nó tham chiếu, module Order phải chọn giữa soft delete (thêm `deleted_at`) hoặc chụp dữ liệu sản phẩm vào dòng đơn. Nghĩa vụ này được ghi ở `specs/006-product-catalog/deferred.md` D3, không phải bỏ ngỏ |
| Tiêu cực | **Việc di chuyển media chạm vào module 02.** Nó là một refactor có test đi kèm (`SetAvatar` gọi cùng ba method qua port chung, test module 02 không đổi), nhưng lịch sử của một năng lực đã ổn định nay phải được đọc lại để biết nó ở đâu |

**Xem lại quyết định khi**: module 07 Order cần giữ tham chiếu tới một sản phẩm đã xoá (khi đó
2.3 phải nhường chỗ cho soft delete hoặc snapshot), hoặc khi một module thứ ba cần một câu hỏi
khác về danh mục (khi đó `CategoryQuery` phải được mở rộng có kiểm soát, không phải `JOIN` lặng
lẽ).

## 4. Cách tiếp cận bị loại

| Phương án | Vì sao không chọn |
|---|---|
| `JOIN` thẳng vào `categories.is_visible` trong câu query công khai | Hiến pháp I cấm một module đọc bảng của module khác; `JOIN` biến schema của module 03 thành một phần câu query của module 04, phá vỡ ranh giới đúng ở chỗ nó cần được giữ nhất |
| Lọc sản phẩm ẩn trong Go sau khi lấy trang | Trả trang ngắn và `total` sai — `total` đếm những dòng caller không bao giờ nhận được; FR-006 và FR-009 không cho phép |
| Sao chép (denormalise) `is_visible` hoặc `slug` của danh mục xuống sản phẩm | Ẩn một danh mục, hoặc đổi tên nó, sẽ không lan sang bản sao — bản sao sai đúng lúc quan trọng nhất |
| Mỗi module tự khai một interface media giống hệt và chỉ chia sẻ adapter | Hai interface cấu trúc giống nhau với một implementation là loại lệch (drift) mà dự án từ chối ở nơi khác; khai báo thứ hai tồn tại chỉ để thoả một luật về một port không phải của module mình |
| Module 04 import thẳng adapter của module 02 | Hai module nghiệp vụ phụ thuộc lẫn nhau trực tiếp — đúng kiểu coupling mà ranh giới module tồn tại để ngăn |
| Xoá mềm sản phẩm ngay bây giờ, để phòng trước | Là một trạng thái không có người đọc, và module cần nó (Order) chưa tồn tại; Hiến pháp VII (YAGNI) nói không dựng trước data model của module sau |
| Xoá dòng nhưng giữ lại tài nguyên ảnh trên provider | Tài nguyên sẽ không thể với tới và vẫn bị tính tiền mà không có chủ |
