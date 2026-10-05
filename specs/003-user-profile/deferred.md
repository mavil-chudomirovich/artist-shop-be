# Deferred — 003-user-profile

Những điểm **đã biết, cố ý chưa làm trong feature này**. Mỗi mục ghi rõ lý do, cái
gì đã thay thế trong khi chờ, và điều kiện để đóng lại. Không mục nào là việc quên.

| # | Điểm | Vì sao hoãn | Hiện trạng | Đóng lại khi |
|---|------|-------------|-----------|--------------|
| D1 | Tài khoản bị disable thì từ chối tạo địa chỉ mới | FR-023 chỉ yêu cầu **giữ lại** địa chỉ. Không request path nào đọc `users.status`, và `contracts/error-codes.md` chưa gán mã lỗi cho tình huống này. Thêm hành vi lúc này là tự ý mở rộng spec | Module User đọc và trả địa chỉ của tài khoản disabled bình thường, đã có test `TestADisabledAccountKeepsItsAddresses` | Có FR + error code, và quyết định module nào sở hữu quy tắc đọc `users.status` (module Auth) |
| D2 | Hai request tạo địa chỉ đầu tiên chạy đồng thời: kẻ thua nhận `INTERNAL_ERROR` | Ràng buộc **vẫn đúng** ở tầng lưu trữ nhờ partial unique index (ADR-003), chỉ là mã trả về chưa đẹp. Sửa cần thêm một FR về `CONFLICT` hoặc một vòng retry ở tầng application, đều vượt phạm vi T034–T043 | Index từ chối đúng; test `TestSetDefaultWithoutClearingIsRejectedAndReported` chứng minh index còn hiệu lực | T064 (security review) chốt giữa retry ở application và `CONFLICT`, hoặc một feature riêng |
| D3 | `maxLength` 120 (`recipientName`) và 255 (`streetAddress`) trong `contracts/openapi.yaml` | Xem mục "Đã xử lý" bên dưới — đã quyết định **thực thi** trong phase này, không còn hoãn | Đã được enforce ở `domain/model/address.go` và `data-model.md` đã cập nhật | — |
| D4 | `AddressRepository.SetDefault(ctx, addressID)` không mang `userID` | Interface đã commit ở T013; phạm vi sửa là thay đổi đã commit, và hiện tại nó an toàn nhờ `FindByOwner` **trong cùng transaction** | An toàn theo hợp đồng tại doc comment; test `TestTheDefaultTransitionJoinsTheCallersTransaction` chứng minh việc rollback giữ nguyên default cũ | Khi có module thứ hai ghi qua adapter này, hoặc khi T064 đánh giá lại rủi ro |
| D5 | Audit ghi sau khi write đã commit: nếu đọc `actorRole` lỗi thì write đã xong nhưng không có audit row | Đây là điểm nhất quán với Phase 3 (`UpdateProfile`), sửa cần thay đổi cả hai đường và thuộc phạm vi FR-019 về "mọi thay đổi đều được ghi" | Đã ghi nhận tại `application/implement/profile.go` và `address.go` | Feature riêng về độ bền của audit, hoặc khi T064 yêu cầu |
| D6 | Test integration cho giới hạn 120/255 khi lưu vào PostgreSQL thật | Quy tắc đã được chứng minh ở entity và qua HTTP với repository in-memory. Cột là `text` không có ràng buộc, nên round-trip không có rủi ro riêng — giá trị thừa vẫn là `text` | Đã có test ở `domain/model`, `application/implement` và `presentation/http` cho cả `displayName`, `recipientName` lẫn `streetAddress` | Nếu sau này `text` bị đổi thành `varchar` hoặc thêm check constraint, test này trở nên cần thiết |
| D7 | Một caller không phải HTTP của `LookupCustomer` (worker, module Order) sẽ ghi `audit_logs` với actor rỗng | Contract `CustomerLookupService` cố ý không mang tham số actor; xem research D11. Hiện chưa có caller nào như vậy | `presentation` điền actor từ session nên mọi đường HTTP đều có actor thật; đường không phải HTTP ghi sự kiện với actor nil | Khi module Order ra đời: quyết định có thêm actor vào contract (breaking) hay chấp nhận actor rỗng cho caller nền |
| D8 | Lookup không giới hạn số địa chỉ trả về ngoài trần 100 dòng hiện có | FR-007d và DTO của contract không có cửa sổ phân trang, nhưng không FR nào giới hạn số địa chỉ mỗi tài khoản | Dùng lại trần sẵn có `maxAddressPageSize`; khách hàng hơn 100 địa chỉ sẽ bị cắt | Nếu sản phẩm cho phép hơn 100 địa chỉ: cần FR mới và contract nhiều trang |

## Ghi chú cho Phase 5 (US3)

Hai ràng buộc trong `contracts/openapi.yaml` **chưa** có nơi nào thực thi, và thuộc về
phase avatar — không phải khoản nợ của phase này:

- `Avatar.width` / `Avatar.height` được mô tả là "luôn không quá 512" nhưng schema không
  khai báo `maximum` và chưa có guard ở domain. T047 (use case avatar) phải thêm guard
  đọc được chiều rộng trả về từ nhà cung cấp.
- `Avatar.url` khai báo `format: uri` và không được kiểm tra. Đây là dữ liệu do nhà cung
  cấp media viết ra; trách nhiệm thuộc T048 (adapter Cloudinary).

## Đã xử lý trong feature này

- **D3** (`maxLength` trong contract): quyết định là **thực thi** trong Phase 4 thay vì
  bỏ khỏi contract. Một contract quảng bá giới hạn mà server không áp dụng là contract
  nói dối client; `data-model.md` được sửa để nói rõ giới hạn thay vì "unbounded".