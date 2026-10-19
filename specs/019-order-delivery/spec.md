# Feature Specification: Order Delivery & Completion

**Feature Branch**: `019-order-delivery`

**Created**: 2026-10-18

**Status**: Draft

**Input**: User description: "Luồng đơn sau khi trả tiền: thêm bước giao hàng có ảnh; đổi tên SHIPPED → SHIPPING (đang giao) và thêm DELIVERED (giao thành công); ở PAID → SHIPPING admin nhập tối đa 5 ảnh; ở SHIPPING → DELIVERED admin nhập ảnh; khách xác nhận hoàn thành (kèm feedback gửi artist) hoặc đơn tự hoàn thành sau 5 ngày. Ca giao thất bại hoãn lại."

## Clarifications

### Session 2026-10-18

- Q: Ảnh bằng chứng ở mỗi bước giao là bắt buộc hay tuỳ chọn? → A: **Tuỳ chọn** (0–5 ảnh) ở cả bước đang giao và giao thành công; một bước chuyển vẫn thành công khi không có ảnh.
- Q: Ngoài khách xác nhận và tự động sau 5 ngày, admin có còn được đánh dấu đơn `COMPLETED` trực tiếp không? → A: **Không** — chỉ khách xác nhận hoặc cơ chế tự động sau 5 ngày làm đơn `COMPLETED`; operator không đánh dấu hoàn tất thay khách.
- Q: Chuyển nhượng đơn (transfer) được phép ở trạng thái nào? → A: **Hoãn** quyết định phạm vi theo các trạng thái giao hàng mới; feature này **không đổi** cơ chế chuyển nhượng hiện có (chỉ áp dụng tên trạng thái mới).
- Q: Khách được gửi feedback cấp đơn ở trạng thái nào? → A: **Chỉ `DELIVERED` hoặc `COMPLETED`**.

## Scope boundary

Kế thừa module 07 (feature `010-order-confirmation`). Feature này **thay đổi vòng đời đơn sau khi trả tiền** (bổ sung bước giao hàng + hoàn tất do khách/hết hạn) và thêm bằng chứng ảnh giao hàng + feedback cấp đơn. Nó **không** giao phí ship, tracking hãng vận chuyển (module 09 đầy đủ), đánh giá sản phẩm (module 13), thông báo in-app (module 12), hoàn tiền/trả hàng.

- **Deliverable**:
  1. Đơn sau khi trả tiền đi qua **hai** bước giao: `PAID → SHIPPING` (đang giao) và `SHIPPING → DELIVERED` (giao thành công), mỗi bước **operator** có thể đính kèm **tối đa 5 ảnh** bằng chứng (**tuỳ chọn**).
  2. Đổi tên trạng thái hiện có **`SHIPPED` → `SHIPPING`** và thêm trạng thái mới **`DELIVERED`**; bộ trạng thái đơn còn **bảy** giá trị.
  3. Khách **xác nhận đã nhận hàng** → đơn `COMPLETED`; hoặc đơn **tự hoàn thành sau 5 ngày** kể từ lúc giao thành công.
  4. Khách **gửi feedback cấp đơn** cho artist (một lần cho mỗi đơn).
  5. **Email**: khách được báo khi đơn đổi trạng thái (đang giao, đã giao, hoàn tất); artist được báo khi có feedback.
- **Deferred**: ca **giao thất bại / khách không nhận hàng** (không có trạng thái thất bại, ghi `deferred.md`); **phạm vi chuyển nhượng đơn** theo các trạng thái giao hàng mới `SHIPPING`/`DELIVERED` (ghi `deferred.md`); phí ship/tracking (module 09); đánh giá sản phẩm (module 13); thông báo in-app (module 12); hoàn tiền/trả hàng.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - The artist hands the order to the carrier, with dispatch photos (Priority: P1)

Một đơn **đã trả tiền** (`PAID`) được artist đánh dấu **đang giao** khi đã giao hàng cho bên vận chuyển; artist đính kèm **tối đa 5 ảnh** làm bằng chứng bàn giao và đơn chuyển sang **đang giao** (`SHIPPING`).

**Why this priority**: Đây là mục đích của feature — nếu không có bước "đang giao" và bằng chứng ảnh thì không có gì mới so với hiện tại.

**Independent Test**: Với một đơn ở `PAID`, artist đánh dấu đang giao kèm ảnh → đơn `SHIPPING`, ảnh đọc lại được; đánh dấu lại lần nữa → bị từ chối, đơn nguyên vẹn.

**Acceptance Scenarios**:

1. **Given** một đơn ở `PAID`, **When** operator đánh dấu đang giao kèm 1–5 ảnh hợp lệ, **Then** đơn chuyển `SHIPPING` và các ảnh là một phần của đơn.
2. **Given** một đơn ở `PAID`, **When** operator đánh dấu đang giao **không** kèm ảnh, **Then** đơn vẫn chuyển `SHIPPING` (ảnh là tuỳ chọn).
3. **Given** một đơn **không** ở `PAID` (chưa trả tiền, đã giao, hoàn tất, đã huỷ), **When** đánh dấu đang giao, **Then** từ chối, **nêu trạng thái hiện tại**, đơn nguyên vẹn.
4. **Given** một đơn ở `PAID`, **When** operator gửi **6 ảnh** hoặc một ảnh sai định dạng/quá lớn, **Then** yêu cầu bị từ chối **trước khi** lưu ảnh, đơn **không** đổi trạng thái.

---

### User Story 2 - The artist marks the order delivered, with proof photos (Priority: P1)

Một đơn **đang giao** (`SHIPPING`) được artist đánh dấu **giao thành công** khi hàng đã tới tay khách; artist đính kèm **ảnh** (tối đa 5) và đơn chuyển sang **đã giao** (`DELIVERED`), mở mốc để khách xác nhận hoặc tự hoàn tất.

**Why this priority**: Có "đang giao" mà không có "đã giao" thì khách không có gì để xác nhận và đơn không bao giờ hoàn tất.

**Independent Test**: Với một đơn ở `SHIPPING`, artist đánh dấu giao thành công kèm ảnh → đơn `DELIVERED`, mốc giao được ghi; đánh dấu giao thành công cho đơn chưa ở `SHIPPING` → bị từ chối.

**Acceptance Scenarios**:

1. **Given** một đơn ở `SHIPPING`, **When** operator đánh dấu giao thành công kèm 1–5 ảnh hợp lệ, **Then** đơn `DELIVERED` và mốc thời gian giao được ghi lại.
2. **Given** một đơn **không** ở `SHIPPING`, **When** đánh dấu giao thành công, **Then** từ chối, nêu trạng thái hiện tại, đơn nguyên vẹn.
3. **Given** một đơn ở `SHIPPING`, **When** operator gửi ảnh sai định dạng/quá lớn hoặc quá 5 ảnh, **Then** từ chối trước khi lưu, đơn vẫn `SHIPPING`.

---

### User Story 3 - The customer confirms receipt and completes the order (Priority: P2)

Khách **đã nhận được hàng** xác nhận hoàn thành đơn đang ở `DELIVERED`; đơn chuyển sang **hoàn tất** (`COMPLETED`, trạng thái cuối). Chỉ **chủ đơn** xác nhận được đơn của mình.

**Why this priority**: P2 — đơn tự hoàn thành được sau 5 ngày nên luồng chính vẫn chạy, nhưng khách xác nhận sớm là luồng mong muốn.

**Independent Test**: Với một đơn `DELIVERED` của khách, khách xác nhận → `COMPLETED`; khách khác xác nhận đơn đó → như không tìm thấy; xác nhận đơn chưa `DELIVERED` → bị từ chối.

**Acceptance Scenarios**:

1. **Given** đơn `DELIVERED` của khách, **When** khách xác nhận đã nhận, **Then** đơn `COMPLETED`, ghi lại người/thời điểm hoàn tất.
2. **Given** đơn của khách khác, **When** khách xác nhận, **Then** từ chối như **không tìm thấy** (không lộ đơn của người khác).
3. **Given** đơn chưa ở `DELIVERED`, **When** khách xác nhận, **Then** từ chối, nêu trạng thái hiện tại, đơn nguyên vẹn.

---

### User Story 4 - A delivered order completes itself after 5 days (Priority: P2)

Đơn đã giao (`DELIVERED`) mà khách **không** xác nhận trong vòng **5 ngày** kể từ lúc giao thành công thì **tự động hoàn tất** (`COMPLETED`) **đúng một lần**; đơn chưa giao thì **không** tự hoàn tất.

**Why this priority**: P2 — bảo vệ đơn khỏi kẹt vô hạn khi khách im lặng; nhưng khách xác nhận (US3) đã đủ cho luồng chính.

**Independent Test**: Một đơn `DELIVERED` quá 5 ngày tự `COMPLETED` một lần; một đơn chưa `DELIVERED` để lâu không tự hoàn tất.

**Acceptance Scenarios**:

1. **Given** đơn `DELIVERED` quá 5 ngày chưa được xác nhận, **When** hệ thống chạy, **Then** đơn `COMPLETED` **đúng một lần** kể cả khi xử lý lặp.
2. **Given** đơn `SHIPPING` (chưa giao), **When** để lâu, **Then** đơn **không** tự hoàn tất.
3. **Given** khách xác nhận đúng lúc hệ thống tự hoàn tất, **When** hai việc xảy ra, **Then** đơn `COMPLETED` **một lần** duy nhất, không ghi đôi.

---

### User Story 5 - The customer sends feedback to the artist (Priority: P3)

Khách **gửi feedback cấp đơn** cho artist (nội dung), một lần cho mỗi đơn, khi đơn đã giao hoặc đã hoàn tất.

**Why this priority**: P3 — tăng giá trị phản hồi nhưng shop bán và hoàn tất đơn được khi chưa có.

**Independent Test**: Khách gửi feedback cho đơn `DELIVERED` → feedback đọc lại được bởi artist; gửi lần hai cho cùng đơn → bị từ chối.

**Acceptance Scenarios**:

1. **Given** đơn `DELIVERED` hoặc `COMPLETED` của khách, **When** khách gửi feedback, **Then** feedback được lưu gắn với đơn và artist đọc được.
2. **Given** đơn của khách chưa giao (`PENDING`…`SHIPPING`), **When** khách gửi feedback, **Then** từ chối, nêu trạng thái hiện tại.
3. **Given** khách đã gửi feedback cho đơn, **When** khách gửi lần nữa, **Then** từ chối (mỗi đơn một lần).

---

### User Story 6 - Both sides are notified as the order moves (Priority: P3)

Khách được báo mỗi khi trạng thái đơn đổi (đang giao, đã giao, hoàn tất); artist được báo khi khách gửi feedback. Một lần gửi lỗi **không** làm hỏng thao tác đơn.

**Why this priority**: P3 — vận hành; nhưng luồng đơn đúng khi chưa có thông báo.

**Independent Test**: Artist đánh dấu đang giao → khách nhận email; khách gửi feedback → artist nhận email; email lỗi vẫn không làm đơn sai.

**Acceptance Scenarios**:

1. **Given** đơn chuyển `SHIPPING` hoặc `DELIVERED` hoặc `COMPLETED`, **When** chuyển xong, **Then** khách nhận thông báo trạng thái mới.
2. **Given** khách vừa gửi feedback, **When** feedback lưu xong, **Then** artist nhận thông báo có feedback.
3. **Given** việc gửi thông báo thất bại, **When** thao tác đơn chạy, **Then** đơn vẫn đổi trạng thái thành công.

### Edge Cases

- Hai nguồn hoàn tất tranh nhau (khách xác nhận và hết hạn 5 ngày) → đơn `COMPLETED` **một lần**.
- Upload ảnh tới nhà cung cấp thành công nhưng bước chuyển trạng thái thất bại → **đơn không đổi**; ảnh mồ côi là chấp nhận được (không ràng buộc DB).
- Đơn nhiều bước bị đánh dấu sai thứ tự (giao thành công khi chưa đang giao; xác nhận khi chưa giao) → từ chối, **nêu trạng thái hiện tại**, đơn nguyên vẹn.
- Ảnh vượt quá 5, sai định dạng, hoặc quá cỡ → từ chối **trước khi** gọi nhà cung cấp media.
- Khách gửi feedback cho đơn chưa giao, hoặc gửi lần hai → từ chối, đơn/feedback nguyên vẹn.
- Khách thao tác đơn của người khác → như không tìm thấy.
- Đơn ở `COMPLETED`/`CANCELLED` (cuối) → không còn bước chuyển nào; mọi bước bị từ chối.
- Chuyển nhượng đơn đang ở `SHIPPING`/`DELIVERED` → hành vi **giữ nguyên như hiện có** (chưa chốt phạm vi mới; ghi `deferred.md`), không chặn feature này.
- Gửi email lỗi → đơn và feedback vẫn đúng.

## Requirements *(mandatory)*

### Functional Requirements — tập trạng thái đơn

- **FR-001**: Vòng đời đơn MUST gồm bảy trạng thái: `PENDING`, `PAYMENT_PENDING`, `PAID`, `SHIPPING`, `DELIVERED`, `COMPLETED`, `CANCELLED`; trạng thái `SHIPPED` hiện có MUST được **đổi tên** thành `SHIPPING` (đang giao) và `DELIVERED` (giao thành công) là trạng thái **mới**.
- **FR-002**: Trạng thái MUST chỉ đổi qua các bước chuyển của miền; ghi thẳng một giá trị ngoài tập hợp lệ MUST là bất khả (ràng buộc ở tầng lưu trữ).
- **FR-003**: Mọi bước chuyển không hợp lệ MUST bị từ chối, **nêu trạng thái hiện tại**, và để đơn **nguyên vẹn**.
- **FR-004**: `COMPLETED` và `CANCELLED` MUST là trạng thái cuối; từ `PAID` trở đi MUST **không** có đường lùi (không huỷ/sửa).

### Functional Requirements — giao hàng

- **FR-005**: Operator MUST đánh dấu **đang giao** một đơn ở `PAID`; đơn MUST chuyển sang `SHIPPING`.
- **FR-006**: Khi đánh dấu đang giao, operator MUST được phép đính kèm **0 đến 5 ảnh**; gửi nhiều hơn 5 ảnh MUST bị từ chối. Ảnh là **tuỳ chọn** (một bước chuyển vẫn thành công với 0 ảnh).
- **FR-007**: Operator MUST đánh dấu **giao thành công** một đơn ở `SHIPPING`; đơn MUST chuyển sang `DELIVERED`.
- **FR-008**: Khi đánh dấu giao thành công, operator MUST được phép đính kèm **0 đến 5 ảnh**; gửi nhiều hơn 5 ảnh MUST bị từ chối. Ảnh là **tuỳ chọn**. Hệ thống MUST ghi lại **mốc thời gian giao thành công**.
- **FR-009**: Ảnh tải lên MUST được **kiểm tra định dạng, kích thước và số lượng** và MUST bị từ chối **trước khi** lưu trữ. Định dạng hợp lệ MUST được xác định bằng việc **nhận dạng nội dung**, không dựa vào tên tệp hay kiểu do client khai báo.
- **FR-010**: Ảnh MUST được lưu bởi nhà cung cấp media (không lưu nhị phân trong cơ sở dữ liệu); đơn chỉ giữ **tham chiếu** ảnh và MUST trả lại ảnh **ở response chi tiết của đơn** (customer và admin), không ở danh sách.

### Functional Requirements — hoàn tất đơn

- **FR-011**: Khách MUST xác nhận **đã nhận hàng** cho một đơn ở `DELIVERED` của mình; đơn MUST chuyển sang `COMPLETED`. Operator MUST NOT đánh dấu hoàn tất thay khách: chỉ khách xác nhận (FR-011) hoặc cơ chế tự động sau 5 ngày (FR-012) đưa đơn sang `COMPLETED`.
- **FR-012**: Đơn ở `DELIVERED` mà khách **không** xác nhận trong **5 ngày** kể từ mốc giao thành công MUST **tự động hoàn tất** (`COMPLETED`) **đúng một lần** (kể cả khi xử lý lặp lại).
- **FR-013**: Đơn **chưa** ở `DELIVERED` MUST **không** tự hoàn tất theo thời gian.
- **FR-014**: Việc hoàn tất (do khách hoặc tự động) MUST ghi lại **thời điểm** và **ai/kết quả** đã hoàn tất.

### Functional Requirements — feedback cấp đơn

- **FR-015**: Khách MUST gửi được **feedback cấp đơn** (nội dung) cho artist, gắn với một đơn của mình, khi đơn ở `DELIVERED` hoặc `COMPLETED`. Nội dung feedback MUST có **1 đến 2000 ký tự** (sau khi cắt khoảng trắng); nội dung rỗng hoặc quá dài MUST bị từ chối.
- **FR-016**: Mỗi đơn MUST nhận **tối đa một** feedback; gửi lần hai MUST bị từ chối.
- **FR-017**: Feedback MUST đọc được bởi operator/artist cùng đơn.
- **FR-025**: Khi gửi feedback, khách MUST được phép đính kèm **0 đến 5 ảnh**; gửi nhiều hơn 5 ảnh MUST bị từ chối. Ảnh là **tuỳ chọn** (feedback không ảnh vẫn hợp lệ). Mỗi ảnh MUST được **kiểm tra định dạng và kích thước** (cùng định dạng/ngưỡng như ảnh bằng chứng giao hàng, FR-009) và MUST bị từ chối **trước khi** lưu trữ. Ảnh MUST được lưu bởi nhà cung cấp media (không lưu nhị phân trong cơ sở dữ liệu) và MUST được trả lại trong `feedback.photos` ở response chi tiết của đơn.

### Functional Requirements — quyền, audit, toàn vẹn

- **FR-018**: Chủ đơn MUST lấy từ **phiên**, không bao giờ từ yêu cầu; khách chỉ xác nhận và gửi feedback cho **đơn của mình** — đơn của khách khác MUST trả cùng một kết quả như **không tìm thấy**.
- **FR-019**: Mọi bước chuyển của operator (đang giao, giao thành công) MUST cần vai trò **admin** và MUST được ghi **audit** nêu đơn và người thực hiện.
- **FR-020**: Các bước ở feature này MUST **không** thay đổi tồn kho (hàng đã bán ở bước `PAID`) và MUST **không** thay đổi tổng tiền; tổng vẫn là **số nguyên đơn vị nhỏ nhất + tiền tệ**, **không** phí ship.

### Functional Requirements — thông báo

- **FR-021**: Hệ thống MUST thông báo cho **khách** mỗi khi trạng thái đơn của họ đổi sang `SHIPPING`, `DELIVERED`, hoặc `COMPLETED`.
- **FR-022**: Hệ thống MUST thông báo cho **artist** khi khách gửi **feedback** cho một đơn.
- **FR-023**: Một lần gửi thông báo **thất bại MUST NOT** làm hỏng hay rollback thao tác đơn.

### Functional Requirements — tương thích dữ liệu

- **FR-024**: Đơn đã tồn tại với trạng thái cũ `SHIPPED` MUST được chuyển sang `SHIPPING` **không mất dữ liệu** và vẫn đọc được; việc đổi tên MUST được thực hiện bằng một thay đổi lược đồ **có thể hoàn tác** và không phá vỡ đơn đang tồn tại.

### Key Entities

- **Order**: đơn của khách — chủ, địa chỉ snapshot, tổng, **trạng thái** (bảy giá trị), phiên bản, **mốc giao thành công**, mốc xác nhận/hạn thanh toán hiện có, thời điểm hoàn tất, thời điểm tạo/đổi.
- **Delivery evidence (ảnh bằng chứng)**: một ảnh gắn với một đơn ở một bước giao (đang giao hoặc giao thành công) — tham chiếu của nhà cung cấp media, thứ tự, thời điểm tạo.
- **Order feedback**: feedback cấp đơn — đơn, người gửi, nội dung, thời điểm; tối đa một cho mỗi đơn; kèm **0–5 ảnh tuỳ chọn** (tham chiếu nhà cung cấp media, thứ tự, thời điểm tạo).
- **Order line / Hold**: giữ nguyên như hiện có; feature này không đụng tới.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Operator đưa được một đơn `PAID` sang `SHIPPING` (kèm tối đa 5 ảnh) rồi sang `DELIVERED` (kèm ảnh); kiểm chứng bằng test tự động.
- **SC-002**: Khách xác nhận được đơn `DELIVERED` của mình thành `COMPLETED`; đơn `DELIVERED` không xác nhận trong 5 ngày **tự hoàn tất đúng một lần**.
- **SC-003**: Mọi bước chuyển sai (sai thứ tự, sai trạng thái, đơn của người khác) bị từ chối **nêu trạng thái hiện tại**, đơn không đổi — kiểm chứng **cả hai chiều** mỗi cạnh.
- **SC-004**: Yêu cầu quá 5 ảnh, ảnh sai định dạng hoặc quá cỡ bị từ chối **trước khi** lưu; đơn không đổi trạng thái.
- **SC-005**: Khách gửi được **đúng một** feedback cho mỗi đơn; artist nhận được thông báo; lỗi gửi thông báo **không** làm hỏng đơn/feedback.
- **SC-006**: Chỉ chủ đơn xác nhận/gửi feedback đơn của mình (đơn người khác như không tìm thấy); chỉ admin đánh dấu giao; mỗi bước chuyển của operator có audit.
- **SC-007**: Sau thay đổi lược đồ, mọi đơn trước đây ở trạng thái "đã giao" vẫn **đọc được** dưới tên mới `SHIPPING`, không mất bản ghi.

## Assumptions

- **Đổi tên `SHIPPED` → `SHIPPING` và thêm `DELIVERED`** (quyết định đã chốt). Tập trạng thái thành bảy giá trị; đây là thay đổi **phá vỡ hợp đồng công khai** với client (client đang đọc `SHIPPED`), nên cần hướng dẫn frontend và phát hành đồng bộ.
- **Hoàn tất do khách** (xác nhận đã nhận) hoặc **tự động sau 5 ngày**; operator **không** còn là người đánh dấu hoàn tất trực tiếp. "5 ngày" là **120 giờ (5 × 24h) kể từ mốc giao thành công** (`delivered_at`) — một khoảng thời gian, không phải mốc ngày dương lịch.
- **Chuyển nhượng đơn giữ nguyên cơ chế hiện có** trong feature này; việc giới hạn/mở rộng nó theo `SHIPPING`/`DELIVERED` là quyết định **hoãn** (ghi `deferred.md`), không phải một phần của deliverable.
- **Ảnh tuỳ chọn** ở cả hai bước; trần **5 ảnh** mỗi bước (bước giao thành công cũng 5, theo cùng ngưỡng đã nêu cho bước đang giao). Ảnh dùng **cùng ngưỡng kích thước/định dạng** như ảnh sản phẩm/avatar hiện có.
- **Feedback cấp đơn, chỉ nội dung văn bản, một lần mỗi đơn**, gửi cho artist; không phải đánh giá sản phẩm (module 13) và không có điểm số/kiểm duyệt trong feature này.
- **Ảnh trong feedback** dùng **cùng định dạng/ngưỡng** và cùng trần **5 ảnh** như ảnh bằng chứng giao hàng (FR-025, FR-009); ảnh là **tuỳ chọn** — feedback không ảnh vẫn hợp lệ. Endpoint gửi feedback chuyển sang **`multipart/form-data`** (`content` + 0–5 `photos`), cùng hình dạng upload với hai route giao hàng.
- **Ca giao thất bại / khách không nhận hàng** được **hoãn** (ghi `deferred.md`): không có trạng thái `FAILED`/`RETURNED`, không hoàn tiền trong feature này.
- **Thông báo chỉ qua email** (cơ chế best-effort hiện có); thông báo in-app và các email khác thuộc module 12.
- **Không phí ship, không đổi tiền/tồn sau `PAID`** (ADR 015 §5, module 05).
- **Cơ sở hạ tầng nền dùng lại**: guard phiên/vai trò, audit, envelope, phân trang, kho media dùng chung, lịch sử chỉnh sửa đơn hiện có.
- **Artist = operator của shop** (admin), một nghệ sĩ; không thêm vai trò mới.
