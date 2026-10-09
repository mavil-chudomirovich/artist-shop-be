# Module 14 — Wishlist

- **Trạng thái Spec Kit**: Chưa specify
- **Spec**: _(chưa có)_
- **Ưu tiên / Giai đoạn**: V2.0 (ngoài MVP)
- **Phụ thuộc**: product, user

## Mục đích

Cho khách lưu sản phẩm quan tâm để mua sau.

## Phạm vi MVP

Có:
- Thêm/xóa sản phẩm khỏi danh sách yêu thích.
- Xem danh sách yêu thích.

Không (hoãn):
- Chia sẻ wishlist, thông báo giảm giá cho sản phẩm yêu thích.

## Thực thể dữ liệu

- `wishlists`: thuộc người dùng.
- `wishlist_items`: sản phẩm.

## Yêu cầu chức năng sơ bộ

- Không trùng sản phẩm trong một wishlist.
- Chỉ chủ sở hữu xem/sửa.
- Sản phẩm đã ngừng bán vẫn có thể còn trong danh sách nhưng đánh dấu không mua được.

## Tiêu chí hoàn thành

- Thêm/xóa/xem có test + test chống trùng.

## Tham chiếu

- `docs/product/project_overview.md` (Shop: Wishlist)
- `docs/product/backend-spec.md` (Database: wishlists, wishlist_items)
