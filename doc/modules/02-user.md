# Module 02 — User

- **Trạng thái Spec Kit**: Chưa specify
- **Spec**: _(chưa có)_
- **Ưu tiên / Giai đoạn**: Giai đoạn 0
- **Phụ thuộc**: auth

## Mục đích

Quản lý hồ sơ khách hàng và địa chỉ giao hàng, làm dữ liệu nền cho đơn hàng và
commission.

## Phạm vi MVP

Có:
- Xem và cập nhật hồ sơ (tên, số điện thoại, avatar).
- Quản lý nhiều địa chỉ giao hàng; đặt một địa chỉ mặc định.
- Upload avatar qua Cloudinary.

Không (hoãn):
- Điểm thưởng, hạng thành viên, giới thiệu (referral).
- Sổ địa chỉ dùng chung / địa chỉ công ty nâng cao.

## Thực thể dữ liệu

- `users` (phần hồ sơ): tên hiển thị, số điện thoại, avatar (public_id/secure_url).
- `addresses`: người nhận, số điện thoại, tỉnh/thành, quận/huyện, địa chỉ chi tiết, cờ mặc định.

## Luồng nghiệp vụ chính

1. Người dùng xem/cập nhật hồ sơ.
2. Người dùng thêm/sửa/xóa địa chỉ; đặt mặc định.
3. Admin xem thông tin khách hàng khi xử lý đơn/commission.

## Yêu cầu chức năng sơ bộ

- Chỉ chủ tài khoản được xem/sửa hồ sơ và địa chỉ của mình.
- Luôn có tối đa một địa chỉ mặc định mỗi tài khoản.
- Xóa địa chỉ đang được đơn hàng tham chiếu phải xử lý an toàn (không phá dữ liệu cũ).
- Avatar phải được kiểm tra loại/kích thước trước khi lưu.

## Tiêu chí hoàn thành

- CRUD hồ sơ + địa chỉ có kiểm tra quyền sở hữu.
- Đảm bảo ràng buộc địa chỉ mặc định có test.
- Avatar upload hoạt động và metadata được lưu.

## Ghi chú / câu hỏi mở

- Địa chỉ có cần đơn vị hành chính chuẩn (tỉnh/quận/phường) không?

## Tham chiếu

- `doc/backend-spec.md` (Database: users, addresses; Cloudinary)
