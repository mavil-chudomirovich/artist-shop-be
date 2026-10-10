# Kế hoạch nghiệp vụ đơn hàng, xác nhận artist, tồn kho và thanh toán

## 1. Mục tiêu

Thiết kế luồng đặt hàng commission trong đó:

- Khách hàng có thể chỉnh sửa đơn trước khi thanh toán thành công.
- Nếu khách chỉnh sửa đơn sau khi artist đã xác nhận, đơn phải quay lại trạng thái chờ xác nhận (`PENDING`).
- Hệ thống không giữ tồn kho trong suốt thời gian chờ artist.
- Chỉ giữ tồn kho khi artist xác nhận đơn và hệ thống kiểm tra đủ số lượng.
- Thời hạn thanh toán 60 phút bắt đầu sau khi giữ hàng thành công và đơn sẵn sàng thanh toán.
- Theo chính sách hiện tại, đơn đã thanh toán (`PAID`) không được khách chỉnh sửa hoặc hủy.2

## 2. Quyết định thiết kế

**Chọn phương án: chỉ giữ hàng khi artist xác nhận.**

Luồng tổng quát:

1. Khách checkout → tạo đơn `PENDING`, chưa giữ tồn kho.
2. Artist xác nhận → hệ thống kiểm tra tồn kho.
3. Nếu đủ hàng, hệ thống tạo reservation an toàn và chuyển đơn sang `PAYMENT_PENDING`.
4. Bắt đầu thời hạn thanh toán 60 phút.
5. Thanh toán thành công hợp lệ → chuyển đơn sang `PAID`.
6. Nếu khách sửa đơn trước khi thanh toán, đơn quay lại `PENDING`; reservation cũ và phiên thanh toán cũ phải được xử lý an toàn.
7. Artist cần xác nhận lại nội dung đơn mới.

## 3. Bảng kế hoạch tổng thể

| STT | Hạng mục | Quy tắc cần triển khai | Ưu tiên |
|---:|---|---|---|
| 1 | Tạo đơn hàng | Khách checkout tạo đơn `PENDING`, chưa giữ tồn kho | Cao |
| 2 | Artist xác nhận | Kiểm tra tồn kho; chỉ khi reserve thành công mới chuyển sang `PAYMENT_PENDING` | Cao |
| 3 | Khách sửa đơn `PENDING` | Cho phép sửa; giữ trạng thái `PENDING` | Cao |
| 4 | Khách sửa đơn `CONFIRMED` | Chuyển về `PENDING`; yêu cầu artist xác nhận lại | Cao |
| 5 | Khách sửa đơn `PAYMENT_PENDING` | Chỉ cho sửa nếu chưa thanh toán thành công; giải phóng reservation cũ, xử lý phiên thanh toán cũ và chuyển về `PENDING` | Cao |
| 6 | Timeout thanh toán | Bắt đầu đếm ngược 60 phút từ lúc reservation thành công và đơn sẵn sàng thanh toán | Cao |
| 7 | Hết hạn thanh toán | Đánh dấu đơn/phiên thanh toán hết hạn theo mô hình trạng thái hiện có; giải phóng reservation đúng một lần | Cao |
| 8 | Artist từ chối | Chuyển sang trạng thái từ chối phù hợp; không cần giải phóng hàng nếu chưa reserve | Cao |
| 9 | Thanh toán thành công | Chuyển sang `PAID` sau khi xác minh giao dịch, số tiền và đơn liên quan | Cao |
| 10 | Callback/IPN đến muộn | Đối chiếu giao dịch, số tiền và phiên bản đơn; không tự động ghi nhận thanh toán cho nội dung đơn mới | Cao |
| 11 | Chống oversell | Dùng transaction, locking hoặc cập nhật có điều kiện để tránh nhiều đơn giữ vượt số lượng tồn kho | Cao |
| 12 | Lịch sử chỉnh sửa | Lưu nội dung cũ/mới, người sửa, thời gian sửa và phiên bản đơn | Trung bình |

## 4. Quy tắc chuyển trạng thái

| Trạng thái hiện tại | Sự kiện | Trạng thái tiếp theo | Xử lý tồn kho |
|---|---|---|---|
| `PENDING` | Artist xác nhận và reserve thành công | `PAYMENT_PENDING` | Tạo reservation |
| `PENDING` | Khách sửa đơn | `PENDING` | Chưa giữ hàng |
| `PENDING` | Artist từ chối | `REJECTED` hoặc trạng thái tương đương hiện có | Không cần giải phóng nếu chưa reserve |
| `CONFIRMED` | Khách sửa đơn | `PENDING` | Chỉ giải phóng nếu thực tế đã reserve |
| `PAYMENT_PENDING` | Khách sửa đơn trước khi thanh toán thành công | `PENDING` | Giải phóng reservation cũ |
| `PAYMENT_PENDING` | Thanh toán thành công hợp lệ | `PAID` | Xác nhận reservation theo thiết kế tồn kho |
| `PAYMENT_PENDING` | Hết hạn thanh toán | `EXPIRED` hoặc trạng thái tương đương hiện có | Giải phóng reservation |
| `PAYMENT_PENDING` | Hủy hợp lệ theo nghiệp vụ | Trạng thái hủy phù hợp | Giải phóng reservation |
| `PAID` | Khách yêu cầu sửa/hủy | Giữ `PAID`; từ chối yêu cầu | Không thay đổi tồn kho do thao tác sửa/hủy |

> **Lưu ý trạng thái:** Không nhất thiết phải thêm tất cả các trạng thái được nêu làm ví dụ. Trước khi cập nhật enum, hãy đối chiếu trạng thái hiện có và dùng lại trạng thái tương đương nếu đã tồn tại. `CONFIRMED` có thể được gộp hoặc tách khỏi `PAYMENT_PENDING`, miễn là định nghĩa và thời điểm reserve/đếm giờ rõ ràng.

## 5. Quy trình chỉnh sửa đơn trước khi thanh toán

### 5.1 Đơn đang `PENDING`

1. Xác thực quyền chỉnh sửa.
2. Cập nhật nội dung đơn.
3. Giữ trạng thái `PENDING`.
4. Ghi lại lịch sử thay đổi nếu hệ thống có audit log.

Vì chưa giữ hàng và chưa bắt đầu thanh toán, không cần giải phóng reservation.

### 5.2 Đơn đã `CONFIRMED` nhưng chưa thanh toán

1. Xác thực đơn chưa `PAID`.
2. Cập nhật nội dung đơn.
3. Chuyển về `PENDING`.
4. Yêu cầu artist xác nhận lại.
5. Khi artist xác nhận lại, kiểm tra tồn kho theo nội dung mới nhất.

Nếu trạng thái `CONFIRMED` trong hệ thống thực tế đã tạo reservation, phải giải phóng reservation đó; không được giả định rằng `CONFIRMED` luôn đồng nghĩa với chưa giữ hàng.

### 5.3 Đơn đang `PAYMENT_PENDING`

1. Kiểm tra trạng thái thanh toán từ backend/provider trước khi cho phép sửa.
2. Nếu giao dịch đã thành công, không xử lý như một đơn chưa thanh toán.
3. Nếu chưa thanh toán thành công, khóa hoặc cập nhật đơn theo cách chống race condition.
4. Giải phóng reservation cũ đúng một lần.
5. Đánh dấu phiên thanh toán cũ là không còn hợp lệ cho nội dung đơn mới; thực hiện hủy phiên nếu cổng thanh toán hỗ trợ.
6. Cập nhật nội dung đơn, tăng `OrderVersion` hoặc dùng cơ chế phiên bản tương đương.
7. Chuyển đơn về `PENDING`.
8. Artist xác nhận lại; hệ thống reserve theo nội dung mới và tạo phiên thanh toán mới.

**Không chỉ đổi trạng thái đơn về `PENDING`** trong khi reservation hoặc phiên thanh toán cũ vẫn còn hiệu lực.

## 6. Thời hạn và xử lý thanh toán

### 6.1 Deadline artist

Nên có deadline riêng cho bước chờ artist, bắt đầu từ lúc checkout. Deadline này tránh đơn `PENDING` tồn tại vô thời hạn nhưng không giữ tồn kho.

### 6.2 Deadline thanh toán

- Bắt đầu khi reservation thành công và đơn sẵn sàng thanh toán.
- Thời lượng: 60 phút.
- Nếu hết hạn mà chưa có thanh toán thành công hợp lệ, đánh dấu hết hạn theo mô hình trạng thái hiện có và giải phóng reservation.

### 6.3 Callback/IPN đến muộn hoặc lặp lại

- Xác minh chữ ký callback/IPN theo yêu cầu của cổng thanh toán.
- Kiểm tra mã giao dịch, đơn hàng, số tiền, loại tiền và trạng thái giao dịch.
- Xử lý callback lặp lại theo cơ chế idempotency.
- Đối chiếu phiên bản đơn hoặc payment attempt để không gắn thanh toán của nội dung cũ vào đơn đã sửa.
- Nếu provider báo thanh toán thành công sau khi đơn/attempt đã hết hạn hoặc bị vô hiệu hóa, chuyển sang quy trình đối soát/xử lý ngoại lệ rõ ràng; không âm thầm gắn giao dịch vào phiên bản đơn mới.

> Hết hạn ở phía ứng dụng không đảm bảo giao dịch ở cổng thanh toán chắc chắn thất bại. Cần thiết kế quy trình xử lý giao dịch đến muộn phù hợp với khả năng của provider.

## 7. Quy tắc tồn kho và chống oversell

- Không giữ tồn kho khi khách checkout.
- Khi artist xác nhận, kiểm tra và reserve số lượng cần thiết một cách nguyên tử.
- Nếu không đủ tồn kho, không chuyển đơn sang `PAYMENT_PENDING`; thông báo lỗi hoặc yêu cầu xử lý đơn theo nghiệp vụ.
- Nếu đơn gồm nhiều mặt hàng, phải reserve toàn bộ thành công hoặc rollback/compensate các phần đã giữ.
- Release reservation phải idempotent: cùng một reservation không được giải phóng nhiều lần làm tăng tồn kho sai.
- Dùng transaction, optimistic concurrency, locking hoặc cập nhật có điều kiện tùy kiến trúc database.
- Khi khách sửa đơn, reservation cũ và nội dung đơn phải được đồng bộ theo quy trình an toàn.

## 8. Kế hoạch triển khai backend

| Giai đoạn | Công việc | Kết quả mong muốn |
|---|---|---|
| 1. Rà soát | Kiểm tra enum trạng thái, checkout, sửa đơn, reservation, payment callback và background jobs | Xác định các điểm cần thay đổi |
| 2. Domain/Application | Định nghĩa transition hợp lệ và điều kiện cho phép chỉnh sửa | Không cho sửa đơn `PAID`; quy tắc nhất quán |
| 3. Inventory | Triển khai reserve/release an toàn và chống xử lý trùng | Không oversell, không release trùng |
| 4. Payment | Quản lý payment attempt, timeout 60 phút, callback/IPN và giao dịch muộn | Không gắn nhầm giao dịch vào đơn/phiên bản mới |
| 5. Background jobs | Tự động hết hạn đơn chờ artist và đơn chờ thanh toán | Không để đơn treo hoặc giữ hàng quá hạn |
| 6. Testing | Unit test, integration test và kiểm thử race condition | Luồng nghiệp vụ hoạt động nhất quán |
| 7. Logging/Audit | Ghi lịch sử sửa đơn, reserve/release và payment attempt | Có thể truy vết lỗi và đối soát |

## 9. Checklist nghiệm thu

- [ ] Checkout tạo `PENDING` mà không giữ hàng.
- [ ] Artist chỉ xác nhận thành công khi reserve đủ hàng.
- [ ] Sửa đơn `PENDING` giữ nguyên trạng thái.
- [ ] Sửa đơn `CONFIRMED` quay về `PENDING` và yêu cầu xác nhận lại.
- [ ] Sửa đơn `PAYMENT_PENDING` giải phóng reservation cũ đúng một lần.
- [ ] Phiên thanh toán cũ không thể được dùng để thanh toán nội dung đơn mới.
- [ ] Thời hạn 60 phút bắt đầu từ lúc reservation thành công.
- [ ] Hết hạn thanh toán giải phóng hàng đúng một lần.
- [ ] Không cho khách sửa/hủy đơn `PAID` theo chính sách hiện tại.
- [ ] Callback/IPN lặp lại không tạo cập nhật thanh toán trùng.
- [ ] Callback/IPN đến muộn không làm đơn mới bị đánh dấu `PAID` nhầm.
- [ ] Hai đơn cạnh tranh cùng tồn kho không gây oversell.
- [ ] Đơn nhiều mặt hàng không bị reserve một phần mà không có rollback/compensation.

## 10. Các điểm cần xác nhận trước khi code

1. **Trạng thái hiện có:** Hệ thống đã có `CONFIRMED`, `PAYMENT_PENDING`, `EXPIRED`, `REJECTED` chưa? Có thể cần ánh xạ sang enum hiện tại thay vì thêm mới.
2. **Định nghĩa `CONFIRMED`:** Artist xác nhận có giữ hàng ngay không, hay chỉ là đồng ý thực hiện đơn?
3. **Sửa đơn khi đang thanh toán:** Cổng thanh toán có hỗ trợ hủy payment link/attempt không? Nếu không, cần quy trình xử lý thanh toán muộn.
4. **Deadline artist:** Bạn muốn đơn chờ artist hết hạn sau bao lâu?
5. **Đơn nhiều mặt hàng:** Có cần reserve tất cả mặt hàng như một thao tác nguyên tử không? Khuyến nghị là có.

---

## Nguyên tắc thiết kế cuối cùng

**Không giữ hàng khi checkout → artist xác nhận → reserve tồn kho → mở thời hạn thanh toán 60 phút → khách thanh toán.**

Mọi chỉnh sửa trước `PAID` phải làm mất hiệu lực xác nhận cũ nếu nội dung thay đổi, giải phóng reservation cũ khi có, xử lý payment attempt cũ và yêu cầu artist xác nhận lại. Đơn `PAID` không cho sửa hoặc hủy theo chính sách hiện tại.
