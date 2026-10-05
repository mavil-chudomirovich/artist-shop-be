# Design Patterns

Các pattern dự án này dùng, kèm **phạm vi áp dụng** và **giới hạn**. Mục tiêu là
để người sau không phải đoán, và không lạm dụng pattern ở nơi không cần.

---

## 1. Modular Monolith

**Là gì**: Một binary, nhiều module nghiệp vụ độc lập về ranh giới dữ liệu. Mỗi module
là một thư mục dưới `internal/modules/<module>`.

**Vì sao**: Ranh giới dữ liệu và phụ thuộc một chiều bắt được lỗi ngay khi viết, thay
vì lúc deploy. Tách microservice sau này vẫn được vì ranh giới đã rõ.

**Giới hạn**: Không được tạo service mới chỉ vì một bảng lớn lên. Không có message
broker ở thời điểm hiện tại. Xem [architecture.md](../architecture.md).

---

## 2. Four-Layer Module

```text
presentation  →  application  →  domain
infrastructure → domain, application/interface
```

| Tầng | Chứa | Không chứa |
|---|---|---|
| `domain` | `model`, `constant`, `error`, `repository` | Framework, SQL, HTTP |
| `application` | `interface`, `implement`, `dto`, `mapper` | `infrastructure`, `presentation` |
| `infrastructure` | `implement` (adapter) | Interface, nghiệp vụ |
| `presentation` | `http`, `cli`, `worker`, `dto` | Nghiệp vụ, SQL |

**Giới hạn**: Nếu một logic đúng nghiệp vụ mà bắt buộc phải gọi DB bên trong hàm, đó
là tín hiệu logic đó chưa nằm ở `domain`. Kiểm bằng câu hỏi: *nghiệp vụ này còn đúng
không nếu đổi PostgreSQL sang thứ khác?*

---

## 3. Repository + Generic Base

**Là gì**: Interface repository nằm ở `domain/repository`; adapter ở
`infrastructure/implement/postgres` và **embed** `share/repository.Base[T, ID]` cho
CRUD, `FindAll` có phân trang.

**Vì sao**: 80% truy vấn là CRUD phân trang lặp lại; generic base gom phần đó một lần,
giữ adapter chỉ còn phần đặc thù.

**Giới hạn**:
- Repository **không** mở transaction.
- Truy vấn đặc thù (join, upsert, truy vấn báo cáo) viết SQL tay trong adapter.
- Adapter chỉ làm map hàng ↔ model, **không** chứa quy tắc nghiệp vụ.

```go
type UserRepository struct {
    repository.Base[model.Account, uuid.UUID]
    pool *pgxpool.Pool
}
```

---

## 4. Unit of Work

**Là gì**: Port `UnitOfWork` ở `application/interface`; adapter là `share/database.DB`.
Ranh giới giao dịch do **use case** giữ, không phải repository.

```go
func (s *Service) SetDefault(ctx context.Context, in dto.SetDefaultInput) error {
    return s.Tx.WithTx(ctx, func(ctx context.Context) error {
        if err := s.Addresses.ClearDefault(ctx, in.UserID); err != nil { return err }
        return s.Addresses.SetDefault(ctx, in.AddressID)
    })
}
```

**Vì sao**: Ghi nhiều bảng phải là một đơn vị thành công/thất bại; nếu repository tự
`Begin`, sẽ có transaction lồng nhau và mất tính nguyên tử ở tầng use case.

**Giới hạn**: Chỉ dùng khi thao tác thực sự cần giao dịch. Một truy vấn đọc đơn lẻ thì
đừng bọc.

---

## 5. Ports & Adapters (Ports and Adapters)

**Là gì**: `application/interface` khai báo port (`MediaStore`, `Auditor`, `Email`,
`PasswordHasher`, `TokenIssuer`); `infrastructure/implement` cung cấp adapter.

**Vì sao**: Use case được test bằng fake trong bộ nhớ, không cần Cloudinary hay
PostgreSQL; đổi nhà cung cấp không đụng nghiệp vụ.

**Giới hạn**: Port mô tả **khả năng**, không mô tả công nghệ. Đặt tên `MediaStore` chứ
không `CloudinaryClient` — nếu tên port chứa tên công nghệ thì abstraction đã hỏng.

---

## 6. Two DTO Layers + Single Mapper

**Là gì**: `presentation/dto` mô tả HTTP; `application/dto` mô tả use case; đổi kiểu
chỉ xảy ra ở `application/mapper`.

**Vì sao**: HTTP contract và use case contract thay đổi vì lý do khác nhau. Khi đổi
JSON field, không kéo theo mô hình nghiệp vụ; khi đổi nghiệp vụ, không phá client.

**Giới hạn**: **Chỉ một mapper cho mỗi module.** Nếu thấy nhiều chỗ tự map struct, đó
là dấu hiệu logic lọt vào handler.

---

## 7. Result Envelope

**Là gì**: `share/httpx.WriteSuccess` / `WriteError` bọc mọi response trong
`{data, meta}` hoặc `{error}`; mã lỗi ổn định do `httpx.ErrorCode` định nghĩa.

**Vì sao**: Client xử lý thống nhất, không rà nhiều shape; mỗi lỗi có mã để branch và
`requestId` để tra log.

**Giới hạn**: Endpoint vận hành (`/healthz`, `/readyz`) cố ý **ngoài** envelope vì
công cụ hạ tầng phải đọc được chúng. Chỉ đặc biệt có chủ ý mới được phép.

---

## 8. Audit Writer

**Là gì**: `share/audit.Writer` chạy nền trong process, đưa event vào hàng đợi rồi ghi
`audit_logs` với số lần thử lại. Module dùng qua port `Auditor`.

**Vì sao**: Ghi audit không được làm chậm request; sự kiện vẫn còn trong hàng đợi khi DB
chập chờn.

**Giới hạn**: Hàng đợi **trong bộ nhớ**, nên sập giữa lúc ghi có thể mất event; writer
phải log cảnh báo khi phải reconcile thủ công. Với tiền và thanh toán, có thao tác bù
(bổ sung) ngoài. Đọc kỹ [constitution](../../.specify/memory/constitution.md) §VI.

---

## 9. Testsupport

**Là gì**: `internal/share/testsupport` bọc testcontainers để test integration tự tạo
PostgreSQL, và tự tắt reaper.

**Vì sao**: Test không phụ thuộc stack của dev; `make test-integration` chạy được ở
bất kỳ máy nào có Docker.

**Giới hạn**: Chỉ dùng cho test có build tag `integration`. Với Redis, dùng
`miniredis` (in-memory) vì nhẹ hơn nhiều.

---

## 10. Value Object

**Là gì**: Kiểu giá trị trong `domain/model` ôm luật: `phone.go` chuẩn hoá số điện
thoại, `avatar.go` ôm quy tắc "null hết hoặc đầy đủ".

**Vì sao**: Luật nằm ở một chỗ và áp dụng mọi lối gọi — kể cả từ CLI hay worker. Không
thể bỏ qua vì "cổng HTTP đã kiểm rồi".

**Giới hạn**: Đừng tạo value object cho thứ chỉ là chuỗi không có luật nào.

---

## Pattern không dùng, và vì sao

| Pattern | Vì sao không dùng |
|---|---|
| Unit of Work phức tạp, đăng ký handler | Ở quy mô này, transaction tường minh quanh use case dễ đọc hơn |
| CQRS / tách query khỏi command | Chưa có báo cáo phức tạp; thêm lớp gián tiếp mà không đổi nhu cầu |
| Repository dùng ORM tự sinh | Cần SQL rõ ràng cho index phần đạnh, ràng buộc và truy vấn phân trang |
| DI container (kiểu Spring) | Composition root thuần `cmd/api` đã đủ và dễ đọc |
| Domain event / bus | Chưa có module nào cần giao tiếp bất đồng bộ; REST + polling đủ (chat cũng vậy) |