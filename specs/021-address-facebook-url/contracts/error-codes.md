# Error codes — 021-address-facebook-url

Không thêm mã lỗi mới.

- Mọi vi phạm `facebookUrl` (thiếu, rỗng, quá dài, sai scheme/host) dùng mã dùng
  chung `VALIDATION_ERROR` (400) với `details[].field = "facebookUrl"`, theo quy
  ước hiện hành của module user (`contracts/error-codes.md`, "Codes deliberately
  not added").
- Checkout/sửa đơn dùng địa chỉ giữ chỗ (`https://facebook.com/unknown`) cũng trả
  `VALIDATION_ERROR` (400) với `details[].field = "facebookUrl"`.
- Không đổi mã `USER_ADDRESS_NOT_FOUND`, `USER_UNKNOWN_*`, `USER_INVALID_PHONE`;
  `404`/`401`/`429` giữ nguyên.
