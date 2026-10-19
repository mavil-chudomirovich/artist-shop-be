# Data Model: Address Facebook URL

**Feature**: `021-address-facebook-url` | **Date**: 2026-10-19

## 1. Shipping Address (module user, bảng `addresses`)

Thêm một thuộc tính vô hướng bắt buộc, không quan hệ mới, không state machine mới.

| Thuộc tính mới | Kiểu logic | Ràng buộc domain (app rule) | Ràng buộc lưu trữ |
|---|---|---|---|
| `facebookUrl` (`FacebookURL` / `facebook_url`) | chuỗi URL đã trim, giữ nguyên case path | required; trim; 1–500 runes; `https`, host `facebook.com`/`fb.com` hoặc mọi subdomain (case-insensitive, bỏ qua port), path phải có nội dung, từ chối sentinel `https://facebook.com/unknown` trên mọi write mới; sai → `AddressFieldError{facebookUrl}` | `text NOT NULL` + `CHECK (char_length(facebook_url) > 0 AND char_length(facebook_url) <= 500)` |

- `AddressDraft`/`AddressEdit` thêm `FacebookURL string` / `*string`; `NewAddress`/`Apply` stage toàn bộ draft rồi validate (vi phạm → entity giữ nguyên, kể cả `IsDefault`); `Apply` với edit vắng mặt `facebookUrl` vẫn từ chối khi giá trị đang lưu là sentinel (FR-004 ngoại lệ).
- `Update` (infra) phải ghi thêm cột nhưng **vẫn không** đặt `is_default`/`deleted_at` (giữ nguyên tắc của module user: edit không bao giờ dời default hay hồi sinh row ẩn — xem `address.go` trong infra).
- Projection `addressColumns` + `scanAddress` thêm đúng 1 cột ở cùng vị trí cho `Create`/`Update`/`FindByOwner`/`ListByOwner`.
- Migration `00020_user_address_facebook_url.sql`: `ADD COLUMN text` → backfill `https://facebook.com/unknown` → `SET NOT NULL` → `ADD CONSTRAINT ..._nonempty_ck`; Down đảo ngược.

## 2. Order Delivery Snapshot (module order, bảng `orders`)

| Thuộc tính mới | Kiểu logic | Ràng buộc | Lưu trữ |
|---|---|---|---|
| `facebookUrl` (`FacebookURL` / `facebook_url`) | ảnh chụp từ `CustomerAddress` tại checkout/edit-đổi-địa-chỉ | copy nguyên văn; đơn cũ giữ bất biến; địa chỉ giữ chỗ `.../unknown` → từ chối checkout (FR-009) | `text NOT NULL` + CHECK tương tự (`00021_order_address_facebook_url.sql`, backfill cùng sentinel) |

- `orderColumns`/insert/update/scan thêm 1 cột cùng vị trí; snapshot không có đường sửa sau tạo (chỉ ghi lúc checkout/edit).
- Đơn cũ sau backfill hiển thị sentinel nguyên văn (đã chốt ở clarify).

## 3. Luồng dữ liệu

```text
POST/PATCH /users/me/addresses (+facebookUrl)
  → handler required-check → app dto → domain NewAddress/Apply (validate https+host+length)
  → postgres addresses (NOT NULL + CHECK)
  → mapper → AddressResponse / CustomerAddress{+FacebookURL}
  → checkout/edit order: resolve qua CustomerLookupService
      → placeholder? từ chối VALIDATION_ERROR : toAddress() snapshot vào orders.facebook_url
  → OrderResponse/OperatorAddressResponse hiển thị facebookUrl
```

## 4. Quy tắc kiểm thử dữ liệu

- Mọi bảng kiểm thử (domain/app/http/infra/order) thêm case: thiếu, rỗng/toàn trắng, >500 runes, thiếu scheme, `http://`, `ftp://`, host lạ/gần giống/subdomain độc (`facebook.com.evil.com`), host trần không path, sentinel trên write mới → từ chối đúng field; `https://facebook.com/x`, `https://www.facebook.com/x`, `m.facebook.com`, `https://fb.com/x`, host hoa, port tường minh → chấp nhận; PATCH vắng mặt giữ cũ trên địa chỉ thường nhưng bị từ chối trên địa chỉ sentinel; checkout/sửa-đơn với sentinel bị chặn.
