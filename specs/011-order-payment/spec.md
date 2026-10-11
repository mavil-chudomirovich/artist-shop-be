# Feature Specification: Online Order Payment

**Feature Branch**: `011-order-payment`

**Created**: 2026-10-11

**Status**: Approved (ready for implementation)

**Input**: User description: "làm module 08 Payment" — module 08 Payment (`docs/modules/08-payment.md`), module cuối của V1.0: khách trả tiền online cho đơn hàng qua cổng thanh toán, xác nhận idempotent, an toàn với tồn kho và trạng thái đơn.

## Clarifications

### Session 2026-10-11

- Q: Khách mở thanh toán lần nữa cho cùng một đơn đã có yêu cầu thanh toán còn hiệu lực thì sao? → A: **Dùng lại** yêu cầu còn hiệu lực và trả lại **chính liên kết/QR đó**; **không** tạo yêu cầu mới khi một yêu cầu còn hiệu lực đang tồn tại.
- Q: Khi một thanh toán đã xác minh không áp được vào đơn (đơn đã sửa/đã huỷ/hết hạn) và V1.0 không hoàn tiền thì sao? → A: **Đơn giữ nguyên**, không tự ghi `PAID`, **không tự hoàn tiền**; lưu một bản ghi **"cần đối soát"** cho operator, người xử lý **ngoài hệ thống**.
- Q: Trạng thái thanh toán được phơi ra client thế nào? → A: **Endpoint thanh toán riêng** — khách có một đường đọc thanh toán của đơn mình, operator có đường đọc mọi bản ghi thanh toán + các khoản cần đối soát; **không** trộn trường thanh toán vào response của đơn.

## Scope boundary

Đây là **module 08 Payment**, phụ thuộc module 07 Order (đã xong, feature `009` + `010`). Nó giao phần **trả tiền online** cho một đơn đang chờ thanh toán và **tin cậy xác nhận của cổng thanh toán đúng một lần**; nó **không** giao hoàn tiền, không đa tiền tệ, không thanh toán định kỳ, và không giao giao diện quản trị đầy đủ (module 16).

- **Cổng thanh toán**: **PayOS** (theo [ADR 015 §5](../../docs/decisions/015-shop-first-version-plan-and-payos.md)). Tài liệu `docs/modules/08-payment.md` còn ghi MoMo/VietQR là bản cũ; ADR 015 thắng và sẽ được sửa cùng feature này.
- **Deliverable**:
  1. **Bắt đầu thanh toán** — khách mở thanh toán cho một đơn **đang chờ thanh toán**; hệ thống tạo một yêu cầu thanh toán cho **đúng tổng tiền hàng** và trả cho khách thứ để trả (liên kết/QR).
  2. **Xác nhận của cổng được tin cậy đúng một lần** — cổng báo kết quả; hệ thống **kiểm tra nguồn gốc**, **đối chiếu đơn + số tiền**, rồi áp kết quả **một lần** và đơn chuyển sang **đã trả tiền** (hàng giữ chỗ thành bán). Gửi lại/lặp **không** tạo hiệu ứng thứ hai.
  3. **Xác nhận cũ/không khớp không bị áp nhầm** — xác nhận cho đơn **đã bị sửa** sau khi tạo yêu cầu, hoặc đến **sau khi đơn hết hạn/bị huỷ**, **không** được tự động ghi nhận; nó được **lưu lại để operator đối soát**.
  4. **Huỷ đơn chưa trả thì huỷ yêu cầu thanh toán** — huỷ đơn đang chờ thanh toán cũng **huỷ yêu cầu thanh toán ở cổng** (nỗ lực tối đa) và nhả hàng giữ chỗ (việc nhả là của module 07).
  5. **Nhìn thấy trạng thái thanh toán** — khách thấy trạng thái thanh toán của đơn mình; operator thấy mọi bản ghi thanh toán và **đối soát** các khoản không áp được.
  6. **Không có hoàn tiền** ở phiên bản này; đơn **đã trả tiền không huỷ** (được chuyển nhượng như module 07).
- **Deferred**: hoàn tiền/từ chối hoàn tiền tự động; đa tiền tệ; thanh toán định kỳ; giao diện quản trị đầy đủ (module 16); thông báo in-app (module 12).

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A customer pays an awaiting-payment order online (Priority: P1)

Khách có một đơn đang chờ thanh toán mở thanh toán; hệ thống tạo một yêu cầu thanh toán cho đúng tổng tiền hàng và đưa khách tới nơi trả (liên kết/QR). Khách trả tiền; cổng báo kết quả; đơn chuyển sang đã trả tiền.

**Why this priority**: Đây là lý do module tồn tại — không có nó thì hàng đã giữ chỗ không bao giờ được bán.

**Independent Test**: Với một đơn `PAYMENT_PENDING`, mở thanh toán → nhận được yêu cầu thanh toán đúng tổng; mô phỏng cổng xác nhận → đơn thành `PAID` và hàng giữ chỗ thành bán.

**Acceptance Scenarios**:

1. **Given** một đơn của khách đang `PAYMENT_PENDING`, **When** khách mở thanh toán, **Then** hệ thống tạo một yêu cầu thanh toán cho **đúng tổng tiền hàng** và trả về thứ để trả.
2. **Given** một yêu cầu thanh toán đã tạo, **When** cổng xác nhận đã trả hợp lệ và khớp đơn, **Then** đơn chuyển sang `PAID` và hàng giữ chỗ thành bán (một lần).
3. **Given** một đơn không ở trạng thái chờ thanh toán, **When** khách mở thanh toán, **Then** bị từ chối.
4. **Given** một đơn của khách khác, **When** khách mở thanh toán, **Then** bị từ chối như không tìm thấy.

---

### User Story 2 - The shop trusts the gateway's confirmation exactly once (Priority: P1)

Cổng báo kết quả thanh toán; hệ thống chỉ chấp nhận khi **xác minh được nguồn gốc**, **đối chiếu đơn và số tiền**, và áp **một lần**. Báo lặp lại không tạo hiệu ứng thứ hai.

**Why this priority**: Tiền và tồn kho là dữ liệu rủi ro nhất; áp trùng hoặc áp nhầm là lỗi nghiêm trọng.

**Independent Test**: Gửi cùng một xác nhận hợp lệ hai lần → đơn chỉ thành `PAID` một lần, hàng chỉ bán một lần; gửi một xác nhận không xác minh được → bị từ chối, đơn không đổi.

**Acceptance Scenarios**:

1. **Given** một xác nhận không xác minh được nguồn gốc, **When** nó tới, **Then** bị từ chối và đơn không đổi.
2. **Given** một xác nhận hợp lệ đã được áp, **When** nó tới lần nữa, **Then** không có hiệu ứng thứ hai (đơn vẫn `PAID`, hàng không bán lần hai).
3. **Given** một xác nhận hợp lệ nhưng **số tiền không khớp** tổng đơn, **When** nó tới, **Then** bị từ chối, đơn không đổi, và được lưu để đối soát.
4. **Given** một xác nhận hợp lệ và khớp, **When** nó được áp, **Then** việc ghi nhận thanh toán và việc đơn chuyển `PAID` cùng nằm trong **một** giao dịch.

---

### User Story 3 - A stale confirmation is never applied to the wrong content (Priority: P2)

Một xác nhận đến cho **nội dung đơn cũ** (khách đã sửa đơn sau khi tạo yêu cầu) hoặc đến **sau khi đơn hết hạn/bị huỷ** **không** được tự động ghi nhận; nó được lưu lại cho operator đối soát.

**Why this priority**: P2 — luồng chính chạy được khi chưa có, nhưng nếu thiếu thì một giao dịch cũ có thể gắn nhầm vào đơn mới, gây sai tiền.

**Independent Test**: Sửa một đơn sau khi tạo yêu cầu thanh toán, rồi gửi xác nhận của yêu cầu cũ → đơn **không** thành `PAID`; bản ghi được đánh dấu để đối soát.

**Acceptance Scenarios**:

1. **Given** đơn đã bị sửa sau khi yêu cầu thanh toán được tạo, **When** xác nhận của yêu cầu cũ tới, **Then** đơn không thành `PAID` và bản ghi được lưu để đối soát.
2. **Given** đơn đã hết hạn hoặc bị huỷ, **When** xác nhận tới, **Then** đơn không bị ghi `PAID` nhầm và bản ghi được lưu để đối soát.
3. **Given** một xác nhận đã bị đánh dấu đối soát, **When** nó tới lần nữa, **Then** không tạo bản ghi trùng.

---

### User Story 4 - Cancelling an unpaid order cancels its payment request (Priority: P2)

Khi một đơn đang chờ thanh toán bị huỷ (khách huỷ hoặc hết hạn), yêu cầu thanh toán ở cổng cũng bị huỷ (nỗ lực tối đa) và hàng giữ chỗ được nhả (việc nhả là của module 07).

**Why this priority**: P2 — không có nó thì khách vẫn có thể trả một đơn đã huỷ.

**Independent Test**: Huỷ một đơn đang chờ thanh toán → yêu cầu thanh toán ở cổng được huỷ và hàng được nhả.

**Acceptance Scenarios**:

1. **Given** một đơn đang chờ thanh toán có yêu cầu thanh toán, **When** đơn bị huỷ, **Then** hệ thống huỷ yêu cầu thanh toán ở cổng (nỗ lực tối đa) và hàng được nhả.
2. **Given** việc huỷ yêu cầu ở cổng thất bại, **When** đơn bị huỷ, **Then** đơn vẫn bị huỷ và hàng vẫn được nhả; lỗi được ghi lại (không chặn việc huỷ đơn).

---

### User Story 5 - The payment state is visible and anomalies are reviewable (Priority: P2)

Khách thấy trạng thái thanh toán của đơn mình; operator thấy mọi bản ghi thanh toán và **đối soát** các khoản không áp được (không khớp, đến muộn).

**Why this priority**: P2 — vận hành cần đối soát các trường hợp không tự động xử lý được.

**Independent Test**: Sau khi tạo và áp một thanh toán, khách thấy trạng thái đã trả; operator thấy bản ghi; một khoản không khớp xuất hiện trong danh sách cần đối soát.

**Acceptance Scenarios**:

1. **Given** một đơn của khách đã có thanh toán, **When** khách mở **đường đọc thanh toán riêng** của đơn đó, **Then** khách thấy trạng thái thanh toán của đơn mình (trạng thái thanh toán **không** nằm trong response của đơn).
2. **Given** một khoản thanh toán không áp được, **When** operator xem danh sách thanh toán, **Then** khoản đó xuất hiện để đối soát.
3. **Given** một khách không phải chủ đơn, **When** họ cố xem thanh toán của đơn đó, **Then** bị từ chối.

### Edge Cases

- Cổng báo cùng một giao dịch nhiều lần → chỉ một hiệu ứng.
- Cổng báo một giao dịch cho đơn đã sửa (nội dung cũ) → không ghi nhận; đối soát.
- Cổng báo thanh toán sau khi đơn đã hết hạn hoặc bị huỷ → không ghi `PAID` nhầm; đối soát.
- Số tiền xác nhận khác tổng đơn → từ chối, đối soát.
- Khách mở thanh toán hai lần cho cùng một đơn → không tạo hai yêu cầu xung đột.
- Khách mở thanh toán khi đã có payment còn hiệu lực nhưng link ở cổng đã huỷ/hết hạn → từ chối để operator đối soát, không tạo link thứ hai.
- Cổng không phản hồi/lỗi khi tạo yêu cầu → đơn **không** đổi trạng thái; khách được báo để thử lại.
- Xác nhận đến đúng lúc khách vừa huỷ đơn → một kết quả duy nhất, không ghi `PAID` nhầm.

## Requirements *(mandatory)*

### Functional Requirements — bắt đầu thanh toán

- **FR-001**: Hệ thống MUST cho khách đã đăng nhập **mở thanh toán** cho một đơn **của chính họ** đang **chờ thanh toán**.
- **FR-002**: Mở thanh toán MUST tạo một **yêu cầu thanh toán** cho **đúng tổng tiền hàng** của đơn và trả về thứ khách dùng để trả (liên kết/QR).
- **FR-003**: Mở thanh toán MUST bị **từ chối** nếu đơn không ở trạng thái chờ thanh toán, hoặc không phải của khách (chủ lấy từ phiên, không từ request).
- **FR-004**: Số tiền của yêu cầu thanh toán MUST bằng tổng đơn, là **số nguyên đơn vị nhỏ nhất + tiền tệ**, **không** phí ship.
- **FR-005**: Mở thanh toán lặp cho cùng một đơn MUST **dùng lại** yêu cầu thanh toán còn hiệu lực và trả lại **chính liên kết/QR đó**; hệ thống MUST **không** tạo yêu cầu mới khi một yêu cầu còn hiệu lực đang tồn tại.
- **FR-006**: Nếu cổng lỗi khi tạo yêu cầu, đơn MUST **không** đổi trạng thái và khách MUST nhận một lỗi có thể thử lại.

### Functional Requirements — xác nhận của cổng

- **FR-007**: Hệ thống MUST chỉ chấp nhận một xác nhận khi **xác minh được nó đến từ cổng**; một xác nhận không xác minh được MUST bị từ chối.
- **FR-008**: Một xác nhận đã xác minh MUST được **đối chiếu** với **đơn** và **số tiền**; không khớp MUST bị từ chối và **không** áp dụng.
- **FR-009**: Một xác nhận đã xác minh và khớp MUST chuyển đơn sang **đã trả tiền** và biến hàng giữ chỗ thành **bán**, **đúng một lần** kể cả khi xác nhận **lặp lại**.
- **FR-010**: Việc ghi nhận thanh toán, việc đơn đổi trạng thái và việc bán hàng MUST nằm trong **một giao dịch**, ghi **audit**, và lỗi cổng MUST NOT để lại trạng thái trung gian không nhất quán.
- **FR-011**: Trạng thái thanh toán MUST chỉ đổi qua **state machine** của module; không ghi thẳng giá trị.

### Functional Requirements — xác nhận cũ/không khớp

- **FR-012**: Một xác nhận cho một đơn **đã bị sửa** sau khi yêu cầu thanh toán được tạo MUST **không** được áp vào nội dung mới; đơn MUST **giữ nguyên** và bản ghi MUST được **lưu lại** cho operator đối soát; hệ thống MUST **không** tự hoàn tiền.
- **FR-013**: Một xác nhận đến khi đơn **không còn ở trạng thái chờ thanh toán** (đã huỷ/hết hạn) MUST **không** tự động ghi `PAID`; đơn MUST giữ nguyên và bản ghi MUST được lưu lại để đối soát; hệ thống MUST **không** tự hoàn tiền.
- **FR-014**: Các bản ghi đối soát MUST **không** trùng khi cùng một sự việc được xử lý lại.

### Functional Requirements — huỷ

- **FR-015**: Huỷ một đơn đang chờ thanh toán MUST cũng **huỷ yêu cầu thanh toán ở cổng** (nỗ lực tối đa); việc nhả hàng giữ chỗ là của module 07.
- **FR-016**: Việc huỷ yêu cầu ở cổng thất bại MUST NOT chặn việc huỷ đơn; lỗi MUST được ghi lại.

### Functional Requirements — nhìn thấy & đối soát

- **FR-017**: Khách MUST thấy được **trạng thái thanh toán** của đơn mình qua **một đường đọc thanh toán riêng** (không gộp vào response của đơn); một khách MUST **không** thấy thanh toán của đơn người khác.
- **FR-018**: Operator MUST có **một đường đọc riêng** để xem **mọi bản ghi thanh toán** và các khoản **cần đối soát**.

### Functional Requirements — an toàn & phạm vi

- **FR-019**: Điểm nhận xác nhận của cổng MUST truy cập được **công khai** (không cần phiên) nhưng MUST **xác minh nguồn gốc**; một yêu cầu không xác minh được MUST bị từ chối.
- **FR-020**: Thông tin xác thực cổng MUST lấy từ **biến môi trường**; **không** bí mật nào bị commit.
- **FR-021**: Phiên bản này MUST **không** có hoàn tiền; một đơn **đã trả tiền** MUST **không** bị huỷ (được chuyển nhượng như module 07).
- **FR-022**: Mọi bản ghi thanh toán MUST lưu **đơn**, **số tiền + tiền tệ**, **nhà cung cấp**, **tham chiếu giao dịch**, **khoá chống trùng**, **trạng thái** và **thời gian**.

### Key Entities

- **Payment**: một lần thanh toán cho một đơn — đơn được trả, số tiền + tiền tệ, nhà cung cấp, tham chiếu giao dịch của cổng, khoá chống trùng, **trạng thái** (chờ, đã trả, đã huỷ, cần đối soát), thời điểm tạo/đổi.
- **Payment event**: một dòng ghi lại điều module đã làm với một thanh toán — tạo, áp dụng, huỷ, hoặc ghi nhận cần đối soát — cùng người thực hiện (nếu có) và một lý do ngắn. Xác nhận của cổng là nguồn của các sự kiện áp dụng và cần đối soát.
- **Order (module 07)**: đơn đang chờ thanh toán được trả; module 08 **không** thêm trục trạng thái thanh toán vào đơn — đơn giữ **một** trục trạng thái; `PAID` là đã xác nhận thanh toán.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Khách trả được một đơn đang chờ thanh toán online; khi cổng xác nhận hợp lệ, đơn thành **đã trả tiền** — kiểm chứng bằng test tự động.
- **SC-002**: Một xác nhận **lặp lại** không tạo hiệu ứng thứ hai; hàng chỉ bán **một lần**.
- **SC-003**: Một xác nhận **không xác minh được**, **sai số tiền**, cho **nội dung đơn cũ**, hoặc cho đơn **không còn chờ thanh toán** **không** bao giờ được áp; nó được lưu để đối soát.
- **SC-004**: Huỷ một đơn đang chờ thanh toán cũng huỷ yêu cầu thanh toán ở cổng và nhả hàng (nỗ lực tối đa; lỗi cổng không chặn việc huỷ đơn).
- **SC-005**: Khách thấy trạng thái thanh toán của đơn mình và **không** thấy của người khác; operator thấy mọi bản ghi và các khoản cần đối soát.
- **SC-006**: Không có bí mật nào bị commit; điểm nhận xác nhận **từ chối** mọi yêu cầu không xác minh được.
- **SC-007**: Cổng lỗi khi tạo yêu cầu thanh toán **không** để đơn ở trạng thái sai.

## Assumptions

- **Cổng thanh toán là PayOS** (ADR 015 §5): tạo liên kết/QR thanh toán và xác nhận qua **webhook** có **chữ ký**. Tích hợp dùng **thư viện chuẩn** (không thêm dependency); nếu đổi sang SDK chính thức thì ghi Complexity Tracking ở plan.
- **Không hoàn tiền** ở V1.0; huỷ chỉ áp cho đơn **chưa trả tiền** (ADR 015 §5 D5).
- **Bước "đã trả tiền" là của module 07** (`MarkPaid`, từ `PAYMENT_PENDING`); module 08 **điều khiển** nó bằng một danh tính sự kiện ổn định, đúng một lần.
- **Phát hiện xác nhận cũ** dựa vào **phiên bản đơn** (`order_version`) và **hạn thanh toán** (`payment_expires_at`) mà feature `010-order-confirmation` đã để lại.
- **Điểm nhận xác nhận phải truy cập công khai**: domain VPS ở production; dev local cần tunnel.
- **Tiền là số nguyên đơn vị nhỏ nhất + tiền tệ** (VND), không số thực, không phí ship (ADR 015 §5).
- **Đơn giữ một trục trạng thái**; module 08 giữ **trạng thái thanh toán riêng** trong bản ghi của nó (clarification của feature 009).
- **Cơ sở hạ tầng nền dùng lại**: guard phiên/vai trò, envelope, error catalogue, audit, `UnitOfWork` (đã transaction-aware), phân trang.
- **Vận hành**: cần tài khoản merchant PayOS + credentials (client id, API key, checksum key) đặt trong biến môi trường.
