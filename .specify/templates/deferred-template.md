# Deferred — {{feature name}}

Việc phát hiện khi xây {{feature name}} nhưng **không phải việc của feature này**.
Không mục nào ở đây chặn việc đóng {{feature name}}; không mục nào là task của
{{feature name}}.

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

<!--
Định dạng mỗi mục:

### D-nn — <tiêu đề ngắn>

- **Vấn đề**: <cái gì sai, cụ thể đủ để người đọc tái hiện hoặc đánh giá mà không cần
  suy luận lại>
- **Vì sao ngoài phạm vi**: <vì sao không phải việc của {{feature name}} — không phải
  "hết thời gian", không phải "khó", mà là ranh giới thật: trách nhiệm của feature khác,
  harness test dùng chung, một ràng buộc của bên thứ ba>
- **Gỡ bằng cách nào**: <bước gỡ cụ thể, và nó nên nằm ở đâu>
- **Ảnh hưởng tới task đã tick**: <số task mà bằng chứng bị suy yếu, hoặc "không">

Xoá khối comment này khi có mục thật đầu tiên.
-->
