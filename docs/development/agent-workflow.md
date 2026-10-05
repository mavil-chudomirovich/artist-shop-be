# Agent Workflow

Quy trình làm việc của agent trong repo này. Bốn quy trình, từ nhẹ đến nặng.

## 1. Chọn quy trình theo công việc

| Công việc | Quy trình |
|---|---|
| Sửa typo, sửa comment, đổi câu chữ tài liệu | Làm luôn, gate = `make fmt-check` |
| Sửa lỗi nhỏ trong code | Làm luôn, gate = `make lint` + `make test` |
| Thêm endpoint, đổi nghiệp vụ, đổi schema | Đặc tả Spec Kit |
| Thêm một module mới | Đặc tả Spec Kit đầy đủ |

Quy trình đặc tả là **bắt buộc** khi thay đổi hành vi người dùng thấy được, vì đặc tả
là nơi khai FR/SC và Constitution Check. Sửa lỗi nhỏ không cần, nhưng vẫn phải tuân
thủ constitution và cập nhật tài liệu liên quan.

## 2. Đặc tả một feature (Spec Kit)

```text
/specify      → đặc tả: user story, FR, SC, edge case, checklist
/clarify      → hỏi tối đa 5 câu mơ hồ có ảnh hưởng thật
/plan         → kỹ thuật + Constitution Check + research + data-model + contracts
/analyze      → rà soát nhất quán spec ↔ plan ↔ tasks (chạy trước khi code)
/tasks        → sinh danh sách task theo user story
/implement    → viết code + test, đánh dấu [X] theo từng task
/converge     → đối chiếu code với spec/plan, nốt phần còn thiếu
```

Chạy trọn một feature: `@speckit-orchestrate` (tự điều phối analyze → implement →
converge).

### Quy tắc khi đặc tả

1. **Đặc tả nói WHAT và WHY, không nói HOW.** Không nhét framework, thư viện hay cấu
   trúc thật vào `spec.md` — phần đó thuộc `plan.md`.
2. **Mỗi FR phải kiểm thử được.** Nếu không biết test thì nghĩa là chưa đủ rõ.
3. **SC phải đo được và không phụ thuộc công nghệ.** "Dưới 200 ms p95" là thông số kỹ
   thuật; "khách hoàn tất đặt hàng trong 3 phút" là tiêu chí thành công.
4. **Tối đa 3 marker `[NEEDS CLARIFICATION]`** trong đặc tả. Hỏi đúng những gì không có
   mặc định an toàn và có nhiều cách hiểu khác nhau.
5. Dữ liệu và quan hệ → `data-model.md`; giao diện với bên ngoài → `contracts/`; kịch
   bản chạy được → `quickstart.md`; quyết định kỹ thuật → `research.md` **và** ADR
   trong [`../decisions/`](../decisions/README.md).

### Quy tắc khi lập kế hoạch

- **Constitution Check là cổng chặn.** Có vi phạm thì phải ghi vào Complexity Tracking
  kèm lý do; vô lý ⇒ sửa kế hoạch chứ đừng ghi chú chung chung.
- Ưu tiên giải pháp đơn giản nhất thoả yêu cầu. Dữ liệu nhỏ và chỉ đọc thì đừng tạo bảng.
- Ràng buộc dữ liệu quan trọng phải có ở tầng lưu trữ, không chỉ ở tầng ứng dụng.

### Quy tắc khi sinh task

- Task theo user story để mỗi story giao diện độc lập.
- Test task **trước** implementation task khi là state transition hoặc logic quan trọng.
- Mỗi task phải đủ cụ thể để chạy không cần hỏi thêm: nêu rõ đường dẫn file.
- Đánh dấu `[X]` **ngay khi xong**, không dồn cuối.

### Quy tắc khi implement

1. Đọc `plan.md`, `data-model.md`, `contracts/` và constitution trước khi viết dòng đầu.
2. Test trước, xác nhận test **fail**, rồi mới viết implementation.
3. Làm theo thứ tự: domain → repository → use case → handler → tích hợp.
4. Không lệch khỏi `plan.md` mà không cập nhật plan hoặc ghi lệch trong báo cáo.
5. Sau mỗi phase: chạy gate tương ứng (xem `AGENTS.md` §3).
6. Cập nhật `docs/api-reference.md` ngay khi có endpoint mới.

## 3. Rà soát trước khi kết thúc (converge)

Đối chiếi code hiện tại với `spec.md` + `plan.md` + `tasks.md`:

- FR nào chưa có code hoặc chưa có test?
- Acceptance scenario nào chưa kiểm chứng?
- Quyết định trong plan/ research đã thực sự làm chưa?
- Có vi phạm MUST nào của constitution không?
- Tài liệu authoritative đã khớp hành vi chưa?

Phần còn thiếu → **ghi thêm task vào cuối `tasks.md`**, không sửa task cũ, không sửa
spec/plan. Nếu mọi thứ đã thoả thì báo cáo đã converge.

## 4. Agent không được tự quyết

- **Git**: không `commit`/`push`/`branch`/`checkout`/`merge`/`rebase` khi chưa được yêu
  cầu. Xem [`git-workflow.md`](git-workflow.md).
- Cần dev chọn giữa các phương án → dùng tool `question`, đánh dấu lựa chọn khuyến
  nghị bằng "(Recommended)" và nêu hệ quả từng lựa chọn.
- Tài liệu mâu thuẫn với code → **dừng lại**, nêu mâu thuẫn; tài liệu thắng, code phải
  sửa cho khớp.
- Tài liệu không nói rõ → chọn mặc định hợp lý và **ghi vào `Assumptions`** thay vì
  hỏi suông.

## 5. Báo cáo kết quả

Mỗi lần làm xong, báo cáo ngắn gọn:

1. Đã làm gì (theo task ID nếu có).
2. Đã xác minh bằng lệnh nào, kết quả ra sao.
3. Còn lại gì chưa làm hoặc đang phụ thuộc.
4. Những điều phải quyết định.

Không báo "đã xong" nếu chưa chạy gate tương ứng.