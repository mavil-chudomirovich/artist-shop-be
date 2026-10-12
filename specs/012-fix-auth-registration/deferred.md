# Deferred — OTP-Gated Registration (No Account Before Verification)

Việc phát hiện khi xây feature `012-fix-auth-registration` nhưng **không phải việc của
feature này**. Không mục nào ở đây chặn việc đóng feature; không mục nào là task của nó.

## Các mục

### D-01 — Hằng `constant.AuditRegister` giờ không còn ai dùng

- **Vấn đề**: `internal/modules/auth/domain/constant/audit.go` vẫn khai báo
  `AuditRegister = "AUTH_REGISTER"`, nhưng sau khi use case `Register` cũ bị xoá (T039), cả
  production lẫn test không còn tham chiếu tới nó. Đây là code chết do thay đổi của feature
  này tạo ra.
- **Vì sao ngoài phạm vi**: `golangci-lint` không bắt hằng export không dùng, và bản thân
  hằng thuộc bộ đếm audit chung của module auth (feature 001) — dọn nó là việc vệ sinh
  của module, không phải một hành vi mà feature 012 mô tả. Xoá nó không đổi hành vi
  người dùng thấy được.
- **Gỡ bằng cách nào**: Xoá dòng `AuditRegister` trong
  `internal/modules/auth/domain/constant/audit.go` (grep để chắc chắn không còn tham chiếu),
  ở một thay đổi dọn dẹp của module auth.
- **Ảnh hưởng tới task đã tick**: Không. Không task nào của feature này dựa vào hằng đó.

### D-02 — Giá trị `OTP_TTL` trong `.env` cục bộ khác default mới

- **Vấn đề**: Code default và `.env.example` đã đổi `OTP_TTL` `15m → 5m` (T001/T003), nhưng
  `.env` cục bộ của máy dev vẫn đặt `OTP_TTL=15m`, nên stack đang chạy trả `otpExpiresIn=900`.
  Đây là giá trị môi trường do dev sở hữu, không phải lỗi code.
- **Vì sao ngoài phạm vi**: `.env` bị git-ignore và chứa credential thật; feature không được
  tự sửa nó. Đây là hành động vận hành của dev.
- **Gỡ bằng cách nào**: Dev đặt `OTP_TTL=5m` trong `.env` (hoặc tái tạo từ `.env.example`)
  rồi khởi động lại API nếu muốn khớp default của spec.
- **Ảnh hưởng tới task đã tick**: Không. Task T046 (validate quickstart) đã chạy với giá trị
  môi trường hiện có; hành vi (TTL riêng, cooldown, verify) đều đúng.
