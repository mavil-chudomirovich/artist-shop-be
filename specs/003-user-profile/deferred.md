# Deferred — 003-user-profile

Những điểm **đã biết, cố ý chưa làm trong feature này**. Mỗi mục ghi rõ lý do, cái
gì đã thay thế trong khi chờ, và điều kiện để đóng lại. Không mục nào là việc quên.

| # | Điểm | Vì sao hoãn | Hiện trạng | Đóng lại khi |
|---|------|-------------|-----------|--------------|
| D1 | Tài khoản bị disable thì từ chối tạo địa chỉ mới | FR-023 chỉ yêu cầu **giữ lại** địa chỉ. Không request path nào đọc `users.status`, và `contracts/error-codes.md` chưa gán mã lỗi cho tình huống này. Thêm hành vi lúc này là tự ý mở rộng spec | Module User đọc và trả địa chỉ của tài khoản disabled bình thường, đã có test `TestADisabledAccountKeepsItsAddresses` | Có FR + error code, và quyết định module nào sở hữu quy tắc đọc `users.status` (module Auth) |
| D2 | Hai request tạo địa chỉ đầu tiên chạy đồng thời: kẻ thua nhận `INTERNAL_ERROR` | Ràng buộc **vẫn đúng** ở tầng lưu trữ nhờ partial unique index (ADR-003), chỉ là mã trả về chưa đẹp. Sửa cần thêm một FR về `CONFLICT` hoặc một vòng retry ở tầng application, đều vượt phạm vi T034–T043 | Index từ chối đúng; test `TestSetDefaultWithoutClearingIsRejectedAndReported` chứng minh index còn hiệu lực | T064 (security review) chốt giữa retry ở application và `CONFLICT`, hoặc một feature riêng |
| D3 | `maxLength` 120 (`recipientName`) và 255 (`streetAddress`) trong `contracts/openapi.yaml` | Xem mục "Đã xử lý" bên dưới — đã quyết định **thực thi** trong phase này, không còn hoãn | Đã được enforce ở `domain/model/address.go` và `data-model.md` đã cập nhật | — |
| D4 | `AddressRepository.SetDefault(ctx, addressID)` không mang `userID` | Interface đã commit ở T013; phạm vi sửa là thay đổi đã commit, và hiện tại nó an toàn nhờ `FindByOwner` **trong cùng transaction** | An toàn theo hợp đồng tại doc comment; test `TestTheDefaultTransitionJoinsTheCallersTransaction` chứng minh việc rollback giữ nguyên default cũ | Khi có module thứ hai ghi qua adapter này, hoặc khi T064 đánh giá lại rủi ro |
| D5 | Ghi audit **sau** khi write đã commit: nếu đọc `actorRole` lỗi thì client nhận `500` cho một thay đổi **đã lưu**, và client retry sẽ tạo trùng địa chỉ | Đây là điểm nhất quán với Phase 3 (`UpdateProfile`), sửa cần thay đổi cả hai đường và thuộc phạm vi FR-019 về "mọi thay đổi đều được ghi" | **Đã sửa trong Phase 7** sau security review T064: role đọc trong transaction và audit dùng role có sẵn, đúng như `UpdateProfile`. | Đóng lại |
| D6 | Test integration cho giới hạn 120/255 khi lưu vào PostgreSQL thật | Quy tắc đã được chứng minh ở entity và qua HTTP với repository in-memory. Cột là `text` không có ràng buộc, nên round-trip không có rủi ro riêng — giá trị thừa vẫi là `text` | Đã có test ở `domain/model`, `application/implement` và `presentation/http` cho cả `displayName`, `recipientName` lẫn `streetAddress` | Nếu sau này `text` bị đổi thành `varchar` hoặc thêm check constraint, test này trở nên cần thiết |
| D7 | Một caller không phải HTTP của `LookupCustomer` (worker, module Order) sẽ ghi `audit_logs` với actor rỗng | Contract `CustomerLookupService` cố ý không mang tham số actor; xem research D11. Hiện chưa có caller nào như vậy | `presentation` điền actor từ session nên mọi đường HTTP đều có actor thật; đường không phải HTTP ghi sự kiện với actor nil | Khi module Order ra đời: quyết định có thêm actor vào contract (breaking) hay chấp nhận actor rỗng cho caller nền |
| D8 | Lookup không giới hạn số địa chỉ trả về ngoài trần 100 dòng hiện có | FR-007d và DTO của contract không có cửa sổ phân trang, nhưng không FR nào giới hạn số địa chỉ mỗi tài khoản | Dùng lại trần sẵn có `maxAddressPageSize`; khách hàng hơn 100 địa chỉ sẽ bị cắt | Nếu sản phẩm cho phép hơn 100 địa chỉ: cần FR mới và contract nhiều trang |
| D9 | `internal/share/logging` liệt kê tên key thủ công, nên `api_secret`, `MEDIA_API_SECRET`, `MEDIA_CLOUD_NAME`, `signature` và `url` **không** bị redact | Danh sách là allow-list tên key, không phải so khớp mẫu. Hôm nay **không rò rỉ**: adapter Cloudinary chỉ log `operation` + `classification` đã khử, `cmd/api` chỉ log **tên** biến chứ không log giá trị, và không chỗ nào log `url`/`signature` | T059 chứng minh mọi hình dạng credential thật đều bị che; security review T064 đã lần theo từng giá trị từ `MediaConfig` đến từng dòng log và xác nhận không rò rỉ | Khi có quyết định riêng về tầng logging: thêm key credential media vào `sensitiveKeys`, hoặc chuyển sang so khớp theo mẫu |
| D10 | Audit writer làm việc không chặn: hàng đợi đầy thì sự kiện bị **bỏ**, chỉ ghi log `audit queue full; event dropped` | Đây là hành vi có sẵn của tầng dùng chung, không phải do feature này đưa vào. Constitution VI yêu cầu thay đổi quản trị phải được ghi | Không im lặng: có dòng log. Nhưng nó giới hạn FR-022a — sự kiện `USER_PROFILE_VIEWED_BY_ADMIN` cũng có thể mất | Feature riêng về độ bền của audit: ví dụ block có thời hạn, hoặc đếm sự kiện bị bỏ và cảnh báo |
| D11 | `MAX_BODY_BYTES` không được kiểm chứng là lớn hơn trần avatar | Tầng `share/config` không được import `domain/model` của module (Constitution I), nên không thể validate chéo ở đó | Đã **ghi rõ** trong `docs/configuration.md` và ADR-008: đặt `MAX_BODY_BYTES` ≤ 2 MB là mọi upload trên 1 MiB bị `413 PAYLOAD_TOO_LARGE` chứ không phải `USER_AVATAR_TOO_LARGE` | Nếu muốn chặn ở lúc khởi động: validation phải nằm trong composition của module User, không phải ở tầng config |

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

  Cùng lập luận đó được áp dụng ngược lại sau security review: `provinceName` và
  `wardName` cũng được giới hạn 120 ký tự. Ở đây contract **chưa** tuyên bố giới hạn
  nào, nhưng hai cột này lại lưu và trả lại nguyên văn chữ do client gửi, nên một
  khách hàng đã đăng nhập có thể gửi markup tuỳ ý vào biểu hiện quản trị viên.
