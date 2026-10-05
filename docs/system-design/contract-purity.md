# Contract Purity

Module này cần dữ liệu của module khác thì đi qua **contract**, không import code
nội bộ và không đọc bảng của nhau.

## Hợp đồng gồm gì

| Loại | Hình thức | Vị trí |
|---|---|---|
| Đồng bộ giữa module | Interface Go + DTO thuần | `internal/contracts/*.go` |
| Hợp đồng HTTP với client bên ngoài | Route + request/response DTO | `internal/modules/<m>/presentation` |
| Mô tả máy-đọc được theo feature | OpenAPI | `specs/<feature>/contracts/openapi.yaml` |
| Văn xuôi cho người đọc | Bảng endpoint + mã lỗi | `docs/api-reference.md` |

## Nguyên tắc

1. **Contract là nguồn chân lý.** Đổi hành vi liên module: sửa contract trước, rồi mới
   sửa cả hai phía. Không sửa phía nhận trước rồi "thử xem có ai dùng không".
2. **Không rò rỉ nội bộ.** Module tiêu thụ **không** được biết entity, bảng, hay cấu
   trúc của module cung cấp. Contract chỉ mô tả dữ liệu được trao đổi.
3. **Kiểu tường minh.** Contract khai báo rõ kiểu. Không dùng `map[string]any`, không
   dùng kiểu nội bộ làm kiểu contract.
4. **Module cung cấp adapter ở composition root.** `internal/contracts` chỉ có interface
   và DTO; bản hiện thực nằm trong module cung cấp và được wire ở `cmd/api`.
5. **Đổi phụ thuộc là thay đổi phá vỡ.** Thêm trường bắt buộc, đổi kiểu, đổi nghĩa, hoặc
   bỏ trường đều là breaking. Thêm trường tuỳ chọn thì tương thích ngược.
6. **Mỗi thay đổi contract phải kèm test** ở phía tiêu thụ.

## Trong Go, "rò rỉ nội bộ" trông như thế nào

| Sai | Đúng |
|---|---|
| Module order dùng `authmodel.Account` | Dùng DTO trong `internal/contracts` |
| Contract trả `*model.Session` (domain model) | Contract trả struct DTO riêng |
| Contract có method nhận `*pgxpool.Pool` | Contract chỉ có DTO và kiểu nguyên thuỷ |
| Consumer đọc thẳng bảng của producer | Consumer gọi interface |

Lý do: nếu contract dùng chung kiểu với domain model, thay đổi model sẽ tự động phá
hợp đồng mà không có gì báo lỗi biên dịch.

## Ví dụ đang dùng

Module 02 (user) cung cấp truy vấn chỉ đọc cho order/commission/admin:

```go
// internal/contracts/user.go
package contracts

type CustomerLookupService interface {
    LookupCustomer(ctx context.Context, userID uuid.UUID) (Customer, error)
}

// Customer là DTO của contract — không phải domain model của module user.
type Customer struct {
    ID          uuid.UUID
    Email       string
    DisplayName string
    Phone       string
}
```

Module 07 order sẽ phụ thuộc **interface này**, không phụ thuộc `internal/modules/user`.
Khi module user đổi cách lưu hồ sơ, module 07 không phải đụng gì.

## Đồng bộ tài liệu

Một hợp đồng có **ba** bản sao có chủ đích: interface Go (để compile), OpenAPI (để
sinh client hoặc kiểm thử), và `docs/api-reference.md` (để con người đọc). Cả ba phải
nói cùng một điều:

- Sửa interface → cập nhật OpenAPI **và** `docs/api-reference.md` trong cùng thay đổi.
- Khi hai bên lệch, `docs/api-reference.md` là chuẩn và bên kia phải được sửa cho
  khớp (Constitution VIII).

## Ranh giới module

Constitution quy định: mỗi module sở hữu dữ liệu của mình và **không** được cho
module khác đọc/ghi bảng của nó. Vì vậy:

- Truy vấn chéo bằng SQL là **vi phạm**, kể cả khi "chỉ để đọc".
- Bảng dùng chung (ví dụ `audit_logs`) là ngoại lệ đã được hiến pháp công nhận, thuộc
  tầng foundation.
- Nếu hai module thật sự cần cùng một dữ liệu, đó là ứng viên để tách thành module
  dữ liệu riêng — không phải lý do để cho đọc chéo bảng.

## Liên quan

- [design-pattern.md](design-pattern.md) — ports & adapters, Unit of Work
- [architecture.md](../architecture.md) — quy tắc sở hữu dữ liệu
- [../api-reference.md](../api-reference.md) — hợp đồng HTTP chuẩn cho client