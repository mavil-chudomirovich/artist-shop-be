# Module 00 — Cross-Cutting Foundation

- **Trạng thái Spec Kit**: Hoàn tất (implement + convergence; tất cả task đã đóng)
- **Spec**: `specs/002-cross-cutting-foundation/spec.md`
- **Plan**: `specs/002-cross-cutting-foundation/plan.md`
- **Tasks**: `specs/002-cross-cutting-foundation/tasks.md`
- **Code**: `cmd/api`, `cmd/migrate`, `internal/share/*`, `migrations/*`
- **Ưu tiên / Giai đoạn**: Giai đoạn 0 (nền tảng dùng chung)
- **Phụ thuộc**: —

## Mục đích

Hạ tầng dùng chung mà mọi module nghiệp vụ dựa vào: cấu hình, kết nối dữ liệu,
migration, xử lý lỗi, logging, audit, và các tiện ích HTTP. Đây là nền tảng bắt
buộc trước khi implement module đầu tiên.

## Phạm vi MVP

Có:
- Cấu hình theo môi trường (dev/staging/prod), quản lý secret qua biến môi trường.
- Kết nối cơ sở dữ liệu + framework migration có version.
- Định dạng phản hồi và lỗi thống nhất cho toàn bộ API.
- Structured logging kèm correlation/request ID.
- Middleware: xác thực, phân quyền, rate limiting, phục hồi lỗi, CORS.
- Hạ tầng audit: ghi vết thao tác admin và sự kiện thanh toán vào `audit_logs`.
- Health/readiness endpoints.
- Khung kiểm thử (unit, API, test database).

Không (hoãn):
- Tracing phân tán, metrics nâng cao, APM.
- Multi-tenancy.

## Thực thể dữ liệu

- `audit_logs`: actor, action, target, metadata, timestamp.

## Yêu cầu chức năng sơ bộ

- Hệ thống MUST đọc toàn bộ cấu hình/secret từ môi trường, không hard-code.
- Mọi phản hồi lỗi MUST theo một định dạng thống nhất, có mã lỗi ổn định.
- Mọi request MUST có correlation ID xuất hiện trong log.
- Log MUST NOT chứa secret/PII không cần thiết.
- Thay đổi schema MUST qua migration có version, review được.

## Tiêu chí hoàn thành

- Service khởi động được ở môi trường sạch, migration chạy tự động.
- Health/readiness trả trạng thái đúng.
- Middleware nền tảng hoạt động và có test.
- Có mẫu test cho cả unit và API.

## Ghi chú / câu hỏi mở

- Chọn framework HTTP, thư viện DB, và công cụ migration (quyết định ở bước `/speckit.plan`).
- Định dạng response envelope và error format cụ thể (chốt ở plan/contracts).
- Đã chốt trong spec: modular monolith một service; migration tự động khi khởi động
  có lock; audit bất đồng bộ best-effort có retry; rate limiting trong tiến trình.

## Tham chiếu

- `doc/backend-spec.md` (Architecture, API, Testing)
- `.specify/memory/constitution.md` (Technology & Architecture Constraints)
