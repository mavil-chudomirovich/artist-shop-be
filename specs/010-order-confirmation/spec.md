# Feature Specification: Order Confirmation & Editing

**Feature Branch**: `010-order-confirmation`

**Created**: 2026-10-10

**Status**: Draft

**Input**: User description: "bổ sung luồng order: đặt hàng → PENDING (chờ artist xác nhận) → artist xác nhận → PAYMENT_PENDING → thanh toán → PAID → …; ở PENDING/PAYMENT_PENDING khách được sửa đơn, sửa khi đang PAYMENT_PENDING thì quay lại PENDING; có email báo cho artist khi cần xác nhận và cho khách khi đổi trạng thái; đọc thêm `docs/modules/ke-hoach-don-hang.md`."

## Clarifications

### Session 2026-10-10

- Q: FR-012 nói khách sửa được "địa chỉ giao" — theo cơ chế nào? → A: Khách chọn một **địa chỉ đã lưu** bằng mã địa chỉ (như checkout); **bỏ trống** thì **giữ nguyên** địa chỉ hiện tại của đơn. Không có mô hình địa chỉ nhập tay mới.
- Q: Một lần sửa khiến đơn còn 0 dòng thì sao? → A: **Từ chối** — một đơn MUST luôn có **≥1 dòng**; khách muốn bỏ hết thì **huỷ đơn**, không dùng sửa.
- Q: `order_version` tăng khi nào? → A: Tăng khi **nội dung đổi** (mỗi lần sửa thành công) **và** khi artist **xác nhận** (chốt nội dung để module 08 đối chiếu).
- Q: Khách huỷ được đơn ở trạng thái nào trước khi trả tiền? → A: Cả **chờ artist xác nhận** (`PENDING`) và **chờ thanh toán** (`PAYMENT_PENDING`).

## Scope boundary

Kế thừa module 07 (feature `009-order`). Feature này **thay đổi vòng đời đơn trước khi trả tiền** và thêm chỉnh sửa đơn + thông báo. Nó **không** giao nhà cung cấp thanh toán (module 08) và **không** giao thông báo in-app (module 12).

- **Deliverable**:
  1. Đơn mới ở trạng thái **chờ artist xác nhận**, **chưa giữ hàng**.
  2. Artist **xác nhận** → hệ thống **giữ hàng toàn bộ** (all-or-nothing) và đơn chuyển sang **chờ thanh toán**; artist **từ chối** đơn.
  3. Khách **chỉnh sửa** đơn khi đang chờ xác nhận / chờ thanh toán; sửa khi đang chờ thanh toán thì **quay lại chờ xác nhận**.
  4. **Hạn thanh toán 60 phút** từ lúc giữ hàng; quá hạn thì tự huỷ và trả hàng. Đơn chờ artist **không** hết hạn theo thời gian.
  5. **Email**: báo artist khi đơn cần xác nhận; báo khách mỗi khi trạng thái đơn đổi.
  6. **Lịch sử chỉnh sửa** + **phiên bản đơn** để module 08 đối chiếu về sau.
- **Deferred**: payment attempt/session, IPN/callback đến muộn, huỷ payment link PayOS → **module 08**; thông báo in-app và các email khác → **module 12**; hoàn tiền/tách đơn/mã giảm giá → ngoài MVP.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A placed order waits for the artist, then becomes payable (Priority: P1)

Khách đặt hàng: đơn được tạo ở trạng thái **chờ artist xác nhận** và chưa giữ hàng. Khi artist xác nhận, đơn chuyển sang **chờ thanh toán** và hàng được giữ; sau đó khách trả tiền và đơn đi tiếp như luồng hiện có.

**Why this priority**: Đây là mục đích của feature — không có bước xác nhận thì không có gì mới.

**Independent Test**: Đặt hàng → thấy đơn "chờ xác nhận" và tồn khả dụng **không đổi**; artist xác nhận → đơn "chờ thanh toán", tồn khả dụng **giảm** (vật lý không đổi); xác nhận thanh toán → `PAID`.

**Acceptance Scenarios**:

1. **Given** khách có giỏ và địa chỉ, **When** đặt hàng, **Then** đơn ở trạng thái chờ artist xác nhận, có snapshot dòng + địa chỉ, giỏ rỗng, và **không** giữ hàng.
2. **Given** đơn chờ xác nhận, **When** artist xác nhận và đủ hàng, **Then** đơn chuyển sang chờ thanh toán, hàng được giữ, và hạn thanh toán mở ra.
3. **Given** đơn chờ thanh toán, **When** thanh toán được xác nhận, **Then** đơn thành `PAID` và hàng giữ chỗ thành bán (một lần).
4. **Given** đơn đã trả tiền, **When** artist đánh dấu giao rồi hoàn tất, **Then** mỗi bước thành công và được ghi audit.

---

### User Story 2 - Goods are held only when the artist accepts, all or nothing (Priority: P1)

Hàng chỉ bị giữ khi artist xác nhận, và giữ **toàn bộ** đơn trong một bước; nếu thiếu bất kỳ dòng nào thì **không** chuyển sang chờ thanh toán và nêu dòng thiếu.

**Why this priority**: Chống giữ hàng vô ích và chống bán quá tồn — điều kiện đúng đắn cốt lõi.

**Independent Test**: Với một đơn nhiều dòng mà một dòng không đủ hàng, artist xác nhận bị từ chối, đơn **vẫn** chờ xác nhận, và **không** dòng nào bị giữ.

**Acceptance Scenarios**:

1. **Given** đơn chờ xác nhận mà tồn không đủ một dòng, **When** artist xác nhận, **Then** từ chối, nêu dòng thiếu (kèm số còn), đơn giữ nguyên chờ xác nhận, không giữ dòng nào.
2. **Given** hai đơn cùng tranh đơn vị cuối cùng, **When** cả hai được xác nhận, **Then** nhiều nhất một đơn được xác nhận; đơn kia bị từ chối và không oversell.
3. **Given** đơn đang chờ xác nhận, **When** không ai làm gì, **Then** tồn khả dụng không đổi (không giữ hàng).

---

### User Story 3 - A customer changes an order before paying (Priority: P2)

Khách sửa nội dung/địa chỉ đơn khi đang chờ xác nhận hoặc chờ thanh toán. Sửa đơn đang chờ xác nhận thì giữ nguyên chờ xác nhận; sửa đơn đang chờ thanh toán thì trả hàng và **quay lại chờ xác nhận** để artist xác nhận lại.

**Why this priority**: P2 — shop bán được khi chưa có nó, nhưng khách cần sửa trước khi trả tiền.

**Independent Test**: Sửa một đơn đang chờ thanh toán → đơn về chờ xác nhận, hàng được trả về khả dụng, artist phải xác nhận lại; sửa đơn chờ xác nhận → vẫn chờ xác nhận.

**Acceptance Scenarios**:

1. **Given** đơn chờ xác nhận, **When** khách sửa dòng/địa chỉ hợp lệ, **Then** đơn cập nhật, tổng tính lại, **vẫn** chờ xác nhận, không đụng tồn.
2. **Given** đơn chờ thanh toán, **When** khách sửa, **Then** hàng cũ được trả về **đúng một lần**, đơn **quay lại chờ xác nhận**, xác nhận/phiên thanh toán cũ mất hiệu lực.
3. **Given** một dòng sau khi sửa không còn bán/đã xoá/vượt tồn, **When** khách sửa, **Then** từ chối, nêu dòng, đơn **không đổi**. Giá mỗi dòng được **chụp lại theo giá hiện tại** khi sửa (khách chọn lại món nên nhận giá hiện tại).
4. **Given** đơn đã trả tiền hoặc xa hơn, **When** khách sửa, **Then** từ chối.
5. **Given** đơn của khách khác, **When** khách sửa, **Then** từ chối như không tìm thấy.

---

### User Story 4 - The artist declines an order (Priority: P2)

Artist **từ chối** một đơn đang chờ xác nhận; đơn chuyển sang đã huỷ và không có hàng nào bị giữ.

**Why this priority**: P2 — cần khi artist không thực hiện được đơn.

**Independent Test**: Artist từ chối một đơn chờ xác nhận → đơn `CANCELLED`, tồn không đổi.

**Acceptance Scenarios**:

1. **Given** đơn chờ xác nhận, **When** artist từ chối, **Then** đơn thành đã huỷ, hàng chưa từng bị giữ nên không cần trả.
2. **Given** đơn không ở trạng thái chờ xác nhận, **When** artist từ chối, **Then** từ chối, nêu trạng thái hiện tại.

---

### User Story 5 - Both sides are notified (Priority: P2)

Artist được báo khi có đơn **cần xác nhận** (đơn mới hoặc đơn bị sửa quay lại); khách được báo mỗi khi **trạng thái đơn đổi**. Một lần gửi lỗi không làm hỏng thao tác đơn.

**Why this priority**: P2 — vận hành cần biết có đơn chờ; khách cần biết đơn đổi trạng thái.

**Independent Test**: Đặt hàng → artist nhận email "cần xác nhận"; artist xác nhận → khách nhận email "đã xác nhận"; lỗi gửi email vẫn làm đơn đổi trạng thái bình thường.

**Acceptance Scenarios**:

1. **Given** một đơn mới, **When** nó được tạo, **Then** artist nhận thông báo cần xác nhận.
2. **Given** một đơn bị sửa và quay lại chờ xác nhận, **When** sửa xong, **Then** artist lại nhận thông báo cần xác nhận.
3. **Given** bất kỳ lần đổi trạng thái đơn, **When** nó xảy ra, **Then** khách nhận thông báo trạng thái mới.
4. **Given** việc gửi thông báo thất bại, **When** thao tác đơn chạy, **Then** đơn vẫn đổi trạng thái thành công (thông báo không chặn giao dịch).

---

### User Story 6 - An accepted-but-unpaid order expires; a waiting order does not (Priority: P3)

Đơn chờ thanh toán quá hạn 60 phút mà chưa trả tiền thì tự huỷ và trả hàng (đúng một lần). Đơn còn chờ artist **không** hết hạn theo thời gian.

**Why this priority**: P3 — bảo vệ tồn kho không bị giữ vô hạn, nhưng luồng chính chạy được khi chưa có.

**Independent Test**: Một đơn chờ thanh toán quá hạn tự huỷ, hàng về khả dụng một lần; một đơn chờ xác nhận để lâu vẫn chờ xác nhận.

**Acceptance Scenarios**:

1. **Given** đơn chờ thanh toán quá hạn, **When** không có thanh toán, **Then** đơn `CANCELLED`, hàng trả về **đúng một lần** (kể cả khi lặp).
2. **Given** đơn chờ xác nhận, **When** để lâu, **Then** đơn vẫn chờ xác nhận (không hết hạn theo thời gian).

### Edge Cases

- Hai xác nhận đồng thời trên cùng đơn vị hàng cuối → nhiều nhất một thắng (chống oversell).
- Sửa đơn khi đang chờ thanh toán đúng lúc thanh toán tới → một kết quả duy nhất: hoặc đã trả tiền (không sửa được), hoặc quay lại chờ xác nhận (không ghi nhận thanh toán cũ).
- Đơn nhiều dòng: giữ hàng phải toàn bộ hoặc không gì cả, không giữ nửa.
- Xác nhận/từ chối một đơn đã ở trạng thái khác → từ chối, nêu trạng thái hiện tại, đơn nguyên vẹn.
- Sửa đơn bằng nội dung y hệt → vẫn là một lần sửa (tăng phiên bản + ghi lịch sử), nhưng **không** đổi trạng thái và **không** đụng tồn.
- Khách sửa đơn của người khác → như không tìm thấy.
- Gửi email lỗi → đơn vẫn đúng.

## Requirements *(mandatory)*

### Functional Requirements — đặt hàng & chờ xác nhận

- **FR-001**: Đặt hàng của khách đã đăng nhập MUST tạo đơn ở trạng thái **chờ artist xác nhận**.
- **FR-002**: Đơn chờ xác nhận MUST mang **snapshot** dòng (tên, slug, giá, tiền tệ) + địa chỉ giao và tổng tiền như luồng hiện có, và MUST làm rỗng giỏ.
- **FR-003**: Đơn chờ xác nhận MUST **không** giữ hàng: tồn khả dụng MUST không đổi.

### Functional Requirements — xác nhận & giữ hàng

- **FR-004**: Artist (operator) MUST xác nhận một đơn chờ xác nhận; chỉ khi đó hàng mới được giữ.
- **FR-005**: Xác nhận MUST giữ **toàn bộ** dòng trong **một bước all-or-nothing**; nếu bất kỳ dòng không giữ được, đơn MUST **giữ nguyên** chờ xác nhận, nêu dòng thiếu (kèm số còn), và **không** giữ dòng nào.
- **FR-006**: Khi xác nhận thành công, đơn MUST chuyển sang **chờ thanh toán** và mở **hạn thanh toán**.
- **FR-007**: Một bước chuyển không hợp lệ (xác nhận lại, xác nhận đơn không chờ xác nhận) MUST bị từ chối, nêu trạng thái hiện tại, đơn nguyên vẹn.

### Functional Requirements — hạn thanh toán & hết hạn

- **FR-008**: Đơn chờ thanh toán quá hạn 60 phút mà chưa trả tiền MUST tự huỷ và trả hàng về khả dụng **đúng một lần** (kể cả khi xử lý lặp).
- **FR-009**: Đơn còn chờ artist xác nhận MUST **không** hết hạn theo thời gian.

### Functional Requirements — trả tiền & hoàn tất

- **FR-010**: Xác nhận thanh toán MUST chuyển chờ thanh toán → đã trả tiền, biến hàng giữ chỗ thành **bán** **đúng một lần**; sau đó giao (`SHIPPED`) và hoàn tất (`COMPLETED`) như hiện có.
- **FR-011**: Đơn **đã trả tiền** MUST **không** cho khách sửa hoặc huỷ (được chuyển nhượng như hiện có). Khách MUST **huỷ được** đơn của mình khi đơn **chưa trả tiền** — cả **chờ artist xác nhận** và **chờ thanh toán**; huỷ đơn đang chờ thanh toán MUST trả hàng đã giữ về khả dụng **đúng một lần**, còn huỷ đơn chờ xác nhận thì không có hàng nào để trả.

### Functional Requirements — chỉnh sửa đơn

- **FR-012**: Khách MUST sửa được **nội dung dòng** (thêm/bớt/đổi số lượng) và **địa chỉ giao** của đơn khi đơn đang **chờ xác nhận** hoặc **chờ thanh toán**. Địa chỉ giao MUST được chọn từ **địa chỉ đã lưu** của khách bằng mã địa chỉ (như checkout); khi khách không nêu địa chỉ, đơn MUST **giữ nguyên** địa chỉ hiện tại.
- **FR-013**: Sửa đơn đang chờ xác nhận MUST giữ đơn ở trạng thái chờ xác nhận.
- **FR-014**: Sửa đơn đang chờ thanh toán MUST trả hàng đã giữ về khả dụng **đúng một lần**, làm **mất hiệu lực** xác nhận/phiên thanh toán cho nội dung cũ, và đưa đơn **quay lại chờ xác nhận** (artist phải xác nhận lại).
- **FR-015**: Mỗi lần sửa MUST **đối chiếu lại** từng dòng với trạng thái bán và tồn khả dụng hiện tại, **chụp lại giá hiện tại** của mỗi dòng, **tính lại tổng**, và MUST **từ chối** (không đổi gì) nếu một dòng không còn bán/đã xoá hoặc vượt tồn — nêu dòng đó. Một lần sửa để lại **0 dòng** MUST bị từ chối: một đơn luôn có **≥1 dòng**.
- **FR-016**: Sửa đơn **đã trả tiền** hoặc ở trạng thái xa hơn MUST bị từ chối.
- **FR-017**: Mọi lần sửa MUST được **ghi lại** (nội dung cũ/mới, người sửa, thời điểm) và **tăng phiên bản** đơn. Việc artist **xác nhận** cũng MUST **tăng phiên bản** đơn, để module 08 đối chiếu nội dung đã chốt.

### Functional Requirements — từ chối

- **FR-018**: Artist MUST từ chối được một đơn đang chờ xác nhận; đơn thành **đã huỷ** (không có hàng nào bị giữ để trả).

### Functional Requirements — thông báo

- **FR-019**: Hệ thống MUST thông báo cho **artist** khi một đơn **cần xác nhận** (đơn mới, hoặc đơn bị sửa quay lại chờ xác nhận).
- **FR-020**: Hệ thống MUST thông báo cho **khách** mỗi khi **trạng thái đơn** của họ thay đổi.
- **FR-021**: Một lần gửi thông báo **thất bại MUST NOT** làm hỏng hay rollback thao tác đơn.

### Functional Requirements — quyền, audit, toàn vẹn

- **FR-022**: Chỉ **chủ đơn** được sửa/huỷ đơn của mình (chủ lấy từ phiên, không từ request); chỉ **operator** được xác nhận/từ chối/giao/hoàn tất/chuyển nhượng.
- **FR-023**: Mỗi thao tác của operator (xác nhận, từ chối, giao, hoàn tất, chuyển nhượng) MUST được ghi **audit** nêu đơn và người thực hiện.
- **FR-024**: Xác nhận MUST **không** oversell: với hai đơn tranh đơn vị hàng cuối, nhiều nhất một đơn được giữ.
- **FR-025**: Tổng tiền MUST là **số nguyên đơn vị nhỏ nhất + tiền tệ**, **không** phí ship, **không** làm tròn.
- **FR-026**: Hệ thống MUST cho operator **lọc theo trạng thái** và **sắp xếp** danh sách đơn, để màn hình xác nhận hiển thị các đơn chờ theo **thứ tự thời gian đặt (FIFO)**.

### Key Entities

- **Order**: đơn của khách — chủ, địa chỉ snapshot, tổng, **trạng thái** (`PENDING`, `PAYMENT_PENDING`, `PAID`, `SHIPPED`, `COMPLETED`, `CANCELLED`), **phiên bản**, **hạn thanh toán**, **mốc xác nhận**, thời điểm tạo/đổi.
- **Order line**: dòng đơn — snapshot sản phẩm tại thời điểm thêm/sửa + số lượng; **sửa được** trước khi trả tiền.
- **Order edit record**: lịch sử một lần sửa — nội dung cũ/mới, người sửa, thời điểm, phiên bản.
- **Hold**: hàng module 05 giữ cho đơn, chỉ tạo khi artist xác nhận.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Khách đặt được đơn ở trạng thái chờ artist; khi artist xác nhận, đơn chuyển sang chờ thanh toán; kiểm chứng bằng test tự động.
- **SC-002**: **Không** hàng nào bị giữ trước khi artist xác nhận; một đơn không đủ hàng bị từ chối và **không** giữ gì (không giữ nửa).
- **SC-003**: Khách sửa được đơn trước khi trả tiền; sửa đơn đang chờ thanh toán đưa đơn về chờ xác nhận và trả hàng **đúng một lần**.
- **SC-004**: Đơn chờ thanh toán quá hạn tự huỷ và trả hàng **đúng một lần**; đơn chờ artist **không** hết hạn theo thời gian.
- **SC-005**: Mọi bước chuyển sai bị từ chối nêu trạng thái hiện tại; hai đơn tranh đơn vị cuối **không** oversell.
- **SC-006**: Artist được báo khi cần xác nhận, khách được báo mỗi lần đổi trạng thái; lỗi gửi thông báo **không** làm hỏng đơn.
- **SC-007**: Chỉ chủ đơn sửa/huỷ được đơn mình; chỉ operator xác nhận/từ chối/đi tiếp; mỗi thao tác operator có audit và mỗi lần sửa có lịch sử.

## Assumptions

- **Artist = operator của shop** (admin). Shop là **một nghệ sĩ** (`docs/product/project_overview.md`); không thêm role mới; email artist là địa chỉ shop đã cấu hình (`ADMIN_EMAIL`).
- **Nhà cung cấp thanh toán (module 08) điều khiển bước "đã trả tiền"** — feature này định nghĩa và test bước đó cùng dữ liệu phiên bản/hạn để 08 nối vào, nhưng chưa có bước xác nhận thanh toán thật.
- **Hạn thanh toán 60 phút** tính từ lúc giữ hàng thành công (theo `docs/modules/ke-hoach-don-hang.md`); đơn chờ artist không có hạn theo thời gian.
- **Không giữ hàng khi đặt** — chỉ giữ khi artist xác nhận (quyết định thiết kế của kế hoạch).
- **Đơn đã trả tiền không sửa/huỷ** — chuyển nhượng thay thế (như 009).
- **Tiền là số nguyên đơn vị nhỏ nhất + tiền tệ**, đơn chỉ gồm tiền hàng, không phí ship (ADR 015 §5).
- **Thông báo chỉ qua email**; in-app thuộc module 12.
- **Cơ sở hạ tầng nền dùng lại**: guard phiên/vai trò, audit, envelope, phân trang, lịch sử đã có.
