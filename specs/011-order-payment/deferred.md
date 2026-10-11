# Deferred — Online Order Payment

Việc phát hiện khi xây **Online Order Payment** (`specs/011-order-payment`, module 08 Payment) nhưng **không
phải việc của feature này**. Không mục nào ở đây chặn việc đóng feature; không mục nào là task của nó.

File này tồn tại vì một ô `- [ ]` trong `tasks.md` mang một ý nghĩa khác. `- [ ]` nghĩa là *feature này
chưa xong*. Việc thuộc về feature khác, hoặc thuộc chính hạ tầng vận hành, không phải phần chưa xong của
feature này; ghi nó như vậy hoặc giữ một feature đã đóng trông như còn mở, hoặc ép điều kiện thoát bị làm
giả.

> **Feature này là module cuối của V1.0.** Cùng với nó, toàn bộ `00`–`08` của kế hoạch V1.0 (ADR 015 §5) đã
> được giao. Các mục dưới đây là mở rộng nằm **ngoài** phạm vi V1.0 hoặc là bước **vận hành** cần credential
> thật, không phải phần còn thiếu của module 08.

## Quy tắc cho file này

- Mỗi mục nêu rõ **nó là gì**, **vì sao ngoài phạm vi**, và **điều gì sẽ gỡ được** nó. Một mục chỉ nêu
  triệu chứng thì chưa phải một mục.
- Mục nào làm mất hiệu lực bằng chứng của một task đã tick `[X]` **phải nói rõ và nêu số task đó**.
- Nếu một mục hoá ra thuộc feature này, nó quay lại `tasks.md` thành task thật. **File này không phải chỗ
  đỗ.**
- Báo cáo mọi mục trong báo cáo cuối của skill.

## Các mục

### D1 — Hoàn tiền / từ chối hoàn tiền tự động

- **Vấn đề**: Feature này **không** có đường hoàn tiền nào. Khi một xác nhận đã xác minh không áp được (đơn
  đã sửa, đã huỷ, hết hạn, hoặc sai số tiền), tiền đã tới nhưng hệ thống **không** tự hoàn, cũng **không**
  tự ghi đơn thành `PAID`; bản ghi thanh toán chỉ thành `UNMATCHED`. Đơn **đã trả tiền** cũng **không** huỷ
  được (được chuyển nhượng như module 07).
- **Vì sao ngoài phạm vi**: ADR 015 §5 D5 chốt V1.0 **không hoàn tiền**; spec khẳng định điều này (FR-021)
  và điều kiện hoàn tất được thoả **bằng cấu trúc** (không có đường hoàn tiền nào được dựng). Hoàn tiền là
  một nghiệp vụ riêng với state machine riêng, không phải một nhánh của luồng trả tiền.
- **Gỡ bằng cách nào**: Một feature **hoàn tiền** riêng dựng state machine hoàn tiền, gọi
  `POST /v2/payment-requests/{id}/cancel` hoặc API hoàn tiền của PayOS, và cho một bản ghi `UNMATCHED` một
  đường ra chính thức. Quyết định dài hạn cần ghi ở `docs/decisions/` (ADR 019 §3 đã nêu điều kiện xem lại).
- **Ảnh hưởng tới task đã tick**: không. Không task nào của feature này hứa một luồng hoàn tiền; `T036`
  (ghi `UNMATCHED`) giao đúng phần nó được giao (ghi nhận, không hoàn).

### D2 — Đa tiền tệ

- **Vấn đề**: Mọi thanh toán lưu `currency` (`char(3)`), nhưng tính đến hiện tại tiền tệ **luôn là `VND`**:
  tầng lưu trữ chỉ ràng buộc `^[A-Z]{3}$`, không có tỷ giá, không quy đổi, không nhiều loại tiền cùng lúc.
- **Vì sao ngoài phạm vi**: Spec giới hạn MVP ở một tiền tệ (VND, ADR 015 §5); đa tiền tệ có hệ quả lan
  rộng (tỷ giá, tổng đơn, báo cáo) và không cần cho việc bán hàng V1.0. `currency` đã có trong lược đồ để
  seam sẵn sàng, nhưng chưa có ngữ nghĩa quy đổi.
- **Gỡ bằng cách nào**: Một feature đa tiền tệ bổ sung nguồn tỷ giá, quy tắc quy đổi và hiển thị; đối chiếu
  cách module 04 (sản phẩm) và module 07 (đơn) đang mang `currency`.
- **Ảnh hưởng tới task đã tick**: không. `T016` (lược đồ) và `T004`/`T022` (tiền là minor unit + currency)
  vẫn đúng như đã giao; đa tiền tệ chỉ thêm ngữ nghĩa chưa có.

### D3 — Thanh toán định kỳ (recurring)

- **Vấn đề**: Không có thanh toán định kỳ, không có lịch trả, không có token hoá thẻ/thoả thuận trả định kỳ.
  Mỗi lần trả là một liên kết PayOS riêng, khởi tạo thủ công bởi khách.
- **Vì sao ngoài phạm vi**: Thanh toán định kỳ thuộc mảng **commission** (V1.1, module 10) và nằm ngoài kế
  hoạch V1.0 (ADR 015 §5). Nó cần thoả thuận với cổng và mô hình dữ liệu riêng (lịch, giai đoạn).
- **Gỡ bằng cách nào**: Module 10 (Commission) hoặc một feature riêng xây thanh toán theo giai đoạn/định kỳ;
  quyết định ghi ở ADR 015 §5 khi tới V1.1.
- **Ảnh hưởng tới task đã tick**: không.

### D4 — Thanh toán end-to-end với provider thật (link → chuyển khoản → webhook)

- **Vấn đề**: Mọi test và mọi scenario của `quickstart.md` dùng **provider giả** (`LogProvider`) hoặc
  **vector chữ ký mẫu** của tài liệu PayOS. Chưa có lần nào chạy **một giao dịch thật**: tạo liên kết trên
  môi trường thật, khách chuyển khoản, và PayOS gọi webhook tới URL công khai của shop. Cần **credential
  merchant thật** và một **URL webhook công khai** (domain VPS production, hoặc tunnel khi dev).
- **Vì sao ngoài phạm vi**: Đây là bước **xác minh vận hành**, không phải code: nó phụ thuộc tài khoản
  merchant và hạ tầng domain nằm ngoài kho code. Test không được phép phụ thuộc một provider sống
  (`research.md` D14). `quickstart.md` đã ghi rõ phần "không xác minh ở đây".
- **Gỡ bằng cách nào**: Chạy `quickstart.md` scenario 1–5 một lần với credential thật và domain công khai
  khi chúng tồn tại; khi đó `PAYOS_*` được đặt trong `.env` (git-ignored), không commit.
- **Ảnh hưởng tới task đã tick**: **T020** (client PayOS), **T034** (webhook→order→stock trên PostgreSQL
  thật). Cả hai đã giao và test đúng như thiết kế — chữ ký chứng minh bằng vector tài liệu, và đường
  apply-once chứng minh trên DB thật — nhưng **đường tới provider sống** chưa được chạy, nên bằng chứng
  end-to-end của chúng phụ thuộc credential/hạ tầng.

### D5 — Đăng ký URL webhook một lần ở cổng (`POST /confirm-webhook`)

- **Vấn đề**: PayOS cần shop **đăng ký một lần** URL webhook của mình với cổng
  (`POST /confirm-webhook`). Feature này chỉ **nhận** webhook; nó **không** đăng ký URL, không có mã nào
  làm việc đó, và không lưu trạng thái "đã đăng ký".
- **Vì sao ngoài phạm vi**: Là bước **cài đặt một lần** với provider sống, cần credential và URL công khai
  (cùng điều kiện D4). Nó là thao tác vận hành, không phải một hành vi của module qua HTTP cho client.
- **Gỡ bằng cách nào**: Ghi lại như một bước deploy trong `docs/docker.md`/`docs/configuration.md` khi có
  domain production, hoặc một công cụ `cmd/*` một lần; đối chiếu `research.md` D1.
- **Ảnh hưởng tới task đã tick**: không. Không task nào của feature hứa việc đăng ký URL.

### D6 — Khôi phục tự động link mồ côi

- **Vấn đề**: Client được gọi **trước** khi dòng thanh toán commit (để transaction không bọc HTTP). Nếu
  bước ghi dòng hỏng, một liên kết **sống** ở PayOS tồn tại mà **không** có bản ghi thanh toán; webhook sau
  đó trả `PAYMENT_NOT_FOUND`, module **chỉ log warning** với `orderCode`, và operator phải đối soát thủ
  công (dò liên kết bằng `GET /v2/payment-requests/{id}` để dành, rồi ghi dòng và áp bằng tay).
- **Vì sao ngoài phạm vi**: `research.md` D7 ghi rõ: **không** có dòng thanh toán thì **không** có
  `order_version`/số tiền đã lưu để đối chiếu an toàn, nên khôi phục tự động từ webhook là không an toàn —
  đã hoãn. Cửa sổ này hẹp và hiếm.
- **Gỡ bằng cách nào**: Một feature/ADR riêng dựng cơ chế an toàn để nhận diện và khôi phục link mồ côi
  (ví dụ ghi dòng trước rồi đối chiếu, hoặc một job tìm link chưa khớp), có đối chiếu `order_version`/
  số tiền trước khi áp.
- **Ảnh hưởng tới task đã tick**: **T032** (webhook từ chối orderCode lạ, chỉ log) và **T033** (route
  webhook) giao **đúng** phần chúng được giao — từ chối và ghi log, **không** khôi phục. Việc khôi phục tự
  động chưa bao giờ thuộc bằng chứng của chúng.

### D7 — Operator giải quyết `UNMATCHED` ngoài hệ thống

- **Vấn đề**: Một khoản `UNMATCHED` (tiền đã tới nhưng không áp được) chỉ **xuất hiện** trong hàng đợi đối
  soát (`GET /admin/payments?status=UNMATCHED`). Không có hành động "áp tay" qua HTTP, không có cách đánh
  dấu "đã xử lý", không có đường đóng một khoản lệch trong hệ thống: operator xử lý **ngoài hệ thống**
  (đối chiếu ngân hàng, liên hệ khách).
- **Vì sao ngoài phạm vi**: Clarification của feature 011 chốt hàng đợi là **chỉ đọc** ở V1.0; hành động
  "áp tay/đối soát" thuộc **giao diện quản trị đầy đủ** (module 16) và có thể cần một state machine đối
  soát riêng. Dựng nó ở đây sẽ mở rộng phạm vi và chạm nghiệp vụ chưa chốt.
- **Gỡ bằng cách nào**: Module 16 (Admin) hoặc một feature riêng thêm trạng thái/hành động đối soát (đánh
  dấu đã xử lý, liên kết tới quyết định hoàn tiền nếu D1 tới). Quyết định ghi ở `docs/decisions/`.
- **Ảnh hưởng tới task đã tick**: **T042** (use case `ListAll` + `GET /admin/payments`) và **T043** (test
  `status=UNMATCHED` là hàng đợi) giao **đúng** phần chúng được giao (đọc, lọc); "giải quyết trong hệ
  thống" chưa bao giờ nằm trong bằng chứng của chúng.
