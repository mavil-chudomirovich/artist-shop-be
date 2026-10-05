# Backend Architecture

Tài liệu chuẩn cho kiến trúc backend (modular monolith + Clean Architecture).
Mọi module PHẢI tuân theo tài liệu này.

## 1. Cây thư mục

```
cmd/                      # composition roots (chỉ lắp ráp, gọi use case)
├── api/
├── migrate/
└── seed/

internal/
├── share/                # code dùng chung ≥ 2 module
│   ├── config/ logging/ reqctx/
│   ├── database/ (+migrate)
│   ├── cache/
│   ├── httpx/ middleware/ httpserver/ health/
│   ├── audit/
│   ├── access/           # role/permission dùng chung
│   └── repository/       # generic repository: interface + base struct (CRUD + list + pagination)
├── contracts/            # interface công bố giao tiếp giữa các module
└── modules/
    └── <module>/
        ├── domain/
        │   ├── model/        # entity, value object
        │   ├── constant/     # hằng nghiệp vụ
        │   ├── error/        # lỗi nghiệp vụ
        │   └── repository/   # interface repository (nhúng share/repository)
        ├── application/
        │   ├── interface/    # interface usecase + port external service + UnitOfWork
        │   ├── implement/    # usecase implementation
        │   ├── dto/          # input/output DTO
        │   └── mapper/       # map domain model ↔ dto (chỉ một mapper ở application)
        ├── infrastructure/
        │   └── implement/    # postgres, redis, token, email, auditor
        └── presentation/
            ├── http/         # handler + router (map presentation/dto ↔ application/dto)
            ├── cli/          # command CLI
            ├── worker/       # background worker (placeholder)
            └── dto/          # request/response DTO của HTTP
migrations/               # TẬP TRUNG ở gốc: NNNNN_<module>_<desc>.sql
```

## 2. Quy tắc phụ thuộc

- `presentation → application → domain`.
- `infrastructure → domain` và `infrastructure → application/interface` (implements port).
- `domain` không import tầng nào khác; chỉ dùng stdlib + `share/access` (gói giá trị thuần).
- `application` không import `infrastructure`; chỉ phụ thuộc port ở `application/interface` và
  `domain`.
- Module không import internals của module khác; giao tiếp liên module qua `internal/contracts`.
- `share/*` không import module.

## 3. Trách nhiệm từng phần

- **domain/model**: entity, value object.
- **domain/constant**: hằng nghiệp vụ.
- **domain/error**: sentinel error nghiệp vụ.
- **domain/repository**: interface repository; nhúng interface generic ở `share/repository`
  khi phù hợp.
- **application/interface**: interface usecase, port cho external service (email, token,
  cache, audit), và `UnitOfWork`.
- **application/implement**: hiện thực use case.
- **application/dto**: DTO vào/ra của use case.
- **application/mapper**: chuyển đổi model ↔ dto (duy nhất ở đây).
- **infrastructure/implement**: adapter (PostgreSQL, Redis, JWT, email, audit).
- **presentation/http**: handler/router; nhận `presentation/dto`, gọi usecase qua
  `application/dto`; map tại tầng này.
- **presentation/cli**: command CLI gọi use case.
- **presentation/worker**: placeholder cho background worker.

## 4. Generic repository (`share/repository`)

- Interface `Repository[T, ID]` cung cấp: `Create`, `Update`, `Delete`, `FindByID`,
  `FindAll` (phân trang), `Exists`.
- `Base[T, ID]` là struct dùng chung hiện thực CRUD + list + pagination bằng pgx.
- Repository của module nhúng `Base` và bổ sung truy vấn đặc thù.
- Repository nhận transaction qua context (không tự mở transaction).

## 5. UnitOfWork

- Port đặt ở `application/interface`.
- `application/implement` mở transaction và điều phối nhiều repository.
- Adapter transaction nằm ở `share/database` (dùng `database.FromContext`).

## 6. Migration

- Tập trung ở `migrations/`, một runner chung, có `-- +goose Up/Down`.
- Tên file: `NNNNN_<module>_<mô tả>.sql`.

## 7. Role & permission

- Ở `internal/share/access`. `share/middleware` nhận role cần kiểm tra làm tham số.

## 8. Composition root & CLI

- `cmd/*` chỉ nạp config, khởi tạo adapter, lắp use case, chạy.
- CLI gọi use case; không viết SQL nghiệp vụ trực tiếp.

## 9. Kiểm thử

- Unit test `application` bằng fake in-memory cho port.
- Integration test (build tag `integration`) cho `infrastructure` và luồng HTTP.
- Test-first cho logic nghiệp vụ trọng yếu.

## 10. Error & HTTP contract

- Constant/error nghiệp vụ ở `domain`; `presentation/http` map sang `share/httpx`.
- Envelope chung + correlation ID `X-Request-Id`.
