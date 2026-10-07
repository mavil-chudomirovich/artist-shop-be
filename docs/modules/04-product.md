# Module 04 — Product

- **Trạng thái Spec Kit**: Đã có spec
- **Spec**: `specs/006-product-catalog`
- **Ưu tiên / Giai đoạn**: Giai đoạn 1
- **Phụ thuộc**: category

## Mục đích

Quản lý sản phẩm vật lý (acrylic stand, shikishi, badge, print, keychain, combo
set) và trạng thái bán, làm nền cho giỏ hàng, đơn hàng và kho.

## Phạm vi MVP

Có:
- Danh sách + chi tiết sản phẩm công khai (ảnh, giá, mô tả, trạng thái).
- Admin tạo/sửa/xóa mềm sản phẩm.
- Ảnh sản phẩm (nhiều ảnh, ảnh chính) qua Cloudinary.
- Trạng thái bán: COMING_SOON → ACTIVE → OUT_OF_STOCK → DISCONTINUED.
- Combo set (nhóm sản phẩm/bundle).
- Pre-order (đặt trước).

Không (hoãn):
- Biến thể phức tạp (size/màu) nâng cao.
- Đa ngôn ngữ nội dung sản phẩm.

## Thực thể dữ liệu

- `products`: tên, slug, mô tả, giá, danh mục, trạng thái bán, cờ pre-order/combo.
- `product_images`: tham chiếu Cloudinary (public_id, secure_url), thứ tự, ảnh chính.

## Luồng nghiệp vụ chính

1. Admin tạo sản phẩm + ảnh + trạng thái.
2. Khách duyệt/tìm sản phẩm, xem chi tiết.
3. Trạng thái tự chuyển khi hết/hồi kho (phối hợp inventory).

## Yêu cầu chức năng sơ bộ

- Slug sản phẩm duy nhất.
- Sản phẩm chỉ bán được khi ở trạng thái ACTIVE.
- Giá lưu dạng số nguyên đơn vị nhỏ + đơn vị tiền tệ.
- Chuyển trạng thái bán theo quy tắc rõ ràng, từ chối chuyển không hợp lệ.

## Tiêu chí hoàn thành

- CRUD sản phẩm + quản lý ảnh + danh sách công khai có test.
- Quy tắc trạng thái bán có test (kể cả chuyển không hợp lệ).
- Ràng buộc giá/tiền tệ tuân thủ constitution.

## Ghi chú / câu hỏi mở

Hai câu hỏi dưới đây đã được chốt lúc specify — xem `specs/006-product-catalog/spec.md`, mục
`Clarifications`, session 2026-10-07:

- **Combo set tính giá thế nào (giá cố định hay tổng thành phần)?** → **Operator đặt giá cho cả
  set** (FR-038). Set là một sản phẩm độc lập có giá riêng; các sản phẩm trong set không quyết
  định giá đó.
- **Pre-order giới hạn số lượng theo đợt? Trạng thái riêng?** → **Không**. Pre-order là một nhãn
  kèm ngày dự kiến trên sản phẩm bình thường, không thêm trạng thái bán (FR-039).

Còn để ngỏ cho **module 05 Inventory**: luồng "trạng thái tự chuyển khi hết/hồi kho". Module 04
không theo dõi tồn kho, nên `OUT_OF_STOCK` hiện do operator đặt (FR-037); phần tự động hoá là việc
của module 05.

## Tham chiếu

- `docs/product/project_overview.md` (Shop, product lifecycle)
- `docs/product/backend-spec.md` (Database: products, product_images)
