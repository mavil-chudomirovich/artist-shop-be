# ADR 001 - Modular monolith đơn binary thay vì microservices

- **Status**: Accepted
- **Decision Date**: 2026-10-05
- **Decision Maker**: Mavil

## 1. Bối cảnh

Dự án là một nền tảng thương mại điện tử với 17 module nghiệp vụ (auth, user, product,
order, payment, commission, chat…), triển khai trên một VPS bằng Docker. Các mô hình
đối lập:

- **Microservices**: mỗi module một service, giao tiếp qua mạng, mỗi service một
  database hoặc schema riêng.
- **Modular monolith**: một binary, module là ranh giới trong code, dữ liệu tách sở hữu.

Quy mô nhóm nhỏ, hạ tầng một VPS, chưa có nhu cầu scale độc lập từng phần.

## 2. Quyết định

Xây **modular monolith**: một binary Go, mỗi module nằm trong
`internal/modules/<module>` với bốn lớp bắt buộc và ranh giới sở hữu dữ liệu rõ
ràng. Module giao tiếp nhau qua interface trong `internal/contracts`, không đọc bảng
của nhau. API REST dưới `/api/v1`, chat dùng REST + polling, không message broker.

Chi tiết ở `docs/architecture.md`; các luật bắt buộc nằm ở
`.specify/memory/constitution.md` (Principle I và VII).

## 3. Hệ quả

| Hệ quả | Chi tiết |
|---|---|
| Tích cực | Một lệnh build, một container, một database. Triển khai rẻ và nhanh |
| Tích cực | Ranh giới được công chứng bằng quy tắc import, lỗi lộ ra lúc viết chứ không lúc deploy |
| Tích cực | Giao dịch đa bảng trong một use case rất tự nhiên (đơn hàng + tồn kho + thanh toán) |
| Tiêu cực | Một máy chủ, một tiến trình: giới hạn scale ngang |
| Tiêu cực | Cần kỷ luật để không phá vỡ ranh giới module trong lúc làm nhanh |
| Bù đắp | Không có broker ⇒ giao tiếp bất đồng bộ phải chờ đến khi có nhu cầu thật, đo được |

**Xem lại quyết định khi**: một module phải scale hoặc deploy độc lập về mặt lịch
trình; hoặc khi độ trễ của một luồng nào đó bị chặn bởi các luồng khác trong cùng
tiến trình.

## 4. Cách tiếp cận bị loại

| Phương án | Vì sao không chọn |
|---|---|
| Microservices từ đầu | Trả giá vận hành (nhiều service, nhiều network, tracing) khi chưa có nhu cầu scale; tách service sau vẫn được vì ranh giới đã rõ |
| Shared database không tách bảng | Xoá sạch ranh giới dữ liệu, mọi module sẽ sửa chung bảng |
| Message broker ngay từ đầu | Không có luồng nào cần bất đồng bộ; thêm broker là tăng độ phức tạp vô nghĩa |
