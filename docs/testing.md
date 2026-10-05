# Kiểm thử & kiểm tra chất lượng

## Tổng quan

| Tầng | Lệnh | Khi nào chạy | Cần Docker |
|---|---|---|---|
| Format | `make fmt-check` | trước khi commit | ⭕ |
| Tidy | `make tidy-check` | trước khi commit | ⭕ |
| Vet | `make vet` | trước khi commit | ⭕ |
| Lint | `make lint` | trước khi commit | ⭕ |
| Unit test | `make test` | mỗi đổi code | ⭕ |
| Race detector | `make test-race` | trước khi merge | ✅ |
| Integration test | `make test-integration` | trước khi merge | ✅ |
| Toàn bộ | `make check` | xác nhận trước khi merge | ✅ |

`make check` chính là CI chạy: [`.github/workflows/ci.yml`](../.github/workflows/ci.yml).

---

## 1. Cấu trúc test trong repo

```
internal/
  modules/auth/…
    application/implement/*_test.go     unit test với fake trong bộ nhớ
    application/mapper/mapper_test.go
    infrastructure/implement/token/*_test.go
    infrastructure/implement/redis/*_test.go   miniredis (không cần Docker)
    infrastructure/implement/postgres/*_test.go  build tag integration
    presentation/http/http_test.go       unit test handler (stub service)
    presentation/http/http_integration_test.go   build tag integration
  share/
    repository/repository_integration_test.go    build tag integration
    database/tx_integration_test.go              build tag integration
    testsupport/postgres.go                      testcontainers helper
```

## 2. Build tag `integration`

Test cần PostgreSQL/Redis thật được gắn tag để `go test ./...` mặc định **không**
chạy:

```go
//go:build integration
```

Chạy:
```bash
make test-integration       # go test -count=1 -p 1 -tags integration ./...
```

Ba điều cần biết:

**a) Container được tự tạo.** `internal/share/testsupport` khởi động PostgreSQL bằng
testcontainers, không phụ thuộc stack của bạn. Nên **không** cần `make up`.

**b) Reaper (ryuk) đã bị tắt có chủ đích.** `testsupport.disableReaper()` tự đặt
`TESTCONTAINERS_RYUK_DISABLED=true` khi biến này chưa có. Lý do: Docker Desktop trên
Windows expose daemon qua **named pipe**, mà container ryuk không với tới được, nên
mọi lần khởi động container sẽ timeout sau 60 giây.

> Hệ quả: trước khi có fix này, các integration test **đã từng pass giả** — thực
> ra là `--- SKIP` sau 60 giây. Nếu testcontainers log
> `reaper: new reaper: run container`, hãy kiểm tra lại biến môi trường này.

**c) Vì sao `-p 1`.** Chạy song song, mỗi package tự tạo một container PostgreSQL.
Trên máy yếu sẽ timeout. `-p 1` chạy tuần tự: chậm hơn nhưng ổn định.

## 3. Unit test không cần Docker

Các test sau chạy tức thì, không cần hạ tầng:

- **Application** (`application/implement`): dùng fake trong bộ nhớ
  (`fakes_test.go` — fake users, sessions, resets, OTP, blacklist, guard, email,
  auditor). Nhanh và cô lập.
- **Token**: ký/parse JWT thật với secret cố định trong test.
- **Redis**: dùng `miniredis` (Redis in-memory).
- **Mapper**: thuần tuý.
- **Presentation HTTP**: dựng router với stub service, kiểm tra status/envelope.

```bash
make test        # chạy tất cả, ~2s
```

## 4. Coverage

```bash
make coverage
```

Ghi ra `coverage.out`. Xem từng hàm:

```bash
go tool cover -html=coverage.out       # mở trình duyệt
go tool cover -func=coverage.out | tail -20
```

Hiến pháp (Principle IV) quy định coverage của package **không được giảm** khi thay
đổi code.

## 5. Race detector

```bash
make test-race
```

Cần cgo + trình biên dịch C:

| Hệ điều hành | Cách có |
|---|---|
| Linux / CI | có sẵn |
| Windows | `choco install mingw`, rồi đảm bảo `gcc` nằm trong `PATH` |

Không có gcc, `make test-race` in hướng dẫn và trả về mã lỗi (không im lặng bỏ qua),
còn `make check` tự bỏ qua bước này và nói rõ đang bỏ qua.

## 6. Lint

```bash
make lint
```

Dùng **golangci-lint v2** — cùng version CI dùng. Cài:

```bash
make install-tools     # go install ...@v2.14.0
```

Nếu binary không có trong `PATH`, chỉ định đường dẫn:

```bash
make lint GOLANGCI_LINT='C:/path/to/golangci-lint.exe'
```

Linter đang bật: `errcheck`, `govet`, `misspell`, `revive`, `staticcheck`,
`unused`, và formatter `gofmt` + `goimports` (xem `.golangci.yml`).

| Lỗi hay gặp | Nguyên nhân |
|---|---|
| `can't load config: unsupported version` | `.golangci.yml` chưa có `version: "2"` |
| `the Go language version (go1.24) … lower than the targeted (1.26.0)` | golangci-lint build bằng Go cũ hơn `go.mod` → cần v2 |
| `Error return value of x is not checked` | `errcheck` — kiểm tra error hoặc ghi rõ chủ ý bỏ qua |

## 7. CI

`.github/workflows/ci.yml` chia 7 job chạy song song:

| Job | Nội dung |
|---|---|
| Format and Tidy | `make fmt-check`, `make tidy-check` |
| Go Vet | `make vet` |
| Lint | `golangci-lint-action@v9` với `v2.14.0` |
| Build Binaries | `make build` |
| Unit Tests | `make test` |
| Integration Tests | `make test-integration` (runner có Docker sẵn) |
| Race Detector Tests | `make test-race` |

Có `concurrency` + `cancel-in-progress`: push mới sẽ huỷ run cũ.

## 8. Viết test mới

Theo Constitution IV, logic quan trọng (tiền, tồn kho, state machine) phải
**test-first** — test phải fail trước khi có implementation.

### Unit test cho use case (không cần Docker)

Thêm fake vào `fakes_test.go` rồi viết test theo harness:

```go
func TestSomethingRejects(t *testing.T) {
    h := newHarness()          // đã gắn đủ fake
    err := h.svc.DoSomething(context.Background(), input)
    if !errors.Is(err, domainErr.ErrSomething) {
        t.Fatalf("expected ErrSomething, got %v", err)
    }
}
```

### Integration test (cần PostgreSQL)

```go
//go:build integration

func TestRepositoryXxx(t *testing.T) {
    dsn := testsupport.PostgresDSN(t)   // tự tạo container, tự dọn
    // … migrate, tạo dữ liệu, assert
}
```

Nguyên tắc: **mỗi test tự chứa** — tạo dữ liệu riêng, không phụ thuộc test khác,
không để lại dữ liệu ảnh hưởng test sau.

### Test HTTP

- Unit (`http_test.go`): stub service, kiểm tra status code + envelope.
- Integration (`http_integration_test.go`): stack thật, kiểm tra luồng end-to-end
  (register → OTP → verify → login → `/me` → refresh → reuse).

## 9. Test hiện có của module Auth

| Nhóm | Phủ |
|---|---|
| `application/implement` | Register (email chuẩn hoá, pending, chống email trùng), verify OTP, resend + cooldown, đổi email sai, login, lockout 10 lần, refresh + phát hiện reuse, logout, đổi mật khẩu + thu hồi phiên, quên/reset mật khẩu, **ProvisionAdmin** |
| `infrastructure/token` | Ký/parse access token, TTL hết hạn, sai secret, refresh token sinh + hash |
| `infrastructure/redis` | OTP (3 lần sai → khoá), cooldown, blacklist JTI, login guard |
| `presentation/http` | Phân quyền guest/customer/admin, endpoint `/me`, audit khi bị từ chối |
| Integration | Flow register → OTP → login → `/me` → refresh → reuse; sign-out 1 thiết bị không ảnh hưởng thiết bị khác |
| `share/logging` | Redact password/token/`jwt_secret`/`redis_password`, cả trong group lồng |
| `share/repository` | Generic `Base`: Create/FindByID/Exists/FindAll/Update/Delete |
| `share/database` | `WithinTx` commit/rollback/rollback-khi-panic, `Health` cần `SetReady` |

## 10. Trước khi mở pull request

```bash
make check
git status              # không có file sinh ra ngoài ý muốn
```

Ngoài ra: nếu thêm/sửa/xoá endpoint → **phải** cập nhật
[api-reference.md](api-reference.md) trong cùng thay đổi (Constitution VIII).