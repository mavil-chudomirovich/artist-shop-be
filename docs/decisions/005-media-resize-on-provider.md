# ADR 005 - Thu nhỏ ảnh nhờ media provider, không thêm thư viện xử lý ảnh

- **Status**: Accepted
- **Decision Date**: 2026-10-05
- **Decision Maker**: Mavil
- **Feature**: `specs/003-user-profile`

## 1. Bối cảnh

Avatar phải có bề rộng tối đa 512 px trước khi lưu. Dự án hiện dùng Cloudinary làm
kho media, không có thư viện xử lý ảnh nào trong Go.

## 2. Quyết định

Tải **nguyên bản** lên media provider và yêu cầu provider thực hiện biến đổi kích
thước ngay trong lúc upload (target width 512). Không thêm dependency giải mã ảnh vào
dịch vụ.

Kiểm tra loại và kích thước vẫn thực hiện ở máy chủ trước khi upload, nên việc kiểm
tra không phụ thuộc vào provider.

## 3. Hệ quả

| Hệ quả | Chi tiết |
|---|---|
| Tích cực | Không thêm dependency lớn (kèm bề mặt CVE) vào binary |
| Tích cực | Build nhanh hơn, image nhỏ hơn, ít CPU hơn mỗi request |
| Tích cực | Định dạng đầu ra do provider quyết định, không phải do hai bên hiểu khác nhau |
| Tiêu cực | Phụ thuộc provider có hỗ trợ biến đổi khi upload |
| Tiêu cực | Nếu đổi provider, phải tìm được cơ chế tương đương |
| Bù đắp | Port `MediaStore` che giấu khác biệt, nên đổi provider chỉ đụng adapter |

## 4. Cách tiếp cận bị loại

| Phương án | Vì sao không chọn |
|---|---|
| Giải mã và resize trong Go | Dependency nặng, tốn CPU, và hai nơi có thể cho ra kết quả khác nhau |
| Để client resize trước khi upload | Máy chủ không thể tin kích thước do client khai; client có thể bỏ qua |
