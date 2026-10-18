# Deferred — Order Delivery & Completion

Việc phát hiện khi xây **Order Delivery & Completion (feature 019)** nhưng **không phải việc của feature
này**. Không mục nào ở đây chặn việc đóng feature 019; không mục nào là task của feature 019.

File này tồn tại vì một ô `- [ ]` trong `tasks.md` mang một ý nghĩa khác. `- [ ]` nghĩa là *feature này
chưa xong*. Việc thuộc về feature khác, hoặc thuộc chính hạ tầng test, không phải phần chưa xong của
feature này; ghi nó như vậy hoặc giữ một feature đã đóng trông như còn mở, hoặc ép điều kiện thoát bị
làm giả.

## Quy tắc cho file này

- Mỗi mục nêu rõ **nó là gì**, **vì sao ngoài phạm vi**, và **điều gì sẽ gỡ được** nó. Một mục chỉ nêu
  triệu chứng thì chưa phải một mục.
- Mục nào làm mất hiệu lực bằng chứng của một task đã tick `[X]` **phải nói rõ và nêu số task đó**. Nếu
  không, `[X]` sẽ bị đọc là không điều kiện trong khi thực tế không phải.
- Nếu một mục hoá ra thuộc feature này, nó quay lại `tasks.md` thành task thật. **File này không phải
  chỗ đỗ.**
- Báo cáo mọi mục trong báo cáo cuối của skill. Hoãn trong im lặng thì vô hiệu mục đích của việc ghi lại.

## Các mục

### D1 — Ca giao thất bại / khách không nhận hàng (không có `FAILED`/`RETURNED`, không hoàn tiền)

- **Vấn đề**: Nếu bên vận chuyển giao không được hoặc khách từ chối nhận, feature 019 **không** có
  trạng thái nào để biểu diễn nó: đơn chỉ có thể dừng ở `SHIPPING` hoặc `DELIVERED`, và **không** có
  luồng trả hàng/hoàn tiền. Một đơn giao thất bại sẽ nằm lại ở `SHIPPING` vô thời hạn, và đơn đã
  `DELIVERED` (oái oăm) vẫn **tự hoàn tất sau 120 giờ** như một đơn thành công.
- **Vì sao ngoài phạm vi**: Một trạng thái thất bại cần một **domain trả hàng/hoàn tiền** mà dự án
  chưa có (V1.0 cố ý **không** hoàn tiền — ADR 015 §5; spec 019 ghi rõ *"feature này không hoàn
  tiền/trả hàng"*). Thêm `FAILED`/`RETURNED` **mà không** có hoàn tiền chỉ tạo một trạng thái chết chưa
  ai xử lý. Đây là ranh giới **scope** của feature (Constitution VII, YAGNI), không phải thiếu thời gian.
- **Gỡ bằng cách nào**: Một feature riêng "Order returns & refunds" bổ sung trạng thái `FAILED`/`RETURNED`
  (kiểm cả ở `orders_status_ck`), một luồng hoàn tiền (liên quan module 08 Payment), và reset
  `delivered_at`/cửa sổ hoàn tất khi giao thất bại. Quyết định và đánh đổi phải ghi một ADR mới (supersede
  ADR 023 ở đúng các cạnh này).
- **Ảnh hưởng tới task đã tick**: Không. Các integration test của feature (T032, T037, T041) chỉ khẳng
  định `DELIVERED`/`COMPLETED` và việc **không** đụng tồn kho; chúng không tuyên bố có trạng thái thất bại.

### D2 — Phạm vi chuyển nhượng đơn theo trạng thái giao hàng mới (`SHIPPING`/`DELIVERED`)

- **Vấn đề**: Feature 019 **đổi tên** `SHIPPED` → `SHIPPING` (và thêm `DELIVERED`), nhưng **không** chốt
  chuyển nhượng (`POST /admin/orders/{orderId}/transfer`) nên bị **giới hạn** hay **mở rộng** theo các
  trạng thái giao hàng mới. Cơ chế chuyển nhượng hiện có giữ nguyên (chỉ áp tên trạng thái mới), nên
  hành vi trên một đơn đang giao/đã giao là **chưa chốt** chứ không phải được quyết định.
- **Vì sao ngoài phạm vi**: Spec 019 (Clarifications, Assumptions) **hoãn** quyết định này và nói rõ
  "feature này **không đổi** cơ chế chuyển nhượng hiện có". Chuyển nhượng một đơn `DELIVERED` (đã có
  ảnh bằng chứng, sắp tự hoàn tất) đặt câu hỏi về bằng chứng, cửa sổ hoàn tất và trách nhiệm — vượt
  scope giao hàng.
- **Gỡ bằng cách nào**: Một quyết định (ADR/feature riêng) chốt tập trạng thái chuyển nhượng được theo
  bảy giá trị, và — nếu khác hiện tại — bổ sung điều kiện vào `CodeNotTransferable`. Đến lúc đó,
  `docs/modules/07-order.md` và `docs/api-reference.md` §9.14 mô tả đúng hành vi **hiện có**.
- **Ảnh hưởng tới task đã tick**: Không. `transfer` chỉ bị chạm ở khâu **áp tên trạng thái** (T020);
  không test nào của 019 khẳng định chuyển nhượng trên `SHIPPING`/`DELIVERED`. Mục **D3** của
  `specs/010-order-confirmation/deferred.md` vẫn mở liên quan.

### D3 — Phí vận chuyển và tracking hãng vận chuyển (module 09)

- **Vấn đề**: Feature 019 giao **ảnh bằng chứng giao hàng** nhưng **không** giao **phí ship theo khu
  vực**, **mã vận đơn (tracking)**, hay **tích hợp API hãng vận chuyển**, cũng **không** có bảng
  `shipments`. Tổng đơn **không** gồm phí ship (ADR 015 §5).
- **Vì sao ngoài phạm vi**: Đây là **module 09 Shipping** (V1.2), chưa specify, và phụ thuộc một nhà
  cung cấp vận chuyển bên thứ ba. `docs/modules/09-shipping.md` ghi rõ ảnh bằng chứng do feature 019
  giao **không** thay module 09.
- **Gỡ bằng cách nào**: Specify & triển khai **module 09 Shipping** ở V1.2: bảng `shipments`, phí ship
  theo khu vực chốt tại lúc tạo đơn, mã vận đơn/trạng thái vận chuyển. `docs/modules.md` §Còn để ngỏ đã
  ghi điểm cần chốt (nhà vận chuyển ngoài — ViettelPost).
- **Ảnh hưởng tới task đã tick**: Không. Không task của 019 tuyên bố phí ship hay tracking.

### D4 — Đánh giá sản phẩm có điểm số/kiểm duyệt (module 13 Review)

- **Vấn đề**: Feature 019 chỉ giao **feedback cấp đơn** (một nội dung văn bản, một lần mỗi đơn, gửi cho
  artist). Nó **không** giao **đánh giá sản phẩm** (điểm số, nhiều đánh giá, kiểm duyệt, hiển thị công
  khai cho sản phẩm).
- **Vì sao ngoài phạm vi**: Review sản phẩm là **module 13** (V2.0), sở hữu bảng và hợp đồng riêng; spec
  019 ghi rõ feedback "không phải đánh giá sản phẩm (module 13)". Trộn hai khái niệm sẽ nhân đôi mô hình
  và khoá tương lai của review.
- **Gỡ bằng cách nào**: Specify & triển khai **module 13 Review** ở V2.0, có thể đọc `order_feedback`
  như một tín hiệu nhưng sở hữu bảng đánh giá riêng (điểm số, kiểm duyệt).
- **Ảnh hưởng tới task đã tick**: Không. `order_feedback` là schema nhỏ, có chủ (module 07); T046 chỉ
  khẳng định unique index của **feedback cấp đơn**, không nói gì về review sản phẩm.

### D5 — Thông báo in-app (module 12 Notification)

- **Vấn đề**: Feature 019 thông báo **chỉ qua email** (port `Notifier` best-effort hiện có): khách khi
  đơn đổi sang `SHIPPING`/`DELIVERED`/`COMPLETED`, artist khi có feedback. **Không** có thông báo in-app
  (chuông/kênh trong ứng dụng) và không có các loại email khác.
- **Vì sao ngoài phạm vi**: **Module 12 Notification** (V1.2) chưa tồn tại; spec 019 ghi rõ thông báo
  in-app thuộc module 12. Dựng nó bây giờ vượt scope và tạo một nguồn thông báo thứ hai chưa được module
  sở hữu.
- **Gỡ bằng cách nào**: Specify & triển khai **module 12 Notification** (đa kênh: in-app, email khác),
  rồi để module 07 phát sự kiện/đăng ký — có thể **hợp nhất** hai mailer (module 01 và module 07) như
  ADR 018 §3 đã ghi nợ.
- **Ảnh hưởng tới task đã tick**: Không. T047–T049 chỉ khẳng định email best-effort cho các trạng thái
  mới và feedback, và rằng lỗi gửi **không** làm hỏng đơn; chúng không tuyên bố có in-app.

## Ghi chú

- Không mục nào ở trên làm mất hiệu lực một task `[X]` của feature 019: mỗi `[X]` được chốt bằng test
  hoặc bằng một tuyên bố hẹp hơn phạm vi các mục hoãn này.
- Hai mục **D1** và **D2** xuất hiện trực tiếp trong `spec.md` (Scope boundary, Assumptions) và
  `research.md` **D14**; **D3**–**D5** là nợ module đã biết của dự án, được feature 019 nhắc lại vì nó
  chạm đúng các bề mặt đó.
- Quyết định dài hạn của feature 019 ở
  [`docs/decisions/023-order-delivery-and-completion.md`](../../docs/decisions/023-order-delivery-and-completion.md).
