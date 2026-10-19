# Xử lý sự cố

Các lỗi **thật** đã gặp khi dựng và vận hành dự án này, kèm nguyên nhân và cách
sửa.

---

## Docker

### `Bind for 0.0.0.0:8080 failed: port is already allocated`

Container của dự án khác (hoặc service trên máy) đang giữ port 8080.

```bash
docker ps --format "{{.Names}} {{.Ports}}" | Select-String 8080
```

Cách 1 — đổi port (khuyến nghị):
```bash
make up API_PORT=8090
```

Cách 2 — chỉ port db/redis, không cần đổi port app:
```bash
make up-tools DB_PORT=5433 REDIS_PORT=6380
```

> Chỉ `api` mới publish port mặc định; `db`/`redis` chỉ mở port ở chế độ tools, nên
> đây gần như luôn là xung đột với app khác chứ không phải với database.

### `cannot start postgres container: ... reaper: new reaper: run container`

Docker Desktop trên Windows expose daemon qua **named pipe**, container ryuk
(reaper của testcontainers) không với tới được → timeout 60 giây.

`internal/share/testsupport/postgres.go` đã tự tắt reaper
(`TESTCONTAINERS_RYUK_DISABLED=true`). Nếu vẫn lỗi khi tự viết test:

```bash
TESTCONTAINERS_RYUK_DISABLED=true go test -tags integration ./...
```

Dấu hiệu bạn đang gặp đúng lỗi này: test "PASS" nhưng mất ~60 giây và log có
`--- SKIP`. Kiểm tra bằng `go test -v -tags integration ./...` và tìm `SKIP`.

### `migrate: DATABASE_URL is required`

Service `migrate` trong compose đã có env qua YAML anchor `x-common-env` —
lỗi này chỉ gặp khi chạy image **trực tiếp từ Docker Desktop** (nút Run) hoặc
`docker run` thủ công mà không kèm env. Luôn chạy qua compose:

```bash
make migrate-up-docker   # == docker compose run --rm migrate up
```

Nếu bạn thêm service mới cần database, nhớ thêm:

```yaml
    <<: *common-env
```

### Không start được container `migrate`/`seed` cũ trong Docker Desktop

Các container one-shot `artist-shop-migrate-1` / `artist-shop-seed-1` tạo thủ
công từ image (không qua compose) vừa thiếu env vừa trỏ tới network đã bị
xoá: bấm Start báo `network ... not found`, log báo `DATABASE_URL is required`.
Xoá chúng đi và chạy lại bằng lệnh compose ở trên:

```bash
docker rm artist-shop-migrate-1 artist-shop-seed-1
make migrate-up-docker
```

### `seed-sql` báo thiếu `scripts/seed.vars.local.psql`

Chưa tạo file biến local. Tạo theo mẫu trong header `scripts/seed_admin.sql`
(email + hash từ `make seed-hash`), rồi chạy lại `make seed-sql`. File này
đã bị `.gitignore` chặn — không bao giờ commit hash thật.

### `seed-sql` báo hash không hợp lệ

Hash phải có dạng `$argon2id$...` do `make seed-hash` sinh ra (đọc
`ADMIN_PASSWORD` từ môi trường, có kiểm tra chính sách: **≥ 8 ký tự, có chữ,
có số, có ký tự đặc biệt**). Password sinh tự động (ngẫu nhiên thuần) dễ
thiếu một trong các loại nên bị từ chối — đây là hành vi đúng.

### `mailpit` không nhận email

Kiểm theo thứ tự:

1. **Service `api` có đang chạy bằng dev overlay không?** `docker-compose.dev.yml` mới là
   thứ đặt `SMTP_HOST=mailpit` cho container. Nếu chạy `docker-compose.yml` trần thì không
   có Mailpit và cũng không có override — dùng `make up-tools`.
2. **API có chạy trên host không?** Khi đó container override không áp dụng: host cần
   `.env.local` với `SMTP_HOST=localhost`, `SMTP_PORT=1025` (container gọi bằng tên service,
   host gọi bằng cổng đã publish).
3. **Đã tạo lại container chưa?** `environment:` chỉ áp dụng lúc tạo:
   `docker compose up -d --force-recreate api`.
4. **`SMTP_HOST` trong `.env` có đang đè không?** Không — `.env.local` và biến môi trường
   thật đều thắng `.env`, còn compose `environment:` thắng cả hai. Nhưng nếu bạn *đã* sửa
   `.env` từ trước, giá trị đó vẫn được dùng ở chế độ host-run.

Kiểm nhanh xem tiến trình thật sự thấy gì:

```bash
docker compose -f docker-compose.yml -f docker-compose.dev.yml config | grep SMTP_HOST
```


### Container `api` restart liên tục

```bash
docker compose logs api
```

| Log | Nguyên nhân |
|---|---|
| `JWT_SECRET must be at least 32 bytes` | `JWT_SECRET` thiếu hoặc quá ngắn |
| `HTTP_ADDR is required` | Thiếu `HTTP_ADDR` |
| `invalid configuration` | Sai định dạng biến (vd `10s` viết sai) |

Lưu ý: `cmd/migrate` **không** cần `JWT_SECRET`/`REDIS_ADDR`, nên
`make migrate-up-docker` vẫn chạy được dù app không khởi động.

### Container không healthy

```bash
docker compose ps
docker compose logs db        # hoặc redis, api
make db-shell                 # SELECT 1
make redis-cli                # PING
```

`api` chỉ healthy khi `/readyz` trả 200, tức cần cả PostgreSQL **và** schema
**và** Redis.

---

## Chạy `make` trên Windows

### `make: The system cannot find the path specified.` (in ra mỗi lần chạy)

`GIT_SHA` thiếu dạng redirect cho cmd.exe. Đã sửa bằng
`2>/dev/null 2>NUL || echo dev` trong Makefile.

### `'files' is not recognized as an internal or external command`

Bản Makefile cũ dùng `SHELL := bash` và cú pháp POSIX (`[ -n "$$files" ]`) nên
`cmd.exe` không hiểu. Đã sửa bằng cách chọn lệnh theo OS (`FMT_CHECK_CMD`,
`ENV_CMD`, ...). Nếu bạn tự thêm target, hãy viết recipe theo POSIX sh và tránh
`if [ ... ]` — make xử lý từng dòng lệnh nên echo/copy vẫn chạy được trên cả hai OS.

### `'C:/Program Files/Git/bin/bash.exe' is not recognized`

Bản Makefile cũ cần Git Bash. Bản hiện tại không cần. Nếu bạn vẫn thấy lỗi này,
đang chạy nhầm Makefile cũ trong cache.

### `make: command not found` (Linux/WSL)

Cài GNU make, hoặc chạy target trực tiếp bằng lệnh gốc trong `docs/makefile.md`.

---

## Go / chất lượng code

### `gofmt -l .` liệt kê hàng trăm file, nhưng CI lại xanh

Máy đang để `core.autocrlf=true`, nên working tree là **CRLF** trong khi repo lưu
**LF**. CI (Linux) checkout LF nên không sao, còn local thì `gofmt` thấy khác biệt.

Đã thêm `.gitattributes`:
```
* text=auto eol=lf
```

Nếu vẫn còn, chuẩn hoá lại:
```bash
git add --renormalize .
```

Triệu chứng khi file vừa được sửa bằng công cụ Windows: `git status` hiện hàng
loạt file "M" nhưng `git diff --name-only` chỉ có vài file — đó là stat cache, chạy
`git add --renormalize .` là sạch.

### `golangci-lint exit with code 3` / `can't load config`

| Thông báo | Nguyên nhân | Cách sửa |
|---|---|---|
| `unsupported version of the configuration: ""` | `.golangci.yml` chưa ở schema v2 | Thêm `version: "2"` ở dòng đầu |
| `the Go language version (go1.24) … lower than the targeted (1.26.0)` | golangci-lint build bằng Go cũ hơn `go.mod` | Dùng golangci-lint **v2** (v1 dừng ở bản build Go 1.24) |
| `golangci-lint v2 is not supported by golangci-lint-action@v6` | Action v6 chỉ hỗ trợ v1 | Lên **action v9** |

### `make check` báo đỏ ở bước `test-race`

```text
ERROR: the race detector requires CGO_ENABLED=1 and a C compiler.
```

Cài GCC (`choco install mingw`) hoặc để `make check` tự bỏ qua bước này (nó sẽ in
`SKIPPED test-race`). CI vẫn chạy đủ.

### `lint: 0 issues` nhưng có vẻ nhiều lỗi hơn

golangci-lint mặc định giới hạn **3 issue cùng nội dung** (`max-same-issues: 3`).
Xem đầy đủ:

```bash
golangci-lint run --max-same-issues 0 --max-issues-per-linter 0
```

### `make coverage` không có dữ liệu

Chỉ package có test mới xuất hiện. Xem danh sách phủ:
`go tool cover -func=coverage.out`.

---

## Test

### Integration test "pass" nhưng chạy 60 giây rồi SKIP

Xem mục `reaper` ở trên. Chạy `go test -v -tags integration ./...` để phát hiện
`--- SKIP`.

### Integration test timeout khi chạy song song

Mỗi package tự tạo một container PostgreSQL. Dùng target có `-p 1`:
```bash
make test-integration
```

### Test fail vì dùng chung database

Test integration phải **tự chứa**: tạo dữ liệu riêng, không dựa vào test khác,
không để lại dữ liệu. `internal/share/testsupport` đã tạo database mới cho mỗi
test — nếu bạn tự viết test dùng chung database đã có, hãy tách riêng.

### `mail: command not found` (WSL) hoặc curl không có (Windows cũ)

Dùng trình duyệt mở `http://localhost:8025`, hoặc `curl` trong PowerShell:
```powershell
Invoke-WebRequest -UseBasicParsing http://localhost:8025/api/v1/messages | Select-Object -ExpandProperty Content
```

---

## Auth

### `AUTH_INVALID_CREDENTIALS` dù mật khẩu đúng

| Cần kiểm tra | Cách |
|---|---|
| Tài khoản đã xác nhận email chưa | `POST /auth/register/verify-otp` trước; đăng ký chưa xác thực thì chưa có tài khoản để đăng nhập |
| Tài khoản có bị khoá không | 10 lần sai ⇒ `AUTH_LOGIN_LOCKED` trong 15 phút |
| Email đã normalize chưa | Email được trim + lowercase; dùng `User@Example.com ` cũng được, nhưng nhớ khớp khi verify |
| Admin có được seed chưa | `make seed-sql` (email trong `ADMIN_EMAIL`, hash từ `make seed-hash`) |

### `AUTH_OTP_TOO_MANY_ATTEMPTS` khi OTP vẫn còn hạn

Sai 5 lần ⇒ mã bị khoá 60 giây (`OTP_BLOCK_TTL`). Chờ hoặc gửi lại mã
`POST /auth/register/resend-otp` (có cooldown 60 giây).

### `AUTH_REFRESH_REUSED`

Đã dùng refresh token rồi dùng lại token cũ. Đây là **cơ chế phát hiện replay**:
cả phiên của user bị thu hồi. Nếu gặp khi client xử lý chậm, kiểm tra client có
gọi `refresh` hai lần song song không — phải serialize.

### `UNAUTHENTICATED` dù đã gửi token

Header phải đúng dạng: `Authorization: Bearer <token>`. Kiểm tra access token còn
hạn (15 phút) và chưa bị revoke.

### Đổi `JWT_SECRET` thì client phải đăng nhập lại

Đúng và cần thiết: mọi access token ký bằng secret cũ trở nên không hợp lệ. Refresh
token vẫn dùng được (opaque, lưu ở PostgreSQL) — nhưng sẽ fail vì access token mới
cần secret mới.

---

## Tài liệu

### Endpoint mới xuất hiện nhưng không có trong tài liệu

Constitution VIII bắt buộc cập nhật [api-reference.md](api-reference.md) **trong
cùng thay đổi**. Quy trình: thêm mục endpoint → cập nhật bảng tổng hợp → thêm dòng
change log → nếu endpoint mới thì cập nhật `specs/<feature>/contracts/openapi.yaml`.

### Link trong tài liệu hỏng sau khi đổi đường dẫn

Tài liệu dùng **đường dẫn tương đối**. Khi di chuyển file, sửa cả hai phía:

```bash
grep -rn "docs/" docs/ .specify/ README.md
```
