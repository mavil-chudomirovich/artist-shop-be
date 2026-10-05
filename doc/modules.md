# Backend Modules & Delivery Roadmap

Mục lục các module backend, tách từ `project_overview.md` và `backend-spec.md`.
Mỗi module có một file riêng trong `doc/modules/` để làm nguồn cho quy trình
Spec Kit (`/speckit.specify` → `/speckit.plan` → `/speckit.tasks` → `/speckit.implement`).

## Nguyên tắc

- Mỗi module sở hữu dữ liệu của mình; giao tiếp liên module qua interface/domain event.
- Phụ thuộc một chiều: module nền tảng trước, module nghiệp vụ sau.
- Một module = một feature Speckit, triển khai và kiểm thử độc lập.

## Danh sách module

| # | Module | File | Giai đoạn | Phụ thuộc | Đã specify |
|---|--------|------|-----------|-----------|------------|
| 00 | Cross-cutting foundation | [00-cross-cutting.md](modules/00-cross-cutting.md) | 0 | — | ✔ `specs/002-cross-cutting-foundation` |
| 01 | Auth | [01-auth.md](modules/01-auth.md) | 0 | cross-cutting | ✔ `specs/001-user-auth` |
| 02 | User | [02-user.md](modules/02-user.md) | 0 | auth | ⬜ |
| 03 | Category | [03-category.md](modules/03-category.md) | 0 | cross-cutting | ⬜ |
| 04 | Product | [04-product.md](modules/04-product.md) | 1 | category | ⬜ |
| 05 | Inventory | [05-inventory.md](modules/05-inventory.md) | 1 | product | ⬜ |
| 06 | Cart | [06-cart.md](modules/06-cart.md) | 1 | product | ⬜ |
| 07 | Order | [07-order.md](modules/07-order.md) | 1 | cart, product, inventory, user | ⬜ |
| 08 | Payment | [08-payment.md](modules/08-payment.md) | 1 | order | ⬜ |
| 09 | Shipping | [09-shipping.md](modules/09-shipping.md) | 1 | order | ⬜ |
| 10 | Commission | [10-commission.md](modules/10-commission.md) | 2 | user, payment | ⬜ |
| 11 | Chat | [11-chat.md](modules/11-chat.md) | 2 | user | ⬜ |
| 12 | Notification | [12-notification.md](modules/12-notification.md) | 2 | user | ⬜ |
| 13 | Review | [13-review.md](modules/13-review.md) | 3 | product, order | ⬜ |
| 14 | Wishlist | [14-wishlist.md](modules/14-wishlist.md) | 3 | product, user | ⬜ |
| 15 | Content | [15-content.md](modules/15-content.md) | 3 | cross-cutting | ⬜ |
| 16 | Admin | [16-admin.md](modules/16-admin.md) | 1+ | tất cả | ⬜ |

## Thứ tự triển khai

**Giai đoạn 0 — Nền tảng**: 00 cross-cutting → 01 auth → 02 user → 03 category
**Giai đoạn 1 — Shop (MVP)**: 04 product → 05 inventory → 06 cart → 07 order → 08 payment → 09 shipping → 16 admin
**Giai đoạn 2 — Commission**: 10 commission → 11 chat → 12 notification
**Giai đoạn 3 — Mở rộng**: 13 review → 14 wishlist → 15 content

## Việc phải chốt trước khi specify

- `10-commission`: thống nhất state machine giữa `project_overview.md` và `backend-spec.md`.
- `07-order`: danh sách trạng thái đơn chuẩn.
- `12-notification` / `01-auth`: chọn nhà cung cấp email.
- `00-cross-cutting`: chọn framework, thư viện DB, công cụ migration.

## Trạng thái Spec Kit

- `⬜` chưa specify — `✅` đã có spec — `🟦` đã có plan — `🟩` đã có tasks — `✔` đã implement

## Tham chiếu gốc

- `doc/project_overview.md`, `doc/backend-spec.md`, `.specify/memory/constitution.md`
