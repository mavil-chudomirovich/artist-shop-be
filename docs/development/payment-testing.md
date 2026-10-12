# Test thanh toán (PayOS, module 08)

Hướng dẫn chạy thử **thanh toán đơn hàng thật** (module 08 Payment, cổng **PayOS**) trên máy
local, gồm cả bước mở backend ra internet để PayOS gọi được webhook.

> Đọc kèm: [api-reference.md](../api-reference.md) §10 (endpoint thanh toán),
> [modules/08-payment.md](../modules/08-payment.md) (thiết kế module),
> [specs/011-order-payment/quickstart.md](../../specs/011-order-payment/quickstart.md) (kịch bản kiểm
> chứng), và [frontend-guide](../../specs/011-order-payment/frontend-guide.md) của feature 011.

## 0. Điều cần biết trước

- **PayOS chỉ có môi trường production** (không có base URL sandbox riêng). Vì vậy "test thật" nghĩa là
  **chuyển khoản thật** — hãy dùng **số tiền nhỏ**. Nếu kênh thanh toán của bạn có chế độ test thì dùng
  kênh đó.
- **Webhook là bắt buộc để đơn tự chuyển `PAID`.** PayOS gọi webhook **từ internet** vào backend; backend
  trên `localhost` **không** truy cập được. Không mở tunnel / không đăng ký webhook thì:
  - tạo link thanh toán **vẫn chạy** (khách trả được),
  - nhưng **đơn không tự chuyển `PAID`** — nó nằm `PAYMENT_PENDING` rồi bị sweeper **tự huỷ sau 60 phút**.
- **Không có hoàn tiền** ở V1.0; đơn đã `PAID` **không** huỷ (chuyển nhượng, module 07).

## 1. Chuẩn bị `.env`

`.env` là file git-ignored. Điền (không commit giá trị thật):

```dotenv
# PayOS merchant credentials (dashboard my.payos.vn)
PAYOS_CLIENT_ID=<client id>
PAYOS_API_KEY=<api key>
PAYOS_CHECKSUM_KEY=<checksum key>
PAYOS_BASE_URL=https://api-merchant.payos.vn
# URL trình duyệt khách được redirect về sau khi trả / khi huỷ
PAYMENT_RETURN_URL=http://localhost:3000/payment/return
PAYMENT_CANCEL_URL=http://localhost:3000/payment/cancel

# ngrok (bước 3)
NGROK_AUTHTOKEN=<authtoken từ dashboard ngrok>
NGROK_DOMAIN=            # tuỳ chọn: domain reserved để URL webhook ổn định
```

Khi `PAYOS_CLIENT_ID/API_KEY/CHECKSUM_KEY` **đều có giá trị**, backend dùng **client PayOS thật**; để
trống cả ba thì dùng **log provider** (chỉ log, không gọi PayOS).

## 2. Chạy backend (Docker)

```bash
make up            # build image + chạy db, redis, api; API ở http://localhost:8080
```

Kiểm tra sống:

```bash
curl http://localhost:8080/healthz     # {"status":"alive"}
```

Migration `00012_payment.sql` tự áp khi khởi động (đã kiểm: `payments`, `payment_events` tồn tại).

> **Sau mỗi lần sửa code**, chạy lại `make up` để build lại image (container đang chạy là image cũ nếu
> bạn không rebuild).

## 3. Mở backend ra internet (ngrok)

Backend phải có một URL **HTTPS công khai**. Có hai cách; chọn một.

### Cách A — service ngrok trong Docker (khuyến nghị, opt-in)

Service `ngrok` nằm sau profile `ngrok` nên `make up-tools` **không** tự chạy nó:

```bash
docker compose -f docker-compose.yml -f docker-compose.dev.yml --profile ngrok up -d ngrok
```

- Service dùng `NGROK_DOMAIN` (domain reserved trong `.env`) làm URL công khai:
  `https://<NGROK_DOMAIN>`; xem lại ở dashboard **http://localhost:4040** (`/api/tunnels`).
- Không đặt `NGROK_DOMAIN` thì dùng **Cách B** để lấy URL (ngrok sẽ cấp URL ngẫu nhiên).
- Dừng: `docker compose -f docker-compose.yml -f docker-compose.dev.yml --profile ngrok stop ngrok`.

### Cách B — ngrok chạy trực tiếp trên host

```bash
ngrok config add-authtoken <authtoken>     # một lần
ngrok http 8080                            # trỏ tới API đã publish 127.0.0.1:8080
```

Lấy URL `https://...` ở output hoặc dashboard `http://localhost:4040`.

> **Vì sao cần URL ổn định**: URL webhook phải được **đăng ký** với PayOS (bước 4). URL đổi mỗi lần
> restart thì phải **đăng ký lại**. Domain reserved (free tier có) tránh việc đó.

## 4. Đăng ký webhook URL với PayOS (một lần)

Backend **không** tự đăng ký webhook; đây là bước vận hành (xem
[deferred D5](../../specs/011-order-payment/deferred.md)).

```bash
curl -X POST https://api-merchant.payos.vn/confirm-webhook ^
  -H "x-client-id: <PAYOS_CLIENT_ID>" ^
  -H "x-api-key: <PAYOS_API_KEY>" ^
  -H "Content-Type: application/json" ^
  -d "{\"webhookUrl\":\"https://<ngrok-domain>/api/v1/payments/payos/webhook\"}"
```

- Đường dẫn webhook là **`/api/v1/payments/payos/webhook`** (công khai, không cần phiên; xác thực bằng
  chữ ký PayOS).
- Khi đăng ký, PayOS gửi **một webhook mẫu** để kiểm tra URL — nếu backend trả lỗi chữ ký thì việc đăng
  ký thất bại; xem mục 7.
- Đổi domain → **đăng ký lại**.

## 5. Chạy luồng thật

Trên Windows, ghi body JSON ra file rồi gửi bằng `--data-binary @file`.

1. **Đặt hàng** (khách): `POST /api/v1/orders` → đơn `PENDING`.
2. **Artist xác nhận** (admin): `POST /api/v1/admin/orders/{id}/confirm` → đơn `PAYMENT_PENDING`, hàng
   được giữ, mở hạn thanh toán 60 phút.
3. **Bắt đầu thanh toán** (khách): `POST /api/v1/orders/{id}/payment` → `PaymentResponse` với
   `status: "PENDING"`, `checkoutUrl`, `qrCode`.
4. **Trả tiền**: mở `checkoutUrl` (hoặc quét `qrCode`) và chuyển khoản thật.
5. **Webhook**: PayOS gọi `/api/v1/payments/payos/webhook` → backend xác minh chữ ký, đối chiếu, áp
   **một lần**.
6. **Kiểm chứng**:
   - `GET /api/v1/orders/{id}/payment` → `status: "PAID"`.
   - `GET /api/v1/orders/{id}` → đơn `PAID`.
   - `GET /api/v1/admin/payments` (admin) → bản ghi thanh toán.

Chi tiết cấu trúc JSON và mã lỗi ở [api-reference.md](../api-reference.md) §10 và
[frontend-guide](../../specs/011-order-payment/frontend-guide.md).

## 6. Không dùng ngrok — giả lập webhook

Nếu chỉ muốn kiểm tra **đường áp xác nhận** mà không mở tunnel: tự ký HMAC-SHA256 (bằng
`PAYOS_CHECKSUM_KEY`) theo đúng thuật toán PayOS — `data` object sắp xếp alphabet, `key=value&...`,
null → chuỗi rỗng — rồi POST vào backend local:

```bash
curl -X POST http://localhost:8080/api/v1/payments/payos/webhook ^
  -H "Content-Type: application/json" ^
  --data-binary @webhook.json
```

**Đây là giả lập, không phải môi trường thật.** Cách này trùng với cách integration test kiểm chứng
(`internal/modules/payment/presentation/http/payment_integration_test.go`) và dùng vector chữ ký thật
của tài liệu PayOS.

## 7. Xử lý sự cố

| Hiện tượng | Nguyên nhân thường gặp | Cách xử lý |
|---|---|---|
| Tạo link OK nhưng đơn **không** `PAID` | Webhook chưa tới | Kiểm tra tunnel còn chạy; URL đã đăng ký `confirm-webhook`; log `docker logs artist-shop-api-1` |
| Webhook trả `401 PAYMENT_WEBHOOK_INVALID_SIGNATURE` | Sai `PAYOS_CHECKSUM_KEY`, hoặc body bị sửa | Đối chiếu checksum key đúng kênh; gửi **nguyên** body PayOS gửi |
| `409 PAYMENT_ORDER_NOT_PAYABLE` khi bắt đầu trả | Đơn không ở `PAYMENT_PENDING` (chưa confirm / đã trả / đã huỷ) | Tải lại đơn |
| `422 PAYMENT_PROVIDER_ERROR` | Cổng lỗi/không tới được; **đơn không đổi** | Thử lại sau giây lát; kiểm tra `PAYOS_BASE_URL` và mạng |
| Đơn thành `CANCELLED` sau ~60 phút | Quá hạn thanh toán mà webhook chưa tới | Đúng thiết kế (FR-008); đặt lại đơn |
| `GET /admin/payments?status=UNMATCHED` có bản ghi | Xác nhận hợp lệ nhưng **không áp được** (sai tiền / nội dung đơn cũ / đơn đã huỷ-hết hạn) | **Không** tự đánh dấu đã trả; operator đối soát ngoài hệ thống (V1.0 không hoàn tiền) |

## 8. Ghi chú vận hành

- URL webhook **phải** là HTTPS công khai; đổi domain → đăng ký lại `confirm-webhook`.
- Credentials và authtoken **chỉ** ở `.env` (git-ignored); không commit, không truyền trên command line
  trong log.
- Khi test xong, dừng tunnel: `docker compose ... --profile ngrok stop ngrok`.
- Các phần **cố ý hoãn** (hoàn tiền, đa tiền tệ, định kỳ, tự khôi phục link mồ côi): xem
  [deferred.md](../../specs/011-order-payment/deferred.md).
