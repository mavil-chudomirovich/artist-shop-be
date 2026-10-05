# Agent Rules & Navigation

Dự án: **artist-shop-be** — modular monolith Go 1.26, PostgreSQL, Redis, REST API dưới
`/api/v1`. Media lưu ở Cloudinary. Deploy Docker trên VPS.

## 1. Khởi động & điều hướng

- **Đọc file này trước tiên**, rồi chỉ đọc tài liệu liên quan tới task.
- **Không đọc toàn bộ `docs/`** mỗi lần. Chọn theo loại việc:

| Việc | Đọc |
|---|---|
| Sửa code, đặt tên, xử lý lỗi, viết test | [docs/development/](docs/development/README.md) |
| Đổi kiến trúc, thêm module, boundary giữa module | [docs/system-design/](docs/system-design/README.md) |
| Biết hệ thống đã có endpoint nào | [docs/api-reference.md](docs/api-reference.md) |
| Chạy, cấu hình, debug | [docs/getting-started.md](docs/getting-started.md), [docs/docker.md](docs/docker.md), [docs/troubleshooting.md](docs/troubleshooting.md) |
| Vì sao lại chọn cách này | [docs/decisions/](docs/decisions/README.md) |

- **Skills** mô tả *workflow*, không phải tài liệu:
  - `.opencode/skills/` — gọi bằng `@<skill-name>`.
  - Chạy trọn một feature (specify → clarify → plan → tasks → analyze → implement → converge):
    `@speckit-orchestrate`.
  - `@speckit-*` khác dùng cho từng bước một.

## 2. Quy tắc bắt buộc về Git

- **KHÔNG tự thao tác git thay đổi lịch sử.** Không chạy `commit`, `push`, `tag`,
  `branch`, `checkout`, `merge`, `rebase`, `reset`, `amend`, `restore`, `clean`
  khi chưa được dev yêu cầu rõ ràng. Được đọc: `git status`, `git log`, `git diff`,
  `git ls-files`.
- Điều này vẫn áp dụng khi dev đã duyệt một plan có nhiều commit — mỗi thao tác git
  vẫn cần được cho phép riêng.
- Khi được phép commit: message **tiếng Anh**, theo
  [Conventional Commits](https://www.conventionalcommits.org/) —
  xem [docs/development/git-workflow.md](docs/development/git-workflow.md).
- `git add` chỉ đúng các file thuộc thay đổi đang làm, không `git add -A` bừa bãi.

## 3. Gate theo cỡ thay đổi

Bảng dưới là **nguồn duy nhất** cho gate. Chọn đúng mức, đừng chạy `make check` cho
một thay đổi typo.

| Thay đổi | Gate |
|---|---|
| Sửa nhỏ (typo, doc, comment) | `make fmt-check` |
| Sửa một phase của feature | `make lint` + `make test` |
| Thêm/sửa endpoint hoặc logic nghiệp vụ | `make lint` + `make test` + `make test-integration` |
| Trước `push` / `tag` / khi mở PR | `make check` đầy đủ |

- `make lint` = format + tidy + vet + golangci-lint → **fast gate**.
- `make check` = tất cả + test + integration + build → **close-out gate**.
- `test-race` cần `CGO_ENABLED=1` và gcc; thiếu thì `make check` tự bỏ qua và in
  thông báo — không cần ép chạy.

## 4. Quy tắc bắt buộc về code

- **Tuân thủ Constitution**: với **mọi** hành động, kể cả sửa lỗi đơn giản, đọc và
  tuân thủ [`.specify/memory/constitution.md`](.specify/memory/constitution.md).
  Constitution thắng mọi tài liệu khác khi có xung đột.
- Bốn lớp bắt buộc: `presentation → application → domain`, `infrastructure → domain`
  và `application/interface`. `domain` chỉ import stdlib + `share/access`.
- Mã dùng chung nhiều module → `internal/share/`. Hợp đồng liên module →
  `internal/contracts/`.
- **Đặc biệt trong dự án này**:
  - Id của chủ tài khoản **luôn lấy từ session**, không bao giờ từ body/query của
    client — đây là cách duy nhất chống được truy cập chéo.
  - Tiền và tồn kho dùng **integer minor units + currency**, không dùng float.
  - Ràng buộc dữ liệu quan trọng phải có **ràng buộc ở tầng lưu trữ** (unique index,
    check constraint), không chỉ ở tầng ứng dụng.
- **Ưu tiên target Makefile** hơn lệnh thô (xem [docs/makefile.md](docs/makefile.md)).
- **Comment trong code và trong `.env` viết bằng tiếng Anh.** Tài liệu `docs/` và trao
  đổi với dev dùng tiếng Việt.
- **Luôn dùng đường dẫn tương đối** khi tham chiếu file trong doc, spec, comment.
  Tuyệt đối không dùng đường dẫn tuyệt đối kiểu `file:///C:/...`.

## 5. Thêm tài liệu hoặc sửa code

- Tài liệu vận hành mới → `docs/development/` hoặc `docs/system-design/`.
- Tài liệu thiết kế authoritative (`docs/api-reference.md`, `docs/architecture.md`,
  `docs/modules.md`) phải sửa **cùng lúc** với hành vi nó mô tả.
- Quyết định kỹ thuật đáng nhớ → viết ADR trong `docs/decisions/`, **không** chỉ ghi
  trong `research.md` của feature.
- Thêm/sửa/xoá endpoint → cập nhật `docs/api-reference.md` trong cùng thay đổi
  (Constitution VIII).

## 6. Khi có xung đột hoặc thiếu quyết định

- Nếu tài liệu và code mâu thuẫn, **dừng lại** và nêu rõ mâu thuẫn.
- Khi cần dev quyết định: dùng **tool `question`** với các lựa chọn cụ thể, đánh dấu
  lựa chọn khuyến nghị bằng "(Recommended)" và nêu hệ quả của từng lựa chọn.
  Chỉ hỏi bằng văn bản tự do khi thật sự không có lựa chọn rời nào.
- Khi tài liệu không nói rõ, chọn mặc định hợp lý và **ghi lại giả định** trong phần
  `Assumptions` của spec hoặc trong báo cáo, thay vì hỏi suông.

## 7. Thứ tự ưu tiên khi tài liệu mâu thuẫn

```
constitution  >  docs/system-design  >  docs/development  >  tài liệu khác  >  code
```

Nếu code mâu thuẫn với tài liệu, tài liệu thắng và code phải được sửa cho khớp.