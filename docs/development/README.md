# Quy trình phát triển

Tài liệu vận hành cho người và agent làm việc trong repo này. Tài liệu thiết kế
nằm ở [`../system-design/`](../system-design/README.md), quyết định đã chốt nằm ở
[`../decisions/`](../decisions/README.md).

Khi tài liệu mâu thuẫn, theo thứ tự:
`constitution` > `docs/system-design` > `docs/development` > tài liệu khác > code.

| Tài liệu | Dùng khi nào |
|---|---|
| [coding-conventions.md](coding-conventions.md) | Viết hoặc sửa code Go: đặt tên, xử lý lỗi, tổ chức file |
| [git-workflow.md](git-workflow.md) | Commit, đặt tên branch, mở pull request |
| [code-hygiene.md](code-hygiene.md) | Trước khi commit và trước khi mở PR |
| [migration.md](migration.md) | Thêm hoặc sửa migration SQL |
| [api-testing.md](api-testing.md) | Kiểm thử endpoint bằng tay, end-to-end |
| [agent-workflow.md](agent-workflow.md) | Chạy một feature qua Spec Kit |

## Bốn câu hỏi trước khi viết code

1. Đây là **nghiệp vụ** hay **hạ tầng**? Nghiệp vụ thuộc một module; hạ tầng dùng
   chung thì vào `internal/share/`.
2. Nó thuộc tầng nào? Xem [coding-conventions.md](coding-conventions.md#tầng-và-phụ-thuộc).
3. Có cần ràng buộc ở **tầng lưu trữ** không? Nếu là bất biến dữ liệu thì có.
4. Test nào chứng minh nó đúng? Nếu là state transition hoặc logic tiền thì viết
   test **trước** (Constitution IV).

## Lệnh thường dùng

```bash
make help            # index các nhóm lệnh
make check           # đúng những gì CI chạy
make up              # db + redis + api
make up-tools        # thêm port host + Mailpit
```

Chi tiết: [`../makefile.md`](../makefile.md).
