# ADR 025 - Production deploy bằng GitHub Actions, image GHCR pin digest trên VPS dùng chung

- **Status**: Accepted
- **Decision Date**: 2026-10-19
- **Decision Maker**: Mavil

## 1. Bối cảnh

`artist-shop-be` là modular monolith, đóng gói **một image** chứa cả `api` và
`migrate` (migration nhúng qua `go:embed`). Cần đưa lên production trên một VPS
**dùng chung với littlecafe**:

- littlecafe đang giữ `80/443` và các port `8081/8082/8443/8444`.
- Không có nhu cầu staging; frontend test trực tiếp trên production.
- Secret (PostgreSQL, Redis, JWT, SMTP, Cloudinary, PayOS) không được vào repo.
- Cần rollback nhanh và biết chính xác binary nào đang chạy.

## 2. Quyết định

- **Một môi trường production**, không tạo staging.
- CD chạy bằng GitHub Actions (`.github/workflows/cd.yml`) khi push tag `v*`
  hoặc `workflow_dispatch`; không deploy tự động khi push `main`.
- Image build `linux/amd64`, push GHCR
  `ghcr.io/mavil-chudomirovich/artist-shop-be` với tag `<sha>` và `latest`.
  **Deploy theo digest**, không theo tag: workflow ghi
  `API_IMAGE=<image>@sha256:<digest>` vào `/opt/artist-shop/.env.deploy`.
- Stack production dùng `docker-compose.prod.yml`: `db` + `redis` (không publish
  port) + `api` + `nginx`; `migrate` là job one-shot chạy **trước** rollout.
- nginx publish port `8083`/`8445` để không đụng littlecafe; TLS cấp riêng bằng
  lego DNS-01 qua Vercel, mount vào `/etc/nginx/tls`.
- `APP_ENV=production`, `MIGRATIONS_AUTO_APPLY=false`, `SWAGGER_ENABLED=false`.
- Không bật Required reviewers cho environment `production`; tag/dispatch là
  hành động có chủ ý.
- Rollback bằng cách sửa digest trong `.env.deploy` rồi `up -d api`, hoặc
  dispatch lại tag cũ.

## 3. Hệ quả

| Hệ quả | Chi tiết |
|---|---|
| Tích cực | Biết chính xác digest đang chạy; rollback không cần build lại |
| Tích cực | Không cần registry credential ghi trên VPS; VPS chỉ cần PAT `read:packages` |
| Tích cực | Không đụng littlecafe: port, volume, network, cert tách biệt |
| Tích cực | Migration chạy tách khỏi API nên lỗi schema không làm API restart loop |
| Tiêu cực | Mọi thay đổi image phải qua GitHub Actions; sửa tay trên VPS sẽ bị CD ghi đè |
| Tiêu cực | Không có staging để thử trước; lỗi cấu hình chỉ lộ ở production |
| Bù đắp | Smoke test container + `/healthz` + `/readyz` trong workflow; backup trước thay đổi lớn |
| Điều kiện xem lại | Khi cần staging, hoặc khi có nhiều replica (rate limit trong process hiện tại không chia sẻ giữa các replica) |

## 4. Cách tiếp cận bị loại

| Phương án | Vì sao không chọn |
|---|---|
| Deploy tự động khi push `main` | Mỗi merge lên production; không có bước phát hành rõ ràng |
| Build image trên VPS | Tốn RAM/CPU của VPS dùng chung; không tái lập được artifact |
| Deploy theo tag `latest` | Không biết binary nào đang chạy; rollback không xác định |
| Dùng chung port littlecafe | Xung đột bind port và lẫn cert/volume giữa hai dự án |
| Tách image riêng cho `migrate` | Cùng binary set, thêm một artifact không mang lại lợi ích |
| Tạo staging trên cùng VPS | Tốn thêm ~1GB RAM; chưa có nhu cầu |

## 5. Bổ sung

Runbook vận hành nằm ngoài repo tại `../Docs/deploy/README.md`.
