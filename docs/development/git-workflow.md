# Git Workflow

## 1. Nguyên tắc cho agent

- **Không tự thao tác git thay đổi lịch sử.** Không chạy `commit`, `push`, `tag`,
  `branch`, `checkout`, `merge`, `rebase`, `reset`, `amend`, `restore`, `clean` khi
  chưa được dev yêu cầu rõ ràng.
- Được đọc bất kỳ lúc nào: `git status`, `git diff`, `git log`, `git ls-files`.
- Kể cả khi dev đã duyệt một plan có nhiều commit, **mỗi thao tác git vẫn cần được
  cho phép riêng**. Plan được duyệt ≠ được phép commit.
- `git add` chỉ đúng các file thuộc thay đổi đang làm. Không `git add -A` / `git add .`
  khi repo còn thay đổi chưa xong của người khác.
- Sau khi được phép: báo lại rõ đã commit gì, trên branch nào, với message gì.

## 2. Đặt tên branch

| Loại | Mẫu | Ví dụ |
|---|---|---|
| Feature của Spec Kit | `NNN-<short-name>` | `003-user-profile` |
| Sửa lỗi | `fix/<mô-tả-ngắn>` | `fix/refresh-token-reuse` |
| Chỉnh tài liệu | `docs/<mô-tả-ngắn>` | `docs/docker-setup` |
| Bảo trì | `chore/<mô-tả-ngắn>` | `chore/golangci-v2` |

`NNN` lấy từ thư mục feature trong `specs/`. Feature mới đã có branch thì không cần tạo
branch khác.

## 3. Message commit — Conventional Commits

```
<type>(<scope>): <mô tả ngắn bằng tiếng Anh>
```

**Tiếng Anh**, imperative mood, không chấm cuối, ≤ 72 ký tự dòng đầu.

| `type` | Dùng khi |
|---|---|
| `feat` | Thêm tính năng mới |
| `fix` | Sửa lỗi |
| `refactor` | Đổi cấu trúc, không đổi hành vi |
| `perf` | Tối ưu hiệu năng |
| `test` | Chỉ thêm/sửa test |
| `docs` | Chỉ tài liệu |
| `build` | Build, dependency, Dockerfile, CI |
| `ci` | Workflow, gate |
| `chore` | Việc bảo trì khác |
| `revert` | Hoàn tác |

`scope` là phạm vi nhỏ nhất hợp lý: module (`auth`, `user`, `address`), tầng dùng
chung (`middleware`, `logging`, `database`) hoặc phạm vi tài liệu.

```bash
# Đúng
git commit -m "feat(user): add address book with a single default invariant"
git commit -m "fix(auth): revoke all sessions when a refresh token is replayed"
git commit -m "docs(adr): record the embedded administrative dataset decision"

# Sai
git commit -m "update code"
git commit -m "fix bug"
```

## 4. Commit gộp thế nào

| Trường hợp | Cách commit |
|---|---|
| Một task trong `tasks.md` | 1 commit, đánh dấu `[X]` trong commit đó |
| Một nhóm task cùng lớp, ví dụ 3 test file | 1 commit nếu tách ra thì vô nghĩa |
| Một phase của feature | 1–3 commit theo nhóm logic |
| Tài liệu + code ảnh hưởng lẫn nhau | **Cùng một commit** — tài liệu và hành vi phải khớp |
| Sửa typo | 1 commit riêng, đừng trộn vào thay đổi chức năng |

Không commit file sinh ra ngoài ý muốn: `.env`, `bin/`, `coverage.out`, file tạm.

## 5. Pull request

Trước khi mở PR:

```bash
make check          # đầy đủ, giống CI
git status          # không còn file lạ
```

PR cần nêu rõ:

| Mục | Nội dung |
|---|---|
| Feature | `specs/NNN-…` hoặc mô tả ngắn |
| Constitution Check | Có vi phạm nguyên tắc nào không (không thì ghi "PASS") |
| API | Có thêm/sửa/xoá endpoint không; `docs/api-reference.md` đã cập nhật chưa |
| Migration | Có migration mới không; có `Down` không |
| Test | Đã chạy `make test` / `make test-integration` chưa |
| Gate đã chạy | fast gate hay close-out gate |

Một PR nên là **một feature hoặc một phase của feature**. Đừng trộn hai feature.

## 6. Đánh dấu tiến độ

Task trong `tasks.md` được đánh dấu `[X]` **ngay khi hoàn thành**, không dồn đến cuối.
Converge đọc `tasks.md` để biết còn thiếu gì — nếu không cập nhật, nó sẽ sinh lại task
đã làm.

## 7. Khi có xung đột

- Không tự giải quyết conflict bằng `merge`/`rebase`.
- Báo dev kèm file bị conflict và hai bên khác nhau ở đâu, rồi chờ chỉ đạo.