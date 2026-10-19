# Implementation Plan: Address Facebook URL

**Branch**: `021-address-facebook-url` | **Date**: 2026-10-19 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/021-address-facebook-url/spec.md`

## Summary

Thêm trường link Facebook bắt buộc (`facebookUrl`) vào địa chỉ giao hàng của khách: tạo mới bắt buộc có, sửa không được xóa về rỗng, danh sách của khách + tra cứu admin đều hiển thị, và đơn hàng mới chụp lại link tại lúc chốt đơn (chặn đơn dùng địa chỉ giữ chỗ). Validate: trim, tối đa 500 ký tự, phải là URL `https` với host `facebook.com`/`fb.com` hoặc mọi subdomain của chúng (vd `www.`, `m.`, case-insensitive, bỏ qua port), path phải có nội dung; sai trả `400 VALIDATION_ERROR` nêu `facebookUrl`. Lưu trữ: cột `facebook_url text NOT NULL` + CHECK không rỗng × 2 bảng (`addresses`, `orders`), backfill một lần bằng `https://facebook.com/unknown` cho dữ liệu cũ.

## Technical Context

**Language/Version**: Go 1.26 (module `github.com/mavil-chudomirovich/artist-shop-be`, chi v5, pgx v5, google/uuid)

**Primary Dependencies**: chi router, pgx/pgxpool (+ `internal/share/database` Tx qua context), `internal/share/administrative` (dataset tỉnh/xã), `internal/share/httpx` (envelope lỗi), `internal/contracts` (CustomerLookupService)

**Storage**: PostgreSQL (migrations goose tập trung `migrations/NNNNN_<module>_<desc>.sql`); Redis không liên quan; không đụng Cloudinary

**Testing**: `go test` (unit: domain/app/mapper/presentation), integration test postgres (testcontainers hoặc DB thật theo `make test-integration`), HTTP integration test; gate `make lint` + `make test` + `make test-integration`, close-out `make check` (gồm `swagger-check` + `build`)

**Target Platform**: Linux server / Docker VPS (API), base path `/api/v1`; frontend Vercel ngoài scope repo

**Project Type**: Modular monolith, REST web-service (`presentation → application → domain`, `infrastructure → domain`)

**Performance Goals**: Không đặt mục tiêu latency mới; validate URL là O(1) trên chuỗi ≤ 500 ký tự, không gọi mạng ra ngoài (chỉ parse chuỗi + so host)

**Constraints**: Id chủ tài khoản luôn lấy từ session (không từ body/query); tiền minor units (không liên quan nhưng giữ nguyên); ràng buộc dữ liệu quan trọng phải có đối ứng tầng lưu trữ (NOT NULL + CHECK); endpoint đổi → annotation Swagger + `make swagger` + cập nhật `docs/api-reference.md` trong cùng change; comment code/`.env` tiếng Anh

**Scale/Scope**: Thay đổi cộng thêm 1 trường trên 2 bảng + lan qua contract liên module; không đổi flow phân trang, rate limit, phân quyền hiện tại

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- [x] **Modular Monolith & Clean Architecture**: domain chỉ import stdlib (`net/url`, `strings`, `unicode/utf8`) + `share/access` (đã có) + `google/uuid` (được phép); validate Facebook nằm trong `domain/model/address.go`; dataset tỉnh/xã vẫn qua port `Divisions` ở application; không module nào đọc bảng của module khác (order chỉ nhận `contracts.CustomerAddress`).
- [x] **Transactional Integrity**: không đụng tiền/tồn kho; các write địa chỉ giữ nguyên biên transaction hiện tại (`Create`/`SetDefault` trong `Tx.WithinTx`); migration backfill + NOT NULL chạy một lần, không mở transaction nghiệp vụ.
- [x] **State Machines & Invariants**: bất biến "mọi địa chỉ hiển thị đều có link" được giữ bởi 3 lớp: domain (`NewAddress`/`Apply` từ chối rỗng/sai), storage (NOT NULL + CHECK), và checkout chặn địa chỉ giữ chỗ (FR-009); `is_default`/`deleted_at` không bị chạm bởi edit (giữ nguyên tắc `Update` không đặt 2 cột này).
- [x] **Test-First Critical Logic**: logic này không thuộc pricing/inventory/state-machine trọng yếu, nhưng vẫn có test dương + âm cho mọi nhánh validate (blank/quá dài/sai host/sai scheme/giữ chỗ ở checkout), auth/ownership giữ nguyên.
- [x] **Security & Least Privilege**: ownership qua `FindByOwner`/session giữ nguyên; `facebookUrl` là URL công khai do khách tự nhập nên không coi là PII nhạy cảm, audit chỉ ghi tên field đổi, không ghi giá trị; không log URL vào log cấu trúc.
- [x] **Observability**: tái dùng audit hiện tại (`USER_ADDRESS_CREATED/UPDATED`, `changedFields` thêm `facebookUrl` không kèm giá trị); không thêm PII vào log.
- [x] **Simplicity (YAGNI)**: không module mới, không service mới, không gọi Graph API/Facebook API để xác thực link (chỉ kiểm tra shape chuỗi); không realtime/microservice.
- [x] **Frontend Integration Guide**: đây là changed client-facing DTO → sẽ ship `specs/021-address-facebook-url/frontend-guide.md` trong phase implement (liệt kê endpoint new/changed, before → after từng shape, kèm đếm field/enum/route/status), đồng bộ lại ở phase cuối + sau converge.
- [x] **Swagger annotations**: các route address/order giữ nguyên annotation, chỉ thêm field vào schema response/request DTO (struct tags) rồi chạy `make swagger` regenerate `docs/swagger/`; `make swagger-check` xanh; `tasks.md` sẽ nêu rõ không có endpoint new/removed (chỉ changed DTO).

Re-check sau Phase 1 (research + data-model + contracts bên dưới): không phát sinh vi phạm mới — validate vẫn trong domain bằng stdlib, không thêm dependency, không đổi biên transaction, không thêm error code mới.

## Project Structure

### Documentation (this feature)

```text
specs/021-address-facebook-url/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
│   ├── openapi-addition.yaml  # Delta schema: facebookUrl ở Address/CustomerLookup/OrderAddress
│   └── error-codes.md         # Ghi nhận: không thêm code mới (VALIDATION_ERROR)
├── frontend-guide.md    # Implement phase (Constitution §Governance) — chưa tạo ở /speckit.plan
├── checklists/
│   └── requirements.md
├── spec.md
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
internal/
├── contracts/user.go                                  # CustomerAddress += FacebookURL
├── modules/user/
│   ├── domain/model/address.go                        # FieldFacebookURL, maxFacebookURLRunes, FacebookURL ở Address/Draft/Edit, validateFacebookURL, NewAddress/Apply
│   ├── domain/model/address_test.go                   # case blank/dài/sai-host/sai-scheme/sentinel/bare-host/subdomain/port (đỏ → xanh)
│   ├── application/dto/dto.go                        # Create/Update/AddressOutput += FacebookURL
│   ├── application/implement/address.go               # draft/edit truyền FacebookURL, changedAddressFields += facebookUrl
│   ├── application/mapper/mapper.go                   # Address() + customerAddresses() map thêm
│   ├── infrastructure/implement/postgres/address.go   # addressColumns/Create/Update/scanAddress += facebook_url
│   ├── infrastructure/implement/postgres/address_integration_test.go  # round-trip + direct-DB CHECK/NOT NULL negative
│   └── presentation/{dto/dto.go,http/handler.go,http/errors.go}  # Request/Response (+OperatorAddressResponse/toCustomerAddressResponses) + required-check + field const
└── modules/order/
    ├── domain/model/order.go                          # Address += FacebookURL
    ├── application/{dto/dto.go,mapper/mapper.go,implement/checkout.go,implement/edit.go}  # View/snapshot + chặn giữ chỗ (FR-009)
    ├── infrastructure/implement/postgres/order.go     # orderColumns/insert/update/scan += facebook_url
    └── presentation/{dto/dto.go,http/handler.go}      # OrderAddressResponse += facebookUrl

migrations/
├── 00020_user_address_facebook_url.sql                # addresses.facebook_url NOT NULL + CHECK + backfill
└── 00021_order_address_facebook_url.sql               # orders.facebook_url NOT NULL + CHECK + backfill

specs/003-user-profile/contracts/openapi.yaml         # Address/CustomerLookup/Create/UpdateAddressRequest += facebookUrl
specs/009-order/contracts/openapi.yaml, specs/010-order-confirmation/contracts/openapi.yaml, specs/019-order-delivery/contracts/openapi.yaml  # OrderAddress += facebookUrl
docs/api-reference.md + docs/swagger/                 # regenerate cùng change
```

**Structure Decision**: Giữ nguyên modular monolith 4 lớp; thay đổi trải 2 module (user sở hữu địa chỉ, order sở hữu snapshot) qua contract thuần túy `internal/contracts` — đúng ranh giới đã có, không tạo module/sharing mới.

## Complexity Tracking

> Không có vi phạm Constitution cần biện minh — bảng này để trống có chủ ý.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| — | — | — |

## Phases (định hướng cho /speckit.tasks)

1. **Domain user** — `address.go`: const/field/validate + `NewAddress`/`Apply` + unit test (đỏ → xanh).
2. **App + mapper user** — DTO, use-case wiring, `changedAddressFields`, mapper + test.
3. **Contract liên module** — `contracts/user.go` + admin lookup mapping.
4. **Infra user + migrations** — `00020` (+ integration test), adapter columns/CRUD/scan.
5. **Presentation user** — DTO, handler required-check, error field const, HTTP test, Swagger regen.
6. **Order lan snapshot** — domain Address, `toAddress`, mapper, DTO, postgres `order.go` + `00021`, handler mapping, chặn giữ chỗ FR-009 ở checkout/edit + test.
7. **Docs & close-out** — `docs/api-reference.md`, `docs/swagger/`, `frontend-guide.md`, `make lint/test/test-integration`, `make check`.
