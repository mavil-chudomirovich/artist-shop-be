# Feature Specification: Address Facebook URL

**Feature Branch**: `021-address-facebook-url`

**Created**: 2026-10-19

**Status**: Draft

**Input**: User description: "Mình muốn điều chỉnh lại thông tin giao hàng của người dùng — Thêm facebook url vào (require) — lúc thêm địa chỉ phải bắt buộc có link face"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Thêm địa chỉ mới bắt buộc có link Facebook (Priority: P1)

Khách hàng đã đăng nhập mở sổ địa chỉ giao hàng và thêm một địa chỉ mới. Ngoài họ tên người nhận, số điện thoại, tỉnh/xã và địa chỉ chi tiết như hiện tại, khách phải nhập thêm link Facebook để shop có thể liên hệ xác nhận đơn.

**Why this priority**: Đây là yêu cầu cốt lõi của chủ shop: mọi địa chỉ giao hàng mới đều phải có kênh liên hệ Facebook, phục vụ xác minh đơn và chăm sóc khách. Không có story này thì yêu cầu coi như chưa đạt.

**Independent Test**: Có thể kiểm thử độc lập bằng cách gọi tạo địa chỉ có và không có link Facebook, rồi đọc lại danh sách địa chỉ để xác nhận link được lưu và hiển thị.

**Acceptance Scenarios**:

1. **Given** khách đã đăng nhập và nhập đầy đủ thông tin địa chỉ kèm link Facebook hợp lệ (ví dụ `https://facebook.com/nguyen.van.a`), **When** gửi yêu cầu tạo địa chỉ, **Then** địa chỉ được lưu, trả về `201` và hiển thị đầy đủ các trường cũ cộng thêm `facebookUrl`.
2. **Given** khách bỏ trống link Facebook hoặc chỉ nhập khoảng trắng, **When** gửi yêu cầu tạo địa chỉ, **Then** yêu cầu bị từ chối với `400 VALIDATION_ERROR`, chi tiết chỉ rõ trường `facebookUrl` là bắt buộc, và không có địa chỉ nào được lưu.
3. **Given** khách nhập link không phải Facebook (ví dụ `https://example.com/nguyen.van.a`) hoặc không phải địa chỉ web hợp lệ (ví dụ `khong-phai-link`), **When** gửi yêu cầu tạo địa chỉ, **Then** yêu cầu bị từ chối với `400 VALIDATION_ERROR` nêu trường `facebookUrl`, và không có địa chỉ nào được lưu.

---

### User Story 2 - Sửa địa chỉ giữ quy tắc link Facebook (Priority: P2)

Khách sửa một địa chỉ đã lưu. Quy tắc link Facebook vẫn áp dụng: không được xóa link về rỗng, và nếu đổi link thì link mới cũng phải hợp lệ. Danh sách địa chỉ của khách và màn hình admin tra cứu khách hàng đều hiển thị link Facebook.

**Why this priority**: Nếu chỉ chặn lúc tạo mà cho phép xóa lúc sửa thì quy tắc "bắt buộc" bị lách. Story này khóa lỗ hổng đó và đảm bảo admin nhìn thấy cùng thông tin liên hệ với khách.

**Independent Test**: Có thể kiểm thử độc lập bằng cách sửa `facebookUrl` của một địa chỉ có sẵn (đổi sang link hợp lệ khác, thử xóa về rỗng, thử link sai), rồi đọc lại địa chỉ và màn hình tra cứu của admin.

**Acceptance Scenarios**:

1. **Given** một địa chỉ đã có link Facebook, **When** khách sửa sang một link Facebook hợp lệ khác, **Then** địa chỉ được cập nhật, trả về `200` với `facebookUrl` mới.
2. **Given** một địa chỉ đã có link Facebook, **When** khách sửa với `facebookUrl` là chuỗi rỗng, **Then** yêu cầu bị từ chối với `400 VALIDATION_ERROR` nêu trường `facebookUrl`, địa chỉ cũ (kể cả cờ mặc định) giữ nguyên.
3. **Given** khách không gửi trường `facebookUrl` khi sửa một địa chỉ thường (không mang giá trị giữ chỗ), **When** yêu cầu được xử lý, **Then** link cũ được giữ nguyên và các trường khác vẫn cập nhật bình thường (ngoại lệ: địa chỉ đang mang giữ chỗ `https://facebook.com/unknown` thì mọi sửa đều bị từ chối theo FR-004).
4. **Given** địa chỉ đã có link Facebook, **When** khách mở danh sách địa chỉ của mình và admin mở tra cứu khách hàng, **Then** cả hai màn hình đều hiển thị `facebookUrl` của địa chỉ đó.

---

### User Story 3 - Đơn hàng mới chụp lại link Facebook lúc chốt đơn (Priority: P3)

Khi khách chốt đơn từ một địa chỉ đã lưu, đơn hàng ghi nhận lại toàn bộ thông tin giao hàng tại thời điểm chốt đơn, bao gồm cả link Facebook. Về sau khách có sửa địa chỉ thì đơn đã đặt không thay đổi.

**Why this priority**: Shop cần biết lúc đặt đơn khách đã dùng link nào để liên hệ/giao hàng, kể cả khi khách đổi địa chỉ sau đó. Đây là ảnh chụp lịch sử, không phải liên kết động.

**Independent Test**: Có thể kiểm thử độc lập bằng cách chốt đơn từ một địa chỉ có link Facebook, đọc chi tiết đơn, sau đó sửa link ở địa chỉ gốc và đọc lại chi tiết đơn để xác nhận đơn giữ link cũ.

**Acceptance Scenarios**:

1. **Given** khách chốt đơn từ địa chỉ đã có link Facebook, **When** đơn được tạo, **Then** chi tiết đơn hiển thị đầy đủ thông tin giao hàng cũ cộng thêm `facebookUrl` đã chụp tại lúc chốt.
2. **Given** một đơn đã chốt, **When** khách sửa `facebookUrl` ở địa chỉ gốc, **Then** đơn cũ vẫn giữ `facebookUrl` lúc chốt, không bị đổi theo.
3. **Given** khách sửa đơn và đổi sang một địa chỉ khác đã có link Facebook hợp lệ, **When** yêu cầu sửa được chấp nhận, **Then** đơn mang `facebookUrl` của địa chỉ mới; **Given** địa chỉ mới mang giá trị giữ chỗ `https://facebook.com/unknown`, **When** gửi yêu cầu sửa đơn, **Then** bị từ chối với `400 VALIDATION_ERROR` nêu trường `facebookUrl` và đơn giữ nguyên.

---

### Edge Cases

- Link có khoảng trắng đầu/cuối: hệ thống tự loại bỏ khoảng trắng trước khi kiểm tra và lưu; link chỉ gồm khoảng trắng bị coi là trống và bị từ chối.
- Link quá dài (vượt trần 500 ký tự): bị từ chối với lỗi nêu trường `facebookUrl`, địa chỉ giữ nguyên.
- Link thiếu scheme (`facebook.com/nguyen.van.a`), sai scheme (`http://...`, `ftp://...`), host gần giống (`faceboook.com`, `myfacebook.com`, `facebook.com.evil.com`): đều bị từ chối.
- Link dùng miền rút gọn của Facebook (`fb.com/...`), subdomain (`www.`/`m.`...), chữ hoa/thường ở host hoặc có port tường minh (`https://facebook.com:8443/...`): được chấp nhận như link `facebook.com`, miễn path có nội dung.
- Giá trị giữ chỗ `https://facebook.com/unknown` chỉ tồn tại ở dữ liệu cũ sau chuyển đổi; mọi yêu cầu tạo mới/sửa — kể cả sửa trường khác mà vắng mặt `facebookUrl` trên địa chỉ đang mang giữ chỗ — MUST bị từ chối với lỗi nêu trường `facebookUrl`, buộc khách nhập link thật.
- Link host trần không path (`https://facebook.com`, `https://facebook.com/`): bị từ chối vì không xác định được tài khoản liên hệ.
- Địa chỉ cũ tạo trước khi có quy tắc (chưa có link): vẫn đọc/hiển thị được; khi sửa địa chỉ đó, quy tắc mới áp dụng (không cho lưu rỗng, link mới phải hợp lệ).
- Đơn hàng cũ chốt trước khi có quy tắc: chi tiết đơn vẫn đọc được; trường link ở đơn cũ hiển thị giá trị đã được chuyển đổi khi nâng cấp (xem Assumptions).
- Tạo địa chỉ thất bại vì link sai: không lưu một nửa địa chỉ; thử lại với link đúng thì thành công và không tạo trùng.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Khi tạo địa chỉ giao hàng mới, hệ thống MUST yêu cầu trường link Facebook (`facebookUrl`): trống hoặc chỉ khoảng trắng thì từ chối với lỗi nêu đúng trường này và không lưu địa chỉ.
- **FR-002**: Hệ thống MUST chỉ chấp nhận link Facebook là địa chỉ web hợp lệ với scheme `https` và host thuộc Facebook (đúng `facebook.com`/`fb.com` hoặc bất kỳ subdomain nào của chúng như `www.`/`m.`, không phân biệt hoa/thường, bỏ qua port nếu có); path phải có nội dung (không được rỗng hay chỉ `/`); giá trị giữ chỗ `https://facebook.com/unknown` MUST bị từ chối như link không hợp lệ (không bao giờ được tạo mới qua API); mọi giá trị khác (kể cả `http://...`) MUST bị từ chối với lỗi nêu trường `facebookUrl`.
- **FR-003**: Hệ thống MUST giới hạn `facebookUrl` tối đa 500 ký tự (đếm theo ký tự, trên giá trị đã loại bỏ khoảng trắng đầu/cuối); vượt trần thì từ chối với lỗi nêu trường `facebookUrl`.
- **FR-004**: Khi sửa địa chỉ, hệ thống MUST NOT cho phép xóa `facebookUrl` về rỗng; trường vắng mặt thì giữ giá trị cũ — ngoại lệ duy nhất: nếu giá trị đang lưu là giữ chỗ `https://facebook.com/unknown` thì MỌI yêu cầu sửa (kể cả vắng mặt `facebookUrl`) MUST bị từ chối cho tới khi khách gửi link thật; trường có giá trị mới thì MUST kiểm tra lại theo FR-002 và FR-003; vi phạm thì từ chối và giữ nguyên toàn bộ địa chỉ (kể cả cờ mặc định).
- **FR-005**: Danh sách/chi tiết địa chỉ của khách và màn hình tra cứu khách hàng của admin MUST hiển thị `facebookUrl` của từng địa chỉ (địa chỉ nào cũng có trường này sau khi nâng cấp).
- **FR-006**: Khi chốt đơn (và khi sửa đơn có đổi địa chỉ giao), hệ thống MUST chụp `facebookUrl` từ địa chỉ đã chọn vào đơn hàng; đơn đã tạo MUST giữ nguyên ảnh chụp này dù địa chỉ gốc bị sửa sau đó.
- **FR-009**: Khi chốt đơn (và khi sửa đơn có đổi địa chỉ giao), nếu địa chỉ được chọn đang mang giá trị giữ chỗ `https://facebook.com/unknown`, hệ thống MUST từ chối với `400 VALIDATION_ERROR` nêu trường `facebookUrl` và yêu cầu khách cập nhật địa chỉ trước khi đặt đơn.
- **FR-007**: Mọi từ chối do `facebookUrl` MUST trả về lỗi chuẩn `400 VALIDATION_ERROR` với chi tiết nêu `field = "facebookUrl"`; không được lưu một nửa địa chỉ khi bị từ chối.
- **FR-008**: Ràng buộc "mọi địa chỉ đều có link" MUST có đối ứng ở tầng lưu trữ (cột bắt buộc, không cho phép rỗng), không chỉ kiểm tra ở tầng ứng dụng; địa chỉ cũ được chuyển đổi một lần khi nâng cấp để thỏa ràng buộc này.

### Key Entities

- **Shipping Address (Địa chỉ giao hàng)**: Thông tin giao hàng của một tài khoản — người nhận, số điện thoại, tỉnh/xã (mã + tên chụp tại lúc lưu), địa chỉ chi tiết, cờ mặc định — nay có thêm **link Facebook bắt buộc** dùng để liên hệ/xác nhận đơn. Mỗi tài khoản có nhiều địa chỉ, tối đa một địa chỉ mặc định còn hiển thị.
- **Order Delivery Snapshot (Ảnh giao hàng của đơn)**: Bản chụp thông tin giao hàng tại lúc chốt đơn (người nhận, điện thoại, tỉnh/xã, địa chỉ chi tiết + link Facebook), gắn chặt với đơn và bất biến khi địa chỉ gốc thay đổi.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% địa chỉ mới được tạo thành công đều có link Facebook hợp lệ; mọi yêu cầu thiếu link hoặc sai link đều bị từ chối kèm trường lỗi đúng trong vòng một lần thử.
- **SC-002**: 100% lần sửa địa chỉ thử xóa link về rỗng đều bị từ chối và địa chỉ giữ nguyên; 100% lần sửa hợp lệ cập nhật đúng link mới.
- **SC-003**: Khách nhập đúng link lần đầu hoàn thành việc thêm địa chỉ mà không cần hỗ trợ (mục tiêu định tính, kiểm chứng qua luồng thêm địa chỉ mới trong `quickstart.md`; không phải cổng đo lường chặn merge).
- **SC-004**: 100% đơn hàng chốt mới từ địa chỉ có link hiển thị đúng link đã chụp trong chi tiết đơn, và link đó không đổi sau khi địa chỉ gốc bị sửa.

## Clarifications

### Session 2026-10-19

- Q: Địa chỉ/đơn cũ chưa có link face thì backfill giá trị gì để thỏa cột NOT NULL? → A: Giữ chỗ unknown (backfill `https://facebook.com/unknown`; hiển thị nguyên văn ở dữ liệu cũ).
- Q: Khi chốt đơn / sửa đơn mà địa chỉ được chọn đang mang giá trị giữ chỗ unknown thì xử lý thế nào? → A: Chặn, bắt cập nhật (trả `400 VALIDATION_ERROR` trường `facebookUrl`, yêu cầu khách sửa địa chỉ trước khi đặt đơn).
- Q: Link Facebook chấp nhận scheme nào? → A: Chỉ https (chỉ chấp nhận `https://facebook.com` hoặc `https://fb.com`; `http` bị từ chối).

## Assumptions

- Trường API đặt tên là `facebookUrl` (theo quy ước camelCase của các trường địa chỉ hiện tại); cột lưu trữ là `facebook_url`.
- Trần độ dài 500 ký tự và chỉ chấp nhận `https` (không chấp nhận `http`); chỉ so sánh host khi kiểm tra, phần đường dẫn sau host giữ nguyên như khách nhập (đã trim).
- Địa chỉ và đơn hàng cũ (tạo trước quy tắc) được chuyển đổi một lần khi nâng cấp với giá trị giữ chỗ `https://facebook.com/unknown` để thỏa cột bắt buộc; đơn cũ hiển thị nguyên văn giá trị này. Đơn mới chốt/sửa đơn dùng địa chỉ giữ chỗ thì bị chặn theo FR-009.
- Tài liệu API tham chiếu (`docs/api-reference.md`), đặc tả OpenAPI của module user/order và tài liệu hướng dẫn frontend được cập nhật trong cùng thay đổi; đây là thay đổi cộng thêm trường khi đọc nhưng breaking ở chiều ghi (tạo địa chỉ thiếu trường sẽ nhận `400` — đúng thiết kế, nêu trong changelog).
- FR trong feature này đánh số không liên tục (FR-009 chèn sau FR-006); giữ nguyên số để `tasks.md` và `plan.md` tham chiếu ổn định, không đánh lại.
- Id của chủ tài khoản luôn lấy từ phiên đăng nhập, không từ body/query (nguyên tắc chống truy cập chéo hiện hành).
