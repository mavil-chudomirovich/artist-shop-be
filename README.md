# artist-shop-be

Backend cho nền tảng Artist Shop: modular monolith Go, PostgreSQL, Redis, REST
API dưới `/api/v1`.

| Tài liệu | Nội dung |
|---|---|
| `docs/api-reference.md` | **Toàn bộ endpoint hiện có** — cập nhật bắt buộc khi thêm API |
| `docs/architecture.md` | Kiến trúc chuẩn |
| `docs/modules.md` | Danh sách module & lộ trình triển khai |
| `.specify/memory/constitution.md` | Hiến pháp dự án (quy tắc bắt buộc) |

## Yêu cầu

- Docker (chạy PostgreSQL, Redis và API)
- Go 1.26+ (chỉ cần khi chạy `make run` bằng Go trên máy, hoặc chạy test)
- `make` — có sẵn trong Git Bash / WSL / Linux; trên Windows dùng
  [make](https://www.gnu.org/software/make/) hoặc `choco install make`

## Chạy bằng Docker (khuyến nghị)

```bash
make up          # db + redis + api, không mở port db/redis ra host
make ps          # trạng thái container
make logs        # log API
make down        # dừng, giữ dữ liệu
make down-all    # dừng và XOÁ dữ liệu
```

Mở `http://localhost:8080/healthz`. Lần chạy đầu, `make up` sẽ build image và
tự áp migration (`MIGRATIONS_AUTO_APPLY=true`).

### Chế độ tools (cần thao tác DB / xem email)

```bash
make up-tools
```

Lệnh này dùng thêm `docker-compose.dev.yml`, mở port trên `127.0.0.1` và bật
Mailpit:

| Dịch vụ | Địa chỉ | Tài khoản |
|---|---|---|
| API | `http://localhost:8080` | — |
| Mailpit (web UI) | `http://localhost:8025` | — |
| PostgreSQL | `localhost:5432` | user `app`, db `artist_shop`, password `app` |
| Redis | `localhost:6379` | password `redis-dev-password` |

Để nhận email thật (OTP, link đặt lại mật khẩu) thay vì đọc log, sửa `.env`:

```ini
SMTP_HOST=mailpit
SMTP_PORT=1025
```

rồi `docker compose up -d --force-recreate api`. Xem email: `make mail`.

### Tài khoản admin

```bash
ADMIN_PASSWORD='<mật-khẩu-mạnh>' make seed-hash   # in hash Argon2id
# dán email + hash vào scripts/seed.vars.local.psql (xem scripts/seed_admin.sql)
make seed-sql
```

Tài khoản đọc từ `ADMIN_EMAIL` trong `.env`; mật khẩu không nằm trong config.

## Chạy bằng Go trên máy

```bash
make env      # tạo .env từ .env.example (chỉ chạy 1 lần)
make up       # hoặc chỉ db + redis: docker compose up -d db redis
make run      # go run ./cmd/api
```

## Cấu hình

Toàn bộ cấu hình đến từ biến môi trường; `.env` ở thư mục gốc là nguồn duy nhất
cho local (được `.gitignore` chặn). Xem `.env.example` để biết đầy đủ biến và
mặc định.

- `JWT_SECRET` phải dài **≥ 32 byte**, nếu không API sẽ không khởi động.
- `APP_ENV=production` sẽ bỏ qua việc nạp `.env` (chỉ dùng biến thật từ hệ thống).
- `cmd/migrate` và `cmd/hash-password` không cần `JWT_SECRET`/`REDIS_ADDR`.

## Lệnh thường dùng

```bash
make help              # index các nhóm lệnh
make dev-help          # lệnh phát triển
make quality-help      # kiểm tra chất lượng
make docker-help       # lệnh Docker
make migration-help    # lệnh migration

make check             # chạy đúng những gì CI chạy (fmt, tidy, vet, lint, test, race, integration, build)
make test              # unit test
make test-integration  # integration test (cần Docker, tự tạo PostgreSQL)
make lint              # golangci-lint (CI dùng v2.14.0, `make install-tools` để cài)
```

## Triển khai

Production chạy Docker Compose trên VPS dùng chung, port `8083`/`8445`:

```bash
# Trên VPS, sau khi có .env.production và .env.deploy:
make docker-prod-up
make docker-prod-migrate
```

- `.github/workflows/cd.yml` build image `linux/amd64`, push GHCR, pin digest vào
  `API_IMAGE` trong `/opt/artist-shop/.env.deploy`, chạy migration rồi rollout
  `api` và recreate nginx. Trigger: tag `v*` hoặc dispatch thủ công.
- `docker-compose.prod.yml` là stack production; `nginx/prod.conf` là reverse
  proxy + TLS; `.env.production.example` là mẫu biến và secret.
- TLS cấp bằng lego DNS-01 (Vercel) qua `deployments/scripts/issue-certs.sh`;
  gia hạn bằng `renew-certs.sh` qua cron.
- Runbook đầy đủ (chuẩn bị VPS, cert, seed admin, backup, rollback,
  troubleshooting) nằm ngoài repo tại `../Docs/deploy/README.md`.
- Không chạy `docker compose down -v` trên production; thao tác đó xoá database.

## Kiến trúc

Modular monolith: mỗi nghiệp vụ là một module dưới `internal/modules/<module>` với
4 lớp `domain → application → infrastructure → presentation`. Mã dùng chung nằm
ở `internal/share`, migration tập trung ở `migrations/`. Chi tiết:
`docs/architecture.md`.
