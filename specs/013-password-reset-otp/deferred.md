# Deferred — Password Reset via Emailed Code

Việc phát hiện khi xây feature `013-password-reset-otp` nhưng **không phải việc của feature
này**. Không mục nào ở đây chặn việc đóng feature.

## Các mục

### D-01 — `docs/getting-started.md` ghi sai số lượng endpoint

- **Vấn đề**: `docs/getting-started.md` nói "25 endpoint" trong khi bảng tổng hợp API liệt kê
  hơn 60. Con số đã cũ từ lâu, không liên quan tới password reset.
- **Vì sao ngoài phạm vi**: là drift tài liệu có sẵn từ các feature trước, không phải hành vi
  mà feature 013 mô tả; sửa nó không đổi giá trị của feature này.
- **Gỡ bằng cách nào**: cập nhật con số trong `docs/getting-started.md` khớp bảng endpoint (hoặc
  bỏ con số để tránh trôi), ở một thay đổi dọn tài liệu tổng hợp.
- **Ảnh hưởng tới task đã tick**: Không.

### D-02 — `routeClass` của rate limiter phân loại sai khi router mount ở gốc (chỉ trong test)

- **Vấn đề**: `internal/share/middleware/ratelimit.go` trả class `"auth"` chỉ khi path có tiền
  tố `/api/v1/auth/`. Trong test router được mount ở gốc nên các route `/password/*` rơi vào
  class `"write"` và dùng chung bucket với mọi POST khác; ở production (mount dưới `/api/v1`)
  thì đúng là `"auth"`.
- **Vì sao ngoài phạm vi**: là hành vi hạ tầng test có sẵn từ trước, không do feature 013 tạo
  ra; các test rate-limit của feature vẫn kiểm đúng cơ chế `RATE_LIMITED`.
- **Gỡ bằng cách nào**: cho `routeClass` phân loại theo tiền tố không phụ thuộc mount point,
  hoặc cấu hình class tường minh trong test; ở thay đổi hạ tầng rate-limit.
- **Ảnh hưởng tới task đã tick**: Không. Task T023/T024 kiểm `429 RATE_LIMITED` bằng router
  test có cấu hình bucket riêng, nên bằng chứng vẫn đứng vững.
