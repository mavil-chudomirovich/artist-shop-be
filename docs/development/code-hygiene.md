# Code Hygiene

Danh sách kiểm tra trước khi commit và trước khi mở PR. Mục tiêu: bắt lỗi ở mức
rẻ nhất và giữ repo không phình to vì nợ kỹ thuật.

## 1. Trước khi commit

| Kiểm tra | Lệnh |
|---|---|
| Format | `make fmt-check` |
| Module tidy | `make tidy-check` |
| Vet (cả 2 build tag) | `make vet` |
| Lint | `make lint` |
| Test ảnh hưởng | `make test` |
| File lạ trong `git status` | `git status` |

Sửa nhanh:

```bash
make fmt        # gofmt -w .
make tidy       # go mod tidy
make lint GOLANGCI_LINT=<đường/dẫn>   # nếu binary chưa có trong PATH
```

## 2. Trước khi mở PR hoặc push

```bash
make check
```

Chạy đúng những gì CI chạy: format, tidy, vet, lint, unit, integration, build.
Nếu `test-race` báo skip vì máy thiếu gcc, đó là chấp nhận được — CI vẫn chạy đủ.

## 3. Nợ kỹ thuật

| Mẫu | Sai cách | Cách làm |
|---|---|---|
| `TODO` / `FIXME` không kèm mã | `// TODO: fix later` | `// TODO(#issue): …` hoặc không viết; thêm vào `tasks.md` |
| Code chết | hàm không ai gọi, comment đã lỗi thời | xoá; `golangci-lint` (`unused`) sẽ bắt |
| Comment mô tả điều đã đổi | comment còn nhắc logic cũ | sửa hoặc xoá cùng lúc đổi logic |
| Hàm quá dài / file quá lớn | 1 file 800 dòng | tách theo trách nhiệm; `application/implement/<feature>.go` mỗi feature một file |
| Biến trung gian không cần | `tmp := …; result := tmp + 1` | gộp |
| Copy-paste khối logic | hai đoạn giống nhau lệch 1 dòng | một hàm có tham số |
| Nhãn hết hạn | comment cũ hơn 3 tháng không ai đụng tới | xoá hoặc xác nhận còn đúng |

## 4. Bảo mật

| Kiểm tra | Cách |
|---|---|
| Secret không nằm trong code | `grep -rE "(api[_-]?key|secret|password|token)\s*[:=]\s*[\"'][^\"']+" --include=*.go` |
| `.env` không được commit | `.gitignore` chặn; `git status` phải không hiện `.env` |
| Không log secret | key nhạy cảm phải có trong `share/logging/redact.go` |
| Không hardcode URL môi trường | đọc từ config; không có `localhost` trong code production |
| Không cấu hình nhạy cảm trong compose | compose chỉ có `${VAR:-default}`, không có giá trị thật |
| Dependency đáng tin | thêm dependency mới ⇒ ghi lý do vào `research.md`/ADR |

## 5. Encoding và dòng kết thúc

| Kiểm tra | Quy tắc |
|---|---|
| Line ending | **LF** mọi nơi; `.gitattributes` (`* text=auto eol=lf`) đảm bảo điều này |
| Encoding | UTF-8 **không BOM** |
| Dòng cuối | file phải kết thúc bằng newline |
| Ký tự hỏng | không được có `U+FFFD` (ký tự thay thế) trong tài liệu tiếng Việt |

Nếu `gofmt -l .` báo hàng loạt file mà code không đổi, đó thường là CRLF từ
`core.autocrlf=true`:

```bash
git add --renormalize .
```

## 6. Cấu trúc

| Kiểm tra | Quy tắc |
|---|---|
| Đặt file đúng chỗ | code nghiệp vụ trong module; dùng chung trong `internal/share` |
| Package comment | mọi package có `// Package xxx …` |
| Exported có comment | comment bắt đầu bằng tên thành viên |
| Tầng không import ngược | `domain` không import framework; `application` không import `infrastructure` |
| SQL đúng chỗ | trong `infrastructure/implement/postgres`, không ở handler |
| Module không đọc bảng của module khác | qua `internal/contracts` |

## 7. Tài liệu

| Sự kiện | Cập nhật ngay trong thay đổi đó |
|---|---|
| Thêm/sửa/xoá endpoint | `docs/api-reference.md` (Constitution VIII) |
| Thêm biến môi trường | `.env.example` + `docs/configuration.md` |
| Thêm migration | `docs/development/migration.md` (nếu có quy tắc mới) |
| Đổi kiến trúc | `docs/architecture.md` |
| Đổi phạm vi module | `docs/modules.md` + `docs/modules/NN-*.md` |
| Quyết định kỹ thuật đáng nhớ | ADR trong `docs/decisions/` |

Tài liệu hỏng còn tệ hơn không có tài liệu: nó khiến người sau tin sai.

## 8. Hiệu năng

Chỉ tối ưu khi có số đo. Trước khi tối ưu, trả lời:

1. Đo được vấn đề gì? (p95, throughput, kích thước)
2. Ràng buộc thật ở đâu — trong DB, mạng, hay serialize?
3. Tối ưu có làm rõ đọc code khó hơn không?

Tối ưu sớm ở tầng ứng dụng thường vô nghĩa khi nút thắt nằm ở I/O. Ở dự án này,
ràng buộc cần chú ý là: query có index không, truy vấn danh sách có phân trang
không, và có N+1 query trong một use case không.

## 9. Câu hỏi tự kiểm trước khi báo "xong"

- Tôi đã chạy `make check` chưa, và nó sạch thật không?
- Tài liệu liên quan đã cập nhật cùng thay đổi chưa?
- Có task nào trong `tasks.md` phải đánh dấu `[X]` không?
- Có file nào tôi tạo ra ngoài ý muốn không (đã kiểm `git status`)?
- Nếu có người khác đọc diff này thì có hiểu vì sao không?