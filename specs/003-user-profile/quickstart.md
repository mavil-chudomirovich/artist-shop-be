# Quickstart: Validate Customer Profile & Shipping Addresses

Các kịch bản chạy được để chứng minh module hoạt động đúng. Mọi chi tiết về hợp
đồng nằm ở `contracts/openapi.yaml`, còn ý nghĩa trường và ràng buộc ở
`data-model.md`.

## 1. Chuẩn bị

| Yêu cầu | Ghi chú |
|---|---|
| Docker + Compose v2 | `make up-tools` chạy db, redis, api, mailpit |
| `make` | Xem `docs/makefile.md` |
| `.env` | `make env` tạo từ `.env.example` |

```bash
make env
make up-tools          # http://localhost:8080, mailpit 8025, db 5432
```

Migration `00004_user.sql` được API tự áp dụng lúc khởi động
(`MIGRATIONS_AUTO_APPLY=true`), nên không cần chạy lệnh migrate riêng.

## 2. Tạo tài khoản để thử

```bash
BASE=http://localhost:8080/api/v1

curl -s -X POST $BASE/auth/register -H 'Content-Type: application/json' \
  -d '{"email":"shopper@example.com","password":"Str0ng!Pass"}'
```

OTP nằm trong hộp thư Mailpit (`http://localhost:8025`) hoặc trong log API nếu
`SMTP_HOST` rỗng. Lấy `otp` rồi xác nhận và đăng nhập:

```bash
curl -s -X POST $BASE/auth/verify-email -H 'Content-Type: application/json' \
  -d '{"email":"shopper@example.com","otp":"<OTP>"}'

ACCESS=$(curl -s -X POST $BASE/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"shopper@example.com","password":"Str0ng!Pass"}' \
  | sed -E 's/.*"accessToken":"([^"]+)".*/\1/')

AUTH="Authorization: Bearer $ACCESS"
echo "token length: ${#ACCESS}"
```

## 3. Hồ sơ

```bash
# Đọc hồ sơ — displayName và phone rỗng vì tài khoản mới
curl -s $BASE/users/me -H "$AUTH"

# Cập nhật tên và số điện thoại
curl -s -X PATCH $BASE/users/me -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"displayName":"Nguyễn Minh Anh","phone":"0912 345 678"}'

# Số điện thoại được chuẩn hoá về 0912345678
curl -s $BASE/users/me -H "$AUTH"

# Xoá số điện thoại: gửi chuỗi rỗng
curl -s -X PATCH $BASE/users/me -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"phone":""}'
```

**Kết quả mong đợi**: `phone` trả về `0912345678` (không có khoảng trắng); sau lệnh
xoá, `phone` là `null`. Mỗi lần cập nhật sinh một dòng `audit_logs` có action
`USER_PROFILE_UPDATED`.

### Kiểm tra validation

```bash
# Số không hợp lệ -> 400 USER_INVALID_PHONE, có details.field
curl -s -X PATCH $BASE/users/me -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"phone":"12345"}'
```

## 4. Avatar

```bash
curl -s -X POST $BASE/users/me/avatar -H "$AUTH" \
  -F "file=@avatar.jpg;type=image/jpeg"

curl -s $BASE/users/me -H "$AUTH"      # avatar.width tối đa 512

# Tệp không phải ảnh -> 400 USER_AVATAR_TYPE_UNSUPPORTED
echo "not an image" > /tmp/notimage.txt
curl -s -X POST $BASE/users/me/avatar -H "$AUTH" -F "file=@/tmp/notimage.txt"

# Xoá avatar
curl -s -X DELETE $BASE/users/me/avatar -H "$AUTH"
```

**Kết quả mong đợi**: sau lần tải lên thất bại, `avatar` vẫn là ảnh cũ (FR-017).

## 5. Địa chỉ với select cascading

```bash
# 1. Lấy danh sách tỉnh
curl -s $BASE/divisions/provinces -H "$AUTH"

# 2. Lấy phường/xã của một tỉnh (lấy code từ kết quả bước 1)
curl -s $BASE/divisions/provinces/<PROVINCE_CODE>/wards -H "$AUTH"

# 3. Tạo địa chỉ
curl -s -X POST $BASE/users/me/addresses -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{
        "recipientName":"Nguyễn Minh Anh",
        "recipientPhone":"0912345678",
        "provinceCode":"<PROVINCE_CODE>",
        "provinceName":"<tên tỉnh>",
        "wardCode":"<WARD_CODE>",
        "wardName":"<tên phường>",
        "streetAddress":"12 Nguyễn Huệ"
      }'
```

**Kết quả mong đợi**: `201` với `isDefault: true`, vì đây là địa chỉ đầu tiên
(FR-009).

### Ràng buộc địa chỉ mặc định

```bash
# Thêm địa chỉ thứ hai -> isDefault false, vẫn có đúng 1 mặc định
curl -s -X POST $BASE/users/me/addresses -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{... "streetAddress":"45 Lê Lợi" }'

# Đặt mặc định -> trước đó bị bỏ mặc định, sau đó có đúng 1 mặc định
curl -s -X POST $BASE/users/me/addresses/<SECOND_ID>/default -H "$AUTH"

# Liệt kê -> mặc định đứng đầu, có meta.total
curl -s "$BASE/users/me/addresses?page=1&pageSize=20" -H "$AUTH"
```

**Kết quả mong đợi**: sau mọi thao tác, `meta.total` khớp số địa chỉ chưa ẩn và
đúng **một** `isDefault: true` (SC-004).

### Ràng buộc hành chính

```bash
# Ward không thuộc tỉnh đã chọn -> 400 USER_WARD_PROVINCE_MISMATCH
curl -s -X POST $BASE/users/me/addresses -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"recipientName":"A","recipientPhone":"0912345678",
       "provinceCode":"<TỈNH_A>","wardCode":"<WARD_CỦA_TỈNH_B>","streetAddress":"1 A"}'

# Tỉnh không có trong dataset -> 400 USER_UNKNOWN_PROVINCE
curl -s -X POST $BASE/users/me/addresses -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"recipientName":"A","recipientPhone":"0912345678",
       "provinceCode":"ZZZ","wardCode":"ZZZ-1","streetAddress":"1 A"}'
```

### Sửa và ẩn địa chỉ

```bash
curl -s -X PATCH $BASE/users/me/addresses/<ADDRESS_ID> -H "$AUTH" \
  -H 'Content-Type: application/json' -d '{"streetAddress":"99 Nguyễn Huệ"}'

curl -s -X DELETE $BASE/users/me/addresses/<ADDRESS_ID> -H "$AUTH"   # 204

# Địa chỉ đã ẩn -> 404 USER_ADDRESS_NOT_FOUND
curl -s -X PATCH $BASE/users/me/addresses/<ADDRESS_ID> -H "$AUTH" \
  -H 'Content-Type: application/json' -d '{"streetAddress":"x"}'
```

## 6. Quyền sở hữu (SC-003)

Đăng nhập bằng tài khoản thứ hai rồi thử đụng địa chỉ của tài khoản đầu:

```bash
curl -s -X PATCH $BASE/users/me/addresses/<ADDRESS_ID_OF_OTHER> \
  -H "Authorization: Bearer $ACCESS2" -H 'Content-Type: application/json' \
  -d '{"streetAddress":"hack"}'
```

**Kết quả mong đợi**: `404 USER_ADDRESS_NOT_FOUND` — không rò dữ liệu của người khác.

## 7. Truy vấn của quản trị viên (P4)

```bash
# Tài khoản khách -> 403
curl -s -o /dev/null -w "%{http_code}\n" $BASE/users/<USER_ID> -H "$AUTH"

# Tài khoản admin -> 200, chỉ đọc
ADMIN_ACCESS=$(curl -s -X POST $BASE/auth/login -H 'Content-Type: application/json' \
  -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}" \
  | sed -E 's/.*"accessToken":"([^"]+)".*/\1/')

curl -s $BASE/users/<USER_ID> -H "Authorization: Bearer $ADMIN_ACCESS"

# Mỗi lần xem đều được audit
docker compose exec -T db psql -U app -d artist_shop \
  -c "SELECT action, actor_id, target_id FROM audit_logs WHERE action = 'USER_PROFILE_VIEWED_BY_ADMIN' ORDER BY created_at DESC LIMIT 3;"
```

## 8. Test tự động

```bash
make test              # unit test, không cần Docker
make test-integration  # PostgreSQL thật qua testcontainers
make lint GOLANGCI_LINT=<đường/dẫn/golangci-lint>
```

Bộ test quan trọng nhất nằm ở:

| Nhóm | File |
|---|---|
| Ràng buộc 1 địa chỉ mặc định (kể cả ghi thẳng cột) | `infrastructure/implement/postgres/address_integration_test.go` |
| Chuẩn hoá số điện thoại | `domain/model/phone_test.go` |
| Quy tắc cấu trúc địa chỉ, cờ mặc định | `domain/model/address_test.go` |
| Quy tắc tỉnh/phường hợp lệ | `application/implement/address_test.go` (use case gọi port `Divisions`) |
| Giới hạn loại/kích thước avatar, hạn mức 429 | `domain/model/avatar_test.go`, `presentation/http/avatar_test.go` |
| Quyền sở hữu và phân quyền admin | `presentation/http/http_test.go` |
| Luồng HTTP end-to-end | `presentation/http/http_integration_test.go` |

## 9. Kiểm tra dữ liệu

```bash
make db-shell
```

```sql
-- Profile đã đồng bộ
SELECT user_id, display_name, phone, avatar_public_id FROM users
 WHERE display_name IS NOT NULL;

-- Không thể có 2 địa chỉ mặc định: index chặn ở tầng lưu trữ
SELECT user_id, count(*) FROM addresses
 WHERE is_default AND deleted_at IS NULL GROUP BY user_id HAVING count(*) > 1;

-- Audit của module này
SELECT action, actor_id, target_id, outcome FROM audit_logs
 WHERE action LIKE 'USER_%' ORDER BY created_at DESC LIMIT 10;
```
