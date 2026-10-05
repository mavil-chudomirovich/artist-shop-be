# Module 02 — User

- **Trạng thái Spec Kit**: Hoàn tất (implement + converge; tất cả task đã đóng)
- **Spec**: [`specs/003-user-profile/spec.md`](../../specs/003-user-profile/spec.md) —
  đặc tả hồ sơ khách hàng & địa chỉ giao hàng
- **Plan**: [`specs/003-user-profile/plan.md`](../../specs/003-user-profile/plan.md)
- **Tasks**: [`specs/003-user-profile/tasks.md`](../../specs/003-user-profile/tasks.md)
- **Contract**: [`specs/003-user-profile/contracts/openapi.yaml`](../../specs/003-user-profile/contracts/openapi.yaml),
  [`error-codes.md`](../../specs/003-user-profile/contracts/error-codes.md)
- **Code**: `internal/modules/user/{domain,application,infrastructure,presentation}`,
  `internal/share/administrative`, `internal/contracts/user.go`,
  `migrations/00004_user.sql`
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
- `addresses`: người nhận, số điện thoại, tỉnh/thành, phường/xã, địa chỉ chi
  tiết, cờ mặc định. Tỉnh và phường/xã lấy từ kho dữ liệu hành chính Việt Nam
  (2 cấp, không dùng cấp quận/huyện), người dùng chọn bằng select cascading.
- Dữ liệu tham chiếu tỉnh/phường: chỉ đọc, đóng gói kèm bản phát hành.

### Phân chia sở hữu bảng `users`

Bảng `users` được **hai module cùng sở hữu theo cột**, không phải theo bảng. Ranh giới
được ghi bằng `COMMENT ON COLUMN` ngay trong `migrations/00004_user.sql`, để đọc
schema là đủ biết ai được viết cột nào.

| Cột | Module sở hữu | Ghi chú |
|---|---|---|
| `id`, `email`, `password_hash`, `role`, `status`, `created_at`, `updated_at` | **01 auth** | Tạo ở `migrations/00003_auth.sql`; email, mật khẩu đã băm, vai trò, trạng thái tài khoản và hai cột thời gian |
| `display_name`, `phone`, `avatar_public_id`, `avatar_secure_url`, `avatar_width`, `avatar_height` | **02 user** | Sáu cột hồ sơ, thêm ở `migrations/00004_user.sql` |

Module 02 là **cái duy nhất ghi** sáu cột hồ sơ đó, và module 01 không đọc hay ghi chúng.
Ngược lại, module 01 là nơi duy nhất quyết định `email`, `password_hash`, `role` và
`status`; module 02 chỉ **đọc** `id`, `email` và `role` — để dựng câu trả lời và điền
`actor_role` của dòng audit. `GET /users/{userId}` vì vậy trả cả `id`, `email` và `role`
(dữ liệu của module 01) nhưng **không** trả `password_hash` hay `status`.

Bốn cột avatar có ràng buộc `users_avatar_columns_all_or_none_ck` ở tầng lưu trữ:
hoặc cả bốn cùng `NULL` (chưa có ảnh), hoặc cả bốn cùng có giá trị. Một tham chiếu
avatar nửa vời sẽ cho ra hồ sơ client không dựng được, nên nó bị chặn lúc ghi chứ
không chỉ ở tầng ứng dụng.

### Chính sách làm mới dataset hành chính

Dataset tỉnh/phường là **dữ liệu tham chiếu nhúng trong binary**, không phải cấu
hình và không nằm trong PostgreSQL (ADR-002). Chính sách:

1. **Sinh lại toàn bộ từ nguồn đã ghi, không bao giờ sửa tay.** File
   `internal/share/administrative/data/vn-divisions.json` là ảnh chụp của nguồn đã
   ghi trong khối `_provenance` (`open-admin-data/vietnam-administrative-divisions`,
   CC-BY-4.0). Sửa tay một dòng là mất khả năng đối chiếu với nguồn và làm mất ý nghĩa
   của bước kiểm thử số lượng.
2. **Mỗi lần làm mới đi kèm một bản phát hành.** Dataset nằm trong binary, nên đổi
   dataset mà không phát hành lại build là một thay đổi không có tác dụng.
3. **Sai số lượng thì không merge.** `make test` kiểm tra số tỉnh/phường khớp khối
   `counts` của chính file, mã không trùng, mỗi phường thuộc đúng một tỉnh, và file là
   UTF-8 không BOM với xuống dòng LF.
4. **Địa chỉ đã lưu không cần migrate.** Mỗi địa chỉ giữ **mã** tỉnh/phường cùng **tên
   đã chụp** lúc lưu, nên vẫn hiển thị đúng sau khi tên đơn vị hành chính thay đổi.

Các bước thao tác chi tiết nằm ở [configuration.md](../configuration.md) →
*Quy trình làm mới dataset*.

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

- [x] CRUD hồ sơ + địa chỉ có kiểm tra quyền sở hữu.
- [x] Đảm bảo ràng buộc địa chỉ mặc định có test — ở tầng repository *và* bằng partial
  unique index trên `addresses (user_id) WHERE is_default AND deleted_at IS NULL`
  (ADR-003), nên tầng ứng dụng quên kiểm tra vẫn không vi phạm được.
- [x] Avatar upload hoạt động và metadata được lưu.

Hợp đồng HTTP của module nằm ở
[api-reference.md](../api-reference.md) mục 4 — danh sách endpoint và mã lỗi chỉ có
ở đó, file này không nhân bản.

## Ghi chú / câu hỏi mở

- ~~Địa chỉ có cần đơn vị hành chính chuẩn (tỉnh/quận/phường) không?~~ → Đã
  chốt 2026-10-05: **2 cấp tỉnh → phường/xã**, lấy từ kho dữ liệu hành chính Việt
  Nam, chọn bằng select cascading. Xem `specs/003-user-profile/spec.md`.
- ~~Cần chốt có dùng cấp huyện không.~~ → Đã chốt: cấp huyện đã bị bãi bỏ toàn quốc nên
  dataset chỉ còn 2 cấp; xem [configuration.md](../configuration.md).
- Một tài khoản có nhiều địa chỉ; người dùng chọn một địa chỉ khi đặt hàng, địa
  chỉ mặc định được chọn sẵn nhưng không áp ép.

## Tham chiếu

- `docs/product/backend-spec.md` (Database: users, addresses; Cloudinary)
- [api-reference.md](../api-reference.md) — endpoint và error code của module
- [decisions/002](../decisions/002-administrative-dataset-embedded.md) — nhúng dataset hành chính
- [decisions/003](../decisions/003-single-default-address-index.md) — một địa chỉ mặc định
- [decisions/004](../decisions/004-soft-delete-addresses.md) — soft delete địa chỉ
- [decisions/005](../decisions/005-media-resize-on-provider.md) — resize ảnh ở provider
