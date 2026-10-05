# Cài đặt & chạy thử

Mục tiêu: có API chạy được ở `http://localhost:8080` trong ~5 phút.

## Yêu cầu

| Công cụ | Bắt buộc | Ghi chú |
|---|---|---|
| Docker + Docker Compose v2 | ✅ | Chạy PostgreSQL, Redis và API |
| Go 1.26+ | ⭕ | Chỉ cần khi `make run` bằng Go, hoặc chạy test |
| `make` | ✅ | Windows: [GNU make](https://www.gnu.org/software/make/) hoặc `choco install make` |

> `make` trên Windows **không cần Git Bash**. Makefile tự chọn lệnh phù hợp với
> `cmd.exe` (xem [makefile.md](makefile.md#4-chọn-lệnh-theo-hệ-điều-hành)).

## Bước 1 — Tạo file cấu hình

```bash
make env
```

Lệnh này copy `.env.example` → `.env` (nếu `.env` đã tồn tại thì không đụng vào).
`.env` được `.gitignore` chặn — **không bao giờ commit**.

Quan trọng nhất trong `.env` là `JWT_SECRET`:

```ini
# Phải dài ≥ 32 byte, nếu không API sẽ không khởi động
JWT_SECRET=change-me-to-at-least-32-bytes-long!!
```

Sinh secret ngẫu nhiên thật (PowerShell):

```powershell
$b = New-Object byte[] 48
[System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($b)
[Convert]::ToBase64String($b)
```

## Bước 2 — Chạy stack

```bash
make up
```

Lệnh này build image, khởi động `db` + `redis` + `api`, **tự áp migration**, và đợi
tới khi mọi container báo `healthy` rồi mới trả về.

Kiểm tra:

```bash
curl http://localhost:8080/healthz     # {"status":"alive"}
curl http://localhost:8080/readyz      # {"status":"ready","checks":{...}}
```

`/readyz` chỉ `200` khi PostgreSQL ping được, schema đã migrate và Redis ping được.

## Bước 3 — Thử luồng đăng ký

Email được ghi ra log khi `SMTP_HOST` rỗng, nên lấy OTP từ log:

```bash
docker compose logs -f api
```

```bash
# 1. Đăng ký -> 202, mã OTP nằm trong log
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@example.com","password":"Str0ng!Pass"}'

# 2. Xác nhận OTP
curl -X POST http://localhost:8080/api/v1/auth/verify-email \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@example.com","otp":"123456"}'

# 3. Đăng nhập -> nhận accessToken + refreshToken
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@example.com","password":"Str0ng!Pass"}'

# 4. Gọi endpoint cần token
curl http://localhost:8080/api/v1/auth/me -H "Authorization: Bearer $ACCESS_TOKEN"
```

Mọi chi tiết về 13 endpoint: [api-reference.md](../docs/api-reference.md).

## Bước 4 — Tài khoản admin

```bash
make seed-docker      # trong container
make seed             # nếu chạy Go trên máy
```

Đọc `ADMIN_EMAIL` / `ADMIN_PASSWORD` trong `.env`. Mật khẩu phải đạt chính sách:
≥ 8 ký tự, có chữ, có số, có ký tự đặc biệt.

## Dừng / dọn dẹp

```bash
make stop         # dừng container, giữ dữ liệu
make down         # xoá container + network, giữ volume
make down-all     # xoá cả volume -> MẤT database, phải migrate lại từ đầu
```

## Chế độ tools (debug DB, xem email)

```bash
make up-tools
```

Lệnh này dùng thêm `docker-compose.dev.yml`: mở port PostgreSQL/Redis trên
`127.0.0.1` và thêm Mailpit.

| Dịch vụ | Địa chỉ | Tài khoản |
|---|---|---|
| API | `http://localhost:8080` | — |
| Mailpit (web UI) | `http://localhost:8025` | — |
| PostgreSQL | `localhost:5432` | user `app`, db `artist_shop`, password `app` |
| Redis | `localhost:6379` | password trong `.env` → `REDIS_PASSWORD` |

Hai lệnh hỗ trợ:

```bash
make db-shell      # psql trong container
make redis-cli     # redis-cli trong container (đã tự đăng nhập)
make mail          # in email Mailpit bắt được
```

### Bật Mailpit để nhận email thật

Mặc định app **ghi email ra log** (`LogSender`). Muốn nhận qua SMTP thật, sửa `.env`:

```ini
SMTP_HOST=mailpit
SMTP_PORT=1025
```

rồi `docker compose up -d --force-recreate api`. Mã OTP sẽ nằm trong hộp thư web
`http://localhost:8025`.

## Chạy bằng Go trên máy (không Docker hoá app)

```bash
make env
docker compose up -d db redis   # chỉ hạ tầng
make run                        # go run ./cmd/api
```

Ở chế độ này app dùng `DATABASE_URL` / `REDIS_ADDR` trong `.env` (trỏ `localhost`),
nên không dùng file `docker-compose.dev.yml`.

## Bước tiếp theo

- Muốn hiểu cấu hình đầy đủ → [configuration.md](configuration.md)
- Muốn hiểu Docker bên trong → [docker.md](docker.md)
- Muốn chạy đúng những gì CI chạy → [testing.md](testing.md)
- Gặp lỗi → [troubleshooting.md](troubleshooting.md)
