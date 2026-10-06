# Feature Specification: Fix Verification Defects

**Feature Branch**: `004-fix-pending-defects`

**Created**: 2026-10-06

**Status**: Draft

**Input**: User description: "fix các lỗi hiện tại"

## Clarifications

### Session 2026-10-06

Context: module 02 User (`specs/003-user-profile`) was delivered, then verified against
real third-party services. That verification surfaced four unresolved items. All are now
settled; no question is left open.

- Q: Avatar quá lớn hiện trả mã lỗi chung "request body quá lớn" khi client khai báo độ
  dài, nhưng lại trả mã riêng "ảnh quá lớn" khi không khai báo → A: **Sửa code**, không sửa
  tài liệu. Lý do: tài liệu API và hợp đồng là bản hợp đồng (Constitution VIII); một tài liệu
  nói dối client thì phải sửa tài liệu, nhưng ở đây tài liệu đã đúng và client cần phân
  biệt "ảnh của bạn quá to" với "request của bạn quá lớn" để biết sửa cái nào. Cùng lý do đó,
  không hạ trần 2 MB.
- Q: Một cấu hình sai làm endpoint upload avatar chết âm thầm, thay vì báo ngay khi khởi
  động → A: **Từ chối khởi động**, nhưng **chỉ khi media đã được cấu hình**. Lý do: khi
  chưa có media thì avatar vốn đã không dùng được, nên trần thấp vô hại; còn khi media đã
  cấu hình mà trần thấp thì mọi upload đều hỏng theo cách khó hiểu. Cấu hình sai phải
  nổ lên lúc khởi động chứ không nên chờ đến request đầu tiên.
- Q: Trần body dùng chung đặt dưới trần riêng của route avatar → giữ nguyên cơ chế "trần
  dùng chung là chặn thô, trần riêng mới là luật nghiệp vụ"; trần dùng chung mặc định
  gấp đôi trần avatar. Đây là quyết định đã ban hành ở `ADR-008`, giữ nguyên.
- Q: Khi không gửi được email xác minh, tài khoản đã tạo thì giữ hay xoá, và nói gì với
  khách → A: **Trả `503`, giữ tài khoản, và nói rõ trong chính câu trả lời rằng mã chưa
  được gửi và khách có thể yêu cầu mã mới**. Lý do: đây là lựa chọn duy nhất vừa thành thật
  với khách, vừa không mất dữ liệu. Trả `202` chung như cũ sẽ giữ được hình thức "đăng ký
  không bao giờ bị chặn", nhưng đó chính là nguyên nhân khiến khách kẹt mà không hề hay biết;
  còn tạo tài khoản sau khi gửi mail thì buộc mỗi lần đăng ký phải chờ hạ tầng và một lần
  timeout của nhà cung cấp sẽ làm hỏng một đăng ký hợp lệ.

### Session 2026-10-06 (clarify)

Run after `/speckit.specify` to close the gaps that only implementation planning would
expose. Decisions are kept separate from the session above so their provenance stays visible.

- Q: Nhà cung cấp mail từ chối gửi thì thử lại, hay thất bại là báo ngay → A: **Thử lại tự
  động có giới hạn, hết cách mới trả `503`**. Lý do: lỗi tạm thời phổ biến hơn lỗi cấu hình,
  nên thử lại giúp phần lớn lượt đăng ký không bị từ chối vô ích. Giới hạn số lần và tổng thời
  gian để một sự cố thật không treo request. **Lỗi đã phân loại là cấu hình thì không thử
  lại**, vì thử lại không thể đổi kết quả.

- Q: Mã xác minh được sinh khi nào, và mã đã sinh còn dùng được sau khi gửi thất bại không →
  A: **Giữ nguyên luật hiện tại: mã vẫn được sinh và cất như cũ, không xoá khi gửi hỏng.**
  Khách yêu cầu mã mới sẽ được sinh mã mới. Lý do: thay đổi vòng đời mã là thay đổi rủi ro bảo
  mật ở một feature vốn chỉ sửa lỗi, và không cần thiết cho câu chuyện này. Điều kiện giữ nguyên
  là **chỉ mã mới nhất được chấp nhận**: một mã bị thay thế không được dùng để xác minh, và mã
  sinh cho lần gửi hỏng không được tồn tại lâu hơn thời hạn hiện tại.

- Q: Lần đăng ký mà không gửi được mã thì ghi lại ở đâu → A: **Ghi cả hai: một dòng nhật ký
  kiểm toán cho sự kiện, và một dòng log có phân loại cho operator**. Lý do: Constitution VI yêu
  cầu mọi thay đổi quản trị phải được ghi kiểm toán, và việc tài khoản được tạo là một thay đổi
  — hiện tại nó **không** để lại dòng audit nào, dù tài khoản vẫn tồn tại. Dòng log bổ sung
  là để operator chẩn đoán được mà không phải truy vấn cơ sở dữ liệu.
- Q: Khách bấm "gửi lại mã" ngay sau khi nhận `503` thì sao → A: **Lần gửi thất bại không tính
  vào khoảng nghỉ; hệ thống phải bỏ dấu hiệu nghỉ khi gặp lỗi gửi.** Lý do: hiện dấu hiệu nghỉ
  được đặt *trước* khi gửi, nên nếu giữ nguyên thì lời hứa "yêu cầu mã mới" của FR-006 sai ngay
  trong 60 giây đầu — đúng khoảng thời gian khách cần nhất. Bỏ dấu hiệu nghỉ vẫn an toàn vì
  giới hạn theo luồng vẫn còn, nên không mở đường lạm dụng.
## Problem

Verifying the delivered user module against real providers produced three failures that a
customer or an operator would experience, and none of them is visible from reading the
documentation alone:

1. A customer who uploads an image that is too large receives the wrong reason. The
   documented answer is "this image is too large"; the delivered answer is the generic
   "request body is too large". It depends on whether the client happened to declare its
   request length, so the same mistake produces two different answers.

2. A customer whose verification email cannot be delivered is told the request failed with
   a server error, and is left holding an account they cannot complete: the account exists
   but no code ever arrived, and retrying registration does not help.

3. An operator who lowers the shared request-size ceiling below what avatars need does not
   learn about it. The service starts happily and every avatar upload then fails with a
   message that does not point at the setting.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A customer learns why their photo was refused (Priority: P1)

A customer picks a photo that is too large and uploads it. The system refuses it with the
reason that actually applies — the image is too large — and tells them which input to
change. The refusal does not alter the profile they already had, and the photo they
uploaded before is still there.

**Why this priority**: This is the most common upload failure and the only one where the
customer currently cannot tell what to do, because the answer they get names a limit they
are not hitting.

**Independent Test**: Upload an oversized image three ways — declaring its length, omitting
it, and understating it — and confirm all three answer the same documented reason and leave
the stored profile untouched. Delivers value on its own.

**Acceptance Scenarios**:

1. **Given** a customer with an existing avatar, **When** they upload an image larger than
   the documented ceiling and declare the request length, **Then** the system refuses with
   the avatar-specific too-large code naming the file, and the stored avatar is unchanged.
2. **Given** the same customer, **When** they upload the same oversized image without
   declaring its length, **Then** the refusal is identical to scenario 1.
3. **Given** an image exactly at the ceiling, **When** it is uploaded, **Then** it is
   accepted.
4. **Given** a request that is not an avatar upload and is larger than the shared ceiling,
   **When** it is sent, **Then** the generic request-too-large answer is unchanged.

---

### User Story 2 - A customer can finish registering even when mail is down (Priority: P1)

A customer registers while the mail provider is refusing messages. The system tells them
that the request could not be completed because the message could not be sent — not that
they did anything wrong — and it tells them what to do next. Their account is kept, and once
the problem clears, requesting a new verification message completes the registration without
any help from anyone.

**Why this priority**: Today this path leaves a customer permanently stuck. They hold an
account, received a server error, and retrying the obvious action changes nothing.

**Independent Test**: Make message delivery fail, register, then confirm the failure is
reported as a service problem with a stated next step, then recover delivery and confirm
requesting a new verification message completes the registration without operator action.
Delivers value on its own.

**Acceptance Scenarios**:

1. **Given** the mail provider refuses the verification message, **When** a customer
   registers, **Then** the system answers `503`, states that no code was sent and that a new
   one can be requested, and reveals nothing about the provider.
2. **Given** the same situation, **When** the customer asks for a new verification message
   and delivery has recovered, **Then** registration completes and the customer can verify.
3. **Given** a failed delivery followed by the customer retrying registration, **When** they
   do, **Then** they reach the same recovery path, and no second account is created.
4. **Given** delivery succeeded, **When** a customer registers, **Then** the system answers
   the same generic accepted answer it always did, so the endpoint still cannot be used to
   discover whether an email is registered.

---

### User Story 3 - An operator finds a misconfiguration before a customer does (Priority: P2)

An operator deploys the service with the shared request-size ceiling set too low for
avatars, or with provider credentials that are wrong or not yet authorised. The service
either refuses to start with a message naming both values, or starts and logs a
classification that distinguishes "my configuration is wrong" from "the provider is having
a bad day" — without ever writing a credential into a log.

**Why this priority**: Every silent misconfiguration becomes a customer-visible outage that
looks like a bug. It is P2 because the guard only matters once the provider is configured,
and because nothing here is customer-facing while it works.

**Independent Test**: Start the service with a ceiling below the avatar route's own ceiling
and with provider credentials present, and confirm the refusal names both values; then
start it with no credentials and confirm it starts and logs the consequence. Confirm no
credential appears in any log line.

**Acceptance Scenarios**:

1. **Given** media upload is configured and the shared ceiling is below the avatar route's
   own ceiling, **When** the service starts, **Then** it refuses to start and the message
   names the setting and both values.
2. **Given** media upload is configured and the shared ceiling equals the avatar route's own
   ceiling, **When** the service starts, **Then** it starts.
3. **Given** media credentials are absent, **When** the service starts, **Then** it starts,
   logs the consequence once, and logs no credential value.
4. **Given** a provider refuses a request because of configuration rather than outage,
   **When** the operator reads the logs, **Then** the classification distinguishes the two,
   and the log contains no provider response body, no credential and no uploaded content.

### Edge Cases

- **An image sits exactly at the ceiling.** It is accepted. Only one byte more is refused,
  and the refusal names the same reason regardless of size.
- **A client declares a length smaller than it actually sends.** The ceiling is still
  enforced, because the declared length is never trusted. This is the case that must not
  slip through: a client could otherwise lie its way past the check.
- **A client declares no length at all.** The ceiling is still enforced. This case already
  behaves correctly today and must keep behaving correctly after the fix.
- **A body that is not an upload is sent to the avatar endpoint.** It is refused as a
  malformed request naming the missing input, and the stored profile is untouched.
- **The mail provider refuses for two different reasons — the sender is not authorised, or
  the sending address is not authorised.** The customer sees the same `503` either way, so
  nothing about the account is revealed; the recorded classification must differ, so an
  operator can tell a setup problem from an outage without reading provider documentation.
- **The mail provider is briefly unavailable.** The system retries within its budget and the
  customer is none the wiser: no `503`, no second account, and a single verification message.
- **The mail provider is down for a long time.** The retry budget runs out, `503` is
  answered, and the response still arrives inside the documented response target rather than
  hanging. The customer can ask again and no further damage accumulates.
- **A customer asks for a new code after a failed delivery.** A fresh code is issued, and the
  code issued for the failed attempt stops being accepted immediately. The customer who never
  received either code holds nothing usable, and no code outlives its existing lifetime.
- **A customer asks for a new code in the same second they received the `503`.** It succeeds.
  No cooldown error stands between the customer and the message they were told to request, and
  the flow-level limit is the only thing still in the way.
- **A customer never returns to request a new code.** The account stays pending and holds no
  avatar or profile data. Reclaiming abandoned accounts is out of scope for this feature.
- **A customer requests a new code repeatedly.** The existing cooldown and rate limit keep
  applying; this feature adds no new budget.
- **Media is configured and the provider is genuinely down.** The upload is refused with the
  retryable media code, the profile is unchanged, and the operator sees an outage
  classification rather than a configuration one.
- **The shared ceiling equals the avatar route's own ceiling exactly.** The service starts;
  only a ceiling strictly lower is refused.
- **The shared ceiling is too low but media is not configured.** The service starts and
  behaves exactly as it does today, because avatar upload is already unavailable and the
  ceiling cannot be the reason anything else fails.

## Requirements *(mandatory)*

### Functional Requirements — avatar size refusal

- **FR-001**: System MUST answer an avatar upload that exceeds the documented size ceiling
  with the module's documented avatar-too-large code, regardless of whether the request
  declared its length.
- **FR-002**: System MUST answer the same code whether the ceiling was reached while
  reading the upload or by comparing the size received.
- **FR-003**: System MUST preserve the existing generic request-too-large answer for
  requests that are not avatar uploads.
- **FR-004**: The avatar ceiling itself MUST NOT change; this feature corrects the reason
  reported, not the limit enforced.
- **FR-005**: A refused upload MUST leave the stored profile and any previously stored
  avatar exactly as they were.

### Functional Requirements — registration when delivery fails

- **FR-006**: When the mail provider refuses to deliver the verification message, System
  MUST answer `503` — a status identifying the failure as a service problem rather than a
  client mistake — and MUST tell the customer, in that response, that no code was sent and
  that they can request a new one.
- **FR-007**: System MUST keep the account created before the failed delivery, in a state
  from which requesting a new verification message succeeds once delivery recovers. The
  account MUST NOT be deleted, and MUST NOT be left in a state that blocks a new request.
- **FR-008**: System MUST NOT let a repeated failed delivery create a second account for
  the same address, and a customer who retries registration after a failure MUST reach the
  same recovery path rather than an "already registered" dead end.
- **FR-009**: System MUST NOT include provider detail, credentials, or recipient addresses
  in the client response.
- **FR-010**: System MUST record the failed delivery attempt for diagnosis, with no secret
  in the record.
- **FR-011**: A registration whose account was created but whose verification message could
  not be delivered MUST leave **both** traces: an audit entry with a failed outcome that
  identifies the account it created, and one classified log line an operator can act on
  without querying stored data.
- **FR-012**: Neither trace may contain the verification code, a credential, or the
  provider's own wording. The account is identified by its reference, not by restating the
  recipient address in free-text metadata.
- **FR-013**: A successful registration MUST leave the same audit trace it leaves today, so
  this feature changes only the failed path's traceability, never the successful one's.
- **FR-014**: System MUST keep the documented generic accepted answer when delivery
  succeeds, so registration still cannot be used to discover whether an email exists.
- **FR-015**: When a delivery failure is classified as transient, System MUST retry a bounded
  number of times before answering `503`, and the total time spent retrying MUST NOT push the
  response past the documented response target, so the retry budget and the wait between
  attempts are sized to fit that target rather than chosen independently of it.
- **FR-016**: When a delivery failure is classified as a configuration problem, System MUST
  NOT retry, because retrying cannot change the outcome, and MUST surface the problem to the
  operator through the recorded diagnostic instead.
- **FR-017**: System MUST classify a delivery failure as transient or as configuration before
  deciding to retry, and the classification MUST NOT depend on the provider's own wording
  changing between releases.
- **FR-018**: A failed delivery MUST NOT change how a verification code is created, stored or
  expired. The code issued for a failed attempt stays subject to the existing lifetime, and
  System MUST accept **only the most recently issued code** — a code that a newer request
  replaced can never complete verification.
- **FR-024**: A failed delivery MUST NOT consume the resend cooldown. The cooldown is armed
  before delivery is attempted, so a failure MUST leave the customer free to request a new code
  immediately — otherwise the promise made in FR-006 is false for exactly as long as the
  cooldown, which is the moment the customer most needs it.
- **FR-025**: The flow-level rate limit MUST still apply to a customer who retries
  immediately after a failed delivery, so disarming the cooldown does not open an unbounded
  sending path.

### Functional Requirements — configuration and diagnostics

- **FR-019**: System MUST refuse to start when the shared request-size ceiling is below the
  ceiling an avatar upload needs **and** media upload is configured.
- **FR-020**: That refusal message MUST name the setting and both values.
- **FR-021**: System MUST still start when media credentials are absent, and MUST log the
  consequence once at startup without any credential value.
- **FR-022**: When a provider refuses a request, System MUST log a classification that lets
  an operator tell a configuration failure from a provider outage, and MUST NOT log the
  provider's response body, any credential, or the uploaded content.
- **FR-023**: System MUST ship a documented procedure for verifying avatar upload and
  verification-mail delivery against the real providers, naming the values an operator has
  to obtain and where each is obtained.

### Key Entities

- **Avatar reference**: the stored pointer to a customer's photo — the opaque identifier,
  the link, and the dimensions. All four travel together; a partially populated reference
  is invalid.
- **Account verification state**: whether an account exists, whether its address is
  confirmed, and whether a verification message has been requested and delivered. The
  recovery path in US2 operates entirely on this.
- **Provider diagnostic**: what the system records when a third party refuses a request —
  enough to classify the failure, containing nothing sensitive.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Every oversized avatar upload answers the documented avatar-too-large reason
  across all three request shapes a client may send, and none of them answers the generic
  reason; verified by automated test, not by inspection.
- **SC-002**: A customer whose verification message cannot be delivered completes
  registration on their own, without contacting support, in at most one additional attempt
  after delivery recovers.
- **SC-003**: An operator who misconfigures the shared request-size ceiling learns about it
  at startup from the message alone, without reading source or documentation.
- **SC-004**: No credential value for media or mail appears in any log line, test output, or
  stored diagnostic; verified by scanning all of them after exercising both failure paths.
- **SC-005**: The documented verification procedure can be executed end to end by an
  operator who has not seen the system before, and every step records its expected outcome.
- **SC-006**: Customers uploading an image at exactly the ceiling, and customers who are
  refused, are both answered within two seconds of finishing their request.

## Assumptions

- The avatar size ceiling (2 MB) and the stored maximum width (512 px) are unchanged from
  `specs/003-user-profile`; this feature corrects the reason reported, not the limit.
- The cloud account name and the mail provider's IP authorisation are configuration the
  operator owns. This feature does not automate obtaining them, but it must make a wrong or
  missing value diagnosable and documented.
- No new third-party service and no new runtime dependency is introduced.
- The provider boundary stays abstract: no provider-specific type reaches the business
  logic, so a different media or mail vendor does not require changing it.
- Existing verification codes already issued stay valid; this feature changes only what
  happens when delivery fails, never the success path.
