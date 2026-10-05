# ADR 003 - Ràng buộc một địa chỉ mặc định bằng partial unique index

- **Status**: Accepted
- **Decision Date**: 2026-10-05
- **Decision Maker**: Mavil
- **Feature**: `specs/003-user-profile`

## 1. Bối cảnh

Bất biến nghiệp vụ: mỗi tài khoản có **nhiều** địa chỉ giao hàng nhưng **tối đa một**
địa chỉ mặc định. Đây là bất biến phải đúng dưới tải đồng thời: hai người cùng bấm
"đặt mặc định" gần như cùng lúc sẽ tạo ra hai mặc định nếu chỉ kiểm tra ở tầng ứng
dụng.

## 2. Quyết định

Ràng buộc được **cả hai** lớp:

```sql
CREATE UNIQUE INDEX addresses_one_default_per_user
    ON addresses (user_id)
    WHERE is_default AND deleted_at IS NULL;
```

- Tầng lưu trữ: partial unique index ⇒ atomic, chặn được cả khi ai đó ghi bằng `psql`.
- Tầng ứng dụng: domain function + transaction qua port `UnitOfWork` để trả lỗi thân
  thiện và bảo đảm "xoá mặc định cũ và đặt mặc định mới" là một kết quả duy nhất.

Không lưu `default_address_id` trên `users` — sẽ tạo nguồn sự thật thứ hai có thể lệch
với cột cờ.

## 3. Hệ quả

| Hệ quả | Chi tiết |
|---|---|
| Tích cực | Bất biến đúng ngay cả khi có race hoặc ghi tay ngoài ứng dụng |
| Tích cực | Test repository thất bại nếu thiếu index ⇒ index không bị xoá nhầm khi refactor |
| Tiêu cực | Xoá mặc định cũ rồi đặt mới là hai câu lệnh ⇒ cần transaction để trông như một thao tác |
| Tiêu cực | Index phần đạnh phải được giữ khi đổi câu truy vấn |
| Bù đắp | Phải có test khẳng định trực tiếp index chặn (không chỉ test qua use case) |

## 4. Cách tiếp cận bị loại

| Phương án | Vì sao không chọn |
|---|---|
| Chỉ kiểm tra ở tầng ứng dụng | TOCTOU: hai request song song đều đọc "chưa có mặc định" rồi cùng ghi |
| Khoá bản ghi `users` (`SELECT … FOR UPDATE`) | Nặng hơn, cần khoá mà generic repository không cung cấp; index đã làm việc này mà không chặn người đọc |
| Cột `default_address_id` trên `users` | Hai nguồn sự thật, có thể lệch nhau |
| Bỏ ràng buộc, coi như quy ước miệng | Bất biến nghiệp vụ mà không có ràng buộc sẽ hỏng ngay lúc có hai client |
