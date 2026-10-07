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

### User — rate limit riêng của module — tiền tố `USER_`
| Biến | Mặc định | Bắt buộc | Ý nghĩa |
|---|---|---|---|
| `USER_AVATAR_UPLOAD_RATE_PER_HOUR` | `10` | ✅ > 0 | Hạn mức `POST /users/me/avatar`, tính theo IP |
| `USER_ADDRESS_WRITE_RATE_PER_MINUTE` | `30` | ✅ > 0 | Hạn mức ghi địa chỉ (`POST`/`PATCH`/`DELETE /users/me/addresses*`), tính theo IP |

Hai biến này tạo **hai bucket riêng**, không dùng chung bộ đếm với nhau và không dùng
chung với `RATE_LIMIT_RPS`; cả hai vẫn cộng dồn trên hạn mức toàn cục. Mục đích là
ngăn một endpoint ghi nặng chiếm hết tài nguyên: lặp lại upload avatar chính là
dịch vụ media bị tấn công, và lặp lại ghi địa chỉ là bảng địa chỉ bị tấn công
(FR-025). Các endpoint đọc của module không có hạn mức riêng.

### Request & audit
| Biến | Mặc định | Bắt buộc | Ý nghĩa |
|---|---|---|---|
| `MAX_BODY_BYTES` | `4194304` | ✅ > 0 | Trần thô cho mọi body, chạy **trước routing** nên route không nâng được. Phải ≥ trần mà route avatar áp (**2 162 688** = 2 MB ảnh + 64 KB đệm `multipart`), và ≥ mọi trần riêng khác. Giá trị này chỉ chặn sớm một body quá lớn, không phải trần nghiệp vụ. **Ràng buộc này được kiểm tra lúc khởi động** — xem khối *Ràng buộc với trần avatar* ngay dưới |
| `AUDIT_QUEUE_SIZE` | `1024` | ✅ > 0 | Hàng đợi ghi `audit_logs` |
| `AUDIT_MAX_RETRIES` | `5` | | Số lần thử lại khi ghi audit thất bại |

> Hàng đợi audit chạy bất đồng bộ trong process. Nếu sập giữa lúc ghi, event có
> thể mất; thiết kế hiện tại ghi log cảnh báo khi phải reconcile thủ công.

#### Ràng buộc với trần avatar

`MAX_BODY_BYTES` không độc lập với trần của route avatar. Pipeline bọc body **trước
routing**, nên một route **không thể nâng** trần này; và route avatar cần một trần
riêng lớn hơn (ảnh 2 MB cộng 64 KB đệm `multipart` = **2 162 688 byte**). Nếu
`MAX_BODY_BYTES` thấp hơn con số đó, **mọi** upload avatar bị chặn ở tầng pipeline
trước khi route kịp áp luật của nó, và lý do trả về không chỉ ra biến nào cần sửa.

Vì vậy mối ràng buộc này **được thực thi, không chỉ được mô tả**: composition root
(`cmd/api/main.go`) gọi `RequireAvatarUploadCeiling` **trước khi** mở database, chạy
migration hay mở cổng. Con số `2 162 688` được đọc từ chính hàm mà route dùng
(`AvatarUploadCeiling`), nên kiểm tra không thể trôi khỏi giới hạn thật.

| Cấu hình | Kết quả lúc khởi động |
|---|---|
| Có media **và** `MAX_BODY_BYTES` **thấp hơn** 2 162 688 | **Từ chối khởi động**, exit code khác 0, in ra `fatal:` kèm tên biến và cả hai giá trị |
| Có media **và** `MAX_BODY_BYTES` **bằng** 2 162 688 | Khởi động bình thường — trần của route là ràng buộc thật sự tại điểm đó |
| Có media **và** `MAX_BODY_BYTES` **lớn hơn** | Khởi động bình thường |
| **Không** có media, bất kỳ giá trị nào | Khởi động bình thường, kèm một dòng cảnh báo |

Một triển khai không có media **không** bị chặn bởi một trần chỉ ảnh hưởng tới upload:
khi thiếu credential, không upload nào thành công được, nên trần này không liên quan và
dòng cảnh báo ghi rõ hệ quả đó.

> **Đừng nâng `MAX_BODY_BYTES` tuỳ tiện.** Nâng nó chỉ nới những gì *mọi* endpoint khác
> chấp nhận, theo một con số không liên quan tới chúng. Rate limit, các trần đọc và quy
> tắc theo field vẫn là lớp bảo vệ thật; `MAX_BODY_BYTES` tồn tại để chặn sớm một body
> quá lớn thay vì đệm nó.
>
> Chi tiết về quyết định này: [ADR 010](decisions/010-avatar-upload-refusal-and-startup-guard.md).

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
| `SMTP_HOST` | *(rỗng)* | | Rỗng thì `LogSender` in email ra log; đặt host để gửi thật. Xem *Phân tầng file env* bên dưới |
| `SMTP_PORT` | `587` | | |
| `SMTP_USERNAME` | *(rỗng)* | | |
| `SMTP_PASSWORD` | *(rỗng)* | | |
| `SMTP_FROM` | `no-reply@artist-shop.local` | | Địa chỉ người gửi. Provider thường **phải được cho phép gửi từ địa chỉ này** — xem [Xác minh với nhà cung cấp thật](#xác-minh-với-nhà-cung-cấp-thật) |

### Phân tầng file env

Cấu hình được nạp theo thứ tự sau, **cái sau không đè cái trước**:

1. **Biến môi trường thật** của tiến trình (nền tảng deploy, hoặc `environment:` của
   compose) — thắng tất cả.
2. **`.env.local`** — override cho riêng máy này. Git-ignored, không bao giờ được commit.
3. **`.env`** — giá trị của môi trường, dùng chung.

Cả hai file đều **không được đọc khi `APP_ENV=production`**: ở đó nền tảng cấp biến môi
trường, và một file lỡ còn sót lại không được phép đổi hướng bất cứ thứ gì.

**Vì sao cần lớp giữa.** Một máy dev cần trỏ mail vào sink local (Mailpit) mà `.env` lại
giữ credential của nhà cung cấp cho deployment. Sửa `.env` để dev nghĩa là mỗi lần đổi phải
sửa tay và có nguy cơ commit nhầm. `.env.local` giữ phần khác biệt đó tách rời:

```bash
# .env.local
SMTP_HOST=localhost
SMTP_PORT=1025
SMTP_USERNAME=
SMTP_PASSWORD=
```

Chỉ khai báo **phần cần khác**; giá trị chỉ `.env` có vẫn được dùng. Mẫu đầy đủ ở
`.env.example.local`.

| Nơi chạy API | `SMTP_HOST` | Vì sao |
|---|---|---|
| Trên host (`make run`) | `localhost` | Đặt trong `.env.local` |
| Trong container (`make up-tools`) | `mailpit` | `docker-compose.dev.yml` tự đặt, không cần `.env.local` |

Hostname khác nhau vì container gọi Mailpit bằng tên service còn host gọi bằng cổng đã
publish. Đó là lý do có hai cơ chế chứ không một.

Khi thư xác nhận không gửi được, `POST /auth/register` trả `503 SERVICE_UNAVAILABLE`
(xem [api-reference.md](api-reference.md) mục `3.1`) và ghi lại **một** phân loại của
hệ thống này, không phải câu chữ của provider:

| Dấu hiệu provider trả về | Phân loại | Nghĩa là gì với operator | Thử lại? |
|---|---|---|---|
| Không kết nối được host/port (`SMTP_HOST` sai, port sai, firewall chặn, provider sập) | `UNREACHABLE` | Cấu hình đường đi sai **hoặc** provider đang sập. Cân hai khả năng trước khi kết luận | Có, trong hạn mức |
| SMTP **4xx** (ví dụ `421` mailbox tạm bận) | `TRANSIENT` | Provider tạm thời không nhận. Thường tự hết | Có, trong hạn mức |
| SMTP **5xx** khi dùng địa chỉ người gửi chưa được cho phép, ví dụ `525 Unauthorized sending address`; hoặc credential sai (`535`), hoặc IP chưa được duyệt | `CONFIGURATION` | **Cấu hình sai của deployment này.** SMTP không có cách nào phân biệt "IP chưa duyệt" với "thư đó bị từ chối" mà không đọc câu của provider — nên cả hai cùng vào đây, và cả hai đều **không** thử lại được | Không |
| Provider từ chối chính thư đó | `REFUSED` | Thư bị chặn (nội dung, người nhận, chính sách) | Không |
| Lỗi không mang mã SMTP nào hệ thống này nhận biết | `UNKNOWN` | Chưa phân loại được; không có bằng chứng rằng thử lại sẽ giúp | Không |

Hệ thống **cố ý không đọc câu chữ** của provider để phân loại, chỉ đọc **mã trả lời**.
Lý do: provider đổi câu chữ giữa các bản phát hành, và việc bám vào câu chữ sẽ biến một
lỗi vĩnh viễn thành lỗi được thử lại chỉ vì một lần sửa câu — làm chậm mọi request gặp
nó. Đổi lại, log lúc phân loại chỉ mang mã của hệ thống này, nên đổi câu chữ của
provider không làm thay đổi điều operator thấy.

Hai dấu hiệu trên là loại **sự cố cấu hình**, không phải lỗi của service: hãy xử lý như
một hạng mục cấu hình.

#### Xác minh với nhà cung cấp thật

Hai kết quả dưới đây **chỉ quan sát được với provider thật**, và cả hai đều bị chặn cho
tới khi cấu hình của bạn đúng. Trước khi chạy, lấy đúng các giá trị sau:

| Cần lấy | Biến | Lấy ở đâu |
|---|---|---|
| **Tên cloud** của tài khoản media | `MEDIA_CLOUD_NAME` | Console media → **Settings → Account details → Cloud name** |
| API key và API secret | `MEDIA_API_KEY`, `MEDIA_API_SECRET` | Cùng trang đó, mục API keys |
| Folder nhận ảnh tải lên | `MEDIA_FOLDER` | Bạn tự đặt; không phải giá trị lấy từ provider |
| Địa chỉ người gửi đã được provider cho phép | `SMTP_FROM` | Console của provider email → mục **Sending Domains / Senders** → xác thực địa chỉ đó |
| Host, port, credential của SMTP | `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD` | Trang SMTP/API của provider |

> **`MEDIA_CLOUD_NAME` KHÔNG phải tên folder.** Đặt nhầm `MEDIA_CLOUD_NAME` bằng
> `MEDIA_FOLDER` là một cấu hình sai rất dễ mắc, và provider trả `401 Invalid cloud_name`.
> `MEDIA_FOLDER` là thư mục con **bên trong** cloud đã đăng nhập; nó chỉ điều hướng nơi
> lưu ảnh, không định danh tài khoản.

Sau khi lấy đủ giá trị:

| Cần xác minh | Cách kiểm | Đáp án đúng |
|---|---|---|
| Upload avatar thật thành công và ảnh được thu nhỏ | `curl -X POST $BASE/users/me/avatar -H "Authorization: Bearer $ACCESS" -F "file=@avatar.jpg"` | `200`, và `data.avatar.width` **≤ 512** |
| Thư xác nhận tới hộp thư thật | Đăng ký một địa chỉ bạn sở hữu, rồi đọc hộp thư | Mã xác nhận tới; verify-email trả `200` |

Cách phân biệt **lỗi cấu hình** với **sự cố provider**: trường `classification` trong dòng
log phân loại. Cả hai đều không chứa câu chữ của provider hay credential. Trường hợp media
đúng lại là dòng `media provider call failed` với `classification` là câu **của hệ thống
này** — ví dụ `the provider refused the request (status 401)` khi tên cloud sai.

### Auth — admin seed (chỉ `cmd/seed`)
| Biến | Mặc định | Bắt buộc | Ý nghĩa |
|---|---|---|---|
| `ADMIN_EMAIL` | *(rỗng)* | ✅ cho seed | |
| `ADMIN_PASSWORD` | *(rỗng)* | ✅ cho seed | Phải đạt chính sách mật khẩu |

### Media — tiền tố `MEDIA_`
| Biến | Mặc định | Bắt buộc | Ý nghĩa |
|---|---|---|---|
| `MEDIA_CLOUD_NAME` | *(rỗng)* | | **Tên cloud** trên Cloudinary — lấy ở Settings → Account details. **Không phải tên folder** |
| `MEDIA_API_KEY` | *(rỗng)* | | API key của cloud |
| `MEDIA_API_SECRET` | *(rỗng)* | | API secret |
| `MEDIA_FOLDER` | `artist-shop` | | Folder **bên trong** cloud nhận avatar tải lên. Tự đặt, không lấy từ provider |

`MEDIA_CLOUD_NAME` và `MEDIA_FOLDER` là hai giá trị **khác nhau, dễ nhầm**: cloud name
định danh tài khoản, còn folder chỉ là nơi lưu ảnh bên trong tài khoản đó. Đặt cloud
name bằng giá trị của folder sẽ khiến provider trả `401`, và mọi upload trả
`503 USER_MEDIA_UNAVAILABLE`. Quy trình lấy đúng giá trị: [Xác minh với nhà cung cấp
thật](#xác-minh-với-nhà-cung-cấp-thật).

| Dấu hiệu provider trả về | Nghĩa là gì | Phải sửa gì |
|---|---|---|
| `401` | Tên cloud không tồn tại — thường là do đặt nhầm cloud name bằng tên folder | Lấy lại `MEDIA_CLOUD_NAME` ở Settings → Account details |
| `4xx` khác `401` | Provider từ chối yêu cầu nhưng có thể thử lại được | Thử lại; nếu lặp lại thì kiểm tra key/secret và quota |
| `5xx` | Provider không phục vụ được | Thử lại sau; không phải lỗi cấu hình |

Cả bốn biến đều **không** nằm trong `Validate()` hay `ValidateForAPI()`: `migrate` và
`seed` phải chạy được mà không cần credential của nhà cung cấp media, và API cũng phải
khởi động được khi thiếu chúng. Vì vậy cấu hình thiếu là **fail-closed ở adapter** chứ
không phải lỗi khởi động:

| Tình huống | Kết quả |
|---|---|
| Đủ cả ba khoá (`CLOUD_NAME` + `API_KEY` + `API_SECRET`) | Avatar tải lên và xoá bình thường |
| Thiếu bất kỳ khoá nào | `POST /api/v1/users/me/avatar` trả `503 USER_MEDIA_UNAVAILABLE`, **hồ sơ không bị đổi** nên khách thử lại được |

**Một cấu hình media thiếu chỉ tắt đúng việc upload avatar.** Đọc hồ sơ
(`GET /users/me`) không bao giờ gọi nhà cung cấp media, nên vẫn trả `200` với tham chiếu
ảnh đã lưu; `PATCH /users/me`, `DELETE /users/me/avatar` và toàn bộ nhóm
`/users/me/addresses` cũng không gọi, nên tất cả vẫn hoạt động. Service cũng ghi một
dòng cảnh báo lúc khởi động, chỉ nêu **tên biến** chứ không nêu giá trị. Xem
[api-reference.md](api-reference.md) mục `POST /users/me/avatar`.

### Swagger — tiền tố `SWAGGER_`
| Biến | Mặc định | Bắt buộc | Ý nghĩa |
|---|---|---|---|
| `SWAGGER_ENABLED` | `false` | | Bật giao diện API reference tại `/swagger`. Chỉ dùng cho development/staging |

> Swagger UI phơi ra toàn bộ endpoint nên **production phải để `false`**. Khi tắt,
> route `/swagger` không tồn tại. File spec đã sinh ở `docs/swagger/` vẫn xem offline
> được bất kể cài đặt này. Sinh lại bằng `make swagger`.

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

Quy trình này là chính sách làm mới dataset của module; tóm tắt và lý do ghi ở
[modules/02-user.md](modules/02-user.md).

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
