# Architecture Decision Records

Nhật ký quyết định kiến trúc (ADR). Mỗi quyết định đáng nhớ một file, đánh số, không
sửa nội dung gốc — sai thì viết ADR mới thay thế.

Vì sao cần: `research.md` của một feature ghi lý do lúc **thiết kế feature**. ADR ghi
lại quyết định **còn hiệu lực sau nhiều tháng và nhiều feature**, để người sau hiểu vì
sao code trông "lạ". Quyết định đáng nhớ phải có ADR, không chỉ có research.

## Quy tắc

1. Tên file: `NNN-ten-quyet-dinh-kebab.md`, đánh số không dùng lại.
2. Trạng thái: `Proposed` → `Accepted` → `Superseded by ADR NNN` | `Deprecated` | `Rejected`.
3. **Không sửa nội dung quyết định gốc.** Sai rồi thì viết ADR mới ở trạng thái
   `Supersedes`, và thêm dòng *Bổ sung* ở ADR cũ nếu cần ghi chú.
4. Chỉ ghi quyết định **còn hiệu lực và có ảnh hưởng lâu dài**. Quyết định nhỏ để trong
   `specs/<feature>/research.md` là đủ.
5. Mỗi ADR dẫn chiếu hiến pháp nếu liên quan, và dẫn chiếu `docs/system-design/` nếu
   có.

## Template

Dùng [template.md](template.md).

## Danh sách

| ADR | Quyết định | Trạng thái |
|---|---|---|
| [001](001-modular-monolith.md) | Modular monolith đơn binary thay vì microservices | Accepted |
| [002](002-administrative-dataset-embedded.md) | Nhúng dataset hành chính vào binary, không tạo bảng | Accepted |
| [003](003-single-default-address-index.md) | Ràng buộc một địa chỉ mặc định bằng partial unique index | Accepted |
| [004](004-soft-delete-addresses.md) | Xoá địa chỉ bằng soft delete | Accepted |
| [005](005-media-resize-on-provider.md) | Thu nhỏ ảnh nhờ media provider, không thêm thư viện ảnh | Accepted |
| [006](006-compose-default-no-host-ports.md) | Stack Docker mặc định không publish port của db/redis | Accepted |
| [007](007-golangci-lint-v2-ci-gates.md) | golangci-lint v2 + action v9 + gate theo cỡ thay đổi | Accepted |
| [008](008-per-route-content-type-and-body-ceiling.md) | Kiểm tra content-type theo từng route, `MAX_BODY_BYTES` bằng 2× trần avatar | Accepted |
| [009](009-operator-address-view-without-review-flag.md) | Tra cứu operator trả hình dạng địa chỉ riêng, không có `divisionNeedsReview` | Accepted |
| [010](010-avatar-upload-refusal-and-startup-guard.md) | Route avatar trả `USER_AVATAR_TOO_LARGE`; từ chối khởi động khi `MAX_BODY_BYTES` thấp hơn trần avatar | Accepted |
| [012](012-category-folding-and-two-response-shapes.md) | Danh mục: chuẩn hoá (fold) ở tầng ứng dụng, và hai hình dạng response trên một bảng | Accepted |

## Khi nào phải viết ADR

| Tình huống | Viết ADR |
|---|---|
| Chọn giữa hai công nghệ có hệ quả lâu dài | ✅ |
| Chọn cách lưu dữ liệu (bảng hay nhúng, xoá cứng hay mềm) | ✅ |
| Thêm tầng trừu tượng mới cho cả hệ thống | ✅ |
| Từ chối một hướng đi mà team có thể đề xuất lại | ✅ (ghi lại để không phải tranh luận lại) |
| Đổi tên biến cấu hình | ❌ |
| Đổi cấu trúc thư mục nội bộ một module | ❌ |
| Thêm dependency nhỏ, thuần tiện | ❌ (ghi vào research của feature) |
