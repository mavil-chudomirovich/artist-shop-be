# Module 05 — Inventory

- **Trạng thái Spec Kit**: Hoàn tất phần giao được (implement + converge; nửa tự động do đơn hàng
  điều khiển **chưa** giao vì module sinh ra nó chưa tồn tại — nêu rõ ở phần "Phần chưa giao")
- **Spec**: [`specs/007-inventory-tracking/spec.md`](../../specs/007-inventory-tracking/spec.md)
- **Plan**: [`specs/007-inventory-tracking/plan.md`](../../specs/007-inventory-tracking/plan.md)
- **Tasks**: [`specs/007-inventory-tracking/tasks.md`](../../specs/007-inventory-tracking/tasks.md)
- **Contract**: [`specs/007-inventory-tracking/contracts/openapi.yaml`](../../specs/007-inventory-tracking/contracts/openapi.yaml),
  [`error-codes.md`](../../specs/007-inventory-tracking/contracts/error-codes.md)
- **Code**: `internal/modules/inventory/{domain,application,infrastructure,presentation}`,
  `migrations/00007_inventory.sql`, `internal/contracts/product.go` (hai hợp đồng liên module)
- **Ưu tiên / Giai đoạn**: Giai đoạn 1
- **Phụ thuộc**: product

## Mục đích

Theo dõi tồn kho và lịch sử biến động kho một cách chính xác, an toàn giao dịch.

## Phạm vi MVP

Có:
- Tồn kho theo sản phẩm.
- Tự động: đơn đã thanh toán → giảm kho; đơn hủy → hoàn kho.
- Thủ công: nhập thêm (restock), hư hỏng (damage), điều chỉnh (adjustment).
- Mọi thay đổi tạo một bản ghi `inventory_transactions`.

Không (hoãn):
- Quản lý kho nhiều địa điểm.
- Dự báo tồn kho, cảnh báo tự động nâng cao.

## Thực thể dữ liệu

- `stock_levels`: số vật lý trên kệ, mỗi sản phẩm một dòng, là **chốt chặn đồng thời** mà luật
  "không âm" được thực thi trên đó (`UPDATE ... WHERE quantity >= $n`).
- `inventory_transactions`: sản phẩm, loại biến động, số lượng thay đổi, số lượng kết quả, tham
  chiếu nguồn (đơn/điều chỉnh), ghi chú, người thao tác, thời gian. Append-only.
- `stock_holds`: phần đang được giữ cho một đơn đang thanh toán, có hạn 15 phút. **Không** phải
  biến động vật lý nên không nằm trong ledger.

Khả dụng = `stock_levels.quantity` − tổng `stock_holds` đang hoạt động; được **tính**, không lưu.

## Luồng nghiệp vụ chính

1. Thanh toán thành công → tiêu thụ giữ chỗ → giảm kho + ghi transaction (`SALE`).
2. Hủy đơn **chưa** thanh toán → nhả giữ chỗ, **không** đổi kho vật lý.
3. Admin restock/damage/adjustment → ghi transaction.
4. Khả dụng cắt qua 0 → module 04 tự chuyển `sellState` (`ACTIVE ↔ OUT_OF_STOCK`) trong cùng
   transaction.

## Yêu cầu chức năng sơ bộ

- Không cho tồn kho âm (chặn oversell).
- Cập nhật kho MUST nằm trong transaction cùng sự kiện nguồn.
- Mọi thay đổi MUST bất biến (append-only) và truy vết được.
- Chống trùng khi xử lý lại cùng sự kiện thanh toán (idempotent).

## Tiêu chí hoàn thành

- [x] Giảm/hoàn kho có test, kể cả chống oversell và xử lý trùng — luật không âm nằm ở **tầng lưu
  trữ** (`CHECK quantity >= 0` + conditional update), chống trùng bằng **partial unique index** trên
  `source_reference`; cả hai được chứng minh trên PostgreSQL thật (tag `integration`).
- [x] Lịch sử biến động đầy đủ, không mất bản ghi — `inventory_transactions` append-only, thứ tự
  `created_at, id`, phân trang; `SUM(delta)` khớp `stock_levels.quantity` (SC-001).
- [x] Tuân thủ constitution về tính toàn vẹn giao dịch kho/tiền — mọi thay đổi vật lý + dòng ledger
  + hệ quả khả dụng nằm trong một transaction (FR-011); tiền không tham gia module này.
- [x] Khả dụng là dữ kiện **dẫn xuất** (vật lý − giữ chỗ hoạt động), không lưu sẵn.
- [x] Nghĩa vụ kế thừa từ feature 006 (D1): sản phẩm tự chuyển trạng thái bán khi khả dụng cắt qua
  0 — xem [decisions/014](../decisions/014-inventory-hold-and-availability.md).

Năm endpoint quản trị (`GET`/`POST /api/v1/admin/inventory/{productId}...`) nằm ở
[api-reference.md](../api-reference.md) mục 7.

### Phần chưa giao

Đây là phần module doc mô tả nhưng feature này **không giao được end to end**, **không** phải phần
đã xong. Ghi rõ ở đây để một luật chưa xong không trở nên vô hình khi module được đánh dấu hoàn tất:
[`specs/007-inventory-tracking/deferred.md`](../../specs/007-inventory-tracking/deferred.md).

| Điểm chưa giao | Vì sao | Gỡ ở đâu |
|---|---|---|
| **Nửa tự động do đơn hàng điều khiển**: đơn đã thanh toán → giảm kho; đơn hủy → hoàn kho (nhả giữ chỗ) | Order (07) và Payment (08) chưa tồn tại — không có gì trong hệ thống có thể bắt đầu checkout, thanh toán hay hủy đơn. **Năng lực** (reserve/release/expire/consume + idempotent theo source reference) đã được giao và test trực tiếp, nhưng **nguồn sự kiện** chưa có | **Module 07 Order / 08 Payment** wire và test end to end; hợp đồng liên module để chúng gọi sẽ được thêm khi Order được specify. `deferred.md` D1 |
| **Hợp đồng `InventoryReservation` liên module** | Chưa có consumer. Viết bây giờ là thiết kế theo yêu cầu đoán mò (Constitution VII) | Khi module 07 Order được specify. `deferred.md` D1 |
| **Xóa mềm/history sống ngoài sản phẩm** | Product xóa cứng; `stock_levels`/`inventory_transactions`/`stock_holds` cascade theo sản phẩm. Lịch sử không sống ngoài vòng đời sản phẩm | Cùng module 07, khi quyết định xóa sản phẩm. `deferred.md` D3 |
| **Ngưỡng cảnh báo tồn kho thấp, kho nhiều địa điểm** | Module doc tự hoãn "cảnh báo tự động nâng cao" và "kho nhiều địa điểm"; spec trả lời câu hỏi mở về ngưỡng: **không** ở MVP (FR-029) | Feature riêng khi có nhu cầu. `deferred.md` D4 |

## Ghi chú / câu hỏi mở

Câu hỏi mở duy nhất của module — "Ngưỡng cảnh báo tồn kho thấp có cần ở MVP không?" — đã được
feature 007 trả lời: **không**. Nó ở lại cùng các cảnh báo nâng cao đã hoãn
(`specs/007-inventory-tracking/spec.md`, mục *Scope boundary* và FR-029).

Câu hỏi về luồng "đơn hủy → hoàn kho" cũng đã được chốt lúc specify: hệ thống **không** có luồng huỷ
đơn **đã thanh toán**; việc hoàn kho vật lý cho đơn đã trả tiền không tồn tại. Một đơn đã thanh toán
có thể được **chuyển nhượng** cho tài khoản khác, và chuyển nhượng **không** đổi tồn kho. Chỉ đơn
**chưa** thanh toán mới "hoàn kho", và đó là **nhả giữ chỗ**, không đổi số vật lý.

Hợp đồng HTTP của module nằm ở [api-reference.md](../api-reference.md) mục 7 — danh sách endpoint và
mã lỗi chỉ có ở đó, file này không nhân bản.

## Tham chiếu

- `docs/product/backend-spec.md` (Inventory)
- [`specs/007-inventory-tracking/deferred.md`](../../specs/007-inventory-tracking/deferred.md) — các
  điểm cố ý hoãn
- [decisions/014](../decisions/014-inventory-hold-and-availability.md) — hai hợp đồng liên module,
  mô hình ba bảng, và sweeper nhả giữ chỗ
