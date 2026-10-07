# Makefile

`Makefile` là **điểm vào duy nhất** cho mọi lệnh thường dùng, để Windows và Linux
chạy giống hệt nhau.

```bash
make            # in trang chỉ mục
make help       # như trên
make docker-help
```

Danh sách đầy đủ 49 target: xem `make help` và 4 nhóm bên dưới.

---

## 1. Cấu trúc file

Makefile gồm 5 khối, theo đúng thứ tự:

```
1. Khai báo biến dùng chung     GO, COMPOSE, BIN_DIR, GIT_SHA, port...
2. ifeq ($(OS),Windows_NT)      chọn lệnh theo hệ điều hành
3. .DEFAULT_GOAL := help        + 4 target help
4. Target theo nhóm             Development / Quality / Migrations / Docker
```

> Thứ tự này **bắt buộc**: make đọc file từ trên xuống, nên `CGO_ENABLED_VALUE`
> phải được khai báo trước khi `ifeq` dùng nó ở phần 2.

## 2. Biến có thể ghi đè từ CLI

```makefile
GO           ?= go
COMPOSE      ?= docker compose
GOLANGCI_LINT ?= golangci-lint
BIN_DIR      := bin
```

| Ký hiệu | Nghĩa |
|---|---|
| `?=` | Gán nếu **chưa** được định nghĩa (cho phép ghi đè từ CLI) |
| `:=` | Gán và **tính ngay** tại thời điểm khai báo |

```bash
make up API_PORT=8090                     # tránh đụng port 8080 đang bận
make lint GOLANGCI_LINT=/path/to/lint     # dùng binary cụ thể
COMPOSE=docker-compose make ps            # compose cũ
```

`API_PORT` được dùng ở **hai** nơi: trong lệnh `docker-compose.yml`
(`${API_PORT:-8080}`) và trong lệnh `echo` báo địa chỉ — nên không lệch nhau.

### `GIT_SHA` — mẹo tránh nhiễu trên Windows

```makefile
GIT_SHA := $(shell git rev-parse --short HEAD 2>/dev/null 2>NUL || echo dev)
```

Cần **cả hai** dạng redirect: `2>/dev/null` cho POSIX, `2>NUL` cho `cmd.exe`. Nếu
chỉ có một dạng, mỗi lần chạy `make` trên Windows in
`The system cannot find the path specified.` và `GIT_SHA` rơi về `dev`.

Dùng cho `make docker-build` → tag `artist-shop-be:<sha>`.

## 3. `COMPOSE_DEV` — gộp hai file compose

```makefile
COMPOSE_DEV = $(COMPOSE) -f docker-compose.yml -f docker-compose.dev.yml
```

Mọi lệnh dev dùng biến này, nên người dùng **không phải nhớ** `docker compose -f
a -f b`. `make up` dùng `$(COMPOSE)` (stack tối thiểu), các lệnh cần port host /
Mailpit dùng `$(COMPOSE_DEV)`.

Chi tiết hai file: [docker.md](docker.md).

## 4. Chọn lệnh theo hệ điều hành

Make trên Windows chạy recipe qua `cmd.exe`, **không phải bash**. Thay vì ép
`SHELL := bash` (bắt buộc phải có Git Bash), Makefile chọn *lệnh* theo OS:

| Biến | Windows | Linux / macOS |
|---|---|---|
| `GO_BIN_EXT` | `.exe` | *(rỗng)* |
| `MKDIR_BIN_CMD` | `if not exist "bin" mkdir "bin"` | `mkdir -p "bin"` |
| `CLEAN_BIN_CMD` | `rmdir /S /Q "bin"` | `rm -rf "bin"` |
| `CLEAN_COVERAGE_CMD` | `del /Q coverage.out` | `rm -f coverage.out` |
| `FMT_CHECK_CMD` | `powershell -Command "..."` | `test -z "$$(gofmt -l .)"` |
| `ENV_CMD` | `if exist .env ... else copy /Y ...` | `if [ -f .env ]; then ... fi` |
| `CGO_ENABLED_VALUE` | dùng chung → `test-race` bị skip nếu không có gcc | |

Ba kỹ thuật đáng chú ý:

**a) `$(subst /,\,$(BIN_DIR))`** — đổi `/` thành `\` cho `cmd.exe`.

**b) `$$` là escape của make.** Trong Makefile, `$$` → make không đội vào, chỉ truyền
một `$` xuống shell. Ví dụ `test -z "$$(gofmt -l .)"` là `$(...)` POSIX.

**c) `ifeq` ở cấp make, không phải trong recipe.** `test-race` dùng `ifeq` để quyết
định *nội dung recipe*, thay vì `if [ ... ]` trong shell (cách đó chỉ chạy được
trên POSIX).

## 5. Nhóm Development

| Target | Việc |
|---|---|
| `env` | Copy `.env.example` → `.env` nếu chưa có (dùng `ENV_CMD`) |
| `run` | `go run ./cmd/api` |
| `seed` | `go run ./cmd/seed` |
| `build` | Build `api`, `migrate`, `seed` vào `bin/` với `-trimpath -ldflags="-s -w"` |
| `dev` | `build` + hướng dẫn cách chạy |
| `fmt` | `gofmt -w .` |
| `tidy` | `go mod tidy` |
| `install-tools` | Cài `golangci-lint` đúng version CI dùng |
| `install-swag` | Cài `swag` (generator OpenAPI) đúng version |
| `swagger` | Sinh OpenAPI spec vào `docs/swagger/` |
| `clean` | Xoá `bin/` + `coverage.out` |

`install-tools` ghim `v2.14.0` — tránh tình trạng dev lint bằng version khác CI.
`install-swag` ghim `v1.16.4`. `swagger` chạy
`swag init -g cmd/api/main.go -o docs/swagger --parseInternal --parseDependency
--outputTypes go,json,yaml`; `--parseInternal` là bắt buộc vì handler và DTO nằm trong
`internal/`. Xem [decisions/011-swagger-from-code-annotations.md](decisions/011-swagger-from-code-annotations.md).

## 6. Nhóm Quality

| Target | Lệnh |
|---|---|
| `fmt-check` | `FMT_CHECK_CMD` (không sửa file) |
| `tidy-check` | `go mod tidy -diff` |
| `vet` | `go vet ./...` **và** `go vet -tags integration ./...` |
| `lint` | `golangci-lint run` |
| `static-check` | `fmt-check` + `tidy-check` + `vet` |
| `swagger-check` | Tái sinh spec rồi `git diff --exit-code -- docs/swagger` (phát hiện lệch) |
| `test` | `go test -count=1 ./...` |
| `test-race` | `go test -count=1 -race ./...` (cần gcc) |
| `test-integration` | `go test -count=1 -p 1 -tags integration ./...` |
| `coverage` | `go test -coverprofile` + `go tool cover -func` |
| `check` | Toàn bộ (xem bên dưới) |

### Ba quyết định thiết kế

**a) `tidy-check` dùng `go mod tidy -diff`, không `tidy && git diff`.**

`-diff` chỉ so sánh rồi trả mã lỗi — không ghi file. Cách `go mod tidy && git diff
--exit-code` sẽ **sửa file trước**, nên nếu check fail thì cây làm việc đã bị đổi.

**b) `vet` chạy cả hai build tag.** Integration test có build tag `integration`;
chỉ `go vet ./...` sẽ bỏ sót lỗi trong code chỉ tồn tại khi bật tag (ví dụ
`tx_integration_test.go` từng có lỗi).

**c) `test-integration` dùng `-p 1`.** Chạy song song, mỗi package tự khởi động
một container PostgreSQL qua testcontainers; trên máy yếu hoặc nhiều container cùng
lúc sẽ timeout. `-p 1` = tuần tự, chậm hơn nhưng ổn định.

### `check` thích ứng với khả năng máy

```makefile
CGO_ENABLED_VALUE := $(shell $(GO) env CGO_ENABLED)
ifeq ($(CGO_ENABLED_VALUE),1)
CHECK_TARGETS := static-check swagger-check lint test test-race test-integration build
else
CHECK_TARGETS := static-check swagger-check lint test test-integration build
endif
```

`go test -race` cần cgo + trình biên dịch C. Nếu hardcode `test-race` vào `check`,
máy không có gcc sẽ **luôn đỏ**. Nay `make check` in:

```
All checks passed. SKIPPED test-race: it needs CGO_ENABLED=1 and a C
compiler (gcc). Install gcc or run it in CI.
```

CI (ubuntu) có sẵn cả hai nên vẫn chạy đủ 7 bước.

### Không bao giờ pipe `go test` qua filter

`go test | grep -v "no test files"` làm exit code trở thành exit code của `grep`
(= 1 khi lọc hết dòng) — vừa báo đỏ giả, vừa che mất lỗi test thật. Các target test
trong Makefile này không pipe gì.

## 7. Nhóm Migrations

| Target | Cách chạy |
|---|---|
| `migrate-up` | `go run ./cmd/migrate up` |
| `migrate-down` | `go run ./cmd/migrate down` |
| `migrate-status` | `go run ./cmd/migrate status` |
| `migrate-version` | `go run ./cmd/migrate version` |
| `migrate-up-docker` | `docker compose run --rm migrate up` |
| `migrate-status-docker` | `docker compose run --rm migrate status` |

Không có `migrate-create` như ở dự án khác: runner dùng **goose nhúng trong
binary**, không phải Goose CLI, nên tạo file migration mới là thêm file
`migrations/NNNNN_<module>_<mô_tả>.sql` với cả `-- +goose Up` và `-- +goose Down`.

## 8. Nhóm Docker

| Target | Cách chạy |
|---|---|
| `up` | `docker compose up -d --build --wait` |
| `up-tools` | `$(COMPOSE_DEV) up -d --build --wait` |
| `stop` | `stop` — dừng, giữ container |
| `down` | `down` — xoá container + network, **giữ volume** |
| `down-all` | `down -v` — xoá cả volume ⇒ **mất database** |
| `ps` | xem trạng thái |
| `logs` | `logs -f api` |
| `rebuild` | `build --no-cache api` rồi `up --wait` |
| `docker-build` | `docker build -t artist-shop-be:<sha> -t artist-shop-be:latest .` |
| `db-shell` | `exec db psql -U app -d artist_shop` |
| `redis-cli` | `exec redis redis-cli` |
| `mail` | `curl http://localhost:8025/api/v1/messages` |
| `seed-docker` | `run --rm seed` |

Ba điểm cố ý:

**`--wait`** — Compose đợi mọi container `healthy` rồi mới trả về, nên
`make up && curl /readyz` không bị race.

**`db-shell` / `redis-cli` không dùng `sh -c '...'`** bên trong container. Nếu dùng,
`cmd.exe` truyền dấu nháy đơn theo nghĩa đen còn POSIX thì bỏ nó ⇒ cùng một lệnh hỏng
ở một trong hai OS. Truyền thẳng lệnh (`psql -U app -d artist_shop`) là cách duy
nhất portable.

**`redis-cli` không có `-a`** — container `redis` được đặt `REDISCLI_AUTH` nên
`redis-cli` tự đăng nhập. Cách này không lộ password trên dòng lệnh (ra process
list) và giống nhau trên mọi OS.

## 9. Tự thêm target

```makefile
.PHONY: my-target
my-target: ## Mô tả ngắn hiện ở cuối dòng
	@echo "đang chạy..."
	$(GO) run ./cmd/xxx
```

| Quy tắc | Tại sao |
|---|---|
| Luôn có `.PHONY` | `.PHONY: tên` để make không kiểm tra file/target trên đĩa |
| Comment `## mô tả` ngay sau tên | Cho phép `grep '## '` liệt kê, và tự hiện khi gõ `make help` |
| Dùng `$(GO)`, không ghi `go` | Cho phép đổi toolchain |
| Recipe dùng **POSIX sh** | Chạy được trên cả `cmd.exe` (make xử lý từng dòng) và Linux |
| Không dùng `$(shell ls ...)` trong recipe | Gọi chậm và khó debug; đặt biến ở phần khai báo |

Sau khi thêm, chạy `make check` để chắc không phá gì.
