# ADR 024 - Seed admin bằng file SQL thủ công thay vì container seed

- **Status**: Accepted
- **Decision Date**: 2026-10-19
- **Decision Maker**: dev
- **Supersedes**: —
- **Superseded by**: —

## 1. Bối cảnh

Tài khoản admin duy nhất được cấp bởi `cmd/seed` + service `seed` trong compose
(đọc `ADMIN_EMAIL`/`ADMIN_PASSWORD`, hash Argon2id, upsert `users`, ghi audit).
Thực tế vận hành cho thấy chi phí lớn hơn lợi ích:

- Service `seed` nằm sau `profiles: [tools]` vẫn gây nhầm lẫn: dev bấm Start
  container cũ trong Docker Desktop gặp `network ... not found` (trỏ network đã
  xoá) và `DATABASE_URL is required` (container tạo thủ công ngoài compose nên
  không có env) — xem `docs/troubleshooting.md`.
- Seed chỉ chạy một lần mỗi môi trường (upsert idempotent), không xứng đáng một
  binary trong image, một service compose, và hai target make (`seed`,
  `seed-docker`).
- Mật khẩu admin trong `.env` là secret thường trực; giữ nó trong file SQL
  plaintext cũng không tốt hơn — nhưng hash một lần rồi xoá password gốc thì
  tốt hơn cả hai.

Ràng buộc: hasher là Argon2id random-salt trong Go
(`internal/modules/auth/infrastructure/implement/token/password.go`) — SQL
thuần (kể cả `pgcrypto`) không tái tạo được, nên file SQL phải nhận hash làm
tham số thay vì tự hash.

## 2. Quyết định

- Seed admin là `scripts/seed_admin.sql`: upsert `users(email)` thành
  `ADMIN`/`active` + một dòng `AUTH_ADMIN_PROVISIONED` vào `audit_logs`,
  chạy thủ công bằng psql qua container `db` (`make seed-sql`). File là template
  không chứa credential; hai biến `admin_email`/`admin_hash` đến từ file local
  `scripts/seed.vars.local.psql` (git-ignored, ngoài Docker build context), có
  khối guard fail-fast khi thiếu/sai định dạng.
- Hash sinh offline bằng helper dev-only `cmd/hash-password` (đọc
  `ADMIN_PASSWORD` từ môi trường, kiểm tra đúng password policy rồi in hash);
  helper không vào image Docker.
- Xoá hẳn: `cmd/seed`, `internal/modules/auth/presentation/cli`, binary `seed`
  trong `Dockerfile`, service `seed` trong compose, target `make seed`,
  `make seed-docker`, field `ADMIN_PASSWORD` trong config. Giữ `ADMIN_EMAIL`
  vì API còn dùng làm địa chỉ nhận mail đơn hàng (`ArtistEmail`).
- Xoay mật khẩu = sinh hash mới + chạy lại file (upsert).
- Không đặt seed trong `migrations/`: goose sẽ tự áp nó ở mọi môi trường và
  dính guard-test `Down`/`data-migration`.

## 3. Hệ quả

Tích cực: hết cả lớp lỗi container-seed; image nhẹ hơn một binary; không còn
secret password trong `.env` sau khi hash xong; audit vẫn được ghi
(Constitution VI) vì `audit_logs` cho `INSERT`.

Tiêu cực: mất kiểm tra password policy lúc seed (chuyển sang `make seed-hash`
lúc sinh hash — quên bước này thì guard trong SQL chỉ kiểm tra định dạng hash,
không kiểm tra độ mạnh password gốc); operator phải giữ file biến local ngoài
repo.

Xem lại khi: cần seed thêm dữ liệu mẫu (không chỉ admin) — lúc đó cân nhắc thư
mục `scripts/` có versioning thay vì quay lại container.

## 4. Cách tiếp cận bị loại

| Phương án | Vì sao không chọn |
|---|---|
| Seed thành migration goose versioned | Tự chạy ở mọi môi trường, sai ngữ nghĩa versioned schema; dính guard-test |
| SQL tự hash bằng `pgcrypto crypt()` | Đổi định dạng hash → mọi password hiện tại gãy, phải migration lại bảng `users` |
| Giữ `cmd/seed` song song với SQL | Phải bảo trì hai đường seed cho một thao tác một-lần |
