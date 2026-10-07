# ADR 012 - Danh mục: chuẩn hoá ở tầng ứng dụng, và hai hình dạng response trên một bảng

- **Status**: Accepted
- **Decision Date**: 2026-10-07
- **Decision Maker**: Mavil
- **Feature**: `specs/005-category-catalog`

## 1. Bối cảnh

Module 03 Category giao một bảng `categories` phục vụ hai đối tượng: khách duyệt catalogue
công khai, và operator bảo trì qua các endpoint quản trị. Hai quyết định trong feature này
sẽ khiến người đọc code sau này phải dừng lại, và cả hai đều đã có lựa chọn thay thế trông
hợp lý hơn ở bề mặt:

1. **Vì sao khoá chuẩn hoá (`normalized_name`, `normalized_slug`) được tính trong ứng dụng
   thay vì bằng `lower()` của PostgreSQL?**
2. **Vì sao một bảng lại có hai hình dạng response (`PublicCategory` và `AdminCategory`)
   thay vì một hình dạng với một nhánh theo vai trò?**

Cả hai đều là quyết định có hệ quả lâu dài — chúng ảnh hưởng tới cách ghi dữ liệu, tới hợp
đồng HTTP và tới cách một người viết sau này phải sửa khi yêu cầu đổi — nên chúng được ghi
thành ADR thay vì chỉ nằm trong `research.md` của feature.

## 2. Quyết định

### 2.1 Chuẩn hoá (fold) ở tầng ứng dụng, không ở tầng lưu trữ

Hai cột `normalized_name` và `normalized_slug` được **ứng dụng** tính bằng
`strings.ToLower(strings.TrimSpace(...))` — tức simple case mapping theo Unicode của Go, không
theo locale — rồi lưu xuống. Hai unique index nằm trên hai cột đó. Cơ sở dữ liệu **không**
tính lại giá trị; nó chỉ mang một **check hình dạng** xác nhận giá trị đã được trim và đã
lowercase, để bắt một writer tương lai quên chuẩn hoá.

Lý do là tính đúng đắn, không phải sở thích. `lower()` của PostgreSQL fold theo collation của
database. Dưới collation `C` nó chỉ fold ASCII, nên một chữ cái tiếng Việt viết hoa như `Đ`
(U+0110) **không** được fold. Khi đó catalogue sẽ âm thầm chấp nhận hai danh mục chỉ khác
nhau ở hoa/thường của một chữ tiếng Việt — lỗi tệ nhất trong loại lỗi này, vì mọi test tiếng
Anh đều xanh. Go dùng Unicode simple case mapping nên `Đ` → `đ` và tập dấu tiếng Việt được
fold đúng, bất kể database của mỗi deployment được tạo thế nào.

Adapter **tính lại** khoá từ `name` và `slug` trên mọi lần ghi, không đọc lại khoá của
entity. Vì fold là hàm thuần và deterministic, một self-edit cùng tên ghi lại đúng giá trị
đang có và không thể bị nhận nhầm là va chạm với chính nó; và không có đường ghi nào có thể
lưu khoá rỗng hoặc khoá cũ.

### 2.2 Hai hình dạng response trên một bảng

Bề mặt công khai trả `PublicCategory` với đúng bốn member `id`, `name`, `slug`,
`description`. Bề mặt quản trị trả `AdminCategory` với thêm `position`, `isVisible`,
`createdAt`, `updatedAt`. Hai DTO ở hai tầng (application và presentation) và một mapper
chuyển giữa chúng; cả hai hình dạng dùng chung một adapter và một bảng.

Lý do là bảo vệ thông tin theo cấu trúc, không theo điều kiện. Trạng thái hiển thị và thứ
tự là cách operator quản lý catalogue, không phải phần khách đọc; và sự tồn tại của một danh
mục bị ẩn là thông tin khách **không** được biết. Hai hình dạng tách bạch khiến điều đó đúng
theo kiểu dữ liệu chứ không nhờ một `if` đúng ở mọi đường trả về. Đây cũng là bài học đã trả
giá một lần trong dự án: một hình dạng cộng nhánh theo vai trò dễ để lộ member chỉ bằng một
lần quên.

## 3. Hệ quả

| Hệ quả | Chi tiết |
|---|---|
| Tích cực | Tính duy nhất đúng với tiếng Việt ở **mọi** deployment, không phụ thuộc collation hay extension của database |
| Tích cực | Hình dạng công khai không thể lộ trạng thái hiển thị, thứ tự hay sự tồn tại của danh mục ẩn, kể cả khi một đường trả về bị sửa nhầm sau này |
| Tích cực | Ràng buộc duy nhất vẫn do tầng lưu trữ quyết định (hai unique index), nên hai lần tạo đồng thời không thể sinh bản ghi trùng |
| Tiêu cực | **Khoá chuẩn hoá là cột dẫn xuất được lưu.** Nếu quy tắc fold đổi (ví dụ gộp khoảng trắng bên trong — xem `deferred.md`, D3), phải **viết lại** khoá đã lưu bằng migration, chứ không thể tính lại lúc đọc. Đây là cái giá lớn nhất của quyết định 2.1, và nó được chọn có ý thức: tính đúng đắn của việc so khớp quan trọng hơn việc tránh một migration khi luật đổi |
| Tiêu cực | **Hai DTO và một mapper phải giữ đồng bộ.** Không có gì ở thời điểm biên dịch bắt buộc `AdminCategory` và `PublicCategory` cùng phản ánh một đổi trong model. Thêm một member vào model rồi quên một trong hai hình dạng là một lỗi không ai tự bắt; nó chỉ lộ ra qua test hình dạng. Cái giá này được chấp nhận vì bù lại là bảo vệ theo cấu trúc cho hình dạng công khai |
| Tiêu cực | Khoá chuẩn hoá tồn tại như **một cặp cột thừa** trong bảng, phải được ghi đúng trên mọi đường ghi. Adapter tính lại nên điều này được gom về một chỗ; nhưng check hình dạng ở database là lưới an toàn, không phải nguồn sự thật |
| Bù đắp | ADR 009 đã chọn hình dạng riêng cho tra cứu của operator ở module 02; quyết định 2.2 áp cùng nguyên tắc cho module 03, nên không có hai cách làm khác nhau trong dự án |

**Xem lại quyết định khi**: quy tắc chuẩn hoá cần đổi (khi đó cần một migration viết lại
khoá), hoặc khi một hình dạng thứ ba cho cùng bảng `categories` xuất hiện (khi đó cơ chế
"hai DTO cộng một mapper" đã hết cửa và cần một cách khác, ví dụ một projection có kiểu).

## 4. Cách tiếp cận bị loại

| Phương án | Vì sao không chọn |
|---|---|
| Unique index hàm trên `lower(trim(name))` | Cách này dời quy tắc chuẩn hoá vào một câu query, trong khi domain còn cần nó để dựng thông báo — quy tắc có hai nhà, và chúng có thể lệch |
| Kiểu `citext` (case-insensitive text) | Cần cài extension, và cách fold của nó theo collation của database thay vì chuẩn Unicode — đúng vấn đề ở mục 2.1 |
| Dùng `lower()` / ICU collation trong database | Làm tính đúng đắn phụ thuộc vào database của từng deployment; test database và production có thể bất đồng |
| Chỉ kiểm tra trùng ở tầng ứng dụng | Hai lần tạo đồng thời cùng đọc "chưa có tên" rồi cùng insert; chỉ báo trùng khi cuộc đua không xảy ra. Module 03 của feature 003 đã từng mắc đúng lớp lỗi này |
| Một hình dạng response duy nhất, nhánh theo vai trò | Đúng kiểu lỗi mà 2.2 sinh ra để tránh: hình dạng công khai phụ thuộc vào một `if` đúng ở mọi đường trả về |
| Hai bảng / hai view cho hai đối tượng | Nhân đôi nguồn sự thật và buộc mọi thay đổi ghi phải giữ hai nơi đồng bộ; vấn đề chỉ là cách **trình bày** khác nhau, không phải dữ liệu khác nhau |
