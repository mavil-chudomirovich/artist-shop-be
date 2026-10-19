# Module 01 — Auth

- **Trạng thái Spec Kit**: Hoàn tất (implement + converge; tất cả task đã đóng)
- **Spec**: `specs/001-user-auth/spec.md`
- **Plan**: `specs/001-user-auth/plan.md`
- **Tasks**: `specs/001-user-auth/tasks.md`
- **Code**: `internal/modules/auth/{domain,application,infrastructure,presentation}`, `scripts/seed_admin.sql`
- **Ưu tiên / Giai đoạn**: V0 — làm đầu tiên
- **Phụ thuộc**: cross-cutting

## Mục đích

Xác thực và quản lý phiên cho khách hàng và admin: đăng ký, đăng nhập, làm mới
phiên, đăng xuất, đặt lại mật khẩu, và phân quyền theo vai trò.

## Phạm vi MVP

Có:
- Đăng ký bằng email + mật khẩu.
- Bắt buộc xác minh email bằng OTP 6 chữ số trước khi đăng nhập được (đăng ký chỉ lưu
  tạm ở Redis; OTP dùng một lần, 5 lần sai thì khoá 60 giây; tài khoản chính thức chỉ được
  tạo sau khi xác minh).
- Đăng nhập, làm mới phiên, đăng xuất (nhiều thiết bị).
- Đặt lại mật khẩu qua email: gửi mã OTP → xác minh mã → nhận reset token dùng một lần →
  đặt mật khẩu mới (không tự đăng nhập).
- Vai trò CUSTOMER (mặc định) và ADMIN (cấp nội bộ bằng `scripts/seed_admin.sql`).
- Rate limiting cho các endpoint xác thực.
- Ghi vết sự kiện bảo mật.

Không (hoãn):
- Đăng nhập mạng xã hội (Google/Facebook).
- Xác thực hai yếu tố (2FA).

## Thực thể dữ liệu

- `users` (phần tài khoản): email, mật khẩu đã băm, vai trò, trạng thái.
- `sessions` / refresh token: tham chiếu credential, hạn, cờ thu hồi, quan hệ xoay vòng.
- `password_reset_requests`: token dùng một lần, hạn, trạng thái đã dùng.

## Luồng nghiệp vụ chính

1. Đăng ký → lưu đăng ký tạm ở Redis → gửi OTP → xác minh email → tạo tài khoản CUSTOMER
   `active` → đăng nhập được.
2. Đăng nhập → cấp access credential ngắn hạn + session credential dài hạn.
3. Hết hạn access → làm mới bằng session credential (có xoay vòng).
4. Đăng xuất → thu hồi session của thiết bị đó.
5. Dùng lại refresh token đã xoay vòng → từ chối và thu hồi **toàn bộ** phiên của
   tài khoản (phát hiện replay), ghi `audit_logs`.
6. Quên mật khẩu → gửi mã OTP → xác minh mã → cấp reset token dùng một lần → đặt mật khẩu
   mới → thu hồi mọi phiên cũ (người dùng đăng nhập lại).

## Yêu cầu chức năng sơ bộ

- Email duy nhất; mật khẩu đủ mạnh; lưu băm không thể đảo ngược.
- Phản hồi đăng ký/quên mật khẩu không tiết lộ email đã tồn tại.
- Admin không thể tạo qua đăng ký công khai.
- Endpoint riêng tư yêu cầu phiên hợp lệ; endpoint admin yêu cầu vai trò ADMIN.
- Đổi mật khẩu khi đã đăng nhập phải kèm refresh token hiện tại và thu hồi mọi phiên
  của tài khoản.

## Tiêu chí hoàn thành

- Đăng ký + xác minh email + đăng nhập + làm mới + đăng xuất + đặt lại mật khẩu hoạt
  động và có test.
- Test chuyển trạng thái phiên (hợp lệ/hết hạn/thu hồi) đạt.
- Test state machine trạng thái tài khoản (pending/active/disabled) đạt.
- Có test API cho biên xác thực/phân quyền.

## Ghi chú / câu hỏi mở

- Xem mục Assumptions trong spec để biết phạm vi đã chốt.

## Tham chiếu

- `specs/001-user-auth/spec.md`
- `docs/product/backend-spec.md` (Authentication)
