# Tài liệu `artist-shop-be`

Mọi tài liệu của dự án nằm trong `docs/`.

## Cấu trúc

```
docs/
  README.md               ← file này: mục lục + quy tắc
  getting-started.md      Cài đặt, chạy thử trong 5 phút
  configuration.md        Toàn bộ biến môi trường
  docker.md               Dockerfile, docker-compose.yml, docker-compose.dev.yml
  makefile.md             Toàn bộ target Makefile
  testing.md              Unit / integration / race / lint
  troubleshooting.md      Các lỗi thật đã gặp và cách sửa

  api-reference.md        Toàn bộ endpoint (authoritative)
  architecture.md         Kiến trúc (authoritative)
  modules.md              Danh sách module & lộ trình
  modules/NN-*.md         Phạm vi từng module

  development/            Quy trình làm việc
    README.md             Mục lục nhóm này
    coding-conventions.md Đặt tên, xử lý lỗi, tầng, test
    git-workflow.md       Branch, Conventional Commits, PR
    code-hygiene.md       Checklist trước khi commit và trước PR
    migration.md          Quy tắc migration SQL
    api-testing.md        Kiểm thử endpoint bằng tay, end-to-end
    agent-workflow.md     Quy trình Spec Kit

  system-design/          Thiết kế
    README.md
    design-pattern.md     Pattern dùng + giới hạn + pattern không dùng
    contract-purity.md    Luật cho hợp đồng liên module

  decisions/              ADR — quyết định kiến trúc còn hiệu lực
    README.md             Quy tắc + danh sách
    template.md
    00N-*.md

  product/                Nguồn yêu cầu gốc (không sửa khi triển khai)
    project_overview.md
    backend-spec.md

AGENTS.md                          Quy tắc cho agent: điều hướng, gate, git, code
.specify/memory/constitution.md   Hiến pháp — quy tắc bắt buộc
specs/NNN-*/                       Artifact Spec Kit cho từng feature
```

## Bốn nhóm tài liệu

| Nhóm | Vị trí | Đặc điểm | Khi nào đọc |
|---|---|---|---|
| **Vận hành** | `getting-started`, `configuration`, `docker`, `makefile`, `testing`, `troubleshooting` | Hướng dẫn thao tác, đổi theo code | Cần chạy / debug dự án |
| **Quy trình** | `development/*` | Làm việc theo từng bước | Trước khi sửa code |
| **Thiết kế** | `system-design/*`, `api-reference.md`, `architecture.md`, `modules.md`, `modules/*` | **Authoritative** | Cần hiểu hệ thống |
| **Quyết định** | `decisions/*` | **Authoritative**, ghi lý do | Vì sao lại chọn cách này |
| **Quy tắc** | `../AGENTS.md`, `../.specify/memory/constitution.md` | **Authoritative**, có phiên bản | Trước khi sửa code |

> Khi tài liệu mâu thuẫn: `constitution.md` > `docs/system-design` >
> `docs/development` > tài liệu khác > code.

## Bắt đầu từ đâu

| Bạn muốn | Đọc |
|---|---|
| Chạy được API | [getting-started.md](getting-started.md) |
| Viết/sửa code Go đúng chuẩn | [development/coding-conventions.md](development/coding-conventions.md) |
| Commit / mở PR | [development/git-workflow.md](development/git-workflow.md) |
| Trước khi commit | [development/code-hygiene.md](development/code-hygiene.md) |
| Thêm migration SQL | [development/migration.md](development/migration.md) |
| Kiểm thử endpoint bằng tay | [development/api-testing.md](development/api-testing.md) |
| Đang làm một feature | [development/agent-workflow.md](development/agent-workflow.md) |
| Hiểu pattern trong dự án | [system-design/design-pattern.md](system-design/design-pattern.md) |
| Làm hợp đồng liên module | [system-design/contract-purity.md](system-design/contract-purity.md) |
| Vì sao chọn công nghệ này | [decisions/README.md](decisions/README.md) |
| Hiểu endpoint nào đã có | [api-reference.md](api-reference.md) |
| Hiểu cấu trúc code | [architecture.md](architecture.md) |
| Biết đang làm module nào | [modules.md](modules.md) |

## Quy tắc bắt buộc khi thêm/sửa tài liệu

| Quy tắc | Chi tiết |
|---|---|
| Tài liệu vận hành mới → đặt trong `docs/` | Không tạo thư mục tài liệu mới ở nơi khác |
| Quy trình, convention, checklist → `docs/development/` | Một tài liệu cho mỗi chủ đề, có `README.md` làm mục lục |
| Quyết định kiến trúc còn hiệu lực → ADR trong `docs/decisions/` | Đặt số `NNN-ten-kebab.md`, không sửa nội dung gốc; quyết định nhỏ để trong `research.md` |
| Tài liệu thiết kế phải **authoritative** | `api-reference.md`, `architecture.md`, `modules.md`, `system-design/*` — sửa chỗ này khi hành vi thay đổi |
| Dùng **đường dẫn tương đối** | `[api-reference.md](api-reference.md)` để file chạy được cả trên GitHub lẫn editor |
| Tài liệu `product/` là nguồn yêu cầu gốc | Chỉ cập nhật khi *người đặt hàng* đổi yêu cầu, không sửa để "cho khớp code" |
| Không nhân bản thông tin | Ví dụ error code nằm ở `api-reference.md` thì tài liệu khác **link** tới, không chép lại |

## Quy tắc bắt buộc khi sửa code

Rút gọn từ [hiến pháp](../.specify/memory/constitution.md):

| Quy tắc | Nghĩa |
|---|---|
| **4 lớp** | `domain → application → infrastructure → presentation`, phụ thuộc một chiều |
| `domain` không import framework | Chỉ stdlib + `share/access` |
| **Test-first** cho logic tiền / tồn kho / state machine | Test phải fail trước khi có implementation |
| **Tiền là integer minor units** | Không dùng float cho tiền |
| **Mọi API mới/sửa/xoá → cập nhật `api-reference.md`** | Cùng thay đổi, không để sau |
| **Audit** cho thay đổi admin + sự kiện thanh toán | `audit_logs`: actor, action, target, thời điểm |
| **Secret từ môi trường** | Không commit `.env`, không bake vào Docker layer |
| **`cmd/*` không chứa SQL nghiệp vụ** | Gọi application use case |

## Trạng thái

| Module | Trạng thái |
|---|---|
| 00 Cross-cutting foundation | ✔ Hoàn tất |
| 01 Auth | ✔ Hoàn tất — 13 endpoint |
| 02–16 | ⬜ Chưa bắt đầu |

Chi tiết: [modules.md](modules.md).

## Quy trình làm việc với module mới

Dự án dùng **Spec Kit** — mỗi module là một feature độc lập:

```
docs/modules/NN-xxx.md
   ↓ /speckit.specify     đặc tả (FR / SC / user story)
   ↓ /speckit.clarify     làm rõ điểm mơ hồ
   ↓ /speckit.plan        kỹ thuật + Constitution Check
   ↓ /speckit.tasks       danh sách task
   ↓ /speckit.analyze     kiểm tra trước khi code
   ↓ /speckit.implement   viết code + test
   ↓ /speckit.converge    đối chiếu code với spec/plan, nốt gap
```

Đây là slash command của agent (`/speckit.*`), không phải binary trong repo.
Module 01 là ví dụ tham chiếu: [`../specs/001-user-auth/`](../specs/001-user-auth/).

## Lệnh kiểm tra trước khi commit

```bash
make check
```

Chạy đúng những gì CI chạy: format, tidy, vet, lint, unit test, integration test,
race detector, build.