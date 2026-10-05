# Module 09 — Shipping

- **Trạng thái Spec Kit**: Chưa specify
- **Spec**: _(chưa có)_
- **Ưu tiên / Giai đoạn**: Giai đoạn 1
- **Phụ thuộc**: order

## Mục đích

Tính phí vận chuyển và theo dõi thông tin giao hàng cho đơn sản phẩm vật lý.

## Phạm vi MVP

Có:
- Phí vận chuyển theo khu vực (mức đơn giản).
- Thông tin giao hàng và trạng thái vận chuyển.
- Admin cập nhật mã vận đơn/trạng thái giao.

Không (hoãn):
- Tích hợp API hãng vận chuyển tự động (GHTK/GHN…).
- Vận chuyển quốc tế (Phase 2).
- Tính phí theo cân nặng/kích thước phức tạp.

## Thực thể dữ liệu

- `shipments`: đơn, đơn vị vận chuyển, mã vận đơn, phí, trạng thái, thời gian.

## Luồng nghiệp vụ chính

1. Checkout → xác định phí ship theo khu vực.
2. Admin giao hàng → ghi mã vận đơn, cập nhật trạng thái.
3. Khách xem trạng thái giao hàng.

## Yêu cầu chức năng sơ bộ

- Phí ship MUST được chốt tại thời điểm tạo đơn.
- Một đơn có thể có một hoặc nhiều bản ghi shipment.
- Trạng thái vận chuyển là state machine đơn giản.

## Tiêu chí hoàn thành

- Tính phí + cập nhật trạng thái vận chuyển có test.
- Phí ship hiển thị đúng trong tổng tiền đơn.

## Ghi chú / câu hỏi mở

- MVP có giao hàng nội địa Việt Nam với phí cố định/phân vùng?

## Tham chiếu

- `doc/project_overview.md` (Future roadmap)
- `doc/backend-spec.md` (Database: shipments)
