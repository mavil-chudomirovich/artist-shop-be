# Deferred — Hide Guard with Pending Orders (016)

Việc phát hiện khi xây Hide Guard with Pending Orders nhưng **không phải việc của feature này**.
Không mục nào ở đây chặn việc đóng feature 016; không mục nào là task của feature 016.

File này tồn tại vì một ô `- [ ]` trong `tasks.md` mang một ý nghĩa khác.
`- [ ]` nghĩa là *feature này chưa xong*. Việc thuộc về feature khác, hoặc thuộc
chính hạ tầng test, không phải phần chưa xong của feature này; ghi nó như vậy hoặc
giữ một feature đã đóng trông như còn mở, hoặc ép điều kiện thoát bị làm giả.

## Quy tắc cho file này

- Mỗi mục nêu rõ **nó là gì**, **vì sao ngoài phạm vi**, và **điều gì sẽ gỡ được** nó.
  Một mục chỉ nêu triệu chứng thì chưa phải một mục.
- Mục nào làm mất hiệu lực bằng chứng của một task đã tick `[X]` **phải nói rõ và nêu
  số task đó**. Nếu không, `[X]` sẽ bị đọc là không điều kiện trong khi thực tế không phải.
- Nếu một mục hoá ra thuộc feature này, nó quay lại `tasks.md` thành task thật.
  **File này không phải chỗ đỗ.**
- Báo cáo mọi mục trong báo cáo cuối của skill. Hoãn trong im lặng thì vô hiệu mục đích
  của việc ghi lại.

## Các mục

Không có mục nào.

Đợt rà Phase 6 (T022/T023/T025 — tài liệu `docs/api-reference.md`, router của module
category/product, DTO presentation, annotation Swagger của các handler đã chạm, và sinh lại
`docs/swagger/`) không phát hiện nợ ngoài phạm vi: không có `TODO`/`FIXME` trong các file
application đã chạm, không có ràng buộc lưu trữ còn thiếu cho ba mã 409 mới (chúng là từ chối
lúc đọc, không phải bất biến cần unique index/check constraint), và `docs/swagger/` sau khi
sinh lại khớp mã nguồn. Hai ghi nhận trong lúc rà — các ô T018–T021 của US3 chưa tick dù mã
đã có trong cây làm việc, và DTO presentation có thêm member `isVisible` cộng thêm (đã nêu
trong báo cáo Phase 6) — đều thuộc phạm vi feature này hoặc quyết định của người đọc, nên
không ghi thành mục hoãn ở đây. Nếu một đợt rà sau phát hiện nợ ngoài phạm vi, thêm mục
`D-01`, `D-02`, … theo định dạng của template (vấn đề / vì sao ngoài phạm vi / gỡ bằng cách
nào / ảnh hưởng tới task đã tick).
