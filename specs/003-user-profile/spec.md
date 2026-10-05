# Feature Specification: Customer Profile & Shipping Addresses

**Feature Branch**: `003-user-profile`

**Created**: 2026-10-05

**Status**: Draft

**Input**: User description: "làm module tiếp theo" → module 02 User (`docs/modules/02-user.md`): hồ sơ khách hàng và địa chỉ giao hàng, làm dữ liệu nền cho đơn hàng và commission.

## Clarifications

### Session 2026-10-05

- Q: Cỡ ảnh avatar tối đa và định dạng nào? → A: Tối đa 2 MB; chỉ nhận JPEG, PNG, WebP; hệ thống tự thu nhỏ về bề rộng 512 px trước khi lưu.
- Q: Avatar upload trực tiếp lên dịch vụ media hay qua máy chủ? → A: Qua máy chủ, để máy chủ kiểm tra loại/kích thước và giữ bí mật không ghi ra ngoài.
- Q: Có hạn mức riêng cho endpoint ghi của module không? → A: Có. Avatar upload và các
  thao tác ghi phải có rate limit riêng, không chỉ dựa vào hạn mức toàn cục, để một
  tài khoản không thể chiếm dung lượng media.
- Q: Cấu trúc địa chỉ và nguồn dữ liệu hành chính? → A: 2 cấp theo mô hình hành
  chính hiện hành (tỉnh/thành → phường/xã), **lấy từ kho dữ liệu hành chính Việt
  Nam** chứ không tự do nhập; người dùng chọn bằng **ô select dạng cascading**
  (chọn tỉnh → hiện danh sách phường/xã thuộc tỉnh đó). Cấp quận/huyện không dùng.
- Q: Người dùng có nhiều địa chỉ và chọn lúc đặt hàng? → A: Có, một tài khoản có
  nhiều địa chỉ và người dùng chọn một trong số đó khi đặt hàng (như Shopee);
  địa chỉ mặc định được chọn sẵn nhưng không áp ép.
- Q: Phạm vi endpoint cho quản trị viên có nằm trong module này không? → A: Có, nằm
  trong module 02: endpoint **chỉ đọc**, chỉ dành cho role ADMIN, xem được thông tin
  liên hệ và danh sách địa chỉ của một khách hàng, **không** sửa được. Module 16
  Admin và các module order/commission sau này dùng lại hợp đồng này.
- Q: Xoá địa chỉ đang được đơn hàng tham chiếu? → A: Ẩn đi chứ không xoá vật lý, để lịch sử đơn không bị hỏng; đơn hàng lưu bản chụp địa chỉ tại thời điểm đặt.
- Q: Số điện thoại theo định dạng nào? → A: Số di động Việt Nam 10 chữ số bắt đầu bằng 0; tự bỏ khoảng trắng và dấu gạch.
- Q: Có xoá tài khoản trong module này không? → A: Không; vô hiệu hoá tài khoản thuộc module 01 auth và module 16 admin.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - View and update own profile (Priority: P1)

A signed-in customer opens their profile to check the information the shop holds
about them, corrects their display name or phone number, and adds or replaces a
profile photo. The details they enter appear on their orders and messages so the
shop can identify them.

**Why this priority**: The profile is the base record every later capability reads
(order, commission, chat, notification). Without a maintained profile, those
features show incomplete or wrong customer details.

**Independent Test**: Sign in, open the profile, change the display name and phone,
upload a photo, reload and confirm the new values are shown. Delivers a usable
customer profile on its own.

**Acceptance Scenarios**:

1. **Given** a signed-in customer, **When** they open their profile, **Then** they
   see their account identifier, email address, display name, phone number and
   avatar, and no other customer's data.
2. **Given** a signed-in customer whose display name is empty, **When** they save a
   display name and phone number, **Then** the profile is updated and the new
   values are returned on the next read.
3. **Given** a signed-in customer, **When** they submit a phone number that is not
   a valid mobile number, **Then** the change is rejected with a field-level
   message and the stored phone number is unchanged.
4. **Given** a signed-in customer, **When** they clear their display name or phone
   number, **Then** the field is stored as empty rather than keeping the old value,
   and the profile remains valid.
5. **Given** a visitor who is not signed in, **When** they request any profile
   operation, **Then** the request is refused and they are asked to sign in.

---

### User Story 2 - Manage shipping addresses (Priority: P2)

A signed-in customer saves the addresses their orders are delivered to, keeps
several of them side by side, edits one, removes one they no longer use, and marks
one as their default. When placing an order they pick one of their saved addresses,
with the default offered as the starting choice.

**Why this priority**: No order can be delivered without a usable address. It is
required before the order and payment modules, and it depends only on the profile.

**Independent Test**: Sign in, create two addresses, set one as default, edit and
remove the other, then confirm the remaining default address is still the one
marked.

**Acceptance Scenarios**:

1. **Given** a signed-in customer, **When** they add an address, **Then** they pick
   a province and a ward from the official Vietnamese administrative dataset using
   cascading selects — the ward list contains only wards belonging to the chosen
   province — and supply a recipient name, phone number and detailed street
   address, and the address is saved and listed for that customer.
2. **Given** a customer who has no address marked as default, **When** they add
   their first address, **Then** it automatically becomes their default address.
3. **Given** a customer with a default address, **When** they add another address,
   **Then** the new address is not marked as default and exactly one default
   address still exists.
4. **Given** a customer with a default address, **When** they mark another address
   as default, **Then** the previous default is cleared and the new one becomes the
   single default.
5. **Given** a customer, **When** they edit one of their addresses, **Then** only
   that address changes and the default flag is preserved.
6. **Given** a customer who has exactly one address, **When** they delete it,
   **Then** no default address remains, and adding a new address later makes it
   the default.
7. **Given** a customer, **When** they delete an address that past orders refer to,
   **Then** the address is hidden from their address list while those orders keep
   showing the address that was used.
8. **Given** a signed-in customer, **When** they try to read, edit or delete an
   address belonging to another customer, **Then** the request is refused and no
   information about that address is disclosed.
9. **Given** a customer saving addresses for a later purchase, **When** they place
   an order, **Then** they can choose any one of their saved addresses, and the
   default address is the one offered first.

---

### User Story 3 - Upload and remove a profile photo (Priority: P3)

A customer uploads a photo to be recognised in the shop, replaces it when it
outdates, or removes it to go back to the default placeholder.

**Why this priority**: The photo improves trust and recognition but nothing in the
order flow depends on it, so it follows the core profile and address work.

**Independent Test**: Upload a photo, confirm it is shown, replace it, then remove
it and confirm the placeholder returns.

**Acceptance Scenarios**:

1. **Given** a signed-in customer, **When** they upload a supported image up to the
   size limit, **Then** the photo is stored and shown on their profile, and the
   stored details include both an opaque identifier and a displayable link.
2. **Given** a customer who already has a photo, **When** they upload a new one,
   **Then** only the newest photo is shown and the previous one is no longer
   referenced by their profile.
3. **Given** a signed-in customer, **When** they upload a file that is not a
   supported image type, **Then** the upload is rejected and the current photo is
   left unchanged.
4. **Given** a signed-in customer, **When** they upload a file larger than the
   limit, **Then** the upload is rejected and the current photo is left unchanged.
5. **Given** a customer with a photo, **When** they remove it, **Then** the
   placeholder is shown again and the profile no longer references the old photo.

---

### User Story 4 - Look up a customer while handling an order or commission (Priority: P4)

A shop administrator handling an order or a commission needs the customer's
contact details and shipping address to confirm delivery, and must not be able to
change them.

**Why this priority**: It unblocks the order and commission modules, which both need
the customer's contact details and delivery address to fulfil work, and they are not
built yet — so it delivers no visible value on its own.

**Independent Test**: Sign in as an administrator, open a customer by identifier,
and confirm their contact details and addresses are visible while profile editing
is not offered.

**Acceptance Scenarios**:

1. **Given** a signed-in administrator, **When** they open a customer profile, they
   see the customer's contact details and address list, and any attempt to change
   them is refused.
2. **Given** a signed-in customer, **When** they request another customer's
   profile, **Then** the request is refused and the customer identifier's existence
   is not disclosed.

### Edge Cases

- A customer signs in for the first time: the profile has no display name, no phone
  number and no photo, and every profile screen must handle those empty values
  without error.
- A customer uploads an image with an unexpected internal format but a valid
  extension: the content is checked, not the file name.
- A customer adds a large number of addresses: the list is paginated rather than
  returned all at once.
- A customer updates the same field twice in quick succession: the later update
  wins, and the profile never keeps a value that was never submitted.
- A customer edits an address to a ward that does not belong to its province: the
  save is rejected with a field-level message and nothing changes.
- The administrative dataset changes after an address was saved: the saved address
  keeps working and remains readable, and the affected entry is reported so it can
  be remapped rather than silently dropped.
- A customer's default address is edited to become invalid: the address is
  rejected and the previous default stays in place.
- The media service is unavailable while uploading: the profile update fails
  clearly, and the customer's existing details are left untouched.
- Two administrators look up the same customer: neither changes anything, so both
  see identical information.
- A customer's account is disabled while they have addresses on file: their
  addresses are retained for order history and are no longer available for new
  orders.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST let a signed-in customer read their own profile,
  including account identifier, email address, display name, phone number and
  avatar status.
- **FR-002**: System MUST let a signed-in customer update their display name and
  phone number independently of each other.
- **FR-003**: System MUST validate that a phone number is a Vietnamese mobile
  number of 10 digits before accepting it, and MUST reject the whole update when
  validation fails.
- **FR-004**: System MUST allow clearing the display name and phone number so that
  an intentionally emptied field is stored as empty.
- **FR-005**: System MUST reject every profile and address request that carries an
  expired or invalid session, before touching any stored data.
- **FR-006**: System MUST restrict every profile and address operation to the
  account identified by the caller's session, so one customer can never read or
  change another customer's data.
- **FR-007**: System MUST allow a customer to create, read, update and delete
  multiple shipping addresses, each holding recipient name, recipient phone
  number, a province, a ward and a detailed street address.
- **FR-007a**: System MUST source provinces and wards from the official Vietnamese
  administrative dataset rather than accepting free text, and MUST reject a
  province or ward that is not in the dataset.
- **FR-007b**: System MUST reject an address whose ward does not belong to its
  chosen province, and MUST name the mismatched field in the validation result.
- **FR-007c**: System MUST support retrieving the list of provinces and the wards
  of a given province, so a client can populate cascading selects without having
  to know the dataset in advance.
- **FR-007d**: System MUST offer a customer all of their addresses when they start
  an order, with the default address offered first, and MUST let them choose any of
  them.
- **FR-008**: System MUST keep at most one default address per account at all
  times, including immediately after any create, update or delete.
- **FR-009**: System MUST automatically mark an account's first address as the
  default when the account has no default yet.
- **FR-010**: System MUST clear the previous default when a customer marks another
  address as default, as a single indivisible outcome.
- **FR-011**: System MUST preserve the default flag when an address is edited.
- **FR-012**: System MUST hide a deleted address from the customer's address list
  while keeping it intact for orders that already reference it.
- **FR-013**: System MUST reject requests to read, update or delete an address that
  does not belong to the caller, without revealing whether that address exists.
- **FR-014**: System MUST accept profile photos only in JPEG, PNG and WebP format,
  only up to 2 MB, and MUST verify the content type of the uploaded bytes rather
  than trusting the file name.
- **FR-015**: System MUST scale a newly accepted photo down to a maximum width of
  512 pixels before storing it, so stored photos do not slow down profile screens.
- **FR-016**: System MUST store each photo by an opaque media identifier plus a
  displayable link and its width/height, and MUST NOT store image bytes in the
  application database.
- **FR-017**: System MUST let a customer replace or remove their current photo, and
  MUST leave the profile unchanged when an upload is rejected or the media service
  is unavailable.
- **FR-018**: System MUST list a customer's addresses in a stable order with
  pagination, and MUST return the total address count.
- **FR-019**: System MUST record, in an auditable form, every change a customer
  makes to their profile, avatar or addresses, including who made the change, what
  changed and when.
- **FR-020**: System MUST return validation problems as field-level details that
  name the offending field, so a client can highlight the exact input.
- **FR-021**: System MUST keep the profile readable after the media service becomes
  unavailable; an avatar link MUST NOT be a reason for the profile to fail.
- **FR-022**: System MUST let an administrator read one customer's contact details
  and address list through a read-only capability owned by this module, and MUST
  refuse every attempt by an administrator to change those details.
- **FR-022a**: System MUST record, in an auditable form, every time an
  administrator opens a customer's contact details or address list, including who
  looked, at which customer and when.
- **FR-022b**: System MUST expose the administrative customer lookup to later
  modules as a stable read-only contract, so order, commission and admin modules
  reuse it instead of re-implementing customer access.
- **FR-023**: System MUST preserve a customer's addresses when their account is
  disabled, so historical orders remain reconstructable.
- **FR-024**: System MUST NOT provide account deletion, loyalty points, membership
  tiers or referral features; those are out of scope.
- **FR-025**: System MUST rate-limit the write-heavy endpoints of this feature,
  especially avatar upload, so a single account cannot monopolise storage or the
  media service by uploading repeatedly.

### Key Entities

- **Profile**: the customer-facing part of an account. Holds a display name,
  a phone number and avatar references; belongs to exactly one account; the email
  address and role stay owned by the authentication module.
- **Shipping address**: a delivery destination belonging to exactly one account.
  Holds recipient name, recipient phone number, a province, a ward, a detailed
  street address, a default flag and a hidden marker. At most one non-hidden
  address per account carries the default flag.
- **Province / Ward**: the administrative divisions a shipping address points at,
  loaded from the official Vietnamese administrative dataset. Each ward belongs to
  exactly one province. Read-only reference data that customers pick from instead of
  typing.
- **Media asset**: an opaque reference to a stored photo — an identifier, a
  displayable link and its dimensions. The photo bytes live outside the
  application database.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A signed-in customer can complete a profile update, including a
  photo, in under 60 seconds without leaving the page.
- **SC-002**: 100% of profile and address access attempts made without a valid
  session are rejected, and none of them modify stored data.
- **SC-003**: 100% of attempts by one customer to reach another customer's profile
  or address are rejected without disclosing whether the target exists.
- **SC-004**: Across any sequence of address operations, every account is verified
  to end with exactly zero or one default address, never two and never a default
  pointing at a hidden address.
- **SC-005**: 100% of photo uploads that are not a supported image type or exceed
  the size limit are rejected, and in every such case the customer's current photo
  is unchanged.
- **SC-006**: No more than 5% of valid profile update attempts are rejected for
  reasons other than the customer's own input.
- **SC-007**: Deleting an address that past orders reference never changes what
  those orders display, verified by inspection of order history after deletion.
- **SC-008**: Every change to a profile, photo or address is traceable in the
  audit record to a customer, an action and a point in time.
- **SC-009**: A customer with more than 20 addresses can find a specific address
  within a single page view, and address lists stay at a consistent size regardless
  of how many addresses the account holds.
- **SC-010**: An administrator can open a customer's contact details and address
  list in under 30 seconds, and cannot modify them.
- **SC-011**: 100% of saved shipping addresses reference a province and a ward that
  exist in the official dataset, and no saved address pairs a ward with a province
  that does not contain it.
- **SC-012**: A customer with several saved addresses can choose one of them at
  checkout in a single step, without re-typing any address details.
- **SC-013**: A single account that exceeds the avatar upload rate is refused
  further uploads with a clear retry hint, while its existing avatar keeps working.

## Assumptions

- The authentication module from `specs/001-user-auth` is complete and its session
  carries the account identifier, the role and an expiry; this module reuses those
  sessions and adds no new sign-in mechanism.
- The profile is an extension of the account created by the authentication module
  rather than a separate record, so there is exactly one customer identity.
- Customers are Vietnamese, so a shipping address's province and ward are **picked
  from the official Vietnamese administrative dataset** at a two-level hierarchy
  (province → ward), matching the current administrative model. The district level
  is not used. The customer-facing flow is cascading selects: choosing a province
  narrows the ward list.
- The administrative dataset ships with a release and is refreshed when a new
  official version is published; the running system never fetches it at runtime.
  A saved address that references a retired province or ward stays readable and is
  remapped rather than deleted, so customer and order history remain intact.
- A customer may hold many addresses and picks one of them per order; the default
  address is only the one offered first, never the only one allowed.
- Photo bytes are stored by an external media service, which the platform calls
  only from the server side; the customer never receives provider credentials.
- A customer's photo may be uploaded only by that customer; administrators do not
  upload or replace photos.
- Avatar media is not versioned from the customer's point of view: replacing a
  photo shows only the newest one.
- Existing orders store their own copy of the address text, so this module's
  address changes never rewrite order history.
- Deferred by decision, not by omission: loyalty points, membership tiers,
  referrals, shared address books and business/company addresses.
- Cloudinary is the media store of record for the platform, so avatars follow the
  same rule as product images: store the identifier, the link and metadata only.

## Out of Scope

- Account registration, sign-in, session handling and password rules
  (module 01, already delivered).
- Account deletion or deactivation flows.
- Product reviews, wishlist and content that appear on a profile page but are
  owned by other modules.
- Ordering, payment, shipping and commission behaviour, which read this module's
  data but do not change it.
- Administrative management screens and any administrative write operation on
  customer data; this module ships only the read-only lookup that later modules
  reuse.