# Coding Conventions (Go)

Quy ước bắt buộc cho mọi file Go trong repo. Khi tài liệu này mâu thuẫn với
`.golangci.yml` (lint chạy trong CI), **lint thắng**.

## Đặt tên

| Thứ | Quy ước | Ví dụ |
|---|---|---|
| Package (thư mục) | chữ thường, không gạch dưới | `audit`, `reqctx`, `token` |
| Tên file | chữ thường, không gạch dưới | `login_guard.go`, `http.go` |
| Hàm/method/exported | `PascalCase` | `NewOTPStore`, `FindByID` |
| Hàm/method/unexported | `camelCase` | `parseClient`, `withinTx` |
| Biến/tham số | `camelCase` | `refreshToken` |
| Hằng số | `PascalCase` khi exported | `CodeInvalidCredentials` |
| Receiver | 1–2 chữ, ngắn, nhất quán trong package | `func (r *UserRepository) …` |
| Interface | mô tả **khả năng**, không mô tả đối tượng | `MediaStore`, `Auditor`, `UserProvisioner` |
| Test | hàm `Test…`; file `*_test.go`, integration `*_integration_test.go` | `TestProvisionAdminCreatesActiveAdmin` |

Không dùng `SCREAMING_SNAKE_CASE`. Không dùng tiền tố `I` cho interface (thay vì
`IUserRepository`, dùng `UserRepository` hoặc `UserLookup`).

## Tầng và phụ thuộc

```text
presentation  →  application  →  domain
infrastructure → domain, application/interface
```

| Tầng | Chứa được | Không được |
|---|---|---|
| `domain` | `model`, `constant`, `error`, `repository` | Import framework, `database/sql`, `pgx`, `chi`. Chỉ stdlib + `share/access` + `github.com/google/uuid` (ngoại lệ do hiến pháp I công nhận) |
| `application` | `interface` (use case, port, UnitOfWork), `implement`, `dto`, `mapper` | Import `infrastructure`, `presentation` |
| `infrastructure` | `implement` (adapter: postgres, redis, media, auditor, token) | Không chứa interface, không chứa nghiệp vụ |
| `presentation` | `http`, `cli`, `worker`, `dto` | Chứa nghiệp vụ, chứa SQL |

Mã dùng chung nhiều module → `internal/share/`. Hợp đồng liên module →
`internal/contracts/`.

## Comment

- **Ngôn ngữ: tiếng Anh** cho comment trong code.
- Mỗi package có comment `// Package xxx …` ngay trên `package` — linter (`revive`) bắt
  buộc.
- **Mọi thành viên exported** phải có comment bắt đầu bằng tên thành viên:
  `// FindByID loads an account by id.`
- Comment giải thích **tại sao**, không kể lại cái làm. Không lặp lại tên hàm.
- Comment về nghiệp vụ nên dẫn mã FR của feature (`// FR-014`) khi quyết định thể hiện
  một yêu cầu cụ thể.
- **Không** dùng `TODO`/`FIXME` không kèm mã issue; xem
  [code-hygiene.md](code-hygiene.md).

## Xử lý lỗi

1. Sentinel error trong `domain/error`, **không** dùng chuỗi để so sánh:

```go
// domain/error/errors.go
var ErrUserNotFound = errors.New("user not found")
```

2. Bọc để giữ nguyên nhân, dùng `%w`:

```go
if err := repo.ByEmail(ctx, email); err != nil {
    return fmt.Errorf("load account: %w", err)
}
```

3. Kiểm tra bằng `errors.Is`, không so sánh `err ==` với chuỗi hay error của thư viện.

4. **Chỉ `presentation` mới gọi `httpx.WriteError`.** `domain`/`application` trả lỗi
   sentinel; ánh xạ sang HTTP + mã máy-đọc-được nằm trong `presentation/http/errors.go`.

5. Lỗi trả về cho client **không bao giờ** chứa chi tiết nội bộ. Mã 5xx được log kèm
   correlation id, client chỉ nhận message an toàn.

## Ngữ cảnh (context)

- `ctx context.Context` là **tham số đầu tiên** của mọi hàm có I/O, không bao giờ lưu
  vào struct.
- Luôn dùng **thằng `ctx` truyền vào**, không `context.Background()` bên trong hàm
  (trừ khi hàm thực sự bắt đầu một luồng mới).
- Hủy thì trả về `ctx.Err()`; dùng biến thứ tự `defer cancel()`.

## Định dạng và style

- `gofmt` là bắt buộc (`make fmt-check`). Tabs do gofmt quyết định — đừng tự chỉnh.
- Không dùng `interface{}`; dùng `any`.
- Slice rỗng trả về `nil`, không trả `[]T{}` rỗng.
- Group import: stdlib → thư viện ngoài → nội bộ, mỗi nhóm một khối trống.

## DTO và mapper

- **Hai lớp DTO**: `presentation/dto` (HTTP) và `application/dto` (use case).
- Chỉ **một** mapper cho module, ở `application/mapper`. Không tự chuyển đổi kiểu rải
  rác trong handler.

## Query và SQL

- SQL nằm trong `infrastructure/implement/postgres`, **không** trong `presentation` hay
  `application`.
- Dùng `share/repository.Base[T, ID]` cho CRUD + phân trang; chỉ viết SQL tay khi cần
  truy vấn đặc thù hoặc ràng buộc giao dịch.
- Mọi giá trị SQL đều **bind bằng tham số** (`$1`), không nội suy chuỗi.
- Mọi truy vấn có `updated_at = now()` khi sửa, và `WHERE deleted_at IS NULL` khi
  bảng có soft delete.
- Repository **không** tự mở transaction; ranh giới giao dịch do `application` giữ
  qua port `UnitOfWork`.

## Test

- Bốn loại test, mỗi loại một quy ước đặt file:

| Loại | Đặt file | Build tag | Chạy bằng |
|---|---|---|---|
| Unit | cạnh code | không | `make test` |
| Handler | `presentation/http/*_test.go` | không | `make test` |
| Repository / HTTP flow | `*_integration_test.go` | `integration` | `make test-integration` |

- Tên test mô tả **hành vi**, không mô tả hàm:
  `TestProvisionAdminCreatesActiveAdmin`, không `TestProvisionAdmin1`.
- Test phải tự chứa: tạo dữ liệu riêng, không phụ thuộc thứ tự chạy, không để lại dữ
  liệu ảnh hưởng test khác.
- Logic quan trọng (tiền, tồn kho, state transition) viết **test trước** và phải
  **fail** trước khi có implementation.

## Bảo mật

- Id của chủ tài khoản lấy từ session, **không** từ body/query/path.
- Không so khớp mật khẩu bằng `==`; dùng hàm so sánh constant-time của module token.
- Không log secret. Khóa redact nằm ở `share/logging/redact.go` — thêm key mới khi phát
  sinh loại dữ liệu nhạy cảm mới.
- Upload: kiểm tra **nội dung** file, không tin tên file hay `Content-Type` do client
  khai; đặt trần cứu khi đọc body.
- Idempotency: mọi callback từ bên ngoài phải lưu khoá chống xử lý lặp.

## Ngôn ngữ

- Comment trong code và trong `.env`/`.env.example`: **tiếng Anh**.
- Tài liệu trong `docs/` và trao đổi với dev: **tiếng Việt**.