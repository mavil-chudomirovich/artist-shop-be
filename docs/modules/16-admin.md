# Module 16 — Admin

- **Trạng thái Spec Kit**: Chưa specify
- **Spec**: _(chưa có)_
- **Ưu tiên / Giai đoạn**: Giai đoạn 1 (lõi), mở rộng dần theo module
- **Phụ thuộc**: tất cả module nghiệp vụ

## Mục đích

Không gian làm việc cho artist (admin): quản lý sản phẩm, kho, đơn, thanh toán,
khách hàng, commission, đánh giá, nội dung và thông báo; đồng thời cung cấp bảng
điều khiển tổng quan.

## Phạm vi MVP

Có:
- Uỷ quyền admin cho endpoint quản trị của từng module (không sở hữu dữ liệu riêng).
- Bảng điều khiển tổng quan (đơn mới, commission đang xử lý, doanh thu cơ bản).
- Xem audit log thao tác quản trị.

Không (hoãn):
- Phân quyền admin chi tiết theo vai trò con (vì chỉ một artist).
- Báo cáo/phân tích nâng cao (Phase 2).

## Thực thể dữ liệu

- Không có bảng riêng (đọc/ghi qua các module khác; dùng `audit_logs`).

## Luồng nghiệp vụ chính

1. Admin đăng nhập → vào bảng điều khiển.
2. Admin thao tác trên từng khu vực (sản phẩm/đơn/commission…).
3. Mọi thao tác ghi audit.

## Yêu cầu chức năng sơ bộ

- Mọi endpoint admin MUST kiểm tra vai trò ADMIN.
- Thao tác quản trị MUST ghi `audit_logs` (actor, action, target, thời gian).
- Không lộ dữ liệu khách không cần thiết trên bảng điều khiển.

## Tiêu chí hoàn thành

- Endpoint tổng quan + danh sách audit có test.
- Test 100% endpoint admin bị từ chối với phiên customer.

## Ghi chú / câu hỏi mở

- Phạm vi bảng điều khiển MVP gồm chỉ số nào?

## Tham chiếu

- `docs/product/project_overview.md` (Admin Workspace)
- `docs/product/backend-spec.md` (Architecture)
