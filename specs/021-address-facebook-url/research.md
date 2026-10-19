# Research: Address Facebook URL

**Feature**: `021-address-facebook-url` | **Date**: 2026-10-19

Mọi `NEEDS CLARIFICATION` trong Technical Context đã được giải quyết qua spec + clarify (không còn mục mở). Ghi nhận quyết định và phương án đã loại.

## R1. Cách validate link Facebook

- **Decision**: Validate thuần chuỗi trong `domain/model/address.go` bằng stdlib (`strings`, `net/url`, `unicode/utf8`): trim → rỗng thì từ chối; đếm runes trên giá trị đã trim, trần 500; `url.Parse`, yêu cầu `Scheme == "https"`, `Host != ""`, host lower + cắt port rồi yêu cầu bằng hoặc hậu tố `.facebook.com` / `.fb.com` (mọi subdomain: `www.`, `m.`...); `EscapedPath` phải dài hơn `/` (host trần không path bị từ chối); chuỗi đúng bằng sentinel `https://facebook.com/unknown` bị từ chối như link không hợp lệ. Lưu nguyên URL đã trim (không chuẩn hóa path/case, không tự thêm scheme).
- **Rationale**: Domain không được import share/framework (Constitution I) nên chỉ dùng stdlib; không gọi mạng (quy tắc chỉ cần shape link, shop tự liên hệ thủ công). Suffix-match chặn `myfacebook.com` và `facebook.com.evil.com` trong khi vẫn cho `www.`/`fb.com`/hoa-thường host.
- **Alternatives considered**: gọi Facebook Graph API để xác thực profile thật — loại vì cần token/quota, chậm, YAGNI; regex tự viết — loại vì `net/url` chuẩn và dễ đọc hơn; chỉ check prefix `https://facebook.com` — loại vì bỏ sót `fb.com`/`www.`/chữ hoa.

## R2. Backfill dữ liệu cũ + NOT NULL

- **Decision**: Hai migration cộng thêm cột nullable → `UPDATE ... SET facebook_url='https://facebook.com/unknown' WHERE facebook_url IS NULL` → `SET NOT NULL` + `CHECK (char_length(facebook_url) > 0 AND char_length(facebook_url) <= 500)`; áp cho cả `addresses` (`00020`) và `orders` (`00021`).
- **Rationale**: Constitution bắt buộc ràng buộc tầng lưu trữ cho dữ liệu quan trọng; backfill giữ chỗ là cách duy nhất để `NOT NULL` không kẹt trên rows legacy. Giữ chỗ là URL hợp lệ về shape nên không vỡ CHECK, đồng thời là sentinel để nhận diện ở tầng ứng dụng — và vì domain từ chối sentinel trên mọi write mới (FR-002), giữ chỗ chỉ có thể tồn tại ở rows legacy chưa từng sửa, không bao giờ lọt vào dữ liệu mới.
- **Alternatives considered**: giữ cột nullable + chỉ bắt ở app — loại vì vi phạm AGENTS §4/Constitution (thiếu storage-level constraint); backfill `''` — loại vì rỗng không phải link và vẫn cần CHECK ngoại lệ phức tạp; bắt user bổ sung trước khi migrate — loại vì migration không thể tương tác với user.

## R3. Checkout/sửa đơn gặp địa chỉ giữ chỗ

- **Decision**: `checkout.go`/`edit.go` (qua `toAddress` hoặc ngay khi resolve `CustomerAddress`) từ chối địa chỉ có `FacebookURL == "https://facebook.com/unknown"` bằng `VALIDATION_ERROR` trường `facebookUrl`, yêu cầu khách cập nhật địa chỉ trước (FR-009).
- **Rationale**: Ngăn đơn mới mang link giả vào lịch sử bất biến; lỗi nêu đúng field để frontend dẫn khách về màn hình sửa địa chỉ. Kết hợp với việc domain từ chối sentinel trên write địa chỉ (FR-002), vòng "tạo địa chỉ giữ chỗ → kẹt checkout" không thể xảy ra: đường duy nhất còn giữ chỗ là rows legacy chưa từng chạm.
- **Alternatives considered**: cho qua nguyên văn — loại vì tạo đơn mới mang dữ liệu giả, đi ngược mục đích của feature.

## R4. https-only và trần 500

- **Decision**: Chỉ chấp nhận `https` (theo clarify 2026-10-19); trần 500 runes trên giá trị đã trim (đủ cho URL có query `mibt...`, ngắn gọn hơn text không giới hạn).
- **Rationale**: `https` là chuẩn chia sẻ link hiện nay; `http` link FB thực tế đều redirect, chấp nhận sẽ lưu giá trị lỗi thời. 500 dài hơn `streetAddress` (255) vì URL có query, nhưng vẫn chặn abuse text vô hạn.
- **Alternatives considered**: chấp nhận cả http — đã bị clarify loại; trần 255/2048 — 255 dễ tràn query FB, 2048 quá rộng cho một trường liên hệ.

## R5. Không thêm mã lỗi mới, audit không ghi giá trị

- **Decision**: Tái dùng `ErrAddressInvalid` + `AddressFieldError{Field:"facebookUrl"}` → presentation map sẵn sang `VALIDATION_ERROR` (không thêm `USER_*` code); `changedAddressFields` thêm `facebookUrl` nhưng audit chỉ ghi tên field, không ghi URL (giống các field liên hệ khác).
- **Rationale**: Khớp `contracts/error-codes.md` ("Codes deliberately not added") và Constitution VI (PII/contact không vào log). Handler `CreateAddress` thêm required-check tay cho `facebookUrl` giống 5 field hiện tại để báo sớm, domain vẫn là chốt chặn cuối.
- **Alternatives considered**: thêm `USER_INVALID_FACEBOOK_URL` — loại vì client chỉ cần `VALIDATION_ERROR` + field; ghi URL vào audit `changedFields` — loại vì rò rỉ contact vào audit log.

## R6. Phạm vi lan sang order/contract/docs

- **Decision**: Thêm `FacebookURL` vào `contracts.CustomerAddress`, `order/domain/model.Address`, `OrderAddressView/Response`, order postgres columns/scan, admin `OperatorAddressResponse`; cập nhật OpenAPI user (003) + order (009/010/019), `docs/api-reference.md`, regen `docs/swagger/`; ship `frontend-guide.md` ở implement.
- **Rationale**: Scope đã chốt ở clarify: end-to-end để admin và đơn hàng thấy cùng link; snapshot giữ bất biến lịch sử. Đây là changed DTO (không new/remove endpoint) nên guide ghi before → after từng shape.
- **Alternatives considered**: chỉ sửa user module, order giữ 7 field cũ — đã bị loại ở vòng hỏi scope vì đơn mới sẽ thiếu kênh liên hệ.
