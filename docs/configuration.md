# Cấu hình

Toàn bộ cấu hình đến từ **biến môi trường**. Không có hằng số phụ thuộc môi trường
trong code.

## Nguồn cấu hình

| Nguồn | Dùng khi nào |
|---|---|
| `.env` ở thư mục gốc | Local: `make run`, và container (qua `env_file`) |
| Biến môi trường của hệ thống | Production, CI, khi chạy container thủ công |
| `docker-compose.yml` | **Ghi đè** hai giá trị hướng host khi chạy trong Docker |

Nguồn chuẩn là `internal/share/config/config.go`. Quy trình:

```go
config.LoadDotenv()   // nạp .env, BỎ QUA nếu APP_ENV=production
config.Load()         // env.Parse + Validate
config.ValidateForAPI()  // chỉ cmd/api gọi
```

Điểm quan trọng: `LoadDotenv()` **không** ghi đè biến đã có sẵn trong môi
trường, và bị bỏ qua hoàn toàn khi `APP_ENV=production`. Đây là lý do CI không cần
file `.env`.

## Ba mức "bắt buộc"

| Mức | Nghĩa | Kiểm tra ở đâu |
|---|---|---|
| Bắt buộc (validate chung) | Thiếu là `Load()` fail, mọi command đều không chạy | `Validate()` |
| Bắt buộc (chỉ API) | `migrate`/`seed` vẫn chạy được | `ValidateForAPI()` |
| Có mặc định | Không cần khai báo | `envDefault` trong struct |

## Bảng biến đầy đủ

### Runtime
| Biến | Mặc định | Bắt buộc | Ý nghĩa |
|---|---|---|---|
| `APP_ENV` | `development` | | `development` \| `staging` \| `production`. Khi là `production` thì **không** nạp `.env` |

### HTTP server — tiền tố `HTTP_`
| Biến | Mặc định | Bắt buộc | Ý nghĩa |
|---|---|---|---|
| `HTTP_ADDR` | `:8080` | ✅ | Địa chỉ lắng nghe |
| `HTTP_READ_TIMEOUT` | `10s` | | |
| `HTTP_WRITE_TIMEOUT` | `15s` | | |
| `HTTP_IDLE_TIMEOUT` | `60s` | | |
| `HTTP_SHUTDOWN_TIMEOUT` | `10s` | | Thời gian chờ graceful shutdown |
| `HTTP_TRUSTED_PROXIES` | *(rỗng)* | | IP/CIDR được phép set `X-Forwarded-For`. **Rỗng = không tin header** |

> `HTTP_TRUSTED_PROXIES` quyết định rate limit theo IP có đúng không. Nếu chạy sau
> reverse proxy mà để rỗng, mọi request đều mang IP của proxy ⇒ rate limit và
> lockout đăng nhập theo IP sẽ gộp chung. Khi đó đặt đúng dải IP của proxy.

### PostgreSQL
| Biến | Mặc định | Bắt buộc | Ý nghĩa |
|---|---|---|---|
| `DATABASE_URL` | — | ✅ | DSN PostgreSQL |
| `DB_MAX_CONNS` | `10` | ✅ > 0 | Kích thước connection pool |
| `DB_MIN_CONNS` | `1` | ✅ 0..Max | Số kết nối giữ sẵn |
| `DB_CONNECT_TIMEOUT` | `5s` | | Timeout ping lúc khởi động |

### Logging — tiền tố `LOG_`
| Biến | Mặc định | Bắt buộc | Ý nghĩa |
|---|---|---|---|
| `LOG_LEVEL` | `info` | ✅ | `debug` \| `info` \| `warn` \| `error` |

Log là JSON, có `correlation_id`, và tự redact các key nhạy cảm
(`password`, `jwt_secret`, `refresh_token`, `database_url`, … —
xem `internal/share/logging/redact.go`).

### CORS — tiền tố `CORS_`
| Biến | Mặc định | Bắt buộc | Ý nghĩa |
|---|---|---|---|
| `CORS_ALLOWED_ORIGINS` | *(rỗng)* | | Danh sách origin, phân tách bằng dấu phẩy |

### Rate limit — tiền tố `RATE_LIMIT_`
| Biến | Mặc định | Bắt buộc | Ý nghĩa |
|---|---|---|---|
| `RATE_LIMIT_RPS` | `20` | ✅ > 0 | Request/giây mỗi client, theo nhóm `read`/`write`/`auth` |
| `RATE_LIMIT_BURST` | `40` | ✅ > 0 | Burst cho phép |

### Request & audit
| Biến | Mặc định | Bắt buộc | Ý nghĩa |
|---|---|---|---|
| `MAX_BODY_BYTES` | `4194304` | ✅ > 0 | Trần thô cho mọi body. Route avatar áp trần chính xác 2 MB (FR-015); giá trị này chỉ chặn sớm một body quá lớn, không phải trần nghiệp vụ |
| `AUDIT_QUEUE_SIZE` | `1024` | ✅ > 0 | Hàng đợi ghi `audit_logs` |
| `AUDIT_MAX_RETRIES` | `5` | | Số lần thử lại khi ghi audit thất bại |

> Hàng đợi audit chạy bất đồng bộ trong process. Nếu sập giữa lúc ghi, event có
> thể mất; thiết kế hiện tại ghi log cảnh báo khi phải reconcile thủ công.

### Redis
| Biến | Mặc định | Bắt buộc | Ý nghĩa |
|---|---|---|---|
| `REDIS_ADDR` | `localhost:6379` | ✅ (chỉ API) | Lưu OTP, blacklist token, login guard |
| `REDIS_PASSWORD` | *(rỗng)* | | |
| `REDIS_DB` | `0` | | |

### Auth — token
| Biến | Mặc định | Bắt buộc | Ý nghĩa |
|---|---|---|---|
| `JWT_SECRET` | — | ✅ **≥ 32 byte** (chỉ API) | Khóa ký HS256 |
| `ACCESS_TOKEN_TTL` | `15m` | ✅ > 0 | |
| `REFRESH_TOKEN_TTL` | `1080h` (45 ngày) | ✅ > 0 | |

### Auth — OTP
| Biến | Mặc định | Bắt buộc | Ý nghĩa |
|---|---|---|---|
| `OTP_TTL` | `15m` | ✅ > 0 | |
| `OTP_MAX_ATTEMPTS` | `3` | ✅ > 0 | Số lần sai trước khi khoá mã |
| `OTP_BLOCK_TTL` | `60s` | | Thời gian khoá mã |
| `OTP_RESEND_COOLDOWN` | `60s` | | Thời gian chờ giữa 2 lần gửi lại |

### Auth — reset mật khẩu
| Biến | Mặc định | Bắt buộc | Ý nghĩa |
|---|---|---|---|
| `PASSWORD_RESET_TTL` | `30m` | ✅ > 0 | |

### Auth — rate limit riêng
| Biến | Mặc định | Bắt buộc | Ý nghĩa |
|---|---|---|---|
| `AUTH_LOGIN_RATE_PER_MINUTE` | `10` | ✅ > 0 | Hạn mức `POST /auth/login` |
| `AUTH_FLOW_RATE_PER_MINUTE` | `5` | ✅ > 0 | Hạn mức nhóm flow (đăng ký, OTP, reset) |
| `AUTH_LOGIN_MAX_FAILURES` | `10` | ✅ > 0 | Số lần sai trước khi khoá tài khoản |
| `AUTH_LOGIN_LOCKOUT_TTL` | `15m` | ✅ > 0 | Thời gian khoá |

### Auth — email
| Biến | Mặc định | Bắt buộc | Ý nghĩa |
|---|---|---|---|
| `SMTP_HOST` | *(rỗng)* | | Rỗng ⇒ `LogSender` in email ra log |
| `SMTP_PORT` | `587` | | |
| `SMTP_USERNAME` | *(rỗng)* | | |
| `SMTP_PASSWORD` | *(rỗng)* | | |
| `SMTP_FROM` | `no-reply@artist-shop.local` | | |

### Auth — admin seed (chỉ `cmd/seed`)
| Biến | Mặc định | Bắt buộc | Ý nghĩa |
|---|---|---|---|
| `ADMIN_EMAIL` | *(rỗng)* | ✅ cho seed | |
| `ADMIN_PASSWORD` | *(rỗng)* | ✅ cho seed | Phải đạt chính sách mật khẩu |

### Migrations — tiền tố `MIGRATIONS_`
| Biến | Mặc định | Bắt buộc | Ý nghĩa |
|---|---|---|---|
| `MIGRATIONS_AUTO_APPLY` | `true` | | API tự áp migration lúc khởi động |
| `MIGRATIONS_LOCK_TIMEOUT` | `30s` | | Timeout chờ advisory lock |

## Biến riêng của Docker Compose

Các biến này **không** đọc bởi code Go; chúng chỉ nằm trong `docker-compose*.yml` và
cho phép đổi cấu hình stack mà không sửa file:

| Biến | Mặc định | Ảnh hưởng |
|---|---|---|
| `POSTGRES_USER` / `POSTGRES_PASSWORD` / `POSTGRES_DB` | `app` / `app` / `artist_shop` | Container `db` + DSN |
| `API_PORT` | `8080` | Port host của API |
| `DB_PORT` / `REDIS_PORT` | `5432` / `6379` | Port host (chỉ trong `.dev.yml`) |
| `MAILPIT_UI_PORT` / `MAILPIT_SMTP_PORT` | `8025` / `1025` | Port Mailpit |

> `POSTGRES_PASSWORD` mặc định là `app` và **không** dùng `REDIS_PASSWORD` cho
> PostgreSQL. Với môi trường thật, hãy đặt lại cả hai.

## Dataset hành chính (module user)

Dữ liệu tỉnh/phường là **dữ liệu tham chiếu nhúng trong binary**, không phải cấu
hình và không nằm trong PostgreSQL — xem
[ADR 002](decisions/002-administrative-dataset-embedded.md). Vì vậy nó không có
biến môi trường nào.

| Mục | Giá trị |
|---|---|
| File | `internal/share/administrative/data/vn-divisions.json` |
| Nguồn đã ghi | `https://github.com/open-admin-data/vietnam-administrative-divisions`, file `data/hierarchy.json` |
| Giấy phép | CC-BY-4.0, ghi công `Open Admin Data` |
| Kỳ hiệu lực | `2025-07-01`, chỉ **2 cấp** `tỉnh → phường/xã`; cấp huyện đã bị bãi bỏ toàn quốc |
| Số lượng | 34 tỉnh, 3321 phường — ghi ở khối `counts` của chính file đó |

Khối `_provenance` trong file lưu nguồn, giấy phép, ngày lấy và kỳ hiệu lực. Đó là
nguồn sự thật cho quy trình dưới đây.

### Quy trình làm mới dataset

1. **Lấy lại dataset từ nguồn đã ghi trong `_provenance`** (mục trên), rồi cập nhật
   `retrieved` và `effectiveFrom` cho đúng với bản mới.
2. **Sinh lại toàn bộ file, không sửa tay.** File là ảnh chụp của nguồn; sửa tay một
   dòng là mất khả năng đối chiếu với nguồn, và test kiểm tra số lượng sẽ không còn
   ý nghĩa.
3. Chạy `make test`. Test `internal/share/administrative/administrative_test.go`
   khẳng định: số tỉnh/phường khớp khối `counts`, không mã tỉnh hay mã phường nào
   trùng, mỗi phường thuộc **đúng một** tỉnh, và file là UTF-8 không BOM với xuống
   dòng LF. Sai số lượng ⇒ test đỏ, không được merge.
4. **Mỗi lần làm mới đi kèm một bản phát hành.** Dataset nằm trong binary, nên đổi
   dataset mà không phát hành lại build là một thay đổi không có tác dụng.
5. Địa chỉ đã lưu không cần migrate: nó giữ **mã** tỉnh/phường cùng **tên đã chụp**
   khi lưu, nên vẫn hiển thị đúng sau khi tên đơn vị hành chính thay đổi.

## Secret

| Quy tắc | Nơi kiểm tra |
|---|---|
| Không commit `.env` | `.gitignore` chặn `.env` |
| `.env.example` chỉ chứa placeholder | Không có giá trị thật |
| Không bake secret vào Docker layer | `Dockerfile` không `ARG`/`ENV` secret |
| Không lộ secret trên dòng lệnh | `redis-cli` dùng `REDISCLI_AUTH` từ env container |
| Không lộ secret trong log | `logging.IsSensitiveKey` redact theo tên key |

Khi sinh secret cho môi trường thật, dùng biến môi trường / secret store của
hạ tầng, không dùng file trong repo.

## Đổi cấu hình

```bash
# Sửa tay
notepad .env

# Sinh lại JWT_SECRET ngẫu nhiên
make stop && (xoá dòng JWT_SECRET trong .env, chèn giá trị mới) && make up
```

Đổi `.env` thì container cần được tạo lại để nhận giá trị mới:

```bash
docker compose up -d --force-recreate api
```

Lưu ý: đổi `JWT_SECRET` làm **mọi access token đang cấp trở nên không hợp lệ**
(client phải đăng nhập lại); refresh token vẫn dùng được vì nó là opaque và lưu ở
PostgreSQL.
