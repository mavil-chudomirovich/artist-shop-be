# Migration

Schema được quản lý bằng **goose**, tập trung trong một thư mục duy nhất. Migration
được nhúng vào binary (`go:embed`), nên khi chạy trong container không cần volume
mount.

Xem: [../architecture.md](../architecture.md) và
[../docker.md](../docker.md#7-vòng-đời-migration-trong-container).

## 1. Đặt tên file

```text
migrations/NNNNN_<module>_<mô_tả-kebab>.sql
```

| Phần | Quy tắc |
|---|---|
| `NNNNN` | 5 chữ số, tăng dần, không dùng lại số đã xoá |
| `<module>` | tên module sở hữu thay đổi: `auth`, `user`, `order` |
| `<mô tả>` | kebab-case, nêu **việc** làm không phải loại lệnh: `add_user_profile_addresses` |

```text
00001_create_audit_logs.sql
00002_audit_append_only.sql
00003_auth.sql
00004_user.sql          ← ví dụ cho module 02
```

## 2. Cấu trúc file

Mỗi file **bắt buộc** có cả hai khối:

```sql
-- +goose Up
CREATE TABLE addresses (
    id          uuid PRIMARY KEY,
    user_id     uuid NOT NULL REFERENCES users(id),
    -- …
    deleted_at  timestamptz
);

CREATE UNIQUE INDEX addresses_one_default_per_user
    ON addresses (user_id) WHERE is_default AND deleted_at IS NULL;

-- +goose Down
DROP INDEX addresses_one_default_per_user;
DROP TABLE addresses;
```

Runner từ chối file thiếu `Down` (xem `migrations/README.md`). `Down` phải hoàn tác
**đúng** những gì `Up` đã làm, theo thứ tự ngược lại.

## 3. Quy tắc nội dung

| Quy tắc | Lý do |
|---|---|
| **Thêm trước, xoá sau** | Đổi cột không phá vỡ phiên bản đang chạy |
| Không sửa file migration đã merge | Migration đã áp dụng ở môi trường nào đó sẽ không khớp nữa |
| Cột mới **nullable** hoặc có `DEFAULT` | Bảng cũ vẫn dùng được |
| Không dùng `DROP COLUMN` trong cùng release thêm cột | Tách làm 2 release: thêm → sử dụng → mới xoá |
| Kiểu tiền: `integer` + cột `currency` | Không dùng float/double cho tiền |
| Khối: `timestamptz`, múi giờ UTC | Nhất quán với `timestamptz` của bảng hiện có |
| Khóa chính: `uuid` | Không dùng id tăng dần đoán được |
| Ràng buộc quan trọng đặt ở **DB** | Check constraint và unique index không thể bị code khác lách |
| Index cho mọi cột được lọc thường xuyên | Đặc biệt cột lọc mặc định (soft delete) |
| `COMMENT ON COLUMN` cho cột khó hiểu | Người đọc schema không cần mở code |

### Ràng buộc phải ở tầng lưu trữ

Bất biến dữ liệu quan trọng **không** được chỉ dựa vào tầng ứng dụng:

```sql
-- Đúng: DB chặn, nên kể cả ghi bằng psql cũng không tạo được 2 địa chỉ mặc định
CREATE UNIQUE INDEX addresses_one_default_per_user
    ON addresses (user_id) WHERE is_default AND deleted_at IS NULL;
```

Tầng ứng dụng vẫn phải kiểm tra để trả lỗi thân thiện; DB là lưới an toàn cuối.

## 4. Có cần migration không?

| Thay đổi | Cần migration |
|---|---|
| Thêm bảng / cột / index | ✅ |
| Thêm ràng buộc | ✅ |
| Đổi tên cột | ✅ (thêm cột mới → chuyển dữ liệu → xoá cột cũ) |
| Đổi kiểu cột | ✅ (nhiều bước, không làm trực tiếp) |
| Chỉ đổi code Go | ❌ |
| Chỉ đổi tài liệu | ❌ |

## 5. Chạy migration

```bash
make migrate-up          # trên máy, dùng DATABASE_URL trong .env
make migrate-status      # xem đã áp dụng gì
make migrate-down        # hoàn tác 1 bước
make migrate-up-docker   # trong container
```

Khi API khởi động, `MIGRATIONS_AUTO_APPLY=true` (mặc định) khiến nó tự áp dụng,
được bảo vệ bằng **PostgreSQL advisory lock** để nhiều replica khởi động cùng lúc vẫn
chỉ áp dụng một lần.

Với production, nên đặt `MIGRATIONS_AUTO_APPLY=false` và chạy `make migrate-up` như
một job trước khi deploy app.

## 6. Kiểm thử migration

```bash
make test-integration
```

Test repository chạy trên PostgreSQL thật qua testcontainers, nên nó **tự động** kiểm
tra migration áp được. Bổ sung một test khẳng định trực tiếp ràng buộc khi ràng buộc
đó quan trọng — ví dụ thử ghi hai dòng `is_default = true` và kỳ vọng lỗi unique:

```go
// address_integration_test.go
func TestOneDefaultPerUserEnforcedByDatabase(t *testing.T) {
    dsn := testsupport.PostgresDSN(t)
    // … tạo 2 địa chỉ, đánh dấu default cả hai
    // kỳ vọng: lỗi unique constraint
}
```

## 7. Migration và nhiều môi trường

- Migration đã áp dụng ở bất kỳ đâu thì **không được sửa nữa**.
- Đổi định nghĩa cột: thêm migration mới (`ALTER … COMMENT`, `ALTER TYPE`).
- Cần dữ liệu mẫu cho dev? Đặt ở `cmd/seed` (gọi use case) hoặc script SQL riêng
  trong `migrations/` — không nhét DDL và lẫn vào migration thật.

## 8. Checklist trước khi commit migration

- [ ] Đặt tên đúng mẫu `NNNNN_<module>_<mô_tả>.sql`
- [ ] Có cả `-- +goose Up` và `-- +goose Down`
- [ ] `Down` hoàn tác đúng những gì `Up` làm
- [ ] Cột mới nullable hoặc có `DEFAULT`
- [ ] Index cho mọi cột dùng để lọc
- [ ] Bất biến quan trọng có ràng buộc ở DB, không chỉ ở Go
- [ ] `make test-integration` xanh với migration mới
- [ ] Nếu ràng buộc quan trọng: có test khẳng định trực tiếp nó