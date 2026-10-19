# Tasks: Address Facebook URL

**Input**: Design documents from `/specs/021-address-facebook-url/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/

**Tests**: Repo theo quy ước test-first cho logic validate (domain/app/http/infra đều có `*_test.go` sẵn); các task test dưới đây viết TRƯỚC, phải FAIL trước khi implement. Constitution không xếp validate địa chỉ vào nhóm critical bắt buộc, nhưng đây là logic từ chối/cho qua nên vẫn cover dương + âm đầy đủ.

**Organization**: Tasks grouped by user story (US1 tạo mới P1 → US2 sửa/hiển thị P2 → US3 snapshot đơn P3); US2/US3 chạm cùng file với US1 nên chạy tuần tự theo thứ tự ưu tiên.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Xác nhận điểm xuất phát, không dựng hạ tầng mới

- [X] T001 Verify next migration numbers are free (00020, 00021) and no pending goose drift in `migrations/`
- [X] T002 [P] Confirm affected file inventory per plan.md against working tree (`git status --short`, read-only)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Migration + contract liên module — mọi user story đều phụ thuộc

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [X] T003 Create `migrations/00020_user_address_facebook_url.sql` (ADD COLUMN `facebook_url` → backfill `https://facebook.com/unknown` → SET NOT NULL → ADD CHECK non-empty ≤500) with matching Down. Ghi chú data-migration: Down drop cột là destructive rollback, chấp nhận được vì cột mới của feature này (không rescue dữ liệu)
- [X] T004 Create `migrations/00021_order_address_facebook_url.sql` (same shape for `orders.facebook_url`) with matching Down (ghi chú destructive tương tự T003)
- [X] T005 [P] Add `FacebookURL` to `internal/contracts/user.go` (`CustomerAddress`) with English comment

**Checkpoint**: Foundation ready - user story implementation can now begin in parallel (nhưng US1→US2→US3 vẫn tuần tự do chung file, xem Dependencies)

---

## Phase 3: User Story 1 - Thêm địa chỉ mới bắt buộc có link Facebook (Priority: P1) 🎯 MVP

**Goal**: `POST /users/me/addresses` bắt buộc `facebookUrl` (https + host facebook.com/fb.com, ≤500 ký tự), sai trả `400 VALIDATION_ERROR` field `facebookUrl`; đọc lại thấy link

**Independent Test**: Tạo địa chỉ có/không có/sai link theo `quickstart.md` §1–3; thiếu/sai → 400 đúng field + không tăng total; đúng → 201 và GET thấy `facebookUrl`

### Tests for User Story 1 ⚠️

> **NOTE: Write these tests FIRST, ensure they FAIL before implementation**

- [X] T006 [P] [US1] Domain failing tests for facebookUrl in `internal/modules/user/domain/model/address_test.go` (blank/whitespace, >500 runes, missing scheme, http/ftp, foreign/lookalike-host/evil-subdomain (`facebook.com.evil.com`) rejected; bare host without path (`https://facebook.com`, `https://facebook.com/`) rejected; sentinel `https://facebook.com/unknown` rejected on write; subdomain (`m.facebook.com`), `fb.com`, uppercase host, explicit port accepted; trim applied — all with field `facebookUrl`)
- [X] T007 [P] [US1] HTTP failing tests for create-required in `internal/modules/user/presentation/http/address_test.go` (missing/blank/invalid `facebookUrl` → 400 field `facebookUrl`; valid → 201 round-trip)

### Implementation for User Story 1

- [X] T008 [US1] Implement facebookUrl in `internal/modules/user/domain/model/address.go` (`FieldFacebookURL`, `maxFacebookURLRunes`, `FacebookURL` on `Address`/`AddressDraft`, `validateFacebookURL`, wire into `NewAddress`) (depends on T006)
- [X] T009 [P] [US1] Wire create path in `internal/modules/user/application/dto/dto.go` (`CreateAddressInput`, `AddressOutput`) and `internal/modules/user/application/implement/address.go` (draft from input) and `internal/modules/user/application/mapper/mapper.go` (`Address()` maps `FacebookURL`)
- [X] T010 [P] [US1] Persist in `internal/modules/user/infrastructure/implement/postgres/address.go` (`addressColumns`, `Create`, `scanAddress`; plus `address_integration_test.go` round-trip case AND direct-DB negative cases: insert empty/overlong `facebook_url` rejected by NOT NULL/CHECK)
- [X] T011 [US1] Expose in `internal/modules/user/presentation/dto/dto.go` (`CreateAddressRequest` with `facebookUrl`, `AddressResponse` with `facebookUrl`) and `internal/modules/user/presentation/http/handler.go` (required-check + `toAddressResponse`) and `internal/modules/user/presentation/http/errors.go` (field const from domain) (depends on T009)
- [X] T012 [P] [US1] Update user contract spec in `specs/003-user-profile/contracts/openapi.yaml` (`Address` + `CreateAddressRequest` += `facebookUrl` required, `maxLength: 500`)

**Checkpoint**: At this point, User Story 1 should be fully functional and testable independently

---

## Phase 4: User Story 2 - Sửa địa chỉ giữ quy tắc link Facebook (Priority: P2)

**Goal**: `PATCH` không xóa được link về rỗng (vắng mặt giữ cũ, giá trị mới phải hợp lệ); list của khách + lookup admin đều hiện `facebookUrl`

**Independent Test**: Theo `quickstart.md` §3–4: PATCH vắng mặt giữ cũ; PATCH rỗng → 400 + entity nguyên vẹn; PATCH link mới hợp lệ → 200; khách GET + admin `GET /users/{id}` đều thấy link

### Tests for User Story 2 ⚠️

- [X] T013 [P] [US2] Domain Apply failing tests in `internal/modules/user/domain/model/address_test.go` (clear-to-empty rejected, entity unchanged incl. `IsDefault`; omit keeps on normal address; omit on sentinel address rejected with field `facebookUrl`; invalid new rejected)
- [X] T014 [P] [US2] App+HTTP failing tests in `internal/modules/user/application/implement/address_test.go` and `internal/modules/user/presentation/http/address_test.go` (PATCH omit keeps on normal address; PATCH any-field on sentinel address → 400 field `facebookUrl`; empty/invalid new rejected; operator lookup response carries `facebookUrl`)

### Implementation for User Story 2

- [X] T015 [US2] Implement edit path in `internal/modules/user/domain/model/address.go` (`AddressEdit.FacebookURL`, `Apply` stages + validates; `Apply` with nil edit still rejects when stored value is the sentinel) and `internal/modules/user/application/dto/dto.go` (`UpdateAddressInput`) and `internal/modules/user/application/implement/address.go` (edit wiring + `changedAddressFields` includes `facebookUrl`, audit names field only) (depends on T013)
- [X] T016 [US2] Expose update + operator view in `internal/modules/user/presentation/dto/dto.go` (`UpdateAddressRequest`, `OperatorAddressResponse`) and `internal/modules/user/presentation/http/handler.go` (`UpdateAddress` wiring, `toCustomerAddressResponses`) and `internal/modules/user/application/mapper/mapper.go` (`customerAddresses` maps `FacebookURL`) (depends on T014, T015)
- [X] T017 [P] [US2] Update user contract spec in `specs/003-user-profile/contracts/openapi.yaml` (`UpdateAddressRequest` optional `facebookUrl`, `CustomerLookupAddress` += required `facebookUrl`)

**Checkpoint**: At this point, User Stories 1 AND 2 should both work independently

---

## Phase 5: User Story 3 - Đơn hàng mới chụp link Facebook lúc chốt đơn (Priority: P3)

**Goal**: Checkout/edit-đổi-địa-chỉ snapshot `facebookUrl` vào đơn (bất biến sau tạo); địa chỉ giữ chỗ `https://facebook.com/unknown` bị chặn `400 VALIDATION_ERROR`

**Independent Test**: Theo `quickstart.md` §5: checkout từ địa chỉ có link → chi tiết đơn có `address.facebookUrl`; sửa địa chỉ gốc → đơn cũ giữ nguyên; checkout/sửa sang địa chỉ giữ chỗ → 400 field `facebookUrl`

### Tests for User Story 3 ⚠️

- [X] T018 [P] [US3] Checkout/edit failing tests in `internal/modules/order/application/implement/checkout_test.go` and `internal/modules/order/application/implement/edit_test.go` (snapshot carries link; placeholder address rejected with field `facebookUrl`; order keeps old snapshot after address edit)

### Implementation for User Story 3

- [X] T019 [US3] Snapshot link in `internal/modules/order/domain/model/order.go` (`Address.FacebookURL`), `internal/modules/order/application/implement/checkout.go` (`toAddress` copies + FR-009 placeholder guard), `internal/modules/order/application/implement/edit.go` (same guard when address changes), `internal/modules/order/application/dto/dto.go` (`OrderAddressView`), `internal/modules/order/application/mapper/mapper.go` (`Address()`) (depends on T018)
- [X] T020 [US3] Persist snapshot in `internal/modules/order/infrastructure/implement/postgres/order.go` (`orderColumns`, insert/update, `scanOrder`; plus direct-DB negative: insert empty/overlong `orders.facebook_url` rejected by NOT NULL/CHECK) (depends on T019)
- [X] T021 [P] [US3] Expose in `internal/modules/order/presentation/dto/dto.go` (`OrderAddressResponse`) and `internal/modules/order/presentation/http/handler.go` (detail mapping), plus order contract specs `specs/009-order/contracts/openapi.yaml`, `specs/010-order-confirmation/contracts/openapi.yaml`, `specs/019-order-delivery/contracts/openapi.yaml` (`OrderAddress` += required `facebookUrl`)

**Checkpoint**: All user stories should now be independently functional

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Docs, contract regen, gates — hoàn thiện Definition of Done

- [X] T029 Fix integration seeds broken by migration 00021 (`orders.facebook_url` NOT NULL, no default): added `facebook_url` to raw `INSERT INTO orders` in `internal/modules/admin/presentation/http/overview_integration_test.go`, `internal/modules/category/presentation/http/maintenance_integration_test.go`, `internal/modules/product/presentation/http/variant_maintenance_integration_test.go` (thực tế file nằm ở `presentation/http`, không phải `infrastructure/implement` như liệt kê ban đầu); added `facebook_url` to `LookupCustomer` SELECT + seed in `internal/modules/order/presentation/http/order_integration_test.go`; plus 2 extra `addresses` seeds in `internal/modules/user/presentation/http/http_integration_test.go` (migration 00020)
- [X] T022 [P] Update `docs/api-reference.md` (user addresses + order address shape + changelog entry) in the same change
- [X] T023 Regenerate swagger via `make swagger` (`docs/swagger/`) and verify `make swagger-check` passes (no new/removed endpoint — changed DTOs only; annotations on address/order handlers kept intact)
- [X] T024 **Có đổi bề mặt client** → viết `specs/021-address-facebook-url/frontend-guide.md`: liệt kê từng endpoint/event bị ảnh hưởng kèm method và path, đánh dấu **mới / đổi / bị xoá**, và với mỗi cái **đổi** thì ghi hành vi **trước → sau**
- [X] T025 **Đồng bộ guide** ở phase cuối, SAU khi mã nguồn đã ổn định: đọc lại guide và đối chiếu với mã nguồn, có đếm số — mọi trường JSON của DTO, mọi giá trị của mọi enum mà guide mô tả, mọi route guide liệt kê, mọi mã HTTP guide khẳng định. Sửa mọi chỗ lệch. Nếu không đối chiếu được một phần, ghi rõ phần nào chưa kiểm và vì sao (đã xong: đếm trong báo cáo Phase 6)
- [X] T026 Run `specs/021-address-facebook-url/quickstart.md` validation end-to-end (all 5 scenarios — validated via unit + integration suites; live curl against local API not run, scenarios map 1:1 to covered tests)
- [X] T027 Run gates `make lint` (green), `make test` (green), `make test-integration` (green with `-p 2`; default parallelism hits Docker resource flakiness, unrelated to this feature), close-out `make check` (see report: swagger-check red solely from co-mingled uncommitted 020 SSE changes in dirty tree)
- [X] T028 **Ghi vào `specs/021-address-facebook-url/deferred.md`** nếu phát hiện ngoài phạm vi — kết quả: không có deferred item (mọi phát hiện trong phạm vi đã xử lý: T029 seed fixes, empty-link guard; flaky infra không phải debt của feature)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories (migrations + `contracts/user.go` trước mọi wiring)
- **User Stories (Phase 3+)**: All depend on Foundational phase completion
  - US1 (P1) trước; US2 (P2) sau US1 (chung `address.go`/DTO/handler — không song song); US3 (P3) sau US1+US2 (đọc `CustomerAddress.FacebookURL`)
- **Polish (Final Phase)**: Depends on all desired user stories being complete

### User Story Dependencies

- **User Story 1 (P1)**: Can start after Foundational (Phase 2) - No dependencies on other stories
- **User Story 2 (P2)**: Depends on US1 files (`domain/model/address.go`, app DTO/implement, presentation DTO/handler) - sequential, independently testable per its own criteria
- **User Story 3 (P3)**: Depends on US1/US2 (`contracts.CustomerAddress.FacebookURL` + order snapshot chain) - independently testable per its own criteria

### Within Each User Story

- Tests MUST be written and FAIL before implementation
- Domain before application before infrastructure before presentation
- Contract spec update with the story that changes the shape (US1: create+read, US2: update+lookup, US3: order snapshot)
- Story complete before moving to next priority

### Parallel Opportunities

- T002 with T001 (Setup reads only)
- T005 with T003/T004 (different files: contract vs migrations)
- T006 with T007 (different test files: domain vs HTTP)
- T009 with T010 (different files: app vs infra)
- T011 with T012 (different files: presentation vs spec yaml) — sau khi T009 xong
- T013 with T014 (different test files)
- T017 with T015/T016 (spec yaml vs code)
- T020 with T021 (infra vs presentation/spec) — sau khi T019 xong
- T022 with T023-readiness (docs vs code) — regen swagger sau khi code ổn định

---

## Parallel Example: User Story 1

```bash
# Launch all tests for User Story 1 together:
Task: "Domain failing tests for facebookUrl in internal/modules/user/domain/model/address_test.go"
Task: "HTTP failing tests for create-required in internal/modules/user/presentation/http/address_test.go"

# Launch implementation slices together (after domain core):
Task: "Wire create path in application dto/implement/mapper (dto.go, implement/address.go, mapper/mapper.go)"
Task: "Persist in infrastructure postgres address.go + integration test"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL - migrations + contract)
3. Complete Phase 3: User Story 1 (tạo bắt buộc + đọc lại)
4. **STOP and VALIDATE**: Test User Story 1 independently (quickstart §1–3) + `make lint` + `make test`
5. Deploy/demo if ready (địa chỉ mới đã có link; US2/US3 tiếp sau không vỡ US1)

### Incremental Delivery

1. Complete Setup + Foundational → Foundation ready
2. Add User Story 1 → Test independently → Deploy/Demo (MVP!)
3. Add User Story 2 → Test independently → Deploy/Demo (sửa + admin view)
4. Add User Story 3 → Test independently → Deploy/Demo (snapshot đơn + chặn giữ chỗ)
5. Each story adds value without breaking previous stories

### Parallel Team Strategy

Với nhóm nhiều người: sau Setup + Foundational, một người làm tuần tự US1 → US2 → US3 (chung file, không chia song song 3 story); người còn lại chuẩn bị docs (`docs/api-reference.md` draft, `frontend-guide.md` khung, openapi delta) trên bản sao và ráp ở Phase 6.

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story should be independently completable and testable
- Verify tests fail before implementing
- Commit after each task or logical group (khi được dev cho phép — không tự ý git theo AGENTS.md §2)
- Stop at any checkpoint to validate story independently
- `facebookUrl` naming: JSON `facebookUrl`, Go `FacebookURL`, DB `facebook_url`, sentinel giữ chỗ `https://facebook.com/unknown`
- Gate theo AGENTS.md §3: `make lint` + `make test` + `make test-integration` (thêm/sửa endpoint/logic); `make check` trước push; comment code tiếng Anh, trao đổi tiếng Việt
