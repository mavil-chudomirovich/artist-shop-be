# ADR 006 - Stack Docker mặc định không publish port của db/redis

- **Status**: Accepted
- **Decision Date**: 2026-10-05
- **Decision Maker**: Mavil

## 1. Bối cảnh

Máy phát triển thường đã có PostgreSQL/Redis chạy sẵn, hoặc đang chạy container của
dự án khác. Publish `0.0.0.0:5432` và `0.0.0.0:6379` sẽ gây xung đột ngay lần chạy đầu.

## 2. Quyết định

- `docker-compose.yml` (stack mặc định, `make up`) **không** publish port của `db` và
  `redis`. Chỉ `api` publish trên `127.0.0.1`.
- `docker-compose.dev.yml` (lớp override, `make up-tools`) mới publish port của `db` và
  `redis` trên `127.0.0.1`, và thêm Mailpit.
- Mọi lệnh Makefile gói sẵn cả hai file để người dùng không phải nhớ cờ `-f`.
- Secret của Redis truyền vào container qua `REDISCLI_AUTH`, để `redis-cli` tự đăng
  nhập và không lộ password trên dòng lệnh.

## 3. Hệ quả

| Hệ quả | Chi tiết |
|---|---|
| Tích cực | `make up` không thể đụng dịch vụ đang chạy trên máy dev |
| Tích cực | Bề mặt mạng mặc định nhỏ hơn: db/redis chỉ trong network nội bộ |
| Tiêu cực | Debug database cần nhớ dùng `make up-tools` (đã ghi trong `make up-tools` output) |
| Bù đắp | Hai file compose thay vì một; đổi lại độ ổn định khi chạy |

## 4. Cách tiếp cận bị loại

| Phương án | Vì sao không chọn |
|---|---|
| Luôn publish port, dùng `profiles` | Compose không hỗ trợ publish port có điều kiện; port sẽ luôn mở |
| Publish trên cổng lạ (5433, 6380) | Vẫn chiếm cổng và gây nhầm lẫn; người khác không đoán được cổng nào là của dự án |
