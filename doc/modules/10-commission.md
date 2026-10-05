# Module 10 — Commission

- **Trạng thái Spec Kit**: Chưa specify
- **Spec**: _(chưa có)_
- **Ưu tiên / Giai đoạn**: Giai đoạn 2
- **Phụ thuộc**: user, payment

## Mục đích

Quản lý quy trình đặt vẽ tranh theo yêu cầu: từ yêu cầu, báo giá, đặt cọc, giữ
slot, thanh toán theo giai đoạn, đến bàn giao.

## Phạm vi MVP

Có:
- Gửi yêu cầu commission + upload ảnh tham chiếu.
- Báo giá (quote) và chấp nhận/từ chối.
- Đặt cọc và thanh toán theo giai đoạn (cọc, 50%, thanh toán cuối).
- Giữ slot artist; quản lý sức chứa slot.
- Preview → revision → duyệt.
- Tệp đính kèm (tham chiếu, preview, ảnh cuối) qua Cloudinary.

Không (hoãn):
- Đa nghệ sĩ/marketplace (Phase 3).
- Đấu giá, gói dịch vụ phức tạp.

## Thực thể dữ liệu

- `commissions`: khách, mô tả, trạng thái, các mốc thanh toán.
- `commission_quotes`: giá báo, điều kiện, hạn.
- `commission_slots`: sức chứa/đặt chỗ theo giai đoạn.
- `commission_revisions`: yêu cầu chỉnh sửa và phản hồi.
- `commission_files`: tệp Cloudinary theo vai trò (reference/preview/final).

## Luồng nghiệp vụ chính

Yêu cầu → Xem xét → Báo giá → Đặt cọc → Giữ slot → Thanh toán 50% → Đang vẽ →
Preview → Revision → Duyệt → Thanh toán cuối → Hoàn tất → Bàn giao.

## Yêu cầu chức năng sơ bộ

- State machine commission MUST là một nguồn chân lý duy nhất, có test.
- Không bắt đầu vẽ trước khi đặt cọc.
- Giới hạn số revision theo thoả thuận; vượt giới hạn cần phê duyệt.
- Giữ slot MUST chống trùng/oversell.
- Thanh toán giai đoạn dùng chung module payment, idempotent.

## Tiêu chí hoàn thành

- Toàn bộ chuyển trạng thái có test (dương + âm).
- Luồng thanh toán giai đoạn + giữ slot có test giao dịch.
- Quyền: khách chỉ truy cập commission của mình.

## Ghi chú / câu hỏi mở

- **Cần chốt state machine chuẩn**: `project_overview.md` và `backend-spec.md`
  đang lệch nhau (thiếu DELIVERED, APPROVED/PAID_FULL). Đây là việc phải làm
  TRƯỚC khi specify module này.
- Số revision mặc định? Slot định nghĩa theo tháng hay hàng đợi?

## Tham chiếu

- `doc/project_overview.md` (Commission)
- `doc/backend-spec.md` (Commission System)
