# Module 16 — Admin

- **Trạng thái Spec Kit**: Đã implement endpoint tổng quan (feature `017-admin-dashboard`); phần
  **xem audit log** của module chưa có spec riêng
- **Spec**: [`specs/017-admin-dashboard/spec.md`](../../specs/017-admin-dashboard/spec.md)
- **Plan**: [`specs/017-admin-dashboard/plan.md`](../../specs/017-admin-dashboard/plan.md)
- **Tasks**: [`specs/017-admin-dashboard/tasks.md`](../../specs/017-admin-dashboard/tasks.md)
- **Contract**: [`specs/017-admin-dashboard/contracts/openapi.yaml`](../../specs/017-admin-dashboard/contracts/openapi.yaml),
  [`error-codes.md`](../../specs/017-admin-dashboard/contracts/error-codes.md)
- **Frontend guide**: [`specs/017-admin-dashboard/frontend-guide.md`](../../specs/017-admin-dashboard/frontend-guide.md)
- **Deferred**: [`specs/017-admin-dashboard/deferred.md`](../../specs/017-admin-dashboard/deferred.md)
- **Code**: `internal/modules/admin/{domain,application,infrastructure,presentation}`,
  `internal/contracts/dashboard.go`, `migrations/00017_order_item_category_snapshot.sql`
- **Ưu tiên / Giai đoạn**: V1.2 (lõi), mở rộng dần theo module
- **Phụ thuộc**: tất cả module nghiệp vụ

## Mục đích

Không gian làm việc cho artist (admin): quản lý sản phẩm, kho, đơn, thanh toán, khách hàng,
commission, đánh giá, nội dung và thông báo; đồng thời cung cấp bảng điều khiển tổng quan.

Admin là **orchestrator read-only**: module **không sở hữu bảng riêng** — mọi số liệu đi qua các
**hợp đồng đọc** do từng module sở hữu công bố (`internal/contracts/dashboard.go`), adapter do module
sở hữu cấp ở composition root (`cmd/api/main.go`). Đây là điều giữ Constitution I: dashboard không đọc
thẳng storage của capability khác.

## Phạm vi MVP

Có:
- Uỷ quyền admin cho endpoint quản trị của từng module (không sở hữu dữ liệu riêng).
- **Bảng điều khiển tổng quan** (`GET /api/v1/admin/dashboard/overview`): bốn nhóm KPI
  (doanh thu/đơn hàng/khách hàng/tồn kho), chuỗi doanh thu theo ngày (7 hoặc 30 ngày), cơ cấu doanh số
  theo danh mục (snapshot tại thời điểm bán) và 5 đơn mới nhất, tất cả trong **một** request read-only.
- Audit một lần mỗi lần xem dashboard (`ADMIN_DASHBOARD_VIEWED`), không chứa PII.

Không (hoãn — xem [`deferred.md`](../../specs/017-admin-dashboard/deferred.md)):
- **Xem audit log** thao tác quản trị.
- Phân quyền admin chi tiết theo vai trò con (vì chỉ một artist).
- Báo cáo/phân tích nâng cao, drill-down, bộ lọc/date-range tùy ý, export (Phase 2).
- Thu nhận doanh thu theo **thời điểm thanh toán** (`paid_at`) thay vì `created_at`.

## Endpoint đã giao (feature 017)

`GET /admin/dashboard/overview` — vai trò `ADMIN`; query `days` (mặc định 7, ∈ {7,30}) và
`lowStockThreshold` (mặc định 5, 0..10000). Chi tiết đầy đủ ở
[api-reference.md](../api-reference.md) mục 11 và
[`contracts/openapi.yaml`](../../specs/017-admin-dashboard/contracts/openapi.yaml).

## Thực thể dữ liệu

- **Không có bảng riêng** cho admin (đọc/ghi qua các module khác; dùng `audit_logs`).
- **Một thay đổi schema** thuộc feature 017 nhưng nằm ở **module 07 Order**: snapshot danh mục
  (`category_id`, `category_name`) trên mỗi dòng `order_items`, thêm bởi migration
  `00017_order_item_category_snapshot.sql`. Đây là dữ liệu **do order sở hữu**; dashboard chỉ đọc qua
  hợp đồng `OrderStatistics`. Không FK tới `categories` (dòng đơn phải sống lâu hơn danh mục), hai cột
  cùng null hoặc cùng có giá trị; dòng không resolve được thành bucket `"Khác"`.

## Luồng nghiệp vụ chính

1. Admin đăng nhập → mở bảng điều khiển.
2. Một request `GET /admin/dashboard/overview` → application đọc clock **một lần** rồi gọi **8 read
   bounded** qua bốn hợp đồng đọc (order KPI/trend/category/recent, customer KPI/summary, inventory
   low-stock, product discontinued-id), dựng bốn nhóm KPI + trend zero-fill + category breakdown + 5
   recent orders, ghi **một** audit event, trả response.
3. Admin thao tác trên từng khu vực (sản phẩm/đơn/commission…) qua endpoint chuyên biệt của từng
   module.
4. Mọi thao tác **ghi** vẫn ghi audit ở module sở hữu; dashboard **không** có endpoint ghi.

## Yêu cầu chức năng sơ bộ

- Mọi endpoint admin MUST kiểm tra vai trò `ADMIN` (guest 401, CUSTOMER 403; ghi `AUTH_PRIVILEGE_DENIED`).
- Dashboard MUST read-only: không thay đổi order/inventory/customer.
- Mọi giá trị tiền MUST là `{ amount, currency }` (integer minor unit + currency, không float).
- Dashboard MUST dùng số lượng **read bounded** (không tăng theo số đơn/variant/khách) và hoàn tất
  **dưới 2 giây** với 10.000 order + 1.000 variant.
- Thao tác quản trị MUST ghi `audit_logs`; dashboard ghi đúng **một** event `ADMIN_DASHBOARD_VIEWED`,
  **không** chứa PII và **không** audit từng khách hàng được đọc.
- Không lộ dữ liệu khách không cần thiết trên bảng điều khiển (chỉ `id`/`displayName`/`email` của
  recent order, và `customer = null` khi tài khoản đã bị xoá).
- Snapshot danh mục trên dòng đơn MUST được ghi tại checkout/edit để số lịch sử không đổi khi danh mục
  hoặc sản phẩm thay đổi.

## Tiêu chí hoàn thành

- [x] Endpoint tổng quan trả đủ 4 nhóm KPI + trend + cơ cấu danh mục + recent orders trong một response,
  có test (unit + integration trên PostgreSQL thật).
- [x] Test 100% endpoint admin bị từ chối với phiên customer (401 cho guest, 403 cho CUSTOMER).
- [x] Mọi field tiền là money object; trend có đúng `days` phần tử; tổng category khớp doanh thu tháng;
  low stock tôn trọng hold và loại `DISCONTINUED`.
- [x] Tài liệu API (`docs/api-reference.md` mục 11), contract và `docs/swagger/` khớp source.
- [ ] **SC-006**: dashboard dưới 2 giây với 10.000 order/1.000 variant — test hiệu năng có ở
  `internal/modules/admin/presentation/http/overview_integration_test.go` (chạy bằng `make test-integration`
  khi có Docker).
- [ ] **Xem audit log**: chưa specify.

## Ghi chú / câu hỏi mở

- Câu hỏi mở cũ *"Phạm vi bảng điều khiển MVP gồm chỉ số nào?"* **đã chốt** ở feature 017: 4 nhóm KPI,
  trend 7/30 ngày, cơ cấu doanh số theo danh mục (snapshot) và 5 đơn mới nhất.
- Quyết định dài hạn ở [decisions/022](../decisions/022-admin-dashboard-read-model.md) (admin read-only
  trên hợp đồng đọc công bố, snapshot danh mục trên dòng đơn, timezone cửa hàng + so sánh MTD).
- Phần còn lại của module (xem audit log) chưa có feature.

## Tham chiếu

- [`specs/017-admin-dashboard/deferred.md`](../../specs/017-admin-dashboard/deferred.md) — các điểm cố ý hoãn
- [`specs/017-admin-dashboard/frontend-guide.md`](../../specs/017-admin-dashboard/frontend-guide.md) — bàn giao frontend
- [decisions/022](../decisions/022-admin-dashboard-read-model.md) — quyết định của feature 017
- `docs/product/project_overview.md` (Admin Workspace)
- `docs/product/backend-spec.md` (Architecture)
