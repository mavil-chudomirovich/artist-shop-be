# Deferred: Order SSE (020)

| Điểm hoãn | Vì sao | Gỡ ở đâu |
|---|---|---|
| Bảng `notifications`/in-app + đếm chưa đọc | Live-only đã chốt; đủ cho yêu cầu hiện tại | Module 12 Notification đầy đủ |
| Replay `Last-Event-ID` + event log | Reconnect bù bằng REST; tránh lưu trữ | Feature riêng khi có nhu cầu không sót event |
| Redis pub/sub multi-instance | Hiện 1 instance; in-process đủ | Khi deploy >1 replica API |
| Token qua query cho `EventSource` chuẩn | Đã chốt fetch-stream giữ header; query lộ log | Khi FE bắt buộc `EventSource` |
| Notify transfer cho chủ mới | Không phải đổi trạng thái; email hiện cũng không báo | Feature riêng nếu cần |
