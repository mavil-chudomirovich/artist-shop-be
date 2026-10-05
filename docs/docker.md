# Docker

Repo có 4 file Docker:

| File | Vai trò |
|---|---|
| `Dockerfile` | Build image cho cả 3 command (`api`, `migrate`, `seed`) |
| `.dockerignore` | Loại file khỏi build context |
| `docker-compose.yml` | Stack mặc định: `db` + `redis` + `api` |
| `docker-compose.dev.yml` | Override cho dev: mở port, thêm Mailpit |

---

## 1. `Dockerfile`

Multi-stage, 2 tầng.

### Tầng build

```dockerfile
FROM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download          # ← tách riêng để tận dụng Docker cache

COPY . .

ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/api    ./cmd/api && \
    go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate && \
    go build -trimpath -ldflags="-s -w" -o /out/seed    ./cmd/seed
```

| Quyết định | Tại sao |
|---|---|
| `COPY go.mod go.sum` **trước**, `go mod download` **trước** `COPY . .` | Sửa code không làm mất cache tải dependency |
| `CGO_ENABLED=0` | Binary tĩnh, chạy được trên alpine (không cần glibc) |
| `GOOS=linux` | Build trên Windows/macOS vẫn ra binary Linux |
| `-trimpath` | Bỏ đường dẫn máy dev khỏi binary ( reproducible ) |
| `-ldflags="-s -w"` | Bỏ symbol table + DWARF, giảm ~25% kích thước |
| Build **3 binary, 1 image** | `migrate`/`seed` chỉ khác `ENTRYPOINT`; tách image riêng là lãng phí |

### Tầng runtime

```dockerfile
FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 -h /app app

WORKDIR /app
COPY --from=build /out/api    /app/api
COPY --from=build /out/migrate /app/migrate
COPY --from=build /out/seed    /app/seed

USER app
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=20s --retries=5 \
    CMD wget -qO- http://127.0.0.1:8080/readyz > /dev/null || exit 1
ENTRYPOINT ["/app/api"]
```

| Quyết định | Tại sao |
|---|---|
| `ca-certificates` | SMTP/HTTPS cần trust store |
| `tzdata` | Timestamp đúng múi giờ nếu deploy ngoài UTC |
| `adduser -u 10001` + `USER app` | Không chạy root |
| `HEALTHCHECK` dùng `wget` | `alpine` có busybox `wget` sẵn — không cài thêm gói |
| `--start-period=20s` | Cho app thời gian chạy migration trước khi bị coi là chết |
| `ENTRYPOINT` không có `CMD` | `migrate`/`seed` override bằng `entrypoint:` trong compose |

### Vì sao image không chứa thư mục `migrations/`

SQL migration được nhúng vào binary:

```go
// migrations/embed.go
//go:embed *.sql
var FS embed.FS

// internal/share/database/migrate/migrate.go
goose.SetBaseFS(migrations.FS)
```

Nhờ vậy image runtime chỉ có 3 file binary, và `cmd/migrate` chạy được ở bất kỳ đâu
không cần volume mount. Đổi ý này là điểm cần cân nhắc nếu sau này muốn chạy
migration bằng file ngoài (hiện tại không cần).

---

## 2. `.dockerignore`

Không chỉ để giảm context — mà còn để **tránh lộ secret**:

```
.git .github .gitignore .gitattributes .dockerignore Dockerfile docker-compose*.yml
.specify .opencode specs doc
bin dist coverage.out *.log
.env .env.*  !.env.example
.idea .vscode *.swp .DS_Store Thumbs.db
```

| Dòng | Tại sao quan trọng |
|---|---|
| `.env`, `.env.*` | Nếu sót, secret của máy dev nằm trong build cache/layer |
| `!.env.example` | Cho phép copy file mẫu vào image nếu sau này cần |
| `specs`, `doc`, `.specify` | Không ảnh hưởng build |
| **Không loại `migrations/`** | `go:embed` cần |

---

## 3. `docker-compose.yml` — stack mặc định

```yaml
name: artist-shop

x-db-url: &db-url postgres://app:app@db:5432/artist_shop?sslmode=disable

x-common-env: &common-env
  env_file:
    - path: .env
      required: false
  environment:
    APP_ENV: ${APP_ENV:-development}
    DATABASE_URL: *db-url
```

### Service `db`
- Image `postgres:16`, volume `pgdata`.
- Healthcheck `pg_isready` với `interval 5s`, `retries 12`, `start_period 10s`.
- **Không** publish port.

### Service `redis`
```yaml
command:
  - sh
  - -c
  - exec redis-server --appendonly yes --requirepass "$$REDIS_PASSWORD"
environment:
  REDIS_PASSWORD: ${REDIS_PASSWORD:-redis-dev-password}
  REDISCLI_AUTH: ${REDIS_PASSWORD:-redis-dev-password}
```
| Quyết định | Tại sao |
|---|---|
| `--requirepass` | Có password, lấy từ `.env` |
| `--appendonly yes` + volume `redisdata` | Không mất OTP/blacklist khi restart |
| `$$REDIS_PASSWORD` | `$$` để **make** không đội vào; container shell mới expand |
| `REDISCLI_AUTH` | `redis-cli` tự đọc biến này ⇒ `docker compose exec redis redis-cli` chạy được mà **không cần `-a`**, không lộ secret trên dòng lệnh |

### Service `api`
- `depends_on: db (service_healthy)` + `redis (service_healthy)` ⇒ chỉ khởi động
  sau khi hạ tầng thật sự sẵn sàng.
- `ports: 127.0.0.1:${API_PORT:-8080}:8080` — chỉ bind loopback.
- `environment` ghi đè `DATABASE_URL` → host `db`, `REDIS_ADDR` → `redis:6379`.
- `restart: unless-stopped`.

### Service `migrate` và `seed`
Cùng image, override entrypoint, `profiles: [tools]`, `restart: "no"`.

```yaml
migrate:
  entrypoint: ["/app/migrate"]
  command: ["up"]
  <<: *common-env      # ← anchor: không có dòng này thì "DATABASE_URL is required"
  profiles: [tools]
```

Ba bài học từ lỗi thật:

| Vấn đề đã gặp | Nguyên nhân | Cách sửa |
|---|---|---|
| `seed: DATABASE_URL is required` | Chỉ `api` có `env_file` | YAML anchor `x-common-env` gắn cho cả 3 service |
| `migrate`/`seed` chạy mỗi lần `up`, và `seed` fail khi chưa có `ADMIN_*` | Không có profile | Đặt sau `profiles: [tools]`; `docker compose run --rm seed` tự bật profile nên `make seed-docker` vẫn chạy |
| `mailpit` không nhận email dù đã bật | `SMTP_HOST` chỉ nằm trong `env_file`, không được compose nội suy | Sửa `.env` (xem bên dưới) |

### Tại sao `db`/`redis` không publish port
Máy dev thường đã có PostgreSQL/Redis riêng, hoặc đang chạy container của dự án
khác. Publish `0.0.0.0:5432` sẽ conflict ngay. Port chỉ mở ở `docker-compose.dev.yml`.

---

## 4. `docker-compose.dev.yml` — lớp override

```bash
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d
# hoặc: make up-tools
```

Chỉ 3 phần: publish port `db`/`redis` trên `127.0.0.1`, và service `mailpit`
(UI 8025, SMTP 1025).

**Vì sao tách file riêng:** Compose không hỗ trợ "publish port có điều kiện". Nếu
để trong file chính thì port luôn mở — trái với chủ ý "chỉ mở khi cần debug". Lớp
override là cách chuẩn, và `make up-tools` gói sẵn cả hai file nên người dùng chỉ
cần nhớ một lệnh.

`mailpit` có `MP_SMTP_AUTH_ACCEPT_ANY=1` và `MP_SMTP_AUTH_ALLOW_INSECURE=1` vì ở
dev app gửi SMTP không có user/password.

---

## 5. Khác biệt giữa chạy Docker và chạy Go local

Cùng một file `.env`, vì compose ghi đè hai giá trị hướng host:

| Biến | `make run` (Go local) | `make up` (Docker) |
|---|---|---|
| `DATABASE_URL` | `...@localhost:5432/...` | `...@db:5432/...` (compose override) |
| `REDIS_ADDR` | `localhost:6379` | `redis:6379` (compose override) |
| `REDIS_PASSWORD` | từ `.env` | từ `.env` qua `env_file`, và qua compose |

Thứ tự ưu tiên của Compose: `environment:` > `env_file` > biến môi trường của
shell. Vì vậy `docker compose` **không** nội suy được biến chỉ tồn tại trong
`env_file` — biến đó phải được khai báo trong `environment:` của file compose, hoặc
đơn giản là sửa `.env`.

---

## 6. Lệnh thường dùng

```bash
make up            # build + start db/redis/api + đợi healthy
make up-tools      # thêm port host + Mailpit
make ps            # trạng thái
make logs          # log API
make rebuild       # build lại không cache
make stop          # stop, giữ container
make down          # xoá container + network, giữ volume
make down-all      # xoá cả volume (MẤT dữ liệu)
make db-shell      # psql
make redis-cli     # redis-cli (tự đăng nhập)
make mail          # email Mailpit bắt được
make seed-docker   # provision admin
make migrate-up-docker / make migrate-status-docker
make docker-build  # build image, tag artist-shop-be:<git-sha>
```

Xem thêm: [makefile.md](makefile.md).

---

## 7. Vòng đời migration trong container

1. `make up` → container `api` khởi động.
2. `MIGRATIONS_AUTO_APPLY=true` ⇒ `cmd/api` tự chạy migration.
3. Migration được bảo vệ bằng **PostgreSQL advisory lock**, nên nhiều replica khởi
   động cùng lúc vẫn chỉ apply một lần.
4. `start_period 20s` của HEALTHCHECK cho app đủ thời gian.
5. `/readyz` chỉ `200` khi `runner.Version() > 0` — tức schema đã có migration.

`make migrate-up-docker` chạy `cmd/migrate up` trong container một lần, hữu ích khi
muốn migrate **trước** khi app lên (CI, deploy).

---

## 8. Deploy

Hiện **chưa** có file deploy trong repo (không có `docker-compose.prod.yml`). Khi
làm, dự kiến:

- `.env.production` không commit; secret từ secret store.
- `APP_ENV=production` ⇒ `LoadDotenv()` bị bỏ qua, buộc phải truyền biến thật.
- `POSTGRES_PASSWORD`, `REDIS_PASSWORD`, `JWT_SECRET` đặt lại, không dùng giá trị dev.
- `HTTP_TRUSTED_PROXIES` đặt theo dải IP của reverse proxy.
- `MIGRATIONS_AUTO_APPLY=false` + chạy `migrate` một lần như job trước khi deploy app.

Các mục này nằm ngoài phạm vi hiện tại; xem `docs/architecture.md` cho kiến trúc tổng.
